package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/version"
)

// fakeDaemon serves /health and /api/info on a loopback port so doctor can be
// driven end to end against a chosen daemon state.
func fakeDaemon(t *testing.T, ver, infoJSON string) int {
	t.Helper()
	return healthServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprintf(w, `{"status":"ok","version":%q}`, ver)
		case "/api/info":
			fmt.Fprint(w, infoJSON)
		default:
			http.NotFound(w, r)
		}
	}))
}

// isolateHome points config.LogPath/Dir at a temp home so a test never reads the
// developer's real daemon log.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".agent-master")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runDoctor(t *testing.T, port int) (string, int, int) {
	t.Helper()
	var out strings.Builder
	d := &doctor{out: &out, cfg: &config.Config{Host: "127.0.0.1", Port: port, Token: "tok"}}
	d.run()
	return out.String(), d.failures(), len(d.findings) - d.failures()
}

// The month-long failure, as doctor should have reported it: the daemon is up and
// healthy-looking, but has no credential, so every run fails.
func TestDoctorFailsOnMissingCredentials(t *testing.T) {
	isolateHome(t)
	port := fakeDaemon(t, version.Version, `{
		"providers":{"claude":{"path":"/usr/local/bin/claude","available":true}},
		"auth":{"ok":false,"hint":"run claude /login on this machine"},
		"shell_env":{"ok":true,"shell":"/bin/sh","imported":null,"blocked":false}}`)

	out, failed, _ := runDoctor(t, port)

	if failed == 0 {
		t.Errorf("doctor reported no failures for a daemon with no credentials:\n%s", out)
	}
	// One entry, titled with the consequence, followed by pasteable commands.
	for _, want := range []string{"✗ 未找到 Claude 凭证", "修复方式：", "claude /login"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	// And it must explain why editing an rc file alone will not help under sh.
	for _, want := range []string{"/bin/sh", "不读取"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	// The mechanism belongs in the diagnostics block, not in the verdict.
	if !strings.Contains(out, "诊断信息") {
		t.Errorf("report has no diagnostics block:\n%s", out)
	}
}

// A refused-sends state is a failure, not a warning: nothing will run.
func TestDoctorFailsWhenShellEnvBlocked(t *testing.T) {
	isolateHome(t)
	port := fakeDaemon(t, version.Version, `{
		"providers":{"claude":{"path":"/usr/local/bin/claude"}},
		"auth":{"ok":true,"source":"ANTHROPIC_API_KEY"},
		"shell_env":{"ok":false,"blocked":true,"shell":"/bin/zsh",
			"reason":"ANTHROPIC_API_KEY could not be read"}}`)

	out, failed, _ := runDoctor(t, port)

	if failed == 0 {
		t.Errorf("blocked sends reported as non-fatal:\n%s", out)
	}
	if !strings.Contains(out, "shell_env_optional") {
		t.Errorf("report omits the escape hatch:\n%s", out)
	}
}

// A fully working daemon must produce no failures and no warnings, or the report
// becomes noise people learn to ignore.
func TestDoctorQuietWhenHealthy(t *testing.T) {
	isolateHome(t)
	port := fakeDaemon(t, version.Version, `{
		"providers":{"claude":{"path":"/usr/local/bin/claude"}},
		"auth":{"ok":true,"source":"ANTHROPIC_API_KEY","baseUrl":"https://proxy.test"},
		"shell_env":{"ok":true,"blocked":false,"shell":"/bin/zsh",
			"imported":["ANTHROPIC_API_KEY","ANTHROPIC_BASE_URL"]}}`)

	out, failed, warned := runDoctor(t, port)

	if failed != 0 || warned != 0 {
		t.Errorf("healthy daemon produced %d failures / %d warnings:\n%s", failed, warned, out)
	}
	if !strings.Contains(out, "✓ 全部检查通过") {
		t.Errorf("missing the all-clear:\n%s", out)
	}
}

// An older daemon omits auth/shell_env. That is a "cannot tell", not a fault —
// reporting it as a failure would make doctor cry wolf on every un-upgraded box.
func TestDoctorTreatsOlderDaemonAsUnknownNotBroken(t *testing.T) {
	isolateHome(t)
	port := fakeDaemon(t, version.Version, `{
		"name":"old","version":"0.2.2",
		"providers":{"claude":{"path":"/usr/local/bin/claude","available":true}}}`)

	out, failed, warned := runDoctor(t, port)

	if failed != 0 {
		t.Errorf("older daemon reported as failing:\n%s", out)
	}
	if warned == 0 {
		t.Errorf("older daemon reported as fully healthy:\n%s", out)
	}
	if !strings.Contains(out, "版本过旧") {
		t.Errorf("report does not say the daemon is too old:\n%s", out)
	}
}

// The port-squatter: something answers, but it is not this build. That is the
// shape of a stale daemon blocking every restart of the real service.
func TestDoctorFlagsVersionMismatchOnThePort(t *testing.T) {
	isolateHome(t)
	prev := version.Version
	version.Version = "0.3.0" // a non-dev build, so the check applies
	t.Cleanup(func() { version.Version = prev })

	port := fakeDaemon(t, "0.0.1-dev", `{"providers":{"claude":{}}}`)
	out, failed, _ := runDoctor(t, port)

	if failed == 0 {
		t.Errorf("version mismatch on the port not reported as a failure:\n%s", out)
	}
	for _, want := range []string{"0.0.1-dev", "被另一个 agent-master 实例占用", "lsof"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

// With no daemon, doctor must still run local checks — and must not let a pass
// there imply anything about the daemon's environment, which is the discrepancy
// that hides this whole class of failure.
func TestDoctorWithoutDaemonLabelsLocalChecks(t *testing.T) {
	isolateHome(t)
	port := freePort(t)

	out, failed, _ := runDoctor(t, port)

	if failed == 0 {
		t.Errorf("absent daemon not reported as a failure:\n%s", out)
	}
	if !strings.Contains(out, "不代表守护进程的运行环境") {
		t.Errorf("local checks not labelled as the caller's shell:\n%s", out)
	}
}

func TestRecentProblemsFiltersAndCaps(t *testing.T) {
	dir := isolateHome(t)
	path := filepath.Join(dir, "daemon.log")

	var b strings.Builder
	b.WriteString("time=1 level=INFO msg=\"boring\"\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, "time=%d level=ERROR msg=\"bad %d\"\n", i+2, i)
	}
	b.WriteString("time=99 level=INFO msg=\"boring\"\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	got := recentProblems(path, 200)
	if len(got) != 5 {
		t.Fatalf("got %d problem lines, want the last 5: %v", len(got), got)
	}
	if !strings.Contains(got[4], "bad 7") {
		t.Errorf("last line = %q, want the most recent error", got[4])
	}
	for _, l := range got {
		if strings.Contains(l, "INFO") {
			t.Errorf("included a non-problem line: %q", l)
		}
	}
}

func TestRecentProblemsToleratesMissingLog(t *testing.T) {
	if got := recentProblems(filepath.Join(t.TempDir(), "nope.log"), 200); got != nil {
		t.Errorf("recentProblems = %v, want nil for a missing log", got)
	}
}
