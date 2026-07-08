//go:build windows

package service

// Windows has no systemd/launchd equivalent that works without elevation:
// schtasks logon triggers and real Windows services both require admin, and a
// service would run outside the user profile where claude's login lives. So:
//   - autostart: per-user Run registry key (HKCU, no admin), wrapped in
//     `conhost.exe --headless` so no console window appears at logon
//     (needs Windows 10 1903+).
//   - start now: spawn `serve` as a detached, windowless process.
//   - stop: the daemon records its pid in ~/.agent-master/daemon.pid (see
//     cmdServe); kill that pid's tree. /F because a hidden console app has no
//     window to receive a graceful close — SQLite's journal makes this safe.

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/Ken-Chy129/agent-master/internal/config"
)

const (
	runKeyPath   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "agent-master"

	// Process creation flags (syscall only defines CREATE_NEW_PROCESS_GROUP).
	detachedProcess = 0x00000008
)

func installWindows(exe string) error {
	// Autostart at logon. reg.exe rather than the registry API keeps this file
	// free of extra dependencies, matching the systemctl/launchctl style above.
	autostart := fmt.Sprintf(`conhost.exe --headless "%s" serve`, exe)
	if err := runQuiet("reg", "add", runKeyPath, "/v", runValueName, "/t", "REG_SZ", "/d", autostart, "/f"); err != nil {
		return fmt.Errorf("register autostart: %w", err)
	}
	return startDetached(exe)
}

func uninstallWindows() error {
	_ = stopWindows()
	if installedWindows() { // avoid locale-dependent "value not found" errors
		if err := runQuiet("reg", "delete", runKeyPath, "/v", runValueName, "/f"); err != nil {
			return err
		}
	}
	fmt.Println("agent-master service removed")
	return nil
}

func installedWindows() bool {
	return exec.Command("reg", "query", runKeyPath, "/v", runValueName).Run() == nil
}

func stopWindows() error {
	pidPath, err := config.PIDPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return nil // no pidfile → not running (or already cleaned up)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(pidPath)
		return nil
	}
	// On Windows FindProcess opens a handle, so it fails for a dead pid — that
	// distinguishes "already stopped" (success) from a kill that went wrong.
	if proc, err := os.FindProcess(pid); err == nil {
		defer proc.Release()
		// /T takes the claude child processes down with the daemon.
		if err := runQuiet("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F"); err != nil {
			return err
		}
	}
	_ = os.Remove(pidPath)
	return nil
}

func restartWindows() error {
	if err := stopWindows(); err != nil {
		return err
	}
	exe, err := executablePath()
	if err != nil {
		return err
	}
	return startDetached(exe)
}

// startDetached launches `serve` in the background: no console, own process
// group, not tied to this terminal's lifetime. If a daemon is already serving,
// the new instance fails to bind the port and exits — effectively a no-op.
func startDetached(exe string) error {
	cmd := exec.Command(exe, "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
