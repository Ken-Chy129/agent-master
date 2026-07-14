// Package webui serves the production browser client embedded in the daemon.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// embeddedAssets is populated by `make web-assets` before release builds.
// The tracked .keep file lets ordinary Go tests compile before assets exist.
//
//go:embed all:dist
var embeddedAssets embed.FS

// Embedded returns the built Web client rooted at its public directory.
func Embedded() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "dist")
	if err != nil {
		panic(err)
	}
	return assets
}

// NewHandler returns an SPA-aware static handler for a Web client filesystem.
// API paths and non-read methods deliberately remain 404s so the UI cannot
// conceal a misspelled endpoint behind index.html.
func NewHandler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		cleanPath := path.Clean("/" + r.URL.Path)
		if hasSegmentPrefix(cleanPath, "/api") || hasSegmentPrefix(cleanPath, "/health") {
			http.NotFound(w, r)
			return
		}

		name := strings.TrimPrefix(cleanPath, "/")
		if name == "" || name == "." {
			serveAsset(w, r, assets, "index.html", true)
			return
		}

		if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() {
			serveAsset(w, r, assets, name, false)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}

		serveAsset(w, r, assets, "index.html", true)
	})
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: http: https:; connect-src 'self' http: https:")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}

func serveAsset(w http.ResponseWriter, r *http.Request, assets fs.FS, name string, noCache bool) {
	data, err := fs.ReadFile(assets, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if noCache {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func hasSegmentPrefix(value, prefix string) bool {
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}
