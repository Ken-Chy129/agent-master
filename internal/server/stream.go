package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/render"
	"github.com/Ken-Chy129/agent-master/internal/store"
)

// handleRender returns the current render snapshot for a session (one-shot).
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := s.svc.GetSession(sessionID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	events, err := s.svc.EventsAfter(sessionID, 0, renderCap)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, render.Compute(events))
}

// wireEvent is the JSON shape sent to clients over SSE and /messages.
type wireEvent struct {
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	RunID     string          `json:"runId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"createdAt"`
}

func toWireEvent(e store.Event) wireEvent {
	return wireEvent{Seq: e.Seq, Type: e.Type, RunID: e.RunID, Payload: e.Payload, CreatedAt: e.CreatedAt}
}

func toWire(events []store.Event) []wireEvent {
	out := make([]wireEvent, 0, len(events))
	for _, e := range events {
		out = append(out, toWireEvent(e))
	}
	return out
}

// handleStream is the resumable per-session SSE endpoint.
//
// It subscribes to the live broadcast BEFORE replaying history (so no event is
// missed at the boundary), replays committed events after the client's cursor,
// then streams live events, de-duplicating by seq.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := s.svc.GetSession(sessionID); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	afterSeq := resumeCursor(r)

	// Subscribe first to avoid a gap between replay and live.
	subID, live := s.svc.Subscribe(sessionID)
	defer s.svc.Unsubscribe(sessionID, subID)

	// Full committed ledger drives render_state; a suffix drives am_event replay
	// (resume) and its cursor. `events` accumulates live commits for re-render.
	events, _ := s.svc.EventsAfter(sessionID, 0, renderCap)
	lastSeq := int64(0)
	if n := len(events); n > 0 {
		lastSeq = events[n-1].Seq
	}
	for _, e := range events {
		if e.Seq > afterSeq {
			writeSSE(w, e)
			afterSeq = e.Seq
		}
	}
	writeRender(w, render.Compute(events))
	flusher.Flush()

	ctx := r.Context()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case f, ok := <-live:
			if !ok {
				// Dropped by a full buffer: tell the client to reconnect.
				fmt.Fprintf(w, "event: reconnect\ndata: {\"afterSeq\":%d}\n\n", afterSeq)
				flusher.Flush()
				return
			}
			switch {
			case f.Delta != nil:
				// Live-only token delta: no seq, not resumable.
				if data, err := json.Marshal(f.Delta); err == nil {
					fmt.Fprintf(w, "event: am_delta\ndata: %s\n\n", data)
					flusher.Flush()
				}
			case f.Event != nil:
				if f.Event.Seq <= lastSeq {
					continue // already in our accumulated ledger
				}
				events = append(events, *f.Event)
				lastSeq = f.Event.Seq
				if f.Event.Seq > afterSeq {
					writeSSE(w, *f.Event)
					afterSeq = f.Event.Seq
				}
				writeRender(w, render.Compute(events))
				flusher.Flush()
			}
		}
	}
}

// renderCap bounds how much history feeds render_state (v1: recompute in memory).
const renderCap = 2000

func writeRender(w http.ResponseWriter, rs render.RenderState) {
	data, err := json.Marshal(rs)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: am_render\ndata: %s\n\n", data)
}

func writeSSE(w http.ResponseWriter, e store.Event) {
	data, err := json.Marshal(toWireEvent(e))
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %d\nevent: am_event\ndata: %s\n\n", e.Seq, data)
}

// resumeCursor reads the resume position from Last-Event-ID or ?after_seq.
func resumeCursor(r *http.Request) int64 {
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	if v := r.URL.Query().Get("after_seq"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}
