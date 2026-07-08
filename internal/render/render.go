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
}

type textPayload struct {
	Text string `json:"text"`
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

	for _, e := range events {
		rs.BasedOnSeq = e.Seq
		switch e.Type {
		case "user_message":
			var p textPayload
			_ = json.Unmarshal(e.Payload, &p)
			rs.Rows = append(rs.Rows, RenderRow{Kind: "user", ID: fmt.Sprintf("u%d", e.Seq), Seq: e.Seq, Text: p.Text, CreatedAt: e.CreatedAt})
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
			runActive = true
		case "run_finished":
			runActive = false
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
	}
	return rs
}
