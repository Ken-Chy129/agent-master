// Package server exposes the daemon's HTTP API: public health plus
// token-protected endpoints. M0 ships /health and /api/info; later milestones
// add sessions, send, and the SSE stream.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/provider"
	"github.com/Ken-Chy129/agent-master/internal/session"
	"github.com/Ken-Chy129/agent-master/internal/shellenv"
	"github.com/Ken-Chy129/agent-master/internal/store"
	"github.com/Ken-Chy129/agent-master/internal/version"
	"github.com/Ken-Chy129/agent-master/internal/webui"
)

// Server holds the daemon's HTTP dependencies.
type Server struct {
	cfg   *config.Config
	store *store.Store
	svc   *session.Service
	http  *http.Server
}

// New builds the HTTP server and its routes.
func New(cfg *config.Config, st *store.Store, svc *session.Service) *Server {
	return newServer(cfg, st, svc, webui.NewHandler(webui.Embedded()))
}

func newServer(cfg *config.Config, st *store.Store, svc *session.Service, web http.Handler) *Server {
	s := &Server{cfg: cfg, store: st, svc: svc}

	mux := http.NewServeMux()
	// Public.
	mux.HandleFunc("GET /health", s.handleHealth)
	// Token-protected.
	mux.Handle("GET /api/info", s.auth(http.HandlerFunc(s.handleInfo)))
	mux.Handle("GET /api/sessions", s.auth(http.HandlerFunc(s.handleListSessions)))
	mux.Handle("POST /api/sessions", s.auth(http.HandlerFunc(s.handleCreateSession)))
	mux.Handle("GET /api/sessions/{id}", s.auth(http.HandlerFunc(s.handleGetSession)))
	mux.Handle("PATCH /api/sessions/{id}", s.auth(http.HandlerFunc(s.handleRenameSession)))
	mux.Handle("DELETE /api/sessions/{id}", s.auth(http.HandlerFunc(s.handleDeleteSession)))
	mux.Handle("GET /api/sessions/{id}/messages", s.auth(http.HandlerFunc(s.handleMessages)))
	mux.Handle("POST /api/sessions/{id}/send", s.auth(http.HandlerFunc(s.handleSend)))
	mux.Handle("POST /api/sessions/{id}/interrupt", s.auth(http.HandlerFunc(s.handleInterrupt)))
	mux.Handle("GET /api/sessions/{id}/stream", s.auth(http.HandlerFunc(s.handleStream)))
	mux.Handle("GET /api/sessions/{id}/render", s.auth(http.HandlerFunc(s.handleRender)))
	mux.Handle("GET /api/workspaces", s.auth(http.HandlerFunc(s.handleWorkspaces)))
	mux.Handle("GET /api/models", s.auth(http.HandlerFunc(s.handleModels)))
	mux.Handle("GET /api/sessions/{id}/uploads/{name}", s.auth(http.HandlerFunc(s.handleUpload)))
	// The browser client is additive: exact health/API routes above keep their
	// existing behavior, while everything else is handled as a static SPA path.
	mux.Handle("/", web)

	s.http = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           s.corsMiddleware(logMiddleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Listen binds the configured address without serving yet. Callers that must
// act only once the port is actually held — e.g. recording a pidfile — bind
// first, then Serve.
func (s *Server) Listen() (net.Listener, error) {
	return net.Listen("tcp", s.cfg.Addr())
}

// Serve blocks serving HTTP on ln until the server is shut down.
func (s *Server) Serve(ln net.Listener) error {
	slog.Info("agent-master listening", "addr", s.cfg.Addr(), "version", version.Version)
	return s.http.Serve(ln)
}

// ListenAndServe blocks serving HTTP until the server is shut down.
func (s *Server) ListenAndServe() error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

// handleHealth answers liveness *and* readiness, which are different questions.
// "status":"ok" means the process is serving; "ready" means a run can actually be
// expected to succeed. Conflating them is what let a client show a green dot for
// a month while every message failed — the dot was driven by "the request
// returned", which was always true.
//
// The endpoint is unauthenticated, so "degraded" carries coarse machine-readable
// codes only. Anything that would identify variable names, shells or paths stays
// behind the token in /api/info.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	var degraded []string
	if _, blocked := shellenv.Blocked(); blocked {
		degraded = append(degraded, "shell_env")
	}
	if !provider.ClaudeAuth().OK {
		degraded = append(degraded, "credentials")
	}

	body := map[string]any{
		"status":  "ok",
		"version": version.Version,
		"ready":   len(degraded) == 0,
	}
	if len(degraded) > 0 {
		body["degraded"] = degraded
	}
	writeJSON(w, http.StatusOK, body)
}

// handleInfo reports machine identity and provider availability so a client's
// machine list can show whether claude is usable here.
func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	hostname, _ := os.Hostname()

	claudePath := s.cfg.ClaudeBin
	if claudePath == "" {
		claudePath, _ = exec.LookPath("claude")
	}

	// Surface shell-env resolution so a client can warn before the user sends
	// anything, instead of leaving the only trace in daemon.log. Names only —
	// Status never carries values.
	env := shellenv.Current()
	reason, blocked := shellenv.Blocked()
	shellEnv := map[string]any{
		"ok":       env.OK,
		"shell":    env.Shell,
		"imported": env.Imported,
		"retrying": env.Retrying,
		"blocked":  blocked,
	}
	if len(env.MissingAuth) > 0 {
		shellEnv["missing"] = env.MissingAuth
	}
	if reason != "" {
		shellEnv["reason"] = reason
	} else if env.LastError != "" {
		shellEnv["reason"] = env.LastError
	}

	// Whether the CLI has a credential at all — the check that turns a per-message
	// "Not logged in · Please run /login" into something a client can show up front.
	auth := provider.ClaudeAuth()
	authInfo := map[string]any{"ok": auth.OK}
	if auth.Source != "" {
		authInfo["source"] = auth.Source
	}
	if auth.BaseURL != "" {
		authInfo["baseUrl"] = auth.BaseURL
	}
	if auth.Hint != "" {
		authInfo["hint"] = auth.Hint
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":    hostname,
		"version": version.Version,
		"providers": map[string]any{
			"claude": map[string]any{
				// Runs are refused while blocked, so a client that only reads
				// this flag must not show claude as ready. Missing credentials
				// stay out of it: that is a warning, not a refusal (see
				// provider.AuthStatus), so runs still proceed.
				"available":     claudePath != "" && !blocked,
				"path":          claudePath,
				"authenticated": auth.OK,
			},
		},
		"shell_env": shellEnv,
		"auth":      authInfo,
	})
}
