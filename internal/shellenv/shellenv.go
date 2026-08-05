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
//
// # Why a failed probe is not merely cosmetic
//
// The fallback is not a graceful degradation: with no ANTHROPIC_API_KEY the
// spawned claude silently switches to a *different account and endpoint* (its
// own OAuth login) than the user's terminal uses. Requests get billed
// elsewhere, cross a different compliance boundary, and — when that OAuth
// session cannot refresh — fail with "OAuth session expired and could not be
// refreshed" rather than anything pointing back here.
//
// A single 8s timeout during a cold boot once pinned a daemon into that state
// for two weeks, because the probe ran exactly once with no retry and its
// failure was a lone WARN line. So this package:
//
//   - retries with escalating timeouts instead of giving up after one attempt;
//   - keeps retrying in the background, so a transient boot-time failure heals
//     itself without the user restarting anything;
//   - remembers (by name only — never by value) which variables a previous
//     successful probe produced, so the daemon can tell "this user relies on a
//     shell-exported key that is now missing" from "this user has no such key
//     and legitimately relies on claude's own OAuth";
//   - publishes a Status that callers gate on, so runs fail loudly at send time
//     instead of quietly reaching the wrong account.
//
// Gating at send time rather than refusing to start is deliberate: the service
// definitions use KeepAlive/Restart=on-failure, so exiting non-zero at startup
// would crash-loop the daemon and take the diagnostics offline with it.
package shellenv

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// marker delimits the env dump in the shell's stdout so banners printed by rc
// files (which are common) don't get parsed as environment variables.
const marker = "__AGENT_MASTER_SHELLENV__"

// attemptTimeouts bounds each successive login-shell probe. rc files
// occasionally do slow work (version-manager init, network calls) and are
// slowest of all during a cold boot, when launchd starts us alongside
// everything else — the observed failure was a probe killed at 8s that normally
// completes in ~1.5s. Escalate rather than retrying the same deadline, and cap
// the total so a pathological profile can't wedge startup indefinitely.
var attemptTimeouts = []time.Duration{
	8 * time.Second,
	15 * time.Second,
	25 * time.Second,
}

// healInterval is how often the background loop re-probes once the escalating
// attempts are exhausted. It only runs while auth-critical keys are known to be
// missing, so the cost (~1.5s of zsh per interval) buys automatic recovery from
// a transient failure that would otherwise require a manual restart.
const healInterval = 5 * time.Minute

// importPrefixes are the env-var name prefixes worth importing. Scoped to the
// agent's auth/config surface to avoid perturbing the daemon's own environment
// (PATH is handled separately by the service installer's servicePATH).
var importPrefixes = []string{"ANTHROPIC_", "CLAUDE_"}

// authKeys are the variables that select which account and endpoint the spawned
// claude talks to. Only these gate runs: the prefix filter above also sweeps in
// cosmetic settings (CLAUDE_CODE_NO_FLICKER and friends) whose absence changes
// nothing about where a request goes, and blocking on those would turn a
// harmless rc-file edit into an outage.
var authKeys = map[string]bool{
	"ANTHROPIC_API_KEY":           true,
	"ANTHROPIC_AUTH_TOKEN":        true,
	"ANTHROPIC_BASE_URL":          true,
	"CLAUDE_CODE_OAUTH_TOKEN":     true,
	"ANTHROPIC_VERTEX_PROJECT_ID": true,
	"ANTHROPIC_BEDROCK_BASE_URL":  true,
}

// Status is a snapshot of shell-env resolution, safe to serve to clients: it
// carries variable *names* only, never values.
type Status struct {
	// OK reports whether the most recent probe succeeded.
	OK bool
	// Imported are the variable names this process set from the login shell.
	Imported []string
	// Expected are the names a previous successful probe produced (from config).
	Expected []string
	// MissingAuth are auth-critical Expected names absent from the process env.
	// Non-empty means the spawned claude would silently use different
	// credentials than the user's terminal.
	MissingAuth []string
	// Shell is the shell the probe ran. Worth surfacing: probing the wrong one
	// (a POSIX sh that skips the user's rc files) yields an empty environment
	// with no error, and naming it turns that into an obvious diagnosis.
	Shell string
	// Attempts counts probes made so far, across startup and background healing.
	Attempts int
	// Retrying reports whether the background heal loop is still running.
	Retrying bool
	// LastError is the most recent probe failure, if any.
	LastError string
}

var (
	mu       sync.RWMutex
	current  Status
	optional bool
)

// SetOptional records the user's opt-out (config shell_env_optional). When set,
// Blocked never reports true: the user has accepted that claude may fall back to
// its own credential lookup. Call it before Resolve.
func SetOptional(v bool) {
	mu.Lock()
	defer mu.Unlock()
	optional = v
}

// Current returns the latest resolution snapshot.
func Current() Status {
	mu.RLock()
	defer mu.RUnlock()
	return current.clone()
}

func (s Status) clone() Status {
	s.Imported = slices.Clone(s.Imported)
	s.Expected = slices.Clone(s.Expected)
	s.MissingAuth = slices.Clone(s.MissingAuth)
	return s
}

// Blocked reports whether runs should be refused, and why. It is true only when
// a previous successful probe produced auth-critical variables that are now
// absent — the one case where proceeding would silently talk to the wrong
// account. A machine that never had such variables is not blocked: relying on
// claude's own OAuth login is a supported setup.
func Blocked() (string, bool) {
	mu.RLock()
	opt := optional
	mu.RUnlock()
	if opt {
		return "", false
	}
	s := Current()
	if len(s.MissingAuth) == 0 {
		return "", false
	}
	// User-facing: this text reaches the client as the 503 body for a refused send,
	// so it is localized like the rest of the product surface. The probe error it
	// appends stays as the shell reported it.
	reason := "无法从登录 shell 读取 " + strings.Join(s.MissingAuth, "、") +
		"，若继续执行，claude 将使用与你终端不同的账号"
	if s.LastError != "" {
		reason += "（" + s.LastError + "）"
	}
	if s.Retrying {
		reason += "；守护进程正在后台重试"
	}
	return reason, true
}

// Resolve makes one probe attempt and imports what it finds, publishing the
// result to Current. expected is the variable-name list a previous successful
// probe produced (empty on first run); it is used only to detect what is now
// missing, never as a source of values.
//
// It returns the resulting Status. A failed probe is reported in the Status
// (and its error), not as a fatal condition: callers gate on Blocked instead.
func Resolve(expected []string) Status {
	return resolve(expected, attemptTimeouts[0])
}

// StartHealing re-probes in the background for as long as runs stay blocked,
// invoking onSuccess after each successful probe so the caller can log the new
// state and persist the imported variable names. It walks the remaining
// attemptTimeouts first, then settles into healInterval.
//
// Looping on Blocked rather than on probe success covers both ways the daemon
// gets stuck: a probe that keeps timing out (the boot-time flake this exists
// for) and a probe that succeeds but no longer yields the credential — which
// heals on its own once the user fixes their rc file, again with no restart.
//
// Calling it when nothing is missing is a no-op, so a machine with no
// shell-exported credentials pays nothing.
func StartHealing(expected []string, onSuccess func(imported []string)) {
	if _, blocked := Blocked(); !blocked {
		return
	}

	mu.Lock()
	current.Retrying = true
	mu.Unlock()

	go func() {
		defer func() {
			mu.Lock()
			current.Retrying = false
			mu.Unlock()
		}()

		for i := 1; ; i++ {
			// Escalate through the remaining budgets, then hold at the largest.
			timeout := attemptTimeouts[len(attemptTimeouts)-1]
			if i < len(attemptTimeouts) {
				timeout = attemptTimeouts[i]
			} else {
				time.Sleep(healInterval)
			}

			st := resolve(expected, timeout)
			if st.OK && onSuccess != nil {
				onSuccess(st.Imported)
			}
			if _, blocked := Blocked(); !blocked {
				return
			}
		}
	}()
}

// resolve runs one probe with the given deadline, sets any agent-relevant
// variable that is not already present, and publishes the outcome.
//
// Ordering matters: the environment is fully populated *before* the status is
// published, so a run that passes the Blocked gate can never observe a
// half-imported environment.
func resolve(expected []string, timeout time.Duration) Status {
	if runtime.GOOS == "windows" {
		// The Windows autostart (Run key) launches in the user session with the
		// full user environment already, so there is nothing to resolve.
		st := Status{OK: true, Expected: expected}
		publish(st)
		return st
	}

	shell := userShell()
	env, err := resolveLoginShellEnv(shell, timeout)

	mu.Lock()
	attempts := current.Attempts + 1
	mu.Unlock()

	st := Status{Expected: expected, Shell: shell, Attempts: attempts}
	if err != nil {
		st.LastError = err.Error()
		st.MissingAuth = missingAuth(expected)
		publish(st)
		return st
	}

	for k, v := range env {
		if !wantKey(k) {
			continue
		}
		// A non-empty value already in the process wins over the shell's, so an
		// operator override in the service definition sticks. An *empty* one does
		// not: exported-but-blank is how a parent process signals "unset", and
		// treating it as present would let it mask the real credential — the same
		// silent-divergence failure, arrived at from the other direction.
		if v, ok := os.LookupEnv(k); ok && v != "" {
			continue
		}
		if err := os.Setenv(k, v); err == nil {
			st.Imported = append(st.Imported, k)
		}
	}
	slices.Sort(st.Imported)

	st.OK = true
	// A successful probe can still come back without a variable the user used to
	// export — an intentional removal, or a regression. Report it either way;
	// the caller logs it and records the new set as the baseline.
	st.MissingAuth = missingAuth(expected)
	publish(st)
	return st
}

func publish(st Status) {
	mu.Lock()
	defer mu.Unlock()
	st.Retrying = current.Retrying
	current = st
}

// missingAuth returns the auth-critical expected names that are absent from the
// process environment. It checks the live environment rather than this
// attempt's imports so a variable inherited from the service manager (or set by
// an earlier attempt) still counts as present.
func missingAuth(expected []string) []string {
	var missing []string
	for _, k := range expected {
		if !authKeys[k] {
			continue
		}
		if _, ok := os.LookupEnv(k); !ok {
			missing = append(missing, k)
		}
	}
	slices.Sort(missing)
	return missing
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
func resolveLoginShellEnv(shell string, timeout time.Duration) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
		e := &probeError{shell: shell, stderr: strings.TrimSpace(stderr.String()), err: err, timeout: timeout}
		// Distinguish "rc files are slow" from "the shell itself failed": the
		// former is the recoverable case worth retrying with a longer deadline,
		// and saying so turns an opaque "signal: killed" into a diagnosis.
		if ctx.Err() != nil {
			e.timedOut = true
		}
		return nil, e
	}
	return parseEnvBetweenMarkers(stdout.String()), nil
}

// userShell returns the shell to probe for the user's interactive environment.
//
// $SHELL is the obvious source but cannot be trusted under a service manager:
// systemd --user sets SHELL=/bin/sh regardless of the account's real login shell.
// And /bin/sh — even where it is a symlink to bash — runs in POSIX mode, which
// reads /etc/profile and $ENV but never ~/.bash_profile or ~/.bashrc. Probing it
// returns an environment with none of the user's exports, which is
// indistinguishable from "this user has no credentials": it exits 0, so there is
// no error to report, and yields nothing, so there is nothing to import. A remote
// daemon ran claude with no API key for weeks that way.
//
// So take $SHELL only when it is not sh, then fall back to the account's login
// shell from the passwd database, then to a platform default.
func userShell() string {
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" && !isPOSIXSh(s) {
		return s
	}
	if s := loginShellFromPasswd(); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

// isPOSIXSh reports whether a shell path invokes sh, which never sources the
// user's interactive rc files no matter what it is a symlink to.
func isPOSIXSh(path string) bool {
	return filepath.Base(path) == "sh"
}

// loginShellFromPasswd reads this account's login shell from /etc/passwd, which
// — unlike $SHELL — a service manager does not get to rewrite. Linux and the
// BSDs keep real accounts there; macOS serves them from Directory Services, so
// the lookup finds nothing and the caller falls through to the default. Parsing
// the file directly avoids shelling out during startup; the cost is that
// LDAP/SSSD-only accounts are not covered, which the platform default handles.
func loginShellFromPasswd() string {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	uid := strconv.Itoa(os.Getuid())
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 || f[2] != uid {
			continue
		}
		if sh := strings.TrimSpace(f[6]); sh != "" && !isPOSIXSh(sh) {
			return sh
		}
	}
	return ""
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
	shell    string
	stderr   string
	err      error
	timeout  time.Duration
	timedOut bool
}

func (e *probeError) Error() string {
	if e.timedOut {
		msg := e.shell + " -i -l did not finish within " + e.timeout.String() +
			"; a slow rc file (version-manager init, network call) is the usual cause"
		if e.stderr != "" {
			msg += " (" + e.stderr + ")"
		}
		return msg
	}
	msg := "resolve login-shell env via " + e.shell + ": " + e.err.Error()
	if e.stderr != "" {
		msg += " (" + e.stderr + ")"
	}
	return msg
}
