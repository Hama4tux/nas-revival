// Package web serves the built static frontend with an SPA fallback.
package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves files from dist. Hashed files under assets/ are immutable;
// unknown paths fall back to index.html so client-side routes work.
func Handler(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "index.html" {
			serveIndex(w, r, dist)
			return
		}
		if _, err := fs.Stat(dist, p); err != nil {
			if strings.HasPrefix(p, "assets/") {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, dist)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, dist fs.FS) {
	b, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		http.Error(w, "UI not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
}
