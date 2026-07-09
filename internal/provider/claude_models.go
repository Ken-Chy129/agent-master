package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// modelsTTL is how long a successfully fetched model catalog is reused before
// re-fetching. The list changes rarely; this avoids a network hop per UI open.
const modelsTTL = 10 * time.Minute

// builtinClaudeModels is the fallback catalog used when the live list can't be
// fetched (no Claude Code token / offline). It mirrors the CLI's model picker;
// effort levels are the ones each family accepts for --effort.
var builtinClaudeModels = []ModelInfo{
	{ID: "opus", Label: "Opus", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
	{ID: "sonnet", Label: "Sonnet", Efforts: []string{"low", "medium", "high", "max"}},
	{ID: "haiku", Label: "Haiku", Efforts: []string{"low", "medium", "high"}},
}

// defaultModelOption is prepended to every list: "" keeps the CLI's own default.
var defaultModelOption = ModelInfo{ID: "", Label: "默认模型", Description: "Claude Code 的默认模型"}

// Models returns the selectable models. It fetches the live catalog from
// Anthropic using the Claude Code OAuth token, caches it, and falls back to the
// built-in list when the token or network is unavailable.
func (c *Claude) Models(ctx context.Context) ([]ModelInfo, error) {
	c.modelsMu.Lock()
	if c.modelsCache != nil && time.Since(c.modelsAt) < modelsTTL {
		cached := c.modelsCache
		c.modelsMu.Unlock()
		return cached, nil
	}
	c.modelsMu.Unlock()

	models, ok := fetchClaudeModels(ctx)
	if !ok {
		// Don't cache the fallback: we want to retry the live fetch next time.
		return withDefault(builtinClaudeModels), nil
	}
	models = withDefault(models)

	c.modelsMu.Lock()
	c.modelsCache = models
	c.modelsAt = time.Now()
	c.modelsMu.Unlock()
	return models, nil
}

func withDefault(models []ModelInfo) []ModelInfo {
	return append([]ModelInfo{defaultModelOption}, models...)
}

// fetchClaudeModels calls the /v1/models endpoint with the same credential and
// base URL the claude CLI would use, so the picker matches what actually runs.
// It honors ANTHROPIC_BASE_URL (e.g. a proxy/gateway) and selects the auth
// header by credential kind: an API key rides x-api-key, an OAuth token rides
// Authorization: Bearer with the oauth beta header. Returns ok=false on any
// failure so the caller falls back to the built-in list.
func fetchClaudeModels(ctx context.Context) ([]ModelInfo, bool) {
	cred := claudeCredential()
	if cred.token == "" {
		return nil, false
	}

	base := strings.TrimRight(strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")), "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+"/v1/models?limit=100", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("user-agent", "claude-cli/2.0.0 (external, cli)")
	switch cred.kind {
	case credAPIKey:
		req.Header.Set("x-api-key", cred.token)
	case credOAuth:
		req.Header.Set("authorization", "Bearer "+cred.token)
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	default: // credBearer — a custom gateway bearer token, no oauth beta
		req.Header.Set("authorization", "Bearer "+cred.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	var body struct {
		Data []struct {
			ID           string `json:"id"`
			DisplayName  string `json:"display_name"`
			Description  string `json:"description"`
			Capabilities struct {
				// effort is an object of {level: {supported}} for models that
				// support reasoning effort, or a plain bool for those that
				// don't — so decode it lazily.
				Effort json.RawMessage `json:"effort"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, false
	}
	if len(body.Data) == 0 {
		return nil, false
	}

	// Preserve the effort ordering the CLI uses rather than map iteration order.
	order := []string{"low", "medium", "high", "xhigh", "max"}
	out := make([]ModelInfo, 0, len(body.Data))
	for _, m := range body.Data {
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		var effortMap map[string]struct {
			Supported bool `json:"supported"`
		}
		_ = json.Unmarshal(m.Capabilities.Effort, &effortMap) // no-op when effort is a bool
		var efforts []string
		for _, lvl := range order {
			if e, ok := effortMap[lvl]; ok && e.Supported {
				efforts = append(efforts, lvl)
			}
		}
		out = append(out, ModelInfo{ID: m.ID, Label: label, Description: m.Description, Efforts: efforts})
	}
	return out, true
}

// credKind classifies how a credential must be presented to the API.
type credKind int

const (
	credOAuth  credKind = iota // Authorization: Bearer + oauth beta header
	credAPIKey                 // x-api-key header
	credBearer                 // Authorization: Bearer, no oauth beta (custom gateway)
)

// credential is a located Claude Code credential and how to present it.
type credential struct {
	token string
	kind  credKind
}

// claudeCredential locates the Claude Code credential the same way the CLI does:
// env vars first (in priority order), then the credentials file, then the macOS
// Keychain. The kind mirrors how the CLI authenticates each source, so the
// models request matches the auth the actual run will use. Returns a zero-value
// credential (empty token) when none is found.
func claudeCredential() credential {
	// Priority mirrors the CLI: key-based credentials win over OAuth. When both
	// a key and an OAuth token are set (common: ANTHROPIC_API_KEY + a proxy plus
	// a lingering CLAUDE_CODE_OAUTH_TOKEN), the CLI bills the key — so the models
	// request must use the key too, or it would query a different backend than
	// the one that actually runs. OAuth tokens are also only valid against
	// api.anthropic.com, so they can't reach a custom ANTHROPIC_BASE_URL anyway.
	envs := []struct {
		name string
		kind credKind
	}{
		{"ANTHROPIC_AUTH_TOKEN", credBearer}, // explicit custom-gateway bearer
		{"ANTHROPIC_API_KEY", credAPIKey},
		{"CLAUDE_CODE_OAUTH_TOKEN", credOAuth},
		{"CLAUDE_OAUTH_TOKEN", credOAuth},
	}
	for _, e := range envs {
		if v := strings.TrimSpace(os.Getenv(e.name)); v != "" {
			return credential{token: v, kind: e.kind}
		}
	}
	// File and Keychain both hold the claude.ai OAuth access token.
	if tok := tokenFromCredentialsFile(); tok != "" {
		return credential{token: tok, kind: credOAuth}
	}
	if runtime.GOOS == "darwin" {
		if tok := tokenFromMacKeychain(); tok != "" {
			return credential{token: tok, kind: credOAuth}
		}
	}
	return credential{}
}

// oauthCreds is the shape of both the credentials file and the Keychain item.
type oauthCreds struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

func tokenFromCredentialsFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if err != nil {
		return ""
	}
	var creds oauthCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return ""
	}
	return creds.ClaudeAiOauth.AccessToken
}

func tokenFromMacKeychain() string {
	out, err := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return ""
	}
	var creds oauthCreds
	if err := json.Unmarshal(out, &creds); err != nil {
		return ""
	}
	return creds.ClaudeAiOauth.AccessToken
}
