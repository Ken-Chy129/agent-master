package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Ken-Chy129/agent-master/internal/config"
	"github.com/Ken-Chy129/agent-master/internal/session"
	"github.com/Ken-Chy129/agent-master/internal/store"
)

// maxImageBytes caps a single decoded image to keep memory and prompt size
// bounded (screenshots are well under this).
const maxImageBytes = 12 * 1024 * 1024

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
		Effort       string `json:"effort"`
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
		Effort:       body.Effort,
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

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	if body.Title == "" {
		writeErr(w, http.StatusBadRequest, errors.New("title is required"))
		return
	}
	sess, err := s.svc.RenameSession(r.PathValue("id"), body.Title)
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
		Message        string  `json:"message"`
		Model          *string `json:"model"`  // nil = keep session default
		Effort         *string `json:"effort"` // nil = keep session default
		ClientIntentID string  `json:"clientIntentId"`
		Images         []struct {
			Name      string `json:"name"`
			MediaType string `json:"mediaType"`
			Data      string `json:"data"` // base64, no data: prefix
		} `json:"images"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// A turn must carry text or at least one image.
	if strings.TrimSpace(body.Message) == "" && len(body.Images) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("message or an image is required"))
		return
	}

	var images []session.ImageUpload
	for _, img := range body.Images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("image data must be base64"))
			return
		}
		if len(data) > maxImageBytes {
			writeErr(w, http.StatusRequestEntityTooLarge, errors.New("image exceeds size limit"))
			return
		}
		images = append(images, session.ImageUpload{Name: img.Name, MediaType: img.MediaType, Data: data})
	}

	runID, err := s.svc.Send(r.PathValue("id"), session.SendInput{
		Message:        body.Message,
		Model:          body.Model,
		Effort:         body.Effort,
		Images:         images,
		ClientIntentID: body.ClientIntentID,
	})
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

// handleUpload serves a previously staged image for a session. Auth is enforced
// by the wrapping middleware (Bearer header or ?token=, so <img> tags work).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	dir, err := config.UploadsDir(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// filepath.Base defuses path traversal; the file must resolve inside dir.
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == "/" || strings.Contains(name, "..") {
		writeErr(w, http.StatusBadRequest, errors.New("bad name"))
		return
	}
	path := filepath.Join(dir, name)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		writeErr(w, http.StatusNotFound, errors.New("not found"))
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.svc.Models(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
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
