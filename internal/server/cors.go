package server

import "net/http"

// corsMiddleware adds CORS headers and answers preflight so browser clients
// (web/desktop) can call the daemon cross-origin. The bearer token — not the
// origin — is the real access guard, so the default is to allow any origin.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	allowed := s.cfg.AllowedOrigins
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if allowOrigin(origin, allowed) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Last-Event-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowOrigin(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return true // allow any (token-guarded)
	}
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}
