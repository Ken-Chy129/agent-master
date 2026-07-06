// Package service installs the daemon as a background service: a systemd user
// unit on Linux, a launchd LaunchAgent on macOS. It mirrors Garyx's
// install-as-managed-service model so each machine is two commands to set up.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	unitName = "agent-master.service"
	macLabel = "com.agent-master.daemon"
)

// Install writes the service definition for the current OS and starts it.
func Install() error {
	exe, err := executablePath()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "linux":
		return installSystemd(exe)
	case "darwin":
		return installLaunchd(exe)
	default:
		return fmt.Errorf("service install not supported on %s", runtime.GOOS)
	}
}

// Uninstall stops and removes the service.
func Uninstall() error {
	switch runtime.GOOS {
	case "linux":
		return uninstallSystemd()
	case "darwin":
		return uninstallLaunchd()
	default:
		return fmt.Errorf("service uninstall not supported on %s", runtime.GOOS)
	}
}

// Status prints the current service status.
func Status() error {
	switch runtime.GOOS {
	case "linux":
		return runCmd("systemctl", "--user", "status", unitName, "--no-pager")
	case "darwin":
		return runCmd("launchctl", "print", "gui/"+uid()+"/"+macLabel)
	default:
		return fmt.Errorf("service status not supported on %s", runtime.GOOS)
	}
}

// --- systemd (Linux) ---------------------------------------------------------

func installSystemd(exe string) error {
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=agent-master daemon
After=network-online.target

[Service]
ExecStart=%s serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, exe)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", unitPath)

	if err := runCmd("systemctl", "--user", "daemon-reload"); err != nil {
		return systemdHint(err)
	}
	if err := runCmd("systemctl", "--user", "enable", "--now", unitName); err != nil {
		return systemdHint(err)
	}
	fmt.Println("agent-master service installed and started")
	return nil
}

func uninstallSystemd() error {
	_ = runCmd("systemctl", "--user", "disable", "--now", unitName)
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = runCmd("systemctl", "--user", "daemon-reload")
	fmt.Println("agent-master service removed")
	return nil
}

func systemdUnitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

func systemdHint(err error) error {
	return fmt.Errorf("%w\n(the unit file was written; user systemd may be unavailable here. "+
		"On a real machine run: systemctl --user daemon-reload && systemctl --user enable --now %s ; "+
		"for start without an active login session: sudo loginctl enable-linger $USER)", err, unitName)
}

// --- launchd (macOS) ---------------------------------------------------------

func installLaunchd(exe string) error {
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
`, macLabel, exe)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", plistPath)

	target := "gui/" + uid()
	_ = runCmd("launchctl", "bootout", target+"/"+macLabel) // best-effort clear of a stale instance
	if err := runCmd("launchctl", "bootstrap", target, plistPath); err != nil {
		return fmt.Errorf("%w\n(hint: launchctl bootstrap %s %s)", err, target, plistPath)
	}
	fmt.Println("agent-master service installed and started")
	return nil
}

func uninstallLaunchd() error {
	_ = runCmd("launchctl", "bootout", "gui/"+uid()+"/"+macLabel)
	plistPath, err := launchdPlistPath()
	if err != nil {
		return err
	}
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("agent-master service removed")
	return nil
}

func launchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", macLabel+".plist"), nil
}

// --- shared ------------------------------------------------------------------

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

func uid() string { return fmt.Sprintf("%d", os.Getuid()) }

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
