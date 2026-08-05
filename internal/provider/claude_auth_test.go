package provider

import (
	"os"
	"strings"
	"testing"
	"time"
)

// resetAuthCache clears the ClaudeAuth TTL cache so each case probes fresh.
func resetAuthCache(t *testing.T) {
	t.Helper()
	clear := func() {
		authMu.Lock()
		authAt = time.Time{}
		authSt = AuthStatus{}
		authMu.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

// clearCredEnv removes every credential env var the lookup consults, so a value
// in the developer's own environment cannot decide the result.
func clearCredEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN",
		"CLAUDE_OAUTH_TOKEN", "ANTHROPIC_BASE_URL",
		"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
	} {
		if v, ok := os.LookupEnv(k); ok {
			t.Setenv(k, v) // register for restore
			os.Unsetenv(k)
		}
	}
}

// stubKeychain replaces the macOS Keychain lookup for the duration of a test, so
// the "no credential anywhere" path is exercisable on a machine that has one.
func stubKeychain(t *testing.T, token string) {
	t.Helper()
	prev := keychainToken
	keychainToken = func() string { return token }
	t.Cleanup(func() { keychainToken = prev })
}

// Bedrock and Vertex supply credentials through the cloud provider's own chain,
// so finding no Anthropic credential there is correct, not a problem to report.
func TestClaudeAuthAcceptsCloudProviders(t *testing.T) {
	for _, tc := range []struct{ env, want string }{
		{"CLAUDE_CODE_USE_BEDROCK", "Bedrock"},
		{"CLAUDE_CODE_USE_VERTEX", "Vertex"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			resetAuthCache(t)
			clearCredEnv(t)
			t.Setenv(tc.env, "1")

			st := probeClaudeAuth()
			if !st.OK {
				t.Fatalf("OK = false for %s, want true", tc.env)
			}
			if !strings.Contains(st.Source, tc.want) {
				t.Errorf("Source = %q, want it to mention %s", st.Source, tc.want)
			}
			if st.Hint != "" {
				t.Errorf("Hint = %q, want none when authenticated", st.Hint)
			}
		})
	}
}

// A falsy value must not be mistaken for opting in.
func TestCloudProviderFlagRespectsFalsyValues(t *testing.T) {
	for _, v := range []string{"", "0", "false", "no", "FALSE"} {
		if envTruthy2(t, v) {
			t.Errorf("envTruthy(%q) = true, want false", v)
		}
	}
	for _, v := range []string{"1", "true", "yes", "TRUE"} {
		if !envTruthy2(t, v) {
			t.Errorf("envTruthy(%q) = false, want true", v)
		}
	}
}

func envTruthy2(t *testing.T, v string) bool {
	t.Helper()
	t.Setenv("AGENT_MASTER_TEST_FLAG", v)
	return envTruthy("AGENT_MASTER_TEST_FLAG")
}

// The reported source must name where the credential came from, so a machine
// using the wrong one is diagnosable without ever printing the secret.
func TestClaudeAuthReportsEnvSourceAndBaseURL(t *testing.T) {
	resetAuthCache(t)
	clearCredEnv(t)
	stubKeychain(t, "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_BASE_URL", "https://proxy.example.test")

	st := probeClaudeAuth()
	if !st.OK {
		t.Fatal("OK = false with ANTHROPIC_API_KEY set")
	}
	if st.Source != "ANTHROPIC_API_KEY" {
		t.Errorf("Source = %q, want ANTHROPIC_API_KEY", st.Source)
	}
	if st.BaseURL != "https://proxy.example.test" {
		t.Errorf("BaseURL = %q, want the configured proxy", st.BaseURL)
	}
	if strings.Contains(st.Source+st.Hint+st.BaseURL, "sk-test") {
		t.Error("AuthStatus leaked the credential value")
	}
}

// The failure this check exists for: a base URL configured but no credential —
// exactly what produced a per-message "Not logged in" for weeks.
func TestClaudeAuthReportsMissingCredential(t *testing.T) {
	resetAuthCache(t)
	clearCredEnv(t)
	stubKeychain(t, "")           // the machine running tests may have a real login
	t.Setenv("HOME", t.TempDir()) // no ~/.claude/.credentials.json
	t.Setenv("ANTHROPIC_BASE_URL", "https://proxy.example.test")

	st := probeClaudeAuth()
	if st.OK {
		t.Fatalf("OK = true with no credential anywhere (source %q)", st.Source)
	}
	if !strings.Contains(st.Hint, "ANTHROPIC_BASE_URL") {
		t.Errorf("Hint = %q, want it to call out the base URL with no credential", st.Hint)
	}
}

// ClaudeAuth caches, so a credential appearing later is picked up without a
// restart once the TTL lapses — but within it the same answer is reused.
func TestClaudeAuthCaches(t *testing.T) {
	resetAuthCache(t)
	clearCredEnv(t)
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")

	first := ClaudeAuth()
	os.Unsetenv("CLAUDE_CODE_USE_BEDROCK")
	if second := ClaudeAuth(); second.Source != first.Source {
		t.Errorf("Source changed within the TTL: %q then %q", first.Source, second.Source)
	}
}
