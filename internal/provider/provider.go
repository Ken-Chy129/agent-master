// Package provider drives an underlying agent CLI (Claude Code in v1) as a
// subprocess and normalizes its output into StreamEvents. New providers
// (Codex, Gemini) implement the same interface.
package provider

import "context"

// EventKind enumerates the normalized stream event kinds.
type EventKind string

const (
	KindSystem           EventKind = "system"
	KindAssistantDelta   EventKind = "assistant_delta"
	KindAssistantMessage EventKind = "assistant_message"
	KindToolCall         EventKind = "tool_call"
	KindToolResult       EventKind = "tool_result"
	KindResult           EventKind = "result"
	KindError            EventKind = "error"
)

// StreamEvent is one normalized event emitted during a run.
type StreamEvent struct {
	Kind            EventKind
	Text            string // assistant text / final result text / delta fragment
	Index           int    // assistant_delta: content-block index
	ToolName        string // tool_call
	ToolID          string // tool_call / tool_result correlation id
	Input           any    // tool_call input
	Output          any    // tool_result output
	NativeSessionID string // system: the provider's native session id
}

// RunOptions configures a single run (one user turn).
type RunOptions struct {
	SessionID       string
	Message         string
	WorkspaceDir    string
	Model           string       // empty = provider default
	Effort          string       // reasoning effort (low|medium|high|xhigh|max); empty = provider default
	PermissionMode  string       // empty = provider default
	NativeSessionID string       // non-empty resumes that provider session
	Images          []ImageInput // images staged to local files for this turn
}

// RunResult summarizes a finished run.
type RunResult struct {
	NativeSessionID string
	FinalText       string
	IsError         bool
	ErrorMessage    string
}

// ImageInput is one image attached to a run, already staged to a local file the
// agent process can read.
type ImageInput struct {
	Path      string // absolute path on the daemon host
	Name      string // original file name (for prompt labeling)
	MediaType string // e.g. image/png
}

// ModelInfo describes one selectable model and the reasoning-effort levels it
// supports. ID is what gets passed to the provider ("" = provider default).
type ModelInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Efforts     []string `json:"efforts,omitempty"`
}

// Provider drives one agent backend.
//
// Run streams events through onEvent and returns when the turn completes.
// Cancel the context to interrupt the run.
type Provider interface {
	Type() string
	Run(ctx context.Context, o RunOptions, onEvent func(StreamEvent)) (RunResult, error)
	// Models returns the selectable models (with per-model effort support). It
	// should always return a usable list, falling back to a built-in set when a
	// live catalog can't be fetched.
	Models(ctx context.Context) ([]ModelInfo, error)
}
