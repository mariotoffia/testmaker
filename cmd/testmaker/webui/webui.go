package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built UI rooted at dist and whether a real build is present
// (an index.html exists). Callers must treat ok=false as "no UI shipped" and
// fall back; the returned FS is still valid (it holds the placeholder).
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Unreachable: "dist" is a compiled-in directory. Return the root so
		// the contract (never-nil FS) holds even here.
		return dist, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return sub, false
	}
	return sub, true
}
