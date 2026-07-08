//go:build !windows

package provider

import (
	"os"
	"os/exec"
)

// prepareCmd makes interruption graceful: SIGINT rather than SIGKILL, so claude
// can flush its local transcript and a later --resume stays intact. exec kills
// the process after WaitDelay if it ignores the signal.
func prepareCmd(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
}
