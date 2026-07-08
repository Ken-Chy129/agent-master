// Package store owns the SQLite database: the append-only event ledger plus
// the read projections. It uses the pure-Go modernc.org/sqlite driver so the
// daemon builds as a true static binary with CGO_ENABLED=0.
package store

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Store wraps the database handle.
type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. A single connection serializes writes, matching the write-then-derive
// ledger discipline.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One connection keeps writes serial and sidesteps SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}

	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	for _, stmt := range schemaStatements {
		if _, err := s.DB.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Additive columns: ALTER TABLE isn't idempotent, so tolerate the
	// duplicate-column error on re-runs.
	for _, stmt := range []string{
		`ALTER TABLE recent_sessions ADD COLUMN last_run_state TEXT`,
	} {
		if _, err := s.DB.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// schemaStatements is the v1 schema. Statements are idempotent so migrate is
// safe to run on every start.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
		id                TEXT PRIMARY KEY,
		title             TEXT,
		provider          TEXT NOT NULL DEFAULT 'claude',
		model             TEXT,
		workspace_dir     TEXT NOT NULL,
		native_session_id TEXT,
		created_at        TEXT NOT NULL,
		updated_at        TEXT NOT NULL,
		archived          INTEGER NOT NULL DEFAULT 0
	)`,
	// Append-only ledger: the single source of truth for rendered chat.
	`CREATE TABLE IF NOT EXISTS events (
		session_id TEXT NOT NULL,
		seq        INTEGER NOT NULL,
		type       TEXT NOT NULL,
		run_id     TEXT,
		payload    TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (session_id, seq)
	)`,
	`CREATE TABLE IF NOT EXISTS runs (
		id          TEXT PRIMARY KEY,
		session_id  TEXT NOT NULL,
		state       TEXT NOT NULL,
		started_at  TEXT NOT NULL,
		finished_at TEXT,
		error       TEXT
	)`,
	// Read projection for the session list.
	`CREATE TABLE IF NOT EXISTS recent_sessions (
		id            TEXT PRIMARY KEY,
		title         TEXT,
		last_preview  TEXT,
		last_seq      INTEGER,
		active_run_id TEXT,
		updated_at    TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_recent_updated ON recent_sessions(updated_at DESC)`,
	// Idempotency: one client intent maps to exactly one run.
	`CREATE TABLE IF NOT EXISTS intents (
		session_id TEXT NOT NULL,
		intent_id  TEXT NOT NULL,
		run_id     TEXT NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY (session_id, intent_id)
	)`,
}
