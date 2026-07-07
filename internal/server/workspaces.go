package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type workspaceEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type workspaceListing struct {
	Path    string           `json:"path"`   // current directory ("" when listing roots)
	Parent  string           `json:"parent"` // parent directory ("" if none / not allowed)
	Roots   []string         `json:"roots"`  // configured workspace roots (may be empty)
	Entries []workspaceEntry `json:"entries"`
}

// handleWorkspaces lets a client browse directories to choose a session
// workspace. `?path=` selects the directory to list; empty lists the configured
// roots (or $HOME when none are configured). Browsing is confined to the
// configured roots when set.
func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	roots := s.cfg.WorkspaceRoots
	path := strings.TrimSpace(r.URL.Query().Get("path"))

	// No path: list roots directly, or fall back to $HOME.
	if path == "" {
		if len(roots) > 0 {
			entries := make([]workspaceEntry, 0, len(roots))
			for _, root := range roots {
				entries = append(entries, workspaceEntry{Name: root, Path: root})
			}
			writeJSON(w, http.StatusOK, workspaceListing{Roots: roots, Entries: entries})
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		path = home
	}

	clean := filepath.Clean(path)
	if len(roots) > 0 && !s.workspaceAllowed(clean) {
		writeErr(w, http.StatusForbidden, os.ErrPermission)
		return
	}

	dirents, err := os.ReadDir(clean)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	entries := make([]workspaceEntry, 0, len(dirents))
	for _, d := range dirents {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		entries = append(entries, workspaceEntry{Name: d.Name(), Path: filepath.Join(clean, d.Name())})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	parent := filepath.Dir(clean)
	if parent == clean || (len(roots) > 0 && !s.workspaceAllowed(parent)) {
		parent = ""
	}

	writeJSON(w, http.StatusOK, workspaceListing{Path: clean, Parent: parent, Roots: roots, Entries: entries})
}
