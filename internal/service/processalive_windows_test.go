//go:build windows

package service

// processAlive has no cheap, side-effect-free equivalent on Windows, so the
// caller skips the assertion there rather than opening a process handle.
func processAlive(int) (alive, checkable bool) { return false, false }
