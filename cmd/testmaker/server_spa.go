package main

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/mariotoffia/testmaker/cmd/testmaker/webui"
)

// spaCSP locks the app down to same-origin resources; data: images cover the
// generator's inline SVG previews. The media endpoint keeps its own, stricter
// sandbox CSP (ADR-0003) — this policy is only for SPA documents.
const spaCSP = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'"

// handleSPA serves the embedded web app: the exact file when it exists
// (hashed /assets/* immutable), index.html for anything else so client-side
// routes deep-link (ADR-0005 / DESIGN §7.1). Without a UI build it degrades
// to the JSON index at "/" and JSON 404s elsewhere, keeping the Go toolchain
// independent of Bun.
func (s *server) handleSPA(w http.ResponseWriter, r *http.Request) {
	ui, ok := webui.FS()
	if !ok {
		if r.URL.Path == "/" {
			s.handleIndex(w, r)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "not found (web app not built; see `make webui`)",
			"code":  "server.not_found",
		})
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if _, err := fs.Stat(ui, name); err != nil {
		// Client-side route (e.g. /take, /items/123): serve the app shell.
		name = "index.html"
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", spaCSP)
	} else if strings.HasPrefix(name, "assets/") {
		// Vite emits content-hashed filenames under assets/ — safe to cache forever.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, ui, name)
}
