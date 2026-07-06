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
	Text            string // assistant text / final result text
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
	Model           string // empty = provider default
	PermissionMode  string // empty = provider default
	NativeSessionID string // non-empty resumes that provider session
}

// RunResult summarizes a finished run.
type RunResult struct {
	NativeSessionID string
	FinalText       string
	IsError         bool
	ErrorMessage    string
}

// Provider drives one agent backend.
//
// Run streams events through onEvent and returns when the turn completes.
// Cancel the context to interrupt the run.
type Provider interface {
	Type() string
	Run(ctx context.Context, o RunOptions, onEvent func(StreamEvent)) (RunResult, error)
}
