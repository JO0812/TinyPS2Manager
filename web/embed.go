// Package web serves the built Svelte SPA (web/dist, produced by
// scripts/build-web.sh) over HTTP with index fallback for client-side
// view state. Fresh clones without a built UI embed only the .gitkeep
// placeholder; HasUI reports that so serve can explain instead of 404ing.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var distFS embed.FS

// HasUI reports whether a real built UI (index.html) is embedded.
func HasUI() bool {
	f, err := distFS.Open("dist/index.html")
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// FS returns the built UI as an fs.FS for the Wails asset server (desktop
// build); ok is false when no UI is built.
func FS() (fs.FS, bool) {
	if !HasUI() {
		return nil, false
	}
	sub, _ := fs.Sub(distFS, "dist")
	return sub, true
}

// Handler serves the SPA, falling back to index.html for unknown paths.
// A missing build yields 503 with the fix.
func Handler() http.Handler {
	if !HasUI() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("UI not built: run scripts/build-web.sh, then rebuild.\n"))
		})
	}
	sub, _ := fs.Sub(distFS, "dist")
	ui := http.FS(sub)
	fileServer := http.FileServer(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !strings.Contains(r.URL.Path, "..") {
			if f, err := ui.Open(r.URL.Path); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
