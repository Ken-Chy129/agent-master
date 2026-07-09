// Package logging provides a small, dependency-free rolling log file so the
// daemon can own its own bounded log on every platform, instead of relying on
// the service manager to redirect stderr (which differs across launchd/systemd/
// Windows and never rotates).
package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RollingFile is an io.WriteCloser that appends to a file and rotates it once it
// would exceed maxSize bytes, keeping up to maxBackups rotated files
// ("<path>.1" … "<path>.<maxBackups>", newest first). It is safe for concurrent
// writers, since slog may log from many goroutines at once.
type RollingFile struct {
	path       string
	maxSize    int64
	maxBackups int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// NewRollingFile opens (creating/appending) the log file at path. maxSize is the
// per-file byte cap before rotation; maxBackups is how many rotated files to
// keep (0 = just truncate in place, no history).
func NewRollingFile(path string, maxSize int64, maxBackups int) (*RollingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &RollingFile{path: path, maxSize: maxSize, maxBackups: maxBackups, f: f, size: info.Size()}, nil
}

func (r *RollingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		r.rotate()
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate shifts the backups up (<path>.k → <path>.(k+1)), moves the current file
// to <path>.1, and opens a fresh empty file. Best-effort: on error it falls back
// to appending to the existing file so logging never wedges.
func (r *RollingFile) rotate() {
	_ = r.f.Close()
	if r.maxBackups > 0 {
		_ = os.Remove(fmt.Sprintf("%s.%d", r.path, r.maxBackups))
		for i := r.maxBackups - 1; i >= 1; i-- {
			_ = os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
		}
		_ = os.Rename(r.path, r.path+".1")
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		f, _ = os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	r.f = f
	r.size = 0
}

// File returns the current underlying file, e.g. for runtime/debug.SetCrashOutput.
func (r *RollingFile) File() *os.File {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f
}

// Close closes the current file.
func (r *RollingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}
