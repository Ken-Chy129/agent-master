//go:build !windows

package service

import "syscall"

// processAlive probes existence with signal 0, which touches nothing. EPERM means
// the process exists but belongs to another user — still alive for our purposes.
func processAlive(pid int) (alive, checkable bool) {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM, true
}
