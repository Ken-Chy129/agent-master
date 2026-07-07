package render

import (
	"encoding/json"
	"testing"

	"github.com/Ken-Chy129/agent-master/internal/store"
)

func ev(seq int64, typ, payload string) store.Event {
	return store.Event{Seq: seq, Type: typ, Payload: json.RawMessage(payload)}
}

func TestComputeEmpty(t *testing.T) {
	rs := Compute(nil)
	if len(rs.Rows) != 0 || rs.BasedOnSeq != 0 || rs.TailActivity != "idle" {
		t.Fatalf("empty = %+v", rs)
	}
}

func TestComputeTurnWithToolPairing(t *testing.T) {
	events := []store.Event{
		ev(1, "user_message", `{"text":"hi"}`),
		ev(2, "run_started", `{"runId":"r1"}`),
		ev(3, "tool_call", `{"name":"Bash","id":"tool_1","input":{"cmd":"ls"}}`),
		ev(4, "tool_result", `{"id":"tool_1","output":"a\nb"}`),
		ev(5, "assistant_message", `{"text":"done"}`),
		ev(6, "run_finished", `{"runId":"r1","state":"done"}`),
	}
	rs := Compute(events)

	if rs.BasedOnSeq != 6 {
		t.Fatalf("basedOnSeq = %d, want 6", rs.BasedOnSeq)
	}
	if rs.TailActivity != "idle" || rs.LastRunState != "done" {
		t.Fatalf("tail=%q last=%q", rs.TailActivity, rs.LastRunState)
	}
	// run_started / run_finished / tool_result do not produce standalone rows.
	if len(rs.Rows) != 3 {
		t.Fatalf("rows = %d (%+v), want 3 (user, tool, assistant)", len(rs.Rows), rs.Rows)
	}
	if rs.Rows[0].Kind != "user" || rs.Rows[0].Text != "hi" {
		t.Fatalf("row0 = %+v", rs.Rows[0])
	}
	tool := rs.Rows[1]
	if tool.Kind != "tool" || tool.Name != "Bash" || tool.Status != "done" || len(tool.Output) == 0 {
		t.Fatalf("tool row not paired/done: %+v", tool)
	}
	if rs.Rows[2].Kind != "assistant" || rs.Rows[2].Text != "done" {
		t.Fatalf("row2 = %+v", rs.Rows[2])
	}
}

func TestComputeRunningTail(t *testing.T) {
	events := []store.Event{
		ev(1, "user_message", `{"text":"go"}`),
		ev(2, "run_started", `{"runId":"r1"}`),
		ev(3, "tool_call", `{"name":"Read","id":"t1","input":{}}`),
	}
	rs := Compute(events)
	if rs.TailActivity != "running" {
		t.Fatalf("tail = %q, want running", rs.TailActivity)
	}
	// The tool call with no result yet is still 'running'.
	if rs.Rows[1].Status != "running" {
		t.Fatalf("tool status = %q, want running", rs.Rows[1].Status)
	}
}

func TestComputeErrorRow(t *testing.T) {
	events := []store.Event{
		ev(1, "user_message", `{"text":"x"}`),
		ev(2, "run_started", `{"runId":"r1"}`),
		ev(3, "error", `{"message":"boom"}`),
		ev(4, "run_finished", `{"runId":"r1","state":"failed"}`),
	}
	rs := Compute(events)
	if rs.LastRunState != "failed" {
		t.Fatalf("last = %q, want failed", rs.LastRunState)
	}
	last := rs.Rows[len(rs.Rows)-1]
	if last.Kind != "error" || last.Text != "boom" {
		t.Fatalf("error row = %+v", last)
	}
}

func TestComputeToolResultBeforeCallIsIgnored(t *testing.T) {
	// A stray tool_result with no matching call must not panic or add a row.
	events := []store.Event{
		ev(1, "tool_result", `{"id":"nope","output":"x"}`),
		ev(2, "user_message", `{"text":"hi"}`),
	}
	rs := Compute(events)
	if len(rs.Rows) != 1 || rs.Rows[0].Kind != "user" {
		t.Fatalf("rows = %+v, want just the user row", rs.Rows)
	}
}
