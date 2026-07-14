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
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/Ken-Chy129/agent-master/internal/config"
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
	Effort       string
}

// CreateSession creates a new session.
func (s *Service) CreateSession(in CreateSessionInput) (store.Session, error) {
	sess := store.Session{
		ID:           newID("s"),
		Title:        in.Title,
		Provider:     s.prov.Type(),
		Model:        in.Model,
		Effort:       in.Effort,
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

// Models returns the provider's selectable models (with effort support).
func (s *Service) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return s.prov.Models(ctx)
}

// ListSessions returns the recent-session projection and whether more exist.
func (s *Service) ListSessions(limit, offset int) ([]store.RecentSession, bool, error) {
	return s.store.ListRecent(limit, offset)
}

// DeleteSession interrupts any active run and removes the session.
func (s *Service) DeleteSession(id string) error {
	_ = s.Interrupt(id)
	if dir, err := config.UploadsDir(id); err == nil {
		_ = os.RemoveAll(dir) // best-effort cleanup of staged images
	}
	return s.store.DeleteSession(id)
}

// RenameSession updates a session's title and returns the updated session.
func (s *Service) RenameSession(id, title string) (store.Session, error) {
	if err := s.store.RenameSession(id, title); err != nil {
		return store.Session{}, err
	}
	return s.store.GetSession(id)
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

// EventsTail returns the most recent events for a session (ascending), used to
// compute a render snapshot whose tail reflects the live state.
func (s *Service) EventsTail(sessionID string, limit int) ([]store.Event, error) {
	return s.store.EventsTail(sessionID, limit)
}

// Subscribe registers a live SSE listener. Frames are committed events (with a
// seq) or live deltas (ephemeral).
func (s *Service) Subscribe(sessionID string) (int, <-chan Frame) {
	return s.bc.subscribe(sessionID)
}

// Unsubscribe removes a live SSE listener.
func (s *Service) Unsubscribe(sessionID string, id int) { s.bc.unsubscribe(sessionID, id) }

// ImageUpload is one image attached to a send, already base64-decoded.
type ImageUpload struct {
	Name      string
	MediaType string
	Data      []byte
}

// SendInput is a user turn. Model/Effort are per-message overrides: nil keeps
// the session's last-used value; non-nil (including "") is authoritative and
// becomes the new sticky default.
type SendInput struct {
	Message        string
	Model          *string
	Effort         *string
	Images         []ImageUpload
	ClientIntentID string
}

// Send starts a run for a user message. It returns the run id immediately; the
// provider runs asynchronously and streams events into the ledger.
func (s *Service) Send(sessionID string, in SendInput) (string, error) {
	sess, err := s.store.GetSession(sessionID)
	if err != nil {
		return "", err
	}

	// Idempotency: a repeated client intent returns the original run.
	if in.ClientIntentID != "" {
		if existing, err := s.store.IntentRun(sessionID, in.ClientIntentID); err == nil && existing != "" {
			return existing, nil
		}
	}

	// Resolve per-message model/effort against the session's last-used values,
	// and persist any change so it sticks for the next turn and the header.
	if in.Model != nil {
		sess.Model = *in.Model
	}
	if in.Effort != nil {
		sess.Effort = *in.Effort
	}
	if (in.Model != nil || in.Effort != nil) && (sess.Model != "" || sess.Effort != "" || in.Model != nil || in.Effort != nil) {
		if err := s.store.UpdateSessionModel(sessionID, sess.Model, sess.Effort); err != nil {
			slog.Error("update session model", "err", err)
		}
	}

	// Stage attached images to local files the agent can read. Best-effort:
	// staging failures drop the images rather than failing the whole turn.
	images, imageMeta := s.stageImages(sessionID, in.Images)

	s.mu.Lock()
	if _, busy := s.active[sessionID]; busy {
		s.mu.Unlock()
		return "", ErrBusy
	}
	runID := newID("r")
	ctx, cancel := context.WithCancel(context.Background())
	s.active[sessionID] = cancel
	s.mu.Unlock()

	if in.ClientIntentID != "" {
		if err := s.store.PutIntent(sessionID, in.ClientIntentID, runID); err != nil {
			slog.Error("put intent", "err", err)
		}
	}

	// Commit the user turn and run-start before doing any provider work. The
	// stored text is what the user typed; image references are separate so the
	// transcript renders cleanly (the provider gets the read-file augmentation).
	userPayload := map[string]any{"text": in.Message}
	if len(imageMeta) > 0 {
		userPayload["images"] = imageMeta
	}
	// Create the runs row BEFORE committing run_started. render derives the
	// "running" tail from the run_started ledger event, while reconcile/interrupt
	// clean up via the runs table — so if the two ever diverge across a crash, the
	// safe direction is a runs row without a ledger run_started (renders idle),
	// never a run_started without a runs row (renders running forever, invisible to
	// a table-based sweep). Ledger-authoritative recovery covers the rest.
	s.commit(sessionID, "user_message", runID, userPayload)
	if err := s.store.CreateRun(runID, sessionID); err != nil {
		slog.Error("create run", "err", err)
	}
	s.commit(sessionID, "run_started", runID, map[string]any{"runId": runID})
	s.touchRecent(sessionID, preview(in.Message), runID, runRunning)

	go s.runProvider(ctx, cancel, sess, runID, in.Message, images)
	return runID, nil
}

// imageMeta is the per-image metadata persisted in the user_message payload so
// history can show which images were attached.
type imageMeta struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType,omitempty"`
	File      string `json:"file,omitempty"` // staged basename, for serving back
}

// stageImages writes each upload to the session's uploads dir and returns the
// provider inputs plus the metadata to persist. Best-effort per image.
func (s *Service) stageImages(sessionID string, uploads []ImageUpload) ([]provider.ImageInput, []imageMeta) {
	if len(uploads) == 0 {
		return nil, nil
	}
	dir, err := config.UploadsDir(sessionID)
	if err != nil {
		slog.Error("uploads dir", "err", err)
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Error("mkdir uploads", "err", err)
		return nil, nil
	}
	var inputs []provider.ImageInput
	var meta []imageMeta
	for i, up := range uploads {
		if len(up.Data) == 0 {
			continue
		}
		name := sanitizeFilename(up.Name)
		if name == "" {
			name = fmt.Sprintf("image-%d%s", i+1, extForMediaType(up.MediaType))
		}
		stagedName := fmt.Sprintf("%s-%s", newID("img"), name)
		path := filepath.Join(dir, stagedName)
		if err := os.WriteFile(path, up.Data, 0o644); err != nil {
			slog.Error("write image", "err", err)
			continue
		}
		inputs = append(inputs, provider.ImageInput{Path: path, Name: name, MediaType: up.MediaType})
		meta = append(meta, imageMeta{Name: name, MediaType: up.MediaType, File: stagedName})
	}
	return inputs, meta
}

// Interrupt cancels a session's active run. If this process has no live run for
// the session but the ledger still shows one running — orphaned by a previous
// process that died mid-run — it's finalized in place so the stop button works
// and the session stops showing "running" without waiting for a daemon restart.
func (s *Service) Interrupt(sessionID string) error {
	s.mu.Lock()
	cancel := s.active[sessionID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		return nil
	}
	s.finalizeOrphanRun(sessionID)
	return nil
}

// finalizeOrphanRun settles a session whose ledger tail still renders as running
// but has no live run in this process (orphaned by a previous process that died
// mid-run). It is ledger-authoritative: it finalizes the dangling run the ledger
// actually shows — which is what a table-based sweep missed, since a run_started
// can outlive its runs row across a crash. No-op if the tail is already settled.
func (s *Service) finalizeOrphanRun(sessionID string) {
	runID, err := s.store.DanglingRun(sessionID)
	if err != nil {
		slog.Error("finalize orphan: query dangling run", "session", sessionID, "err", err)
		return
	}
	if runID == "" {
		return // ledger tail already settled
	}
	s.commit(sessionID, "run_finished", runID, map[string]any{"runId": runID, "state": runInterrupted})
	if err := s.store.FinishRun(runID, runInterrupted, "interrupted after daemon restart"); err != nil {
		slog.Error("finalize orphan: finish run", "run", runID, "err", err)
	}
	if err := s.store.ClearActiveRun(sessionID, runInterrupted); err != nil {
		slog.Error("finalize orphan: clear active run", "session", sessionID, "err", err)
	}
}

func (s *Service) runProvider(ctx context.Context, cancel context.CancelFunc, sess store.Session, runID, message string, images []provider.ImageInput) {
	var (
		res           provider.RunResult
		runErr        error
		lastAssistant string
	)

	// Finalize in a defer so run_finished is committed on every exit path,
	// including a panic in the provider or an event handler. A dangling run
	// (run_started with no run_finished) would otherwise leave the session's
	// tailActivity stuck "running" forever, since render derives activity purely
	// from the committed ledger — and the run goroutine has no other recover, so
	// an un-caught panic here would crash the whole daemon.
	defer func() {
		if r := recover(); r != nil {
			runErr = fmt.Errorf("run panicked: %v", r)
			slog.Error("run panicked", "session", sess.ID, "run", runID, "panic", r,
				"stack", string(debug.Stack()))
		}

		state := runDone
		switch {
		case errors.Is(runErr, context.Canceled):
			state = runInterrupted
			s.commit(sess.ID, "run_finished", runID, map[string]any{"runId": runID, "state": runInterrupted})
			_ = s.store.FinishRun(runID, runInterrupted, "")
		case runErr != nil:
			state = runFailed
			s.commit(sess.ID, "error", runID, map[string]any{"message": runErr.Error()})
			s.commit(sess.ID, "run_finished", runID, map[string]any{"runId": runID, "state": runFailed})
			_ = s.store.FinishRun(runID, runFailed, runErr.Error())
		default:
			s.commit(sess.ID, "run_finished", runID, map[string]any{"runId": runID, "state": runDone})
			_ = s.store.FinishRun(runID, runDone, "")
		}

		if res.FinalText != "" {
			lastAssistant = res.FinalText
		}
		s.touchRecent(sess.ID, preview(lastAssistant), "", state)

		s.mu.Lock()
		delete(s.active, sess.ID)
		s.mu.Unlock()
		cancel()
	}()

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

	res, runErr = s.prov.Run(ctx, provider.RunOptions{
		SessionID:       sess.ID,
		Message:         message,
		WorkspaceDir:    sess.WorkspaceDir,
		Model:           sess.Model,
		Effort:          sess.Effort,
		Images:          images,
		NativeSessionID: sess.NativeSessionID,
	}, onEvent)
}

// ReconcileStuckRuns finalizes runs left "running" by a previous daemon process
// (crash, kill, restart, or host sleep mid-run). It runs once at startup before
// any new run can begin, so every running row is by definition an orphan: a live
// run holds an in-memory cancel func that does not survive a restart. Each orphan
// gets a committed run_finished(interrupted) so the conversation's tailActivity
// settles to idle, plus a projection clear so the session list stops showing it
// as running.
func (s *Service) ReconcileStuckRuns() {
	// Ledger-authoritative pass: settle every session whose tail still renders as
	// running by committing the run_finished the crashed process never wrote. This
	// is what clients actually display, and it catches ledger-only orphans (a
	// run_started with no matching runs row) that the table-based sweep below can't
	// see — the exact case that left sessions spinning forever.
	dangling, err := s.store.DanglingRuns()
	if err != nil {
		slog.Error("reconcile: list dangling runs", "err", err)
	}
	for _, r := range dangling {
		s.commit(r.SessionID, "run_finished", r.ID, map[string]any{"runId": r.ID, "state": runInterrupted})
		if err := s.store.FinishRun(r.ID, runInterrupted, "daemon restarted mid-run"); err != nil {
			slog.Error("reconcile: finish dangling run", "run", r.ID, "err", err)
		}
		if err := s.store.ClearActiveRun(r.SessionID, runInterrupted); err != nil {
			slog.Error("reconcile: clear active run", "session", r.SessionID, "err", err)
		}
	}

	// Table-hygiene pass: any runs row still marked running is an orphan too (a
	// live run holds an in-memory cancel that does not survive a restart). Mark it
	// terminal so it can't wrongly read as active later. No ledger commit here —
	// the pass above already settled anything the ledger showed running.
	runs, err := s.store.RunningRuns()
	if err != nil {
		slog.Error("reconcile: list running runs", "err", err)
		return
	}
	for _, r := range runs {
		if err := s.store.FinishRun(r.ID, runInterrupted, "daemon restarted mid-run"); err != nil {
			slog.Error("reconcile: finish run", "run", r.ID, "err", err)
		}
		if err := s.store.ClearActiveRun(r.SessionID, runInterrupted); err != nil {
			slog.Error("reconcile: clear active run", "session", r.SessionID, "err", err)
		}
	}

	if len(dangling) > 0 || len(runs) > 0 {
		slog.Info("reconciled stuck runs from a previous process",
			"ledgerDangling", len(dangling), "tableRunning", len(runs))
	}
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

func (s *Service) touchRecent(sessionID, prev, activeRun, lastRunState string) {
	if err := s.store.UpdateRecentMeta(sessionID, prev, activeRun, lastRunState); err != nil {
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

// sanitizeFilename strips path separators and control/space runs so a
// client-supplied image name is safe to use inside the uploads directory.
func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == ".." || name == "/" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, name)
	if len(name) > 80 {
		name = name[len(name)-80:]
	}
	return name
}

func extForMediaType(mt string) string {
	switch mt {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
