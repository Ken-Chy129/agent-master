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

// fetchClaudeModels calls Anthropic's /v1/models with the Claude Code OAuth
// token and the CLI's identifying headers. Returns ok=false on any failure so
// the caller falls back to the built-in list.
func fetchClaudeModels(ctx context.Context) ([]ModelInfo, bool) {
	token := claudeOAuthToken()
	if token == "" {
		return nil, false
	}

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://api.anthropic.com/v1/models?limit=100", nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("user-agent", "claude-cli/2.0.0 (external, cli)")

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

// claudeOAuthToken locates the Claude Code credential: env vars first, then the
// credentials file, then the macOS Keychain. Returns "" when none is found.
func claudeOAuthToken() string {
	for _, env := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	if tok := tokenFromCredentialsFile(); tok != "" {
		return tok
	}
	if runtime.GOOS == "darwin" {
		if tok := tokenFromMacKeychain(); tok != "" {
			return tok
		}
	}
	return ""
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
