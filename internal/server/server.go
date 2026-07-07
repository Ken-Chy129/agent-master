// Package server exposes the daemon's HTTP API: public health plus
// token-protected endpoints. M0 ships /health and /api/info; later milestones
// add sessions, send, and the SSE stream.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/session"
	"github.com/Ken-Chy129/agent-master/internal/store"
	"github.com/Ken-Chy129/agent-master/internal/version"
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
	s := &Server{cfg: cfg, store: st, svc: svc}

	mux := http.NewServeMux()
	// Public.
	mux.HandleFunc("GET /health", s.handleHealth)
	// Token-protected.
	mux.Handle("GET /api/info", s.auth(http.HandlerFunc(s.handleInfo)))
	mux.Handle("GET /api/sessions", s.auth(http.HandlerFunc(s.handleListSessions)))
	mux.Handle("POST /api/sessions", s.auth(http.HandlerFunc(s.handleCreateSession)))
	mux.Handle("GET /api/sessions/{id}", s.auth(http.HandlerFunc(s.handleGetSession)))
	mux.Handle("DELETE /api/sessions/{id}", s.auth(http.HandlerFunc(s.handleDeleteSession)))
	mux.Handle("GET /api/sessions/{id}/messages", s.auth(http.HandlerFunc(s.handleMessages)))
	mux.Handle("POST /api/sessions/{id}/send", s.auth(http.HandlerFunc(s.handleSend)))
	mux.Handle("POST /api/sessions/{id}/interrupt", s.auth(http.HandlerFunc(s.handleInterrupt)))
	mux.Handle("GET /api/sessions/{id}/stream", s.auth(http.HandlerFunc(s.handleStream)))
	mux.Handle("GET /api/sessions/{id}/render", s.auth(http.HandlerFunc(s.handleRender)))
	mux.Handle("GET /api/workspaces", s.auth(http.HandlerFunc(s.handleWorkspaces)))

	s.http = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           s.corsMiddleware(logMiddleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// ListenAndServe blocks serving HTTP until the server is shut down.
func (s *Server) ListenAndServe() error {
	slog.Info("agent-master listening", "addr", s.cfg.Addr(), "version", version.Version)
	return s.http.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": version.Version,
	})
}

// handleInfo reports machine identity and provider availability so a client's
// machine list can show whether claude is usable here.
func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	hostname, _ := os.Hostname()

	claudePath := s.cfg.ClaudeBin
	if claudePath == "" {
		claudePath, _ = exec.LookPath("claude")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":    hostname,
		"version": version.Version,
		"providers": map[string]any{
			"claude": map[string]any{
				"available": claudePath != "",
				"path":      claudePath,
			},
		},
	})
}
