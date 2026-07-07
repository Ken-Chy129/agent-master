// Package session orchestrates the daemon's core loop: it owns sessions, the
// append-only event ledger, run lifecycle, and live event fan-out. It turns a
// provider's normalized StreamEvents into committed ledger events (write first,
// then broadcast) and drives resume via the provider's native session id.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Ken-Chy129/agent-master/internal/provider"
	"github.com/Ken-Chy129/agent-master/internal/store"
)

// Run states.
const (
	runRunning     = "running"
	runDone        = "done"
	runInterrupted = "interrupted"
	runFailed      = "failed"
)

// ErrNotFound is re-exported for handlers.
var ErrNotFound = store.ErrNotFound

// ErrBusy is returned when a session already has an active run.
var ErrBusy = errors.New("session has an active run")

// Service is the session orchestrator.
type Service struct {
	store *store.Store
	prov  provider.Provider
	bc    *broadcaster

	mu     sync.Mutex
	active map[string]context.CancelFunc // sessionID -> cancel of the active run
}

// NewService builds a session service around a store and a provider.
func NewService(st *store.Store, prov provider.Provider) *Service {
	return &Service{
		store:  st,
		prov:   prov,
		bc:     newBroadcaster(),
		active: make(map[string]context.CancelFunc),
	}
}

// CreateSessionInput is the create request.
type CreateSessionInput struct {
	Title        string
	WorkspaceDir string
	Model        string
}

// CreateSession creates a new session.
func (s *Service) CreateSession(in CreateSessionInput) (store.Session, error) {
	sess := store.Session{
		ID:           newID("s"),
		Title:        in.Title,
		Provider:     s.prov.Type(),
		Model:        in.Model,
		WorkspaceDir: in.WorkspaceDir,
	}
	if sess.Title == "" {
		sess.Title = "New session"
	}
	if err := s.store.CreateSession(sess); err != nil {
		return store.Session{}, err
	}
	return s.store.GetSession(sess.ID)
}

// GetSession returns a session by id.
func (s *Service) GetSession(id string) (store.Session, error) { return s.store.GetSession(id) }

// ListSessions returns the recent-session projection and whether more exist.
func (s *Service) ListSessions(limit, offset int) ([]store.RecentSession, bool, error) {
	return s.store.ListRecent(limit, offset)
}

// DeleteSession interrupts any active run and removes the session.
func (s *Service) DeleteSession(id string) error {
	_ = s.Interrupt(id)
	return s.store.DeleteSession(id)
}

// Messages returns a page of ledger events for history (ascending by seq) and
// whether older events exist.
func (s *Service) Messages(sessionID string, beforeSeq int64, limit int) ([]store.Event, bool, error) {
	return s.store.EventsBefore(sessionID, beforeSeq, limit)
}

// EventsAfter returns committed events after a seq (for SSE replay).
func (s *Service) EventsAfter(sessionID string, afterSeq int64, limit int) ([]store.Event, error) {
	return s.store.EventsAfter(sessionID, afterSeq, limit)
}

// Subscribe registers a live SSE listener. Frames are committed events (with a
// seq) or live deltas (ephemeral).
func (s *Service) Subscribe(sessionID string) (int, <-chan Frame) {
	return s.bc.subscribe(sessionID)
}

// Unsubscribe removes a live SSE listener.
func (s *Service) Unsubscribe(sessionID string, id int) { s.bc.unsubscribe(sessionID, id) }

// Send starts a run for a user message. It returns the run id immediately; the
// provider runs asynchronously and streams events into the ledger.
func (s *Service) Send(sessionID, message, clientIntentID string) (string, error) {
	sess, err := s.store.GetSession(sessionID)
	if err != nil {
		return "", err
	}

	// Idempotency: a repeated client intent returns the original run.
	if clientIntentID != "" {
		if existing, err := s.store.IntentRun(sessionID, clientIntentID); err == nil && existing != "" {
			return existing, nil
		}
	}

	s.mu.Lock()
	if _, busy := s.active[sessionID]; busy {
		s.mu.Unlock()
		return "", ErrBusy
	}
	runID := newID("r")
	ctx, cancel := context.WithCancel(context.Background())
	s.active[sessionID] = cancel
	s.mu.Unlock()

	if clientIntentID != "" {
		if err := s.store.PutIntent(sessionID, clientIntentID, runID); err != nil {
			slog.Error("put intent", "err", err)
		}
	}

	// Commit the user turn and run-start before doing any provider work.
	s.commit(sessionID, "user_message", runID, map[string]any{"text": message})
	s.commit(sessionID, "run_started", runID, map[string]any{"runId": runID})
	if err := s.store.CreateRun(runID, sessionID); err != nil {
		slog.Error("create run", "err", err)
	}
	s.touchRecent(sessionID, sess.Title, preview(message), runID)

	go s.runProvider(ctx, cancel, sess, runID, message)
	return runID, nil
}

// Interrupt cancels a session's active run, if any.
func (s *Service) Interrupt(sessionID string) error {
	s.mu.Lock()
	cancel := s.active[sessionID]
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	return nil
}

func (s *Service) runProvider(ctx context.Context, cancel context.CancelFunc, sess store.Session, runID, message string) {
	defer func() {
		s.mu.Lock()
		delete(s.active, sess.ID)
		s.mu.Unlock()
		cancel()
	}()

	var lastAssistant string
	onEvent := func(e provider.StreamEvent) {
		switch e.Kind {
		case provider.KindSystem:
			if e.NativeSessionID != "" && sess.NativeSessionID == "" {
				if err := s.store.SetNativeSessionID(sess.ID, e.NativeSessionID); err != nil {
					slog.Error("set native session id", "err", err)
				}
				sess.NativeSessionID = e.NativeSessionID
			}
		case provider.KindAssistantDelta:
			// Live-only: broadcast, do not commit to the ledger.
			s.bc.publish(Frame{SessionID: sess.ID, Delta: &Delta{RunID: runID, Text: e.Text, Index: e.Index}})
		case provider.KindAssistantMessage:
			lastAssistant = e.Text
			s.commit(sess.ID, "assistant_message", runID, map[string]any{"text": e.Text})
		case provider.KindToolCall:
			s.commit(sess.ID, "tool_call", runID, map[string]any{
				"name": e.ToolName, "id": e.ToolID, "input": e.Input,
			})
		case provider.KindToolResult:
			s.commit(sess.ID, "tool_result", runID, map[string]any{
				"id": e.ToolID, "output": e.Output,
			})
		}
	}

	res, err := s.prov.Run(ctx, provider.RunOptions{
		SessionID:       sess.ID,
		Message:         message,
		WorkspaceDir:    sess.WorkspaceDir,
		Model:           sess.Model,
		NativeSessionID: sess.NativeSessionID,
	}, onEvent)

	switch {
	case errors.Is(err, context.Canceled):
		s.commit(sess.ID, "run_finished", runID, map[string]any{"runId": runID, "state": runInterrupted})
		_ = s.store.FinishRun(runID, runInterrupted, "")
	case err != nil:
		s.commit(sess.ID, "error", runID, map[string]any{"message": err.Error()})
		s.commit(sess.ID, "run_finished", runID, map[string]any{"runId": runID, "state": runFailed})
		_ = s.store.FinishRun(runID, runFailed, err.Error())
	default:
		s.commit(sess.ID, "run_finished", runID, map[string]any{"runId": runID, "state": runDone})
		_ = s.store.FinishRun(runID, runDone, "")
	}

	if res.FinalText != "" {
		lastAssistant = res.FinalText
	}
	s.touchRecent(sess.ID, sess.Title, preview(lastAssistant), "")
}

// commit appends an event to the ledger, then broadcasts it (write-then-derive).
func (s *Service) commit(sessionID, typ, runID string, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("marshal event", "err", err)
		return
	}
	ev, err := s.store.AppendEvent(sessionID, typ, runID, data)
	if err != nil {
		slog.Error("append event", "type", typ, "err", err)
		return
	}
	// Advance the projection tail (write-then-derive), then fan out.
	if err := s.store.BumpRecentSeq(sessionID, ev.Seq); err != nil {
		slog.Error("bump recent seq", "err", err)
	}
	s.bc.publish(Frame{SessionID: sessionID, Event: &ev})
}

func (s *Service) touchRecent(sessionID, title, prev, activeRun string) {
	if err := s.store.UpdateRecentMeta(sessionID, title, prev, activeRun); err != nil {
		slog.Error("update recent meta", "err", err)
	}
}

func preview(s string) string {
	const max = 120
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
