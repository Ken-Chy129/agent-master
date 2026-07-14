package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html":      {Data: []byte(`<!doctype html><div id="root">agent-master web</div>`)},
		"assets/app.js":   {Data: []byte(`console.log("agent-master")`)},
		"assets/app.css":  {Data: []byte(`body { color: #111; }`)},
		"assets/icon.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
	}
}

func TestHandlerServesIndexAndStaticAssets(t *testing.T) {
	h := NewHandler(testAssets())

	index := httptest.NewRecorder()
	h.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK || index.Body.String() == "" {
		t.Fatalf("GET / = %d %q", index.Code, index.Body.String())
	}
	if got := index.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index Cache-Control = %q, want no-cache", got)
	}

	asset := httptest.NewRecorder()
	h.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != `console.log("agent-master")` {
		t.Fatalf("GET asset = %d %q", asset.Code, asset.Body.String())
	}
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control = %q", got)
	}
}

func TestHandlerFallsBackToIndexForClientRoutes(t *testing.T) {
	h := NewHandler(testAssets())
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/machines/local/sessions/123", nil))

	if rec.Code != http.StatusOK || rec.Body.String() == "" {
		t.Fatalf("SPA fallback = %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandlerDoesNotMaskAPIRoutesOrUnsupportedMethods(t *testing.T) {
	h := NewHandler(testAssets())

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/unknown"},
		{method: http.MethodGet, path: "/health/unknown"},
		{method: http.MethodPost, path: "/somewhere"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}
