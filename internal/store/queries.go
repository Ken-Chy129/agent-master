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
	Effort          string `json:"effort"`
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
	ID           string `json:"id"`
	Title        string `json:"title"`
	LastPreview  string `json:"lastPreview"`
	LastSeq      int64  `json:"lastSeq"`
	ActiveRunID  string `json:"activeRunId,omitempty"`
	LastRunState string `json:"lastRunState,omitempty"`
	WorkspaceDir string `json:"workspaceDir"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
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
		`INSERT INTO sessions (id,title,provider,model,effort,workspace_dir,native_session_id,created_at,updated_at,archived)
		 VALUES (?,?,?,?,?,?,?,?,?,0)`,
		sess.ID, sess.Title, sess.Provider, sess.Model, sess.Effort, sess.WorkspaceDir, sess.NativeSessionID, sess.CreatedAt, sess.UpdatedAt,
	)
	if err != nil {
		return err
	}
	// Seed the projection row.
	_, err = s.DB.Exec(
		`INSERT OR IGNORE INTO recent_sessions (id,title,last_preview,last_seq,active_run_id,workspace_dir,created_at,updated_at)
		 VALUES (?,?,'',0,NULL,?,?,?)`,
		sess.ID, sess.Title, sess.WorkspaceDir, sess.CreatedAt, sess.UpdatedAt,
	)
	return err
}

// GetSession returns a session by id, or ErrNotFound.
func (s *Store) GetSession(id string) (Session, error) {
	var sess Session
	var nativeID, effort sql.NullString
	err := s.DB.QueryRow(
		`SELECT id,title,provider,model,effort,workspace_dir,native_session_id,created_at,updated_at,archived
		 FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &effort, &sess.WorkspaceDir, &nativeID, &sess.CreatedAt, &sess.UpdatedAt, &sess.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	sess.NativeSessionID = nativeID.String
	sess.Effort = effort.String
	return sess, nil
}

// UpdateSessionModel records the session's last-used model and reasoning effort
// (per-message selection sticks as the default for the next turn).
func (s *Store) UpdateSessionModel(id, model, effort string) error {
	_, err := s.DB.Exec(
		`UPDATE sessions SET model=?, effort=?, updated_at=? WHERE id=?`,
		model, effort, nowRFC3339(), id,
	)
	return err
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

// ListRecent returns the session-list projection newest first, plus whether
// more rows exist beyond the returned page.
func (s *Store) ListRecent(limit, offset int) ([]RecentSession, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	// Fetch one extra to detect hasMore.
	rows, err := s.DB.Query(
		`SELECT id,title,last_preview,last_seq,active_run_id,last_run_state,workspace_dir,created_at,updated_at
		 FROM recent_sessions ORDER BY updated_at DESC LIMIT ? OFFSET ?`, limit+1, offset,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []RecentSession
	for rows.Next() {
		var r RecentSession
		var preview, activeRun, runState, wsDir, createdAt sql.NullString
		var lastSeq sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Title, &preview, &lastSeq, &activeRun, &runState, &wsDir, &createdAt, &r.UpdatedAt); err != nil {
			return nil, false, err
		}
		r.LastPreview, r.LastSeq, r.ActiveRunID, r.LastRunState = preview.String, lastSeq.Int64, activeRun.String, runState.String
		r.WorkspaceDir, r.CreatedAt = wsDir.String, createdAt.String
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

// BumpRecentSeq advances a session's projected tail seq (monotonic) and its
// updated_at. Called on every committed event.
func (s *Store) BumpRecentSeq(sessionID string, seq int64) error {
	_, err := s.DB.Exec(
		`UPDATE recent_sessions SET last_seq=MAX(last_seq,?), updated_at=? WHERE id=?`,
		seq, nowRFC3339(), sessionID,
	)
	return err
}

// UpdateRecentMeta refreshes the projection's run-derived display fields
// without touching last_seq or title (title changes only via create/rename).
func (s *Store) UpdateRecentMeta(sessionID, preview, activeRunID, lastRunState string) error {
	_, err := s.DB.Exec(
		`UPDATE recent_sessions SET last_preview=?, active_run_id=?, last_run_state=?, updated_at=? WHERE id=?`,
		preview, nullIfEmpty(activeRunID), nullIfEmpty(lastRunState), nowRFC3339(), sessionID,
	)
	return err
}

// RenameSession updates a session's title in both the sessions table and the
// recent_sessions projection.
func (s *Store) RenameSession(id, title string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE sessions SET title=?, updated_at=? WHERE id=?`, title, nowRFC3339(), id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE recent_sessions SET title=? WHERE id=?`, title, id); err != nil {
		return err
	}
	return tx.Commit()
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
// in ascending order, for backward history pagination. hasMore reports whether
// older events exist before the returned window.
func (s *Store) EventsBefore(sessionID string, beforeSeq int64, limit int) ([]Event, bool, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if beforeSeq <= 0 {
		beforeSeq = 1<<62 - 1
	}
	// Fetch one extra (descending) to detect older history, then reverse.
	rows, err := s.DB.Query(
		`SELECT seq,type,run_id,payload,created_at FROM events
		 WHERE session_id=? AND seq<? ORDER BY seq DESC LIMIT ?`, sessionID, beforeSeq, limit+1,
	)
	if err != nil {
		return nil, false, err
	}
	desc, err := scanEvents(rows, sessionID)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(desc) > limit
	if hasMore {
		desc = desc[:limit]
	}
	// Reverse to ascending.
	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, hasMore, nil
}

// IntentRun returns the run id previously associated with a client intent, or
// "" if none.
func (s *Store) IntentRun(sessionID, intentID string) (string, error) {
	var runID string
	err := s.DB.QueryRow(
		`SELECT run_id FROM intents WHERE session_id=? AND intent_id=?`, sessionID, intentID,
	).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return runID, err
}

// PutIntent records the intent→run mapping (idempotent).
func (s *Store) PutIntent(sessionID, intentID, runID string) error {
	_, err := s.DB.Exec(
		`INSERT OR IGNORE INTO intents (session_id,intent_id,run_id,created_at) VALUES (?,?,?,?)`,
		sessionID, intentID, runID, nowRFC3339(),
	)
	return err
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
