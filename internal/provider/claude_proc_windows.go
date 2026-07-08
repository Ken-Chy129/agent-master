//go:build windows

package provider

import (
	"os/exec"
	"syscall"
)

// prepareCmd gives the child its own hidden console: the daemon usually runs
// headless (logon autostart), and without CREATE_NO_WINDOW every claude run
// would pop a visible console window. Because the child console is separate,
// there is no way to deliver a cross-console Ctrl+C, so interruption uses
// exec's default hard kill — claude writes its transcript incrementally, so at
// worst the tail of the interrupted run is lost from --resume.
func prepareCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
