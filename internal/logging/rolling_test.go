package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRollingFileRotatesAndCapsBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	// Small cap so a few writes force rotation; keep 2 backups.
	rw, err := NewRollingFile(path, 32, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rw.Close()

	// Each line is ~20 bytes; several writes should trigger multiple rotations.
	for i := 0; i < 10; i++ {
		if _, err := rw.Write([]byte(strings.Repeat("x", 20) + "\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Current file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("current log missing: %v", err)
	}
	// At most maxBackups rotated files are kept; .3 must not exist.
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf(".1 backup missing: %v", err)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf(".3 backup should not exist (maxBackups=2), stat err = %v", err)
	}
}

func TestRollingFileNoBackupsTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.log")
	rw, err := NewRollingFile(path, 16, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rw.Close()

	for i := 0; i < 5; i++ {
		if _, err := rw.Write([]byte("0123456789\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// With no backups, no .1 file is ever created.
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf(".1 should not exist with maxBackups=0, err = %v", err)
	}
	// And the live file stays bounded near the cap (last write only).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() > 32 {
		t.Fatalf("log size %d not bounded", info.Size())
	}
}
