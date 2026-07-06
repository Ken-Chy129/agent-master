package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// auth wraps a handler, rejecting requests without a valid token.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.tokenOK(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenOK reports whether the request carries the configured token. The token
// may arrive as an Authorization: Bearer header or a ?token= query param.
// Comparison is constant-time.
func (s *Server) tokenOK(r *http.Request) bool {
	if s.cfg.Token == "" {
		return false
	}
	tok := bearerToken(r)
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if tok == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.Token)) == 1
}

func bearerToken(r *http.Request) string {
	if after, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return after
	}
	return ""
}
