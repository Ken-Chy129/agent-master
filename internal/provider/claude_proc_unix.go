//go:build !windows

package provider

import (
	"os/exec"
	"syscall"
	"time"
)

// prepareCmd isolates the child in its own process group and makes interruption
// graceful-then-forceful across the whole process tree. On cancel it SIGINTs the
// group first so claude can flush its transcript (keeping a later --resume
// intact), then SIGKILLs the group after cancelGrace if anything is still alive.
//
// Signaling the group — not just claude's own PID — is what actually stops the
// tool subprocesses claude spawned (a shell running a blocking command, a dev
// server, etc.). A hung one would otherwise keep the run's stdout pipe open and
// wedge the reader, so the run never finalizes and the stop button has no effect.
// exec's WaitDelay remains a final backstop on the group leader.
func prepareCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// A negative pid targets the whole process group; Setpgid made the child a
		// group leader whose pgid == its pid.
		pgid := cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGINT)
		go func() {
			time.Sleep(cancelGrace)
			_ = syscall.Kill(-pgid, syscall.SIGKILL) // ESRCH if already gone — ignored
		}()
		return nil
	}
}
