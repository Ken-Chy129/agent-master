package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/provider"
	"github.com/Ken-Chy129/agent-master/internal/session"
	"github.com/Ken-Chy129/agent-master/internal/store"
)

// fakeProvider satisfies provider.Provider without spawning anything. The
// server tests never trigger a run, so Run is a no-op.
type fakeProvider struct{}

func (fakeProvider) Type() string { return "claude" }
func (fakeProvider) Run(_ context.Context, _ provider.RunOptions, _ func(provider.StreamEvent)) (provider.RunResult, error) {
	return provider.RunResult{NativeSessionID: "fake"}, nil
}
func (fakeProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "sonnet", Label: "Sonnet"}}, nil
}

const testToken = "test-token-123"

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{Host: "127.0.0.1", Port: 0, Token: testToken}
	svc := session.NewService(st, fakeProvider{})
	srv := New(cfg, st, svc)

	ts := httptest.NewServer(srv.http.Handler)
	t.Cleanup(ts.Close)
	return ts, ts.URL
}

func do(t *testing.T, method, url, token, body string) (*http.Response, string) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(data)
}

func TestHealthPublic(t *testing.T) {
	_, base := newTestServer(t)
	resp, body := do(t, "GET", base+"/health", "", "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("health = %d %s", resp.StatusCode, body)
	}
}

func TestAuthRequired(t *testing.T) {
	_, base := newTestServer(t)

	resp, _ := do(t, "GET", base+"/api/sessions", "", "")
	if resp.StatusCode != 401 {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
	resp, _ = do(t, "GET", base+"/api/sessions", "wrong", "")
	if resp.StatusCode != 401 {
		t.Fatalf("wrong token = %d, want 401", resp.StatusCode)
	}
	resp, _ = do(t, "GET", base+"/api/sessions", testToken, "")
	if resp.StatusCode != 200 {
		t.Fatalf("good token = %d, want 200", resp.StatusCode)
	}
}

func TestCreateAndListSession(t *testing.T) {
	_, base := newTestServer(t)
	ws := t.TempDir()

	resp, body := do(t, "POST", base+"/api/sessions", testToken,
		`{"title":"t1","workspaceDir":"`+ws+`"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("create = %d %s", resp.StatusCode, body)
	}
	var sess store.Session
	if err := json.Unmarshal([]byte(body), &sess); err != nil {
		t.Fatalf("decode session: %v (%s)", err, body)
	}
	if sess.ID == "" || sess.WorkspaceDir != ws {
		t.Fatalf("session = %+v", sess)
	}

	_, listBody := do(t, "GET", base+"/api/sessions", testToken, "")
	if !strings.Contains(listBody, sess.ID) || !strings.Contains(listBody, `"hasMore":false`) {
		t.Fatalf("list missing session/hasMore: %s", listBody)
	}
}

func TestRenameSessionEndpoint(t *testing.T) {
	_, base := newTestServer(t)
	ws := t.TempDir()

	_, body := do(t, "POST", base+"/api/sessions", testToken,
		`{"title":"before","workspaceDir":"`+ws+`"}`)
	var sess store.Session
	if err := json.Unmarshal([]byte(body), &sess); err != nil {
		t.Fatalf("decode session: %v (%s)", err, body)
	}

	resp, body := do(t, "PATCH", base+"/api/sessions/"+sess.ID, testToken, `{"title":"after"}`)
	if resp.StatusCode != 200 || !strings.Contains(body, `"title":"after"`) {
		t.Fatalf("rename = %d %s", resp.StatusCode, body)
	}
	_, listBody := do(t, "GET", base+"/api/sessions", testToken, "")
	if !strings.Contains(listBody, `"title":"after"`) {
		t.Fatalf("list after rename: %s", listBody)
	}

	resp, _ = do(t, "PATCH", base+"/api/sessions/"+sess.ID, testToken, `{"title":"  "}`)
	if resp.StatusCode != 400 {
		t.Fatalf("empty title = %d, want 400", resp.StatusCode)
	}
	resp, _ = do(t, "PATCH", base+"/api/sessions/missing", testToken, `{"title":"x"}`)
	if resp.StatusCode != 404 {
		t.Fatalf("missing session = %d, want 404", resp.StatusCode)
	}
}

func TestCreateSessionRejectsMissingDir(t *testing.T) {
	_, base := newTestServer(t)
	resp, _ := do(t, "POST", base+"/api/sessions", testToken,
		`{"workspaceDir":"/no/such/dir/xyz"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("missing dir = %d, want 400", resp.StatusCode)
	}
}

func TestWorkspacesBrowse(t *testing.T) {
	_, base := newTestServer(t)
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, body := do(t, "GET", base+"/api/workspaces?path="+root, testToken, "")
	if !strings.Contains(body, `"alpha"`) || !strings.Contains(body, `"beta"`) {
		t.Fatalf("workspaces missing dirs: %s", body)
	}
	if strings.Contains(body, ".hidden") {
		t.Fatalf("workspaces should skip hidden dirs: %s", body)
	}
}

func TestCORSPreflight(t *testing.T) {
	_, base := newTestServer(t)
	req, _ := http.NewRequest("OPTIONS", base+"/api/sessions", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("preflight = %d, want 204", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("missing ACAO header: %v", resp.Header)
	}
}
