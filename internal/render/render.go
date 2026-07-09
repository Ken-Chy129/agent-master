// Package render folds the committed event ledger into a render snapshot so
// clients can dumb-render it instead of each re-deriving transcript structure
// (tool pairing, run status, row ordering). This is the server-side
// render_state: one reducer, consistent across web/desktop/mobile.
//
// Deltas (live token preview) are ephemeral and NOT part of render_state; the
// committed assistant_message is the source of truth here.
package render

import (
	"encoding/json"
	"fmt"

	"github.com/Ken-Chy129/agent-master/internal/store"
)

// RenderState is the derived, ready-to-display snapshot of a session.
type RenderState struct {
	BasedOnSeq   int64       `json:"basedOnSeq"`             // committed tail this snapshot reflects
	Rows         []RenderRow `json:"rows"`                   // ordered display rows
	TailActivity string      `json:"tailActivity"`           // "idle" | "running"
	LastRunState string      `json:"lastRunState,omitempty"` // done | interrupted | failed
}

// RenderRow is one display row. `kind` selects which fields are meaningful.
type RenderRow struct {
	Kind      string          `json:"kind"`                // user | assistant | tool | error
	ID        string          `json:"id"`                  // stable row id
	Seq       int64           `json:"seq"`                 // originating event seq
	Text      string          `json:"text,omitempty"`      // user/assistant/error
	Name      string          `json:"name,omitempty"`      // tool
	Input     json.RawMessage `json:"input,omitempty"`     // tool
	Output    json.RawMessage `json:"output,omitempty"`    // tool (set once the result lands)
	Status    string          `json:"status,omitempty"`    // tool: running | done
	CreatedAt string          `json:"createdAt,omitempty"` // RFC3339, from the originating event
	Images    []ImageRef      `json:"images,omitempty"`    // user: attached images
}

// ImageRef names an image attached to a user turn (for history display).
type ImageRef struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType,omitempty"`
	File      string `json:"file,omitempty"` // staged basename, served via /uploads/{file}
}

type textPayload struct {
	Text string `json:"text"`
}
type userPayload struct {
	Text   string     `json:"text"`
	Images []ImageRef `json:"images"`
}
type toolCallPayload struct {
	Name  string          `json:"name"`
	ID    string          `json:"id"`
	Input json.RawMessage `json:"input"`
}
type toolResultPayload struct {
	ID     string          `json:"id"`
	Output json.RawMessage `json:"output"`
}
type runFinishedPayload struct {
	State string `json:"state"`
}
type errorPayload struct {
	Message string `json:"message"`
}

// Compute reduces committed events (ascending by seq) into a RenderState.
func Compute(events []store.Event) RenderState {
	rs := RenderState{Rows: []RenderRow{}, TailActivity: "idle"}
	toolRow := make(map[string]int) // tool-call id -> index in Rows
	runActive := false
	openTools := []int{} // tool rows still "running" within the current run

	// A tool row only stays "running" while its run is live. When a run ends —
	// normally, or abruptly (interrupted/crashed, leaving a tool_call with no
	// tool_result) — any still-open tool from it is orphaned: mark it
	// "incomplete" so it renders as a terminal (non-spinning) state. Runs are
	// sequential per session, so a run boundary cleanly scopes which tools to
	// resolve.
	flushOpenTools := func() {
		for _, idx := range openTools {
			if rs.Rows[idx].Status == "running" {
				rs.Rows[idx].Status = "incomplete"
			}
		}
		openTools = openTools[:0]
	}

	for _, e := range events {
		rs.BasedOnSeq = e.Seq
		switch e.Type {
		case "user_message":
			var p userPayload
			_ = json.Unmarshal(e.Payload, &p)
			rs.Rows = append(rs.Rows, RenderRow{Kind: "user", ID: fmt.Sprintf("u%d", e.Seq), Seq: e.Seq, Text: p.Text, Images: p.Images, CreatedAt: e.CreatedAt})
		case "assistant_message":
			var p textPayload
			_ = json.Unmarshal(e.Payload, &p)
			rs.Rows = append(rs.Rows, RenderRow{Kind: "assistant", ID: fmt.Sprintf("a%d", e.Seq), Seq: e.Seq, Text: p.Text, CreatedAt: e.CreatedAt})
		case "tool_call":
			var p toolCallPayload
			_ = json.Unmarshal(e.Payload, &p)
			id := p.ID
			if id == "" {
				id = fmt.Sprintf("t%d", e.Seq)
			}
			rs.Rows = append(rs.Rows, RenderRow{
				Kind: "tool", ID: id, Seq: e.Seq, Name: p.Name, Input: p.Input, Status: "running",
				CreatedAt: e.CreatedAt,
			})
			openTools = append(openTools, len(rs.Rows)-1)
			if p.ID != "" {
				toolRow[p.ID] = len(rs.Rows) - 1
			}
		case "tool_result":
			var p toolResultPayload
			_ = json.Unmarshal(e.Payload, &p)
			if idx, ok := toolRow[p.ID]; ok {
				rs.Rows[idx].Output = p.Output
				rs.Rows[idx].Status = "done"
			}
		case "run_started":
			flushOpenTools() // resolve orphans from any prior run that never finished
			runActive = true
		case "run_finished":
			runActive = false
			flushOpenTools()
			var p runFinishedPayload
			_ = json.Unmarshal(e.Payload, &p)
			if p.State != "" {
				rs.LastRunState = p.State
			}
		case "error":
			var p errorPayload
			_ = json.Unmarshal(e.Payload, &p)
			rs.Rows = append(rs.Rows, RenderRow{Kind: "error", ID: fmt.Sprintf("e%d", e.Seq), Seq: e.Seq, Text: p.Message, CreatedAt: e.CreatedAt})
		}
	}

	if runActive {
		rs.TailActivity = "running"
	} else {
		// Idle tail: no run is live, so nothing can still be executing. Resolve
		// any tool left "running" by a run that ended without a run_finished
		// (legacy data from before crash-safe finalization).
		flushOpenTools()
	}
	return rs
}
