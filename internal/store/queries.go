package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Session mirrors a row in the sessions table.
type Session struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	WorkspaceDir    string `json:"workspaceDir"`
	NativeSessionID string `json:"-"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
	Archived        bool   `json:"archived"`
}

// Event mirrors a row in the append-only events ledger.
type Event struct {
	SessionID string          `json:"-"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	RunID     string          `json:"runId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"createdAt"`
}

// RecentSession mirrors a row in the recent_sessions projection.
type RecentSession struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	LastPreview string `json:"lastPreview"`
	LastSeq     int64  `json:"lastSeq"`
	ActiveRunID string `json:"activeRunId,omitempty"`
	UpdatedAt   string `json:"updatedAt"`
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// CreateSession inserts a new session and seeds its recent projection row.
func (s *Store) CreateSession(sess Session) error {
	now := nowRFC3339()
	if sess.CreatedAt == "" {
		sess.CreatedAt = now
	}
	sess.UpdatedAt = now
	if sess.Provider == "" {
		sess.Provider = "claude"
	}
	_, err := s.DB.Exec(
		`INSERT INTO sessions (id,title,provider,model,workspace_dir,native_session_id,created_at,updated_at,archived)
		 VALUES (?,?,?,?,?,?,?,?,0)`,
		sess.ID, sess.Title, sess.Provider, sess.Model, sess.WorkspaceDir, sess.NativeSessionID, sess.CreatedAt, sess.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return s.UpsertRecent(RecentSession{ID: sess.ID, Title: sess.Title, UpdatedAt: sess.UpdatedAt})
}

// GetSession returns a session by id, or ErrNotFound.
func (s *Store) GetSession(id string) (Session, error) {
	var sess Session
	var nativeID sql.NullString
	err := s.DB.QueryRow(
		`SELECT id,title,provider,model,workspace_dir,native_session_id,created_at,updated_at,archived
		 FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &sess.WorkspaceDir, &nativeID, &sess.CreatedAt, &sess.UpdatedAt, &sess.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	sess.NativeSessionID = nativeID.String
	return sess, nil
}

// SetNativeSessionID records the provider's native session id for resume.
func (s *Store) SetNativeSessionID(id, nativeID string) error {
	_, err := s.DB.Exec(
		`UPDATE sessions SET native_session_id=?, updated_at=? WHERE id=?`,
		nativeID, nowRFC3339(), id,
	)
	return err
}

// DeleteSession removes a session and its events/runs/projection.
func (s *Store) DeleteSession(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM events WHERE session_id=?`,
		`DELETE FROM runs WHERE session_id=?`,
		`DELETE FROM recent_sessions WHERE id=?`,
		`DELETE FROM sessions WHERE id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListRecent returns the session-list projection, newest first.
func (s *Store) ListRecent(limit, offset int) ([]RecentSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.DB.Query(
		`SELECT id,title,last_preview,last_seq,active_run_id,updated_at
		 FROM recent_sessions ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentSession
	for rows.Next() {
		var r RecentSession
		var preview, activeRun sql.NullString
		var lastSeq sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Title, &preview, &lastSeq, &activeRun, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.LastPreview, r.LastSeq, r.ActiveRunID = preview.String, lastSeq.Int64, activeRun.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertRecent updates (or inserts) the projection row for a session.
func (s *Store) UpsertRecent(r RecentSession) error {
	if r.UpdatedAt == "" {
		r.UpdatedAt = nowRFC3339()
	}
	_, err := s.DB.Exec(
		`INSERT INTO recent_sessions (id,title,last_preview,last_seq,active_run_id,updated_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   title=excluded.title,
		   last_preview=excluded.last_preview,
		   last_seq=excluded.last_seq,
		   active_run_id=excluded.active_run_id,
		   updated_at=excluded.updated_at`,
		r.ID, r.Title, r.LastPreview, r.LastSeq, nullIfEmpty(r.ActiveRunID), r.UpdatedAt,
	)
	return err
}

// AppendEvent appends one event to a session's ledger, allocating the next seq
// atomically, and returns the assigned seq. The single DB connection plus the
// transaction serialize seq allocation.
func (s *Store) AppendEvent(sessionID, typ, runID string, payload []byte) (Event, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()

	var seq int64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE session_id=?`, sessionID,
	).Scan(&seq); err != nil {
		return Event{}, err
	}
	created := nowRFC3339()
	if _, err := tx.Exec(
		`INSERT INTO events (session_id,seq,type,run_id,payload,created_at) VALUES (?,?,?,?,?,?)`,
		sessionID, seq, typ, nullIfEmpty(runID), string(payload), created,
	); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return Event{SessionID: sessionID, Seq: seq, Type: typ, RunID: runID, Payload: payload, CreatedAt: created}, nil
}

// EventsAfter returns events with seq > afterSeq in ascending order.
func (s *Store) EventsAfter(sessionID string, afterSeq int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.DB.Query(
		`SELECT seq,type,run_id,payload,created_at FROM events
		 WHERE session_id=? AND seq>? ORDER BY seq ASC LIMIT ?`, sessionID, afterSeq, limit,
	)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows, sessionID)
}

// EventsBefore returns up to limit events with seq < beforeSeq (0 = latest),
// in ascending order, for backward history pagination.
func (s *Store) EventsBefore(sessionID string, beforeSeq int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if beforeSeq <= 0 {
		beforeSeq = 1<<62 - 1
	}
	rows, err := s.DB.Query(
		`SELECT seq,type,run_id,payload,created_at FROM (
		   SELECT seq,type,run_id,payload,created_at FROM events
		   WHERE session_id=? AND seq<? ORDER BY seq DESC LIMIT ?
		 ) ORDER BY seq ASC`, sessionID, beforeSeq, limit,
	)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows, sessionID)
}

func scanEvents(rows *sql.Rows, sessionID string) ([]Event, error) {
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e := Event{SessionID: sessionID}
		var runID sql.NullString
		var payload string
		if err := rows.Scan(&e.Seq, &e.Type, &runID, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.RunID = runID.String
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateRun inserts a run in the running state.
func (s *Store) CreateRun(id, sessionID string) error {
	_, err := s.DB.Exec(
		`INSERT INTO runs (id,session_id,state,started_at) VALUES (?,?,'running',?)`,
		id, sessionID, nowRFC3339(),
	)
	return err
}

// FinishRun marks a run terminal (done|interrupted|failed) with an optional error.
func (s *Store) FinishRun(id, state, errMsg string) error {
	_, err := s.DB.Exec(
		`UPDATE runs SET state=?, finished_at=?, error=? WHERE id=?`,
		state, nowRFC3339(), nullIfEmpty(errMsg), id,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
