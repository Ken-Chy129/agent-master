package shellenv

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// reset clears the package's published state so each test starts from a clean
// snapshot. The package is deliberately global (it mutates the process
// environment), so tests must not run in parallel.
func reset(t *testing.T) {
	t.Helper()
	mu.Lock()
	current = Status{}
	optional = false
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		current = Status{}
		optional = false
		mu.Unlock()
	})
}

// fakeShell writes an executable stand-in for the user's shell that ignores its
// -i -l -c arguments and emits the given lines between the markers.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakeshell")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func envDump(lines ...string) string {
	s := "printf '%s\\n' " + marker + "\n"
	for _, l := range lines {
		s += "echo '" + l + "'\n"
	}
	return s + "printf '%s\\n' " + marker + "\n"
}

func TestResolveImportsOnlyPrefixedKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	reset(t)
	t.Setenv("SHELL", fakeShell(t, envDump(
		"ANTHROPIC_API_KEY=sk-test",
		"CLAUDE_CODE_NO_FLICKER=1",
		"UNRELATED_VAR=nope",
	)))
	for _, k := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_NO_FLICKER", "UNRELATED_VAR"} {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Unsetenv(k) })
	}

	st := Resolve(nil)
	if !st.OK {
		t.Fatalf("probe failed: %s", st.LastError)
	}
	want := []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_NO_FLICKER"}
	if !slices.Equal(st.Imported, want) {
		t.Errorf("Imported = %v, want %v", st.Imported, want)
	}
	if _, ok := os.LookupEnv("UNRELATED_VAR"); ok {
		t.Error("imported a variable outside the ANTHROPIC_/CLAUDE_ prefixes")
	}
	if reason, blocked := Blocked(); blocked {
		t.Errorf("Blocked on a successful probe: %s", reason)
	}
}

// An explicitly-set value must win over the shell's, so an operator override in
// the service definition is not silently replaced by ~/.zshrc.
func TestResolveDoesNotOverwriteExistingEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	reset(t)
	t.Setenv("SHELL", fakeShell(t, envDump("ANTHROPIC_API_KEY=from-shell")))
	t.Setenv("ANTHROPIC_API_KEY", "from-process")

	st := Resolve(nil)
	if !st.OK {
		t.Fatalf("probe failed: %s", st.LastError)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "from-process" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want the pre-set process value", got)
	}
	if slices.Contains(st.Imported, "ANTHROPIC_API_KEY") {
		t.Error("reported a skipped variable as imported")
	}
}

// The core discrimination: a probe failure only blocks runs when this machine is
// known to export an auth-critical variable. Without that history, falling back
// to claude's own OAuth login is a supported setup and must not be an outage.
func TestBlockedOnlyWhenAuthKeysWereExpected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	broken := fakeShell(t, "exit 1")

	t.Run("no history", func(t *testing.T) {
		reset(t)
		t.Setenv("SHELL", broken)
		st := Resolve(nil)
		if st.OK {
			t.Fatal("expected the probe to fail")
		}
		if _, blocked := Blocked(); blocked {
			t.Error("blocked runs on a machine with no shell-exported credentials")
		}
	})

	t.Run("cosmetic key only", func(t *testing.T) {
		reset(t)
		t.Setenv("SHELL", broken)
		Resolve([]string{"CLAUDE_CODE_NO_FLICKER"})
		if _, blocked := Blocked(); blocked {
			t.Error("blocked runs over a non-auth variable")
		}
	})

	t.Run("auth key missing", func(t *testing.T) {
		reset(t)
		t.Setenv("SHELL", broken)
		os.Unsetenv("ANTHROPIC_API_KEY")
		st := Resolve([]string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_NO_FLICKER"})
		if !slices.Equal(st.MissingAuth, []string{"ANTHROPIC_API_KEY"}) {
			t.Fatalf("MissingAuth = %v, want [ANTHROPIC_API_KEY]", st.MissingAuth)
		}
		reason, blocked := Blocked()
		if !blocked {
			t.Fatal("did not block despite a missing credential")
		}
		if reason == "" {
			t.Error("blocked without an explanation")
		}
	})

	// A variable still present in the process environment is not missing, even
	// though this attempt could not re-read it.
	t.Run("auth key already in env", func(t *testing.T) {
		reset(t)
		t.Setenv("SHELL", broken)
		t.Setenv("ANTHROPIC_API_KEY", "inherited")
		Resolve([]string{"ANTHROPIC_API_KEY"})
		if _, blocked := Blocked(); blocked {
			t.Error("blocked on a variable that is present in the environment")
		}
	})

	t.Run("opt-out", func(t *testing.T) {
		reset(t)
		t.Setenv("SHELL", broken)
		os.Unsetenv("ANTHROPIC_API_KEY")
		SetOptional(true)
		Resolve([]string{"ANTHROPIC_API_KEY"})
		if _, blocked := Blocked(); blocked {
			t.Error("blocked despite shell_env_optional")
		}
	})
}

// A probe that succeeds but no longer yields a previously-exported credential is
// just as dangerous as one that fails: claude would still use another account.
func TestBlockedWhenSuccessfulProbeDropsAuthKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	reset(t)
	t.Setenv("SHELL", fakeShell(t, envDump("CLAUDE_CODE_NO_FLICKER=1")))
	os.Unsetenv("ANTHROPIC_API_KEY")

	st := Resolve([]string{"ANTHROPIC_API_KEY"})
	if !st.OK {
		t.Fatalf("probe failed: %s", st.LastError)
	}
	if _, blocked := Blocked(); !blocked {
		t.Error("a successful probe that dropped the credential did not block runs")
	}
}

// A timeout is the failure mode this package exists for, so it must be reported
// as one rather than as an opaque "signal: killed".
func TestProbeTimeoutIsDiagnosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	reset(t)
	t.Setenv("SHELL", fakeShell(t, "sleep 5"))

	st := resolve(nil, 150*time.Millisecond)
	if st.OK {
		t.Fatal("expected the probe to time out")
	}
	if !strings.Contains(st.LastError, "did not finish within") {
		t.Errorf("LastError = %q, want a timeout diagnosis", st.LastError)
	}
}

func TestParseEnvBetweenMarkers(t *testing.T) {
	out := "rc-file banner\n" + marker + "\nA=1\nB=x=y\nnot a pair\n" + marker + "\ntrailing noise\nC=3\n"
	env := parseEnvBetweenMarkers(out)

	if len(env) != 2 {
		t.Fatalf("parsed %d vars, want 2: %v", len(env), env)
	}
	if env["A"] != "1" {
		t.Errorf("A = %q, want 1", env["A"])
	}
	if env["B"] != "x=y" {
		t.Errorf("B = %q, want x=y (only the first = splits)", env["B"])
	}
	if _, ok := env["C"]; ok {
		t.Error("parsed a variable from outside the marker block")
	}
}

func TestStatusIsCopiedNotAliased(t *testing.T) {
	reset(t)
	mu.Lock()
	current = Status{Imported: []string{"ANTHROPIC_API_KEY"}}
	mu.Unlock()

	got := Current()
	got.Imported[0] = "mutated"
	if Current().Imported[0] != "ANTHROPIC_API_KEY" {
		t.Error("Current returned a slice aliasing the package state")
	}
}

// The point of the heal loop: a probe that failed at boot must recover on its
// own, without the user restarting anything. The first escalation attempt runs
// with no delay, so making the shell usable is enough to unblock.
func TestStartHealingRecoversWithoutRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	reset(t)

	shell := filepath.Join(t.TempDir(), "shell")
	t.Setenv("SHELL", shell)
	os.Unsetenv("ANTHROPIC_API_KEY")
	t.Cleanup(func() { os.Unsetenv("ANTHROPIC_API_KEY") })

	Resolve([]string{"ANTHROPIC_API_KEY"})
	if _, blocked := Blocked(); !blocked {
		t.Fatal("expected runs to be blocked after the failed probe")
	}

	// The shell becomes usable — as it would once boot-time contention clears.
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n"+envDump("ANTHROPIC_API_KEY=sk-healed")), 0o700); err != nil {
		t.Fatal(err)
	}

	healed := make(chan []string, 1)
	StartHealing([]string{"ANTHROPIC_API_KEY"}, func(imported []string) { healed <- imported })

	select {
	case imported := <-healed:
		if !slices.Contains(imported, "ANTHROPIC_API_KEY") {
			t.Errorf("imported = %v, want ANTHROPIC_API_KEY", imported)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("heal loop never recovered")
	}
	if reason, blocked := Blocked(); blocked {
		t.Errorf("still blocked after healing: %s", reason)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-healed" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want sk-healed", got)
	}
}

// $SHELL cannot be trusted: systemd --user sets it to /bin/sh regardless of the
// account's real login shell, and sh never sources ~/.bashrc — so probing it
// returns an empty environment with exit 0, which reads as "no credentials".
func TestUserShellRejectsPOSIXSh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell probing is a no-op on Windows")
	}
	t.Setenv("SHELL", "/bin/sh")

	got := userShell()
	if filepath.Base(got) == "sh" {
		t.Errorf("userShell() = %q, want a real login shell rather than sh", got)
	}
	// Whatever it falls back to must at least be an absolute path we can exec.
	if !filepath.IsAbs(got) {
		t.Errorf("userShell() = %q, want an absolute path", got)
	}
}

func TestUserShellHonoursARealShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	if got := userShell(); got != "/usr/bin/fish" {
		t.Errorf("userShell() = %q, want /usr/bin/fish", got)
	}
}

// An exported-but-blank variable is how a parent process says "unset". Treating
// it as present would let it mask the real credential from the login shell.
func TestResolveOverwritesEmptyInheritedValue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	reset(t)
	t.Setenv("SHELL", fakeShell(t, envDump("ANTHROPIC_API_KEY=sk-from-shell")))
	t.Setenv("ANTHROPIC_API_KEY", "") // inherited but blank

	st := Resolve(nil)
	if !st.OK {
		t.Fatalf("probe failed: %s", st.LastError)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "sk-from-shell" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want the shell's value to replace the blank one", got)
	}
	if !slices.Contains(st.Imported, "ANTHROPIC_API_KEY") {
		t.Errorf("Imported = %v, want ANTHROPIC_API_KEY", st.Imported)
	}
}

// Status must name the shell it probed: that is what turns "found nothing" from
// an unexplained silence into a diagnosis.
func TestStatusReportsProbedShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("resolve is a no-op on Windows")
	}
	reset(t)
	shell := fakeShell(t, envDump())
	t.Setenv("SHELL", shell)

	st := Resolve(nil)
	if st.Shell != shell {
		t.Errorf("Shell = %q, want %q", st.Shell, shell)
	}
	if len(st.Imported) != 0 {
		t.Errorf("Imported = %v, want none", st.Imported)
	}
}
