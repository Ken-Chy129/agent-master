package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Ken-Chy129/agent-master/internal/session"
	"github.com/Ken-Chy129/agent-master/internal/store"
)

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit := atoiDefault(r.URL.Query().Get("limit"), 30)
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)
	list, hasMore, err := s.svc.ListSessions(limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []store.RecentSession{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": list, "hasMore": hasMore})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title        string `json:"title"`
		WorkspaceDir string `json:"workspaceDir"`
		Model        string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	body.WorkspaceDir = strings.TrimSpace(body.WorkspaceDir)
	if body.WorkspaceDir == "" {
		writeErr(w, http.StatusBadRequest, errors.New("workspaceDir is required"))
		return
	}
	if info, err := os.Stat(body.WorkspaceDir); err != nil || !info.IsDir() {
		writeErr(w, http.StatusBadRequest, errors.New("workspaceDir must be an existing directory"))
		return
	}
	if !s.workspaceAllowed(body.WorkspaceDir) {
		writeErr(w, http.StatusForbidden, errors.New("workspaceDir is not within an allowed root"))
		return
	}
	sess, err := s.svc.CreateSession(session.CreateSessionInput{
		Title:        body.Title,
		WorkspaceDir: body.WorkspaceDir,
		Model:        body.Model,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.svc.GetSession(r.PathValue("id"))
	if errors.Is(err, session.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteSession(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	beforeSeq := int64(atoiDefault(r.URL.Query().Get("before_seq"), 0))
	limit := atoiDefault(r.URL.Query().Get("limit"), 100)
	events, hasMore, err := s.svc.Messages(r.PathValue("id"), beforeSeq, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": toWire(events), "hasMore": hasMore})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message        string `json:"message"`
		ClientIntentID string `json:"clientIntentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("message is required"))
		return
	}
	runID, err := s.svc.Send(r.PathValue("id"), body.Message, body.ClientIntentID)
	switch {
	case errors.Is(err, session.ErrNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, session.ErrBusy):
		writeErr(w, http.StatusConflict, err)
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"runId": runID})
	}
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Interrupt(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// workspaceAllowed enforces the optional workspace-root whitelist.
func (s *Server) workspaceAllowed(dir string) bool {
	roots := s.cfg.WorkspaceRoots
	if len(roots) == 0 {
		return true // v1 default: no restriction
	}
	for _, root := range roots {
		if dir == root || strings.HasPrefix(dir, strings.TrimRight(root, "/")+"/") {
			return true
		}
	}
	return false
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
