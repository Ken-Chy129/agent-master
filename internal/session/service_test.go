package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/provider"
	"github.com/Ken-Chy129/agent-master/internal/render"
	"github.com/Ken-Chy129/agent-master/internal/shellenv"
	"github.com/Ken-Chy129/agent-master/internal/store"
)

// fakeProvider lets a test drive the run loop's exit path.
type fakeProvider struct {
	run func(ctx context.Context, o provider.RunOptions, onEvent func(provider.StreamEvent)) (provider.RunResult, error)
}

func (f *fakeProvider) Type() string { return "fake" }
func (f *fakeProvider) Run(ctx context.Context, o provider.RunOptions, onEvent func(provider.StreamEvent)) (provider.RunResult, error) {
	return f.run(ctx, o, onEvent)
}
func (f *fakeProvider) Models(context.Context) ([]provider.ModelInfo, error) { return nil, nil }

func newTestService(t *testing.T, prov provider.Provider) (*Service, store.Session) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := NewService(st, prov)
	sess, err := svc.CreateSession(CreateSessionInput{WorkspaceDir: "/tmp"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return svc, sess
}

func renderOf(t *testing.T, s *Service, sessionID string) render.RenderState {
	t.Helper()
	events, err := s.store.EventsAfter(sessionID, 0, 2000)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	return render.Compute(events)
}

// waitIdle polls until the session's tail settles to idle (the run goroutine
// runs asynchronously), failing if it stays "running" past the deadline.
func waitIdle(t *testing.T, s *Service, sessionID string) render.RenderState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rs := renderOf(t, s, sessionID)
		if rs.TailActivity == "idle" {
			return rs
		}
		if time.Now().After(deadline) {
			t.Fatalf("tailActivity still %q after deadline", rs.TailActivity)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A panic inside the provider must not crash the daemon and must still commit
// run_finished, so the session doesn't get stuck showing "running" forever.
func TestRunProviderRecoversPanicAndFinalizes(t *testing.T) {
	prov := &fakeProvider{run: func(context.Context, provider.RunOptions, func(provider.StreamEvent)) (provider.RunResult, error) {
		panic("boom")
	}}
	svc, sess := newTestService(t, prov)

	if _, err := svc.Send(sess.ID, SendInput{Message: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}

	rs := waitIdle(t, svc, sess.ID)
	if rs.LastRunState != runFailed {
		t.Fatalf("lastRunState = %q, want %q", rs.LastRunState, runFailed)
	}
	// The active entry must be released so the session can accept a new run.
	// Poll rather than assert immediately: the finalizer commits run_finished —
	// which is all waitIdle observes — then writes recent-session metadata, and
	// only then releases the entry, so a bare check here races that gap.
	waitActiveReleased(t, svc, sess.ID)
}

// waitActiveReleased polls until the session has no in-flight run registered.
func waitActiveReleased(t *testing.T, s *Service, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		_, busy := s.active[sessionID]
		s.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("active run entry not released after panic")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A normal run commits run_finished(done) and settles to idle.
func TestRunProviderNormalCompletion(t *testing.T) {
	prov := &fakeProvider{run: func(_ context.Context, _ provider.RunOptions, onEvent func(provider.StreamEvent)) (provider.RunResult, error) {
		onEvent(provider.StreamEvent{Kind: provider.KindSystem, NativeSessionID: "native1"})
		onEvent(provider.StreamEvent{Kind: provider.KindAssistantMessage, Text: "hello"})
		return provider.RunResult{NativeSessionID: "native1", FinalText: "hello"}, nil
	}}
	svc, sess := newTestService(t, prov)

	if _, err := svc.Send(sess.ID, SendInput{Message: "hi"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	rs := waitIdle(t, svc, sess.ID)
	if rs.LastRunState != runDone {
		t.Fatalf("lastRunState = %q, want %q", rs.LastRunState, runDone)
	}
}

// Pressing stop on a session whose run was orphaned by a previous process (no
// live cancel in this process) must still finalize it, so the stop button works
// without waiting for a daemon restart.
func TestInterruptFinalizesOrphanRun(t *testing.T) {
	svc, sess := newTestService(t, &fakeProvider{})

	runID := "r_orphan"
	if err := svc.store.CreateRun(runID, sess.ID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	svc.commit(sess.ID, "run_started", runID, map[string]any{"runId": runID})
	svc.commit(sess.ID, "tool_call", runID, map[string]any{"name": "Bash", "id": "t1"})
	svc.touchRecent(sess.ID, "x", runID, runRunning)

	if err := svc.Interrupt(sess.ID); err != nil {
		t.Fatalf("interrupt: %v", err)
	}

	rs := renderOf(t, svc, sess.ID)
	if rs.TailActivity != "idle" {
		t.Fatalf("tailActivity = %q, want idle", rs.TailActivity)
	}
	if rs.Rows[0].Status != "incomplete" {
		t.Fatalf("orphan tool status = %q, want incomplete", rs.Rows[0].Status)
	}
	running, err := svc.store.RunningRuns()
	if err != nil {
		t.Fatalf("running runs: %v", err)
	}
	if len(running) != 0 {
		t.Fatalf("still %d running runs after interrupt", len(running))
	}
}

// A run left "running" by a previous process is healed at startup: run_finished
// is committed (tail settles to idle) and the projection stops flagging it.
func TestReconcileStuckRuns(t *testing.T) {
	svc, sess := newTestService(t, &fakeProvider{})

	// Simulate a previous process that started a run and died mid-flight.
	runID := "r_orphan"
	if err := svc.store.CreateRun(runID, sess.ID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	svc.commit(sess.ID, "user_message", runID, map[string]any{"text": "deploy"})
	svc.commit(sess.ID, "run_started", runID, map[string]any{"runId": runID})
	svc.touchRecent(sess.ID, "deploy", runID, runRunning)

	if rs := renderOf(t, svc, sess.ID); rs.TailActivity != "running" {
		t.Fatalf("precondition: tailActivity = %q, want running", rs.TailActivity)
	}

	svc.ReconcileStuckRuns()

	rs := renderOf(t, svc, sess.ID)
	if rs.TailActivity != "idle" {
		t.Fatalf("tailActivity = %q, want idle", rs.TailActivity)
	}
	if rs.LastRunState != runInterrupted {
		t.Fatalf("lastRunState = %q, want %q", rs.LastRunState, runInterrupted)
	}

	running, err := svc.store.RunningRuns()
	if err != nil {
		t.Fatalf("running runs: %v", err)
	}
	if len(running) != 0 {
		t.Fatalf("still %d running runs after reconcile", len(running))
	}

	// The projection must clear active_run_id but keep the preview intact.
	recents, _, err := svc.store.ListRecent(10, 0)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(recents) != 1 {
		t.Fatalf("recents = %d, want 1", len(recents))
	}
	if recents[0].ActiveRunID != "" {
		t.Fatalf("activeRunId = %q, want empty", recents[0].ActiveRunID)
	}
	if recents[0].LastPreview != "deploy" {
		t.Fatalf("lastPreview = %q, want %q", recents[0].LastPreview, "deploy")
	}
}

// A send must be refused — not misrouted — while the daemon cannot resolve the
// credentials this machine is known to export. Running anyway would spawn claude
// against a different account than the user's terminal, which is the silent
// failure the shellenv gate exists to prevent.
func TestSendRefusedWhenShellEnvBlocked(t *testing.T) {
	svc, sess := newTestService(t, &fakeProvider{
		run: func(context.Context, provider.RunOptions, func(provider.StreamEvent)) (provider.RunResult, error) {
			t.Error("provider ran despite missing credentials")
			return provider.RunResult{NativeSessionID: "s1"}, nil
		},
	})

	// Force the blocked state: a probe that cannot run, plus a history of
	// exporting a credential that is absent from this process.
	prev, had := os.LookupEnv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "no-such-shell"))
	shellenv.Resolve([]string{"ANTHROPIC_API_KEY"})
	t.Cleanup(func() {
		if had {
			os.Setenv("ANTHROPIC_API_KEY", prev)
		}
		shellenv.Resolve(nil) // clears MissingAuth, unblocking other tests
	})

	if _, err := svc.Send(sess.ID, SendInput{Message: "hi"}); !errors.Is(err, ErrShellEnv) {
		t.Fatalf("Send error = %v, want ErrShellEnv", err)
	}

	// The refusal must leave no trace in the ledger: a rejected turn is not a
	// failed run, and the session should still read as idle.
	if rs := renderOf(t, svc, sess.ID); rs.TailActivity != "idle" {
		t.Errorf("tailActivity = %q, want idle after a refused send", rs.TailActivity)
	}
}
