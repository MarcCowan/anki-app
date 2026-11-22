package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestSPAFallback(t *testing.T) {
	h := handlerFS(fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>app</title>")},
		"assets/a.js":          {Data: []byte("console.log(1)")},
		"manifest.webmanifest": {Data: []byte("{}")},
	})
	for _, p := range []string{"/", "/decks/3", "/decks/3/study", "/index.html"} {
		w := get(h, p)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<title>app</title>") {
			t.Fatalf("%s: %d %q", p, w.Code, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("%s: Cache-Control = %q", p, cc)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: Content-Type = %q", p, ct)
		}
	}
	w := get(h, "/assets/a.js")
	if w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("asset: %d %q", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control = %q", cc)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset Content-Type = %q", ct)
	}
	if w := get(h, "/manifest.webmanifest"); w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("manifest: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	if ct := get(h, "/manifest.webmanifest").Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Fatalf("manifest Content-Type = %q", ct)
	}
	for _, p := range []string{"/missing.png", "/assets/missing.js", "/api/decks"} {
		if w := get(h, p); w.Code != 404 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
}

func TestNotBuilt(t *testing.T) {
	h := handlerFS(fstest.MapFS{".gitkeep": {}})
	w := get(h, "/")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "frontend not built: run scripts/build.sh") {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
}

func TestHandlerEmbeds(t *testing.T) {
	// The real embedded FS must at least construct; content depends on whether the web app was built.
	if Handler() == nil {
		t.Fatal("nil handler")
	}
}
