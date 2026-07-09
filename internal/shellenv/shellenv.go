// Package shellenv resolves the user's interactive login-shell environment and
// imports the agent-relevant variables into the current process.
//
// Why this exists: on macOS the daemon is launched by launchd, and on Linux by
// systemd --user. Neither sources the user's shell rc files (~/.zshrc,
// ~/.bash_profile, ...), so a key exported there — most importantly
// ANTHROPIC_API_KEY / ANTHROPIC_BASE_URL — is invisible to the daemon and to
// the claude CLI it spawns. claude then silently falls back to whatever OAuth
// login it can find (Keychain / ~/.claude), diverging from how the same user's
// terminal behaves.
//
// Resolving the login shell at startup (the approach GUI apps like VS Code use)
// makes the daemon behave exactly like the user's terminal with zero manual
// setup: install and it just works, and a restart re-reads ~/.zshrc so a rotated
// key is picked up. ~/.zshrc stays the single source of truth; no secret is ever
// written to the plist/unit file.
package shellenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// marker delimits the env dump in the shell's stdout so banners printed by rc
// files (which are common) don't get parsed as environment variables.
const marker = "__AGENT_MASTER_SHELLENV__"

// resolveTimeout bounds the login-shell probe. rc files occasionally do slow
// work (version-manager init, network calls); cap it so a pathological profile
// can't wedge daemon startup.
const resolveTimeout = 8 * time.Second

// importPrefixes are the env-var name prefixes worth importing. Scoped to the
// agent's auth/config surface to avoid perturbing the daemon's own environment
// (PATH is handled separately by the service installer's servicePATH).
var importPrefixes = []string{"ANTHROPIC_", "CLAUDE_"}

// Import resolves the user's interactive login-shell environment and sets any
// agent-relevant variable that is not already present in the current process.
// It returns the names of the variables it imported (values omitted — they may
// be secrets). Errors are returned for logging but are non-fatal: the daemon
// should start regardless, falling back to whatever auth claude can find itself.
func Import() ([]string, error) {
	if runtime.GOOS == "windows" {
		// The Windows autostart (Run key) launches in the user session with the
		// full user environment already, so there is nothing to resolve.
		return nil, nil
	}

	env, err := resolveLoginShellEnv()
	if err != nil {
		return nil, err
	}

	var imported []string
	for k, v := range env {
		if !wantKey(k) {
			continue
		}
		if _, ok := os.LookupEnv(k); ok {
			continue // an explicitly-set value wins over the shell's
		}
		if err := os.Setenv(k, v); err == nil {
			imported = append(imported, k)
		}
	}
	return imported, nil
}

func wantKey(k string) bool {
	for _, p := range importPrefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// resolveLoginShellEnv runs the user's shell as an interactive login shell and
// captures its environment. Interactive (-i) is required because the common
// case — ANTHROPIC_API_KEY in ~/.zshrc — is only sourced by interactive shells,
// not by a bare login shell.
func resolveLoginShellEnv() (map[string]string, error) {
	shell := userShell()

	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()

	// Print a marker, dump env, print the marker again. Everything between the
	// two markers is the environment; anything outside is rc-file noise.
	script := "printf '%s\\n' " + marker + "; env; printf '%s\\n' " + marker
	cmd := exec.CommandContext(ctx, shell, "-i", "-l", "-c", script)
	// Detach stdin so an rc file that reads from it sees EOF instead of hanging.
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, &probeError{shell: shell, stderr: strings.TrimSpace(stderr.String()), err: err}
	}
	return parseEnvBetweenMarkers(stdout.String()), nil
}

// userShell returns the user's preferred shell. $SHELL is usually set even
// under launchd/systemd, but fall back to a sensible default when it is not.
func userShell() string {
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

// parseEnvBetweenMarkers extracts KEY=VALUE lines that fall between the two
// marker lines. Values may contain '='; only the first splits the pair.
func parseEnvBetweenMarkers(out string) map[string]string {
	env := make(map[string]string)
	inBlock := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimRight(line, "\r") == marker {
			if inBlock {
				break // second marker: end of the env dump
			}
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && k != "" {
			env[k] = strings.TrimRight(v, "\r")
		}
	}
	return env
}

type probeError struct {
	shell  string
	stderr string
	err    error
}

func (e *probeError) Error() string {
	msg := "resolve login-shell env via " + e.shell + ": " + e.err.Error()
	if e.stderr != "" {
		msg += " (" + e.stderr + ")"
	}
	return msg
}
