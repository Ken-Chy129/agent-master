package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxLine bounds a single NDJSON line from the claude CLI. Tool results and
// large assistant messages can be big, so allow up to 16 MiB.
const maxLine = 16 * 1024 * 1024

// Claude drives the Claude Code CLI in print + stream-json mode.
//
// Each run spawns `claude -p "<message>" --output-format stream-json`, optionally
// with `--resume <native_session_id>` to continue an existing session. stdout is
// line-delimited JSON; stdin is unused (the prompt is passed as an argument).
type Claude struct {
	Bin string

	modelsMu    sync.Mutex
	modelsCache []ModelInfo
	modelsAt    time.Time
}

// NewClaude returns a provider that invokes the given claude binary path.
func NewClaude(bin string) *Claude { return &Claude{Bin: bin} }

func (c *Claude) Type() string { return "claude" }

func (c *Claude) Run(ctx context.Context, o RunOptions, onEvent func(StreamEvent)) (RunResult, error) {
	// Images ride along as local files the agent reads with its Read tool: we
	// append a "read this file" instruction per image and allow-list their
	// directories via --add-dir. This keeps the plain -p text path intact
	// instead of switching to stream-json stdin for base64 blocks.
	message := o.Message
	var addDirs []string
	if len(o.Images) > 0 {
		message = appendImageInstructions(message, o.Images)
		addDirs = imageDirs(o.Images)
	}

	args := []string{
		"-p", message,
		"--output-format", "stream-json",
		"--include-partial-messages", // emit token-level content_block_delta events
		"--verbose",                  // required for stream-json to emit the full event stream
	}
	for _, d := range addDirs {
		args = append(args, "--add-dir", d)
	}
	if o.NativeSessionID != "" {
		args = append(args, "--resume", o.NativeSessionID)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}

	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Dir = o.WorkspaceDir
	// Platform-specific process setup (see claude_proc_*.go): on Unix interrupt
	// gracefully (SIGINT) so claude can flush its local transcript, keeping later
	// --resume intact; on Windows hide the child console window. Either way exec
	// falls back to a hard kill after the WaitDelay grace period.
	prepareCmd(cmd)
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return RunResult{}, fmt.Errorf("start claude: %w", err)
	}

	res := RunResult{}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg claudeMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			// Non-protocol noise (e.g. a leaked log line) — skip it.
			continue
		}
		handleMessage(&msg, &res, onEvent)
	}
	scanErr := sc.Err()

	waitErr := cmd.Wait()

	// Context cancelled = interrupted by the caller.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, ctxErr
	}
	if scanErr != nil {
		return res, fmt.Errorf("read claude stream: %w", scanErr)
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return res, fmt.Errorf("claude exited: %s", msg)
	}
	if res.NativeSessionID == "" {
		return res, errors.New("claude stream ended without a session id")
	}
	return res, nil
}

func handleMessage(msg *claudeMsg, res *RunResult, onEvent func(StreamEvent)) {
	switch msg.Type {
	case "system":
		if msg.Subtype == "init" && msg.SessionID != "" {
			res.NativeSessionID = msg.SessionID
			onEvent(StreamEvent{Kind: KindSystem, NativeSessionID: msg.SessionID})
		}
	case "stream_event":
		// Token-level partials. We forward only visible-answer text deltas;
		// thinking/tool-input deltas are ignored (the full assistant message
		// commits the final text and tool calls).
		if e := msg.Event; e != nil && e.Type == "content_block_delta" &&
			e.Delta != nil && e.Delta.Type == "text_delta" && e.Delta.Text != "" {
			onEvent(StreamEvent{Kind: KindAssistantDelta, Text: e.Delta.Text, Index: e.Index})
		}
	case "assistant":
		if msg.Message == nil {
			return
		}
		for _, b := range msg.Message.Content {
			switch b.Type {
			case "text":
				if b.Text != "" {
					onEvent(StreamEvent{Kind: KindAssistantMessage, Text: b.Text})
				}
			case "tool_use":
				onEvent(StreamEvent{
					Kind:     KindToolCall,
					ToolName: b.Name,
					ToolID:   b.ID,
					Input:    rawOrNil(b.Input),
				})
			}
		}
	case "user":
		if msg.Message == nil {
			return
		}
		for _, b := range msg.Message.Content {
			if b.Type == "tool_result" {
				onEvent(StreamEvent{
					Kind:   KindToolResult,
					ToolID: b.ToolUseID,
					Output: rawOrNil(b.Content),
				})
			}
		}
	case "result":
		res.FinalText = msg.Result
		res.IsError = msg.IsError
		onEvent(StreamEvent{Kind: KindResult, Text: msg.Result})
	}
}

// appendImageInstructions augments the user text with a line per image telling
// Claude to read the staged file (its Read tool renders images visually).
func appendImageInstructions(message string, images []ImageInput) string {
	var b strings.Builder
	b.WriteString(message)
	if strings.TrimSpace(message) != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("附带的图片（请用 Read 工具查看）：")
	for _, img := range images {
		name := img.Name
		if name == "" {
			name = filepath.Base(img.Path)
		}
		fmt.Fprintf(&b, "\nRead this image file from disk: %s (name: %s)", img.Path, name)
	}
	return b.String()
}

// imageDirs returns the unique parent directories of the staged images, to pass
// as --add-dir so the agent is permitted to read them.
func imageDirs(images []ImageInput) []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, img := range images {
		d := filepath.Dir(img.Path)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func rawOrNil(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return r
}

// claudeMsg is the subset of the stream-json protocol we consume.
type claudeMsg struct {
	Type      string             `json:"type"`
	Subtype   string             `json:"subtype"`
	SessionID string             `json:"session_id"`
	Message   *claudeInner       `json:"message"`
	Event     *claudeStreamEvent `json:"event"` // type == "stream_event"
	Result    string             `json:"result"`
	IsError   bool               `json:"is_error"`
}

// claudeStreamEvent is the Anthropic streaming envelope inside a stream_event.
type claudeStreamEvent struct {
	Type  string       `json:"type"` // content_block_delta, content_block_start, ...
	Index int          `json:"index"`
	Delta *claudeDelta `json:"delta"`
}

type claudeDelta struct {
	Type string `json:"type"` // text_delta, thinking_delta, input_json_delta
	Text string `json:"text"`
}

type claudeInner struct {
	Role    string        `json:"role"`
	Content []claudeBlock `json:"content"`
}

type claudeBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text"`
	// tool_use
	Name  string          `json:"name"`
	ID    string          `json:"id"`
	Input json.RawMessage `json:"input"`
	// tool_result
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}
