package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mustSession(t *testing.T, st *Store, id string) {
	t.Helper()
	if err := st.CreateSession(Session{ID: id, Title: "t", WorkspaceDir: "/tmp"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
}

func append3(t *testing.T, st *Store, sid string) {
	t.Helper()
	for i := 0; i < 3; i++ {
		ev, err := st.AppendEvent(sid, "assistant_message", "r1", []byte(`{"text":"x"}`))
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if ev.Seq != int64(i+1) {
			t.Fatalf("seq = %d, want %d", ev.Seq, i+1)
		}
	}
}

func TestAppendEventSeqMonotonic(t *testing.T) {
	st := openTestStore(t)
	mustSession(t, st, "s1")
	append3(t, st, "s1")

	// A second session has its own seq space starting at 1.
	mustSession(t, st, "s2")
	ev, err := st.AppendEvent("s2", "user_message", "r2", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Seq != 1 {
		t.Fatalf("s2 first seq = %d, want 1", ev.Seq)
	}
}

func TestEventsAfter(t *testing.T) {
	st := openTestStore(t)
	mustSession(t, st, "s1")
	append3(t, st, "s1")

	got, err := st.EventsAfter("s1", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Seq != 2 || got[1].Seq != 3 {
		t.Fatalf("EventsAfter(1) = %+v, want seq 2,3", got)
	}
}

func TestEventsBeforePagination(t *testing.T) {
	st := openTestStore(t)
	mustSession(t, st, "s1")
	for i := 0; i < 5; i++ {
		if _, err := st.AppendEvent("s1", "assistant_message", "r1", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	// Latest page of 2 → seq 4,5 ascending, older exist.
	page, hasMore, err := st.EventsBefore("s1", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore || len(page) != 2 || page[0].Seq != 4 || page[1].Seq != 5 {
		t.Fatalf("page1 = %+v hasMore=%v, want seq 4,5 hasMore=true", page, hasMore)
	}
	// Page back before seq 4 → seq 2,3, older still exist.
	page2, hasMore2, err := st.EventsBefore("s1", page[0].Seq, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore2 || len(page2) != 2 || page2[0].Seq != 2 || page2[1].Seq != 3 {
		t.Fatalf("page2 = %+v hasMore=%v, want seq 2,3 hasMore=true", page2, hasMore2)
	}
	// Final page → seq 1, no more.
	page3, hasMore3, err := st.EventsBefore("s1", page2[0].Seq, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore3 || len(page3) != 1 || page3[0].Seq != 1 {
		t.Fatalf("page3 = %+v hasMore=%v, want seq 1 hasMore=false", page3, hasMore3)
	}
}

func TestIntentDedup(t *testing.T) {
	st := openTestStore(t)
	mustSession(t, st, "s1")

	if got, _ := st.IntentRun("s1", "i1"); got != "" {
		t.Fatalf("unknown intent = %q, want empty", got)
	}
	if err := st.PutIntent("s1", "i1", "run-A"); err != nil {
		t.Fatal(err)
	}
	// Duplicate put must not overwrite the original run.
	if err := st.PutIntent("s1", "i1", "run-B"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.IntentRun("s1", "i1"); got != "run-A" {
		t.Fatalf("intent run = %q, want run-A", got)
	}
}

func TestListRecentHasMore(t *testing.T) {
	st := openTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		mustSession(t, st, id)
	}
	rows, hasMore, err := st.ListRecent(2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore || len(rows) != 2 {
		t.Fatalf("ListRecent(2) = %d rows hasMore=%v, want 2 true", len(rows), hasMore)
	}
	rows2, hasMore2, err := st.ListRecent(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore2 || len(rows2) != 1 {
		t.Fatalf("ListRecent(2, off 2) = %d rows hasMore=%v, want 1 false", len(rows2), hasMore2)
	}
}

func TestProjectionLastSeq(t *testing.T) {
	st := openTestStore(t)
	mustSession(t, st, "s1")
	for i := 0; i < 3; i++ {
		ev, err := st.AppendEvent("s1", "assistant_message", "r1", []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.BumpRecentSeq("s1", ev.Seq); err != nil {
			t.Fatal(err)
		}
	}
	// UpdateRecentMeta must not clobber last_seq.
	if err := st.UpdateRecentMeta("s1", "new title", "hello", ""); err != nil {
		t.Fatal(err)
	}
	rows, _, err := st.ListRecent(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].LastSeq != 3 || rows[0].Title != "new title" || rows[0].LastPreview != "hello" {
		t.Fatalf("projection = %+v, want last_seq=3 title='new title' preview='hello'", rows[0])
	}
}
