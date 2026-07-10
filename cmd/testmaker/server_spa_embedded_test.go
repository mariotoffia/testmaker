package main

import (
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/cmd/testmaker/webui"
)

// TestServesEmbeddedSPAWhenBuilt asserts the SPA-serving contract, but only when
// a build is embedded (make webui). Without one it skips — the Go toolchain must
// stay green with no Bun, so this can never be a hard failure on CI's Go job.
func TestServesEmbeddedSPAWhenBuilt(t *testing.T) {
	ui, ok := webui.FS()
	if !ok {
		t.Skip("no embedded UI build (run `make webui`); Go tests stay green without Bun")
	}
	ts := newSPATestServer(t)

	// "/" serves the app shell (index.html), as text/html.
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET / content-type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `id="root"`) {
		t.Fatal("index.html did not contain the React root div")
	}

	// A client-side route (no such file) also serves the shell, not a 404.
	deep, err := http.Get(ts.URL + "/take")
	if err != nil {
		t.Fatalf("GET /take: %v", err)
	}
	defer func() { _ = deep.Body.Close() }()
	if deep.StatusCode != http.StatusOK {
		t.Fatalf("GET /take = %d, want 200 (SPA shell)", deep.StatusCode)
	}

	// Content-hashed assets under assets/ are served immutable so browsers cache
	// them forever (server_spa.go). This is part of the SPA-serving contract, so
	// verify it against a real hashed file the build emitted.
	asset := firstEmbeddedAsset(t, ui)
	ar, err := http.Get(ts.URL + "/assets/" + asset)
	if err != nil {
		t.Fatalf("GET /assets/%s: %v", asset, err)
	}
	defer func() { _ = ar.Body.Close() }()
	if cc := ar.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("GET /assets/%s Cache-Control = %q, want an immutable policy", asset, cc)
	}
}

// firstEmbeddedAsset returns the name of a file the build emitted under assets/
// (Vite writes content-hashed filenames there, so the test cannot hard-code one).
// It fails the test if the embedded build has no asset files.
func firstEmbeddedAsset(t *testing.T, ui fs.FS) string {
	t.Helper()
	entries, err := fs.ReadDir(ui, "assets")
	if err != nil {
		t.Fatalf("read embedded assets/: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			return e.Name()
		}
	}
	t.Fatal("embedded build has no files under assets/")
	return ""
}
