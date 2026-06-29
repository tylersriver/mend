package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// staticFS bundles the PWA shell assets into the binary (icons, manifest, the
// service worker, and the offline page) so deployment stays single-file.
//
//go:embed static
var staticFS embed.FS

// staticAssets registers the PWA routes on the mux. The service worker and
// manifest are served from the site root (not under /static/) so the worker's
// scope covers the whole app.
func (s *Server) staticAssets(mux *http.ServeMux) {
	sub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(sub))

	mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))

	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		// Allow a root-scoped worker even though the file path is /sw.js.
		w.Header().Set("Service-Worker-Allowed", "/")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, sub, "sw.js")
	})

	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		http.ServeFileFS(w, r, sub, "manifest.webmanifest")
	})

	mux.HandleFunc("GET /offline", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeFileFS(w, r, sub, "offline.html")
	})

	// Common root-path icon requests browsers make without a <link>.
	mux.HandleFunc("GET /apple-touch-icon.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, sub, "apple-touch-icon.png")
	})
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, sub, "icon-192.png")
	})
}
