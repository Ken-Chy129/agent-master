package provider

import (
	"os"
	"strings"
	"sync"
	"time"
)

// authTTL caches the auth probe briefly. Clients poll /api/info, and on macOS the
// keychain lookup spawns security(1) — cheap once, wasteful per request.
const authTTL = 30 * time.Second

// AuthStatus reports whether the claude CLI on this machine has a credential to
// run with, and where it comes from. It carries sources and endpoints only, never
// the secret, so it is safe to log and to serve to clients.
//
// Why this exists: "claude has no credentials" is knowable *before* a run, from
// exactly the same lookup the CLI performs. Without this check the only symptom
// is a per-message `Not logged in · Please run /login` from the CLI, wrapped in
// `claude exited: exit status 1` — which points nowhere near the daemon's
// environment and left one machine failing every message for weeks while
// `status` reported it healthy.
type AuthStatus struct {
	// OK reports whether a run has some credential to use — either an Anthropic
	// credential was located, or a cloud provider is configured to supply its own.
	OK bool
	// Source names where it came from: an env var name, "~/.claude/.credentials.json",
	// "macOS Keychain", or a cloud provider. Empty when OK is false.
	Source string
	// BaseURL is ANTHROPIC_BASE_URL when set — the endpoint runs will actually hit.
	// Worth surfacing: a base URL with no credential is the exact shape of the
	// failure above, and a base URL pointing somewhere unexpected is worth seeing.
	BaseURL string
	// Hint is what to do about it, when OK is false.
	Hint string
}

var (
	authMu sync.Mutex
	authAt time.Time
	authSt AuthStatus
)

// ClaudeAuth reports the credential the spawned claude CLI would find, using the
// same resolution order as the CLI itself. Result is cached for authTTL, so a
// credential that appears later (the user runs `claude /login`) is picked up
// without restarting the daemon.
func ClaudeAuth() AuthStatus {
	authMu.Lock()
	defer authMu.Unlock()
	if !authAt.IsZero() && time.Since(authAt) < authTTL {
		return authSt
	}
	authSt = probeClaudeAuth()
	authAt = time.Now()
	return authSt
}

func probeClaudeAuth() AuthStatus {
	st := AuthStatus{BaseURL: strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL"))}

	// Bedrock and Vertex authenticate through the cloud provider's own credential
	// chain (AWS profiles/roles, GCP ADC), so there is deliberately no Anthropic
	// credential to find. Reporting those as unauthenticated would be a false
	// alarm on a perfectly working machine.
	if envTruthy("CLAUDE_CODE_USE_BEDROCK") {
		st.OK, st.Source = true, "AWS Bedrock (CLAUDE_CODE_USE_BEDROCK)"
		return st
	}
	if envTruthy("CLAUDE_CODE_USE_VERTEX") {
		st.OK, st.Source = true, "Google Vertex (CLAUDE_CODE_USE_VERTEX)"
		return st
	}

	if cred := claudeCredential(); cred.token != "" {
		st.OK, st.Source = true, cred.source
		return st
	}

	// Nothing found. This is a warning rather than a hard block on runs: the
	// lookup mirrors the CLI's, but an exotic gateway that needs no credential
	// would be a false positive, and refusing to run on one would be worse than
	// the cryptic error this replaces.
	// Localized: Hint is served to clients over /api/info and shown to the user.
	st.Hint = "在这台机器上执行 claude /login，或在登录 shell 中 export ANTHROPIC_API_KEY"
	if st.BaseURL != "" {
		st.Hint = "已设置 ANTHROPIC_BASE_URL 但未找到任何凭证：请在登录 shell 中 export ANTHROPIC_API_KEY，或执行 claude /login"
	}
	return st
}

// envTruthy reports whether an env var is set to something meaning "on". The CLI
// accepts 1/true, and treats 0/false/empty as off.
func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "", "0", "false", "no":
		return false
	}
	return true
}
