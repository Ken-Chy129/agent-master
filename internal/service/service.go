// Package service installs the daemon as a background service: a systemd user
// unit on Linux, a launchd LaunchAgent on macOS, a per-user Run-key autostart
// on Windows (see service_windows.go). It mirrors Garyx's
// install-as-managed-service model so each machine is two commands to set up.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
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
	case "windows":
		return installWindows(exe)
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
	case "windows":
		return uninstallWindows()
	default:
		return fmt.Errorf("service uninstall not supported on %s", runtime.GOOS)
	}
}

// Installed reports whether the service definition exists on disk. Used to tell
// "installed but not responding" apart from "never started".
func Installed() bool {
	var path string
	var err error
	switch runtime.GOOS {
	case "linux":
		path, err = systemdUnitPath()
	case "darwin":
		path, err = launchdPlistPath()
	case "windows":
		return installedWindows()
	default:
		return false
	}
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Stop stops the running service (leaves it installed on Linux). Stopping an
// already-stopped service is treated as success, not an error.
func Stop() error {
	switch runtime.GOOS {
	case "linux":
		return runQuiet("systemctl", "--user", "stop", unitName)
	case "darwin":
		if err := runQuiet("launchctl", "bootout", "gui/"+uid()+"/"+macLabel); err != nil {
			if strings.Contains(err.Error(), "No such process") {
				return nil // already stopped
			}
			return err
		}
		return nil
	case "windows":
		return stopWindows()
	default:
		return fmt.Errorf("service stop not supported on %s", runtime.GOOS)
	}
}

// Restart restarts the service (it must already be installed via `start`).
func Restart() error {
	switch runtime.GOOS {
	case "linux":
		return runQuiet("systemctl", "--user", "restart", unitName)
	case "darwin":
		plistPath, err := launchdPlistPath()
		if err != nil {
			return err
		}
		return bootstrapLaunchd(plistPath)
	case "windows":
		return restartWindows()
	default:
		return fmt.Errorf("service restart not supported on %s", runtime.GOOS)
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
Environment=PATH=%s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, exe, servicePATH())
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}

	if err := runQuiet("systemctl", "--user", "daemon-reload"); err != nil {
		return systemdHint(err)
	}
	if err := runQuiet("systemctl", "--user", "enable", "--now", unitName); err != nil {
		return systemdHint(err)
	}
	return nil
}

func uninstallSystemd() error {
	_ = runQuiet("systemctl", "--user", "disable", "--now", unitName)
	unitPath, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = runQuiet("systemctl", "--user", "daemon-reload")
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
	// Capture the daemon's stdout/stderr (slog output, panic traces) to a log
	// file; without these keys launchd sends both to /dev/null, so a crash
	// leaves no trace. Best-effort: omit them if the path can't be resolved.
	var logKeys string
	if logPath, err := config.LogPath(); err == nil {
		logKeys = fmt.Sprintf(
			"  <key>StandardOutPath</key><string>%s</string>\n"+
				"  <key>StandardErrorPath</key><string>%s</string>\n",
			logPath, logPath)
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
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>%s</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
%s</dict>
</plist>
`, macLabel, exe, servicePATH(), logKeys)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}

	return bootstrapLaunchd(plistPath)
}

// bootstrapLaunchd (re)loads the LaunchAgent, tolerating the teardown race that
// makes a naive bootout→bootstrap fail with "Bootstrap failed: 5: Input/output
// error": the daemon shuts down gracefully (up to a few seconds on SIGTERM), so
// the old job can still be present when bootstrap runs. We boot out, wait for
// the job to actually disappear, then bootstrap with a short retry on the
// transient error. Loading an already-loaded job is treated as success.
func bootstrapLaunchd(plistPath string) error {
	target := "gui/" + uid()
	label := target + "/" + macLabel
	if launchdLoaded(label) {
		_ = runQuiet("launchctl", "bootout", label) // ignore "No such process"
		waitLaunchdUnloaded(label)
	}
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		err = runQuiet("launchctl", "bootstrap", target, plistPath)
		if err == nil || launchdAlreadyLoaded(err) {
			return nil
		}
		if !launchdTransient(err) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("%w\n(hint: launchctl bootstrap %s %s)", err, target, plistPath)
}

// launchdLoaded reports whether the job is currently known to launchd.
func launchdLoaded(label string) bool {
	return runQuiet("launchctl", "print", label) == nil
}

// waitLaunchdUnloaded polls until the job is gone (or a ~3s cap), so a following
// bootstrap doesn't collide with a still-terminating instance.
func waitLaunchdUnloaded(label string) {
	for i := 0; i < 30; i++ {
		if !launchdLoaded(label) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// launchdTransient matches the errors seen while launchd is mid-teardown; they
// clear once the previous instance is fully gone.
func launchdTransient(err error) bool {
	s := err.Error()
	return strings.Contains(s, "Bootstrap failed: 5") ||
		strings.Contains(s, "Input/output error") ||
		strings.Contains(s, "Operation now in progress")
}

// launchdAlreadyLoaded matches "the job is already loaded", which for our
// purpose (ensure it's running) is a success, not a failure.
func launchdAlreadyLoaded(err error) bool {
	s := err.Error()
	return strings.Contains(s, "service already loaded") ||
		strings.Contains(s, "already bootstrapped")
}

func uninstallLaunchd() error {
	_ = runQuiet("launchctl", "bootout", "gui/"+uid()+"/"+macLabel)
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

// servicePATH builds a PATH for the managed service. systemd/launchd start
// daemons with a minimal PATH; include the user's common bin dirs first so the
// daemon (and the claude CLI it spawns) resolve tools installed under the home
// directory, e.g. ~/.local/bin/claude.
func servicePATH() string {
	dirs := []string{"/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append([]string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin"),
		}, dirs...)
	}
	return strings.Join(dirs, ":")
}

// runQuiet runs a command discarding its output on success; on failure it folds
// any output into the error. Used for install/uninstall so best-effort steps
// (e.g. clearing a stale launchd instance) don't leak scary-looking messages.
func runQuiet(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
	}
	return err
}
