package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
)

// healthServer serves /health on a fresh loopback port, returning the port so a
// test can drive probeHealth/waitHealthy, which are hardwired to 127.0.0.1.
func healthServer(t *testing.T, h http.Handler) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h}}
	srv.Start()
	t.Cleanup(srv.Close)
	return ln.Addr().(*net.TCPAddr).Port
}

func TestWaitHealthyReturnsOnceServing(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"status":"ok","version":"0.0.1-dev"}`)
	}))

	start := time.Now()
	if _, ok := waitHealthy(port, 5*time.Second); !ok {
		t.Fatal("waitHealthy did not see a serving daemon")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %s to notice a daemon that was already up", elapsed)
	}
}

// The regression that matters: a service manager can accept the job while the
// daemon exits immediately after. waitHealthy must report that, not succeed.
func TestWaitHealthyFailsWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // nothing is listening on this port now

	if _, ok := waitHealthy(port, 1200*time.Millisecond); ok {
		t.Fatal("waitHealthy reported success with no daemon listening")
	}
}

// A non-200 answer is not a healthy daemon.
func TestWaitHealthyRejectsErrorStatus(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	if _, ok := waitHealthy(port, 1200*time.Millisecond); ok {
		t.Fatal("waitHealthy accepted a 500 as healthy")
	}
}

func TestLogTailReturnsLastLines(t *testing.T) {
	// config.LogPath is derived from the home directory, so point it at a temp one.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	dir := filepath.Join(home, ".agent-master")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.log"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	tail := logTail(5)
	lines := strings.Split(tail, "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5:\n%s", len(lines), tail)
	}
	if lines[0] != "line 26" || lines[4] != "line 30" {
		t.Errorf("tail = %q, want lines 26-30", tail)
	}
}

func TestLogTailToleratesMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if tail := logTail(5); tail != "" {
		t.Errorf("logTail = %q, want empty when there is no log", tail)
	}
}

// A failed start must carry the log tail inline: telling the user to go find the
// file is what made the original failure invisible for two weeks.
func TestStartupFailureIncludesLogTail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".agent-master")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.log"),
		[]byte("level=ERROR msg=\"something broke\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := startupFailure(9999)
	if err == nil {
		t.Fatal("startupFailure returned nil")
	}
	if !strings.Contains(err.Error(), "9999") {
		t.Errorf("error omits the port: %v", err)
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Errorf("error omits the log tail: %v", err)
	}
}

// A daemon that is up but cannot reach the right credentials must say so — a
// bare ✓ there is the same lie as printing ✓ for a daemon that never started.
func TestDegradedWarningSurfacesBlockedShellEnv(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want the config token", got)
		}
		fmt.Fprint(w, `{"shell_env":{"blocked":true,"missing":["ANTHROPIC_API_KEY"]}}`)
	}))

	var out strings.Builder
	writeDegradedWarning(&out, &config.Config{Port: port, Token: "tok"})

	text := out.String()
	if !strings.Contains(text, "ANTHROPIC_API_KEY") {
		t.Errorf("warning omits the missing variable:\n%s", text)
	}
	if !strings.Contains(text, "拒绝") {
		t.Errorf("warning does not say sends are refused:\n%s", text)
	}
}

func TestDegradedWarningSilentWhenHealthy(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"shell_env":{"blocked":false,"ok":true}}`)
	}))

	var out strings.Builder
	writeDegradedWarning(&out, &config.Config{Port: port, Token: "tok"})
	if out.String() != "" {
		t.Errorf("warned about a healthy daemon: %q", out.String())
	}
}

// The failure that hid for a month: a stale daemon holds the port, the new one
// exits with "bind: address already in use" into the service manager's journal,
// and daemon.log says nothing. startupFailure must name it.
func TestStartupFailureNamesThePortSquatter(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"ok","version":"0.0.1-dev"}`)
	}))

	err := startupFailure(port)
	if err == nil {
		t.Fatal("startupFailure returned nil")
	}
	for _, want := range []string{"已被另一个 agent-master 实例占用", "0.0.1-dev", "lsof", "ss -ltnp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}

// Missing credentials are a warning, not a refusal — but they must still be said
// out loud, with the shell that was probed.
func TestDegradedWarningSurfacesMissingCredentials(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"shell_env":{"blocked":false,"ok":true,"shell":"/bin/sh"},`+
			`"auth":{"ok":false,"hint":"run claude /login on this machine"}}`)
	}))

	var out strings.Builder
	writeDegradedWarning(&out, &config.Config{Port: port, Token: "tok"})

	text := out.String()
	for _, want := range []string{"未找到 Claude 凭证", "/bin/sh", "agent-master doctor"} {
		if !strings.Contains(text, want) {
			t.Errorf("warning missing %q:\n%s", want, text)
		}
	}
}

// A blocked shell env is the more specific problem; don't stack both warnings.
func TestDegradedWarningPrefersTheBlockedReason(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"shell_env":{"blocked":true,"missing":["ANTHROPIC_API_KEY"]},`+
			`"auth":{"ok":false,"hint":"run claude /login on this machine"}}`)
	}))

	var out strings.Builder
	writeDegradedWarning(&out, &config.Config{Port: port, Token: "tok"})

	text := out.String()
	if !strings.Contains(text, "ANTHROPIC_API_KEY") {
		t.Errorf("lost the blocked reason:\n%s", text)
	}
	if strings.Contains(text, "未找到 Claude 凭证") {
		t.Errorf("stacked a second warning on top of the blocked one:\n%s", text)
	}
}

// An older daemon has no "auth" field at all. Absent must not read as "no
// credentials", or a new CLI would falsely warn about every daemon it predates.
func TestDegradedWarningSilentAgainstOlderDaemon(t *testing.T) {
	port := healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"name":"old","version":"0.2.2","providers":{"claude":{"available":true}}}`)
	}))

	var out strings.Builder
	writeDegradedWarning(&out, &config.Config{Port: port, Token: "tok"})
	if out.String() != "" {
		t.Errorf("warned about a daemon that predates the auth field: %q", out.String())
	}
}

// freePort returns a port with nothing listening on it.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
