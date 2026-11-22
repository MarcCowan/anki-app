// Package webui serves the built single-page app embedded in the binary.
package webui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist is written by `npm run build` (vite outDir); only .gitkeep is in git.
//
//go:embed all:dist
var dist embed.FS

// Handler serves the embedded app with SPA fallback to index.html.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed pattern guarantees the directory exists
	}
	return handlerFS(sub)
}

func handlerFS(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(fsys, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "frontend not built: run scripts/build.sh\n")
			return
		}
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if strings.HasPrefix(p, "api/") || p == "api" {
			http.NotFound(w, r)
			return
		}
		if serveFile(w, r, fsys, p) {
			return
		}
		if path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
		serveFile(w, r, fsys, "index.html")
	})
}

// serveFile serves a regular file; it reports false if there is none at p.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, p string) bool {
	f, err := fsys.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return true
	}
	if strings.HasPrefix(p, "assets/") {
		// Vite puts a content hash in every asset file name.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	// Go's MIME table has no entry for .webmanifest on every OS (Windows lacks it).
	if path.Ext(p) == ".webmanifest" {
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	// Embedded files have no modification time; ETag-less revalidation is fine for one user.
	http.ServeContent(w, r, path.Base(p), time.Time{}, rs)
	return true
}
