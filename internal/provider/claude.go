package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
}

// NewClaude returns a provider that invokes the given claude binary path.
func NewClaude(bin string) *Claude { return &Claude{Bin: bin} }

func (c *Claude) Type() string { return "claude" }

func (c *Claude) Run(ctx context.Context, o RunOptions, onEvent func(StreamEvent)) (RunResult, error) {
	args := []string{
		"-p", o.Message,
		"--output-format", "stream-json",
		"--verbose", // required for stream-json to emit the full event stream
	}
	if o.NativeSessionID != "" {
		args = append(args, "--resume", o.NativeSessionID)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}

	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Dir = o.WorkspaceDir
	// Interrupt gracefully (SIGINT) rather than SIGKILL so claude can flush its
	// local transcript, keeping later --resume intact. Fall back to kill after a
	// grace period.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
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

func rawOrNil(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return r
}

// claudeMsg is the subset of the stream-json protocol we consume.
type claudeMsg struct {
	Type      string       `json:"type"`
	Subtype   string       `json:"subtype"`
	SessionID string       `json:"session_id"`
	Message   *claudeInner `json:"message"`
	Result    string       `json:"result"`
	IsError   bool         `json:"is_error"`
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
