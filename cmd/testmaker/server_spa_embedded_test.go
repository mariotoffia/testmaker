package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mariotoffia/testmaker/cmd/testmaker/webui"
)

// TestServesEmbeddedSPAWhenBuilt asserts the SPA-serving contract, but only when
// a build is embedded (make webui). Without one it skips — the Go toolchain must
// stay green with no Bun, so this can never be a hard failure on CI's Go job.
func TestServesEmbeddedSPAWhenBuilt(t *testing.T) {
	if _, ok := webui.FS(); !ok {
		t.Skip("no embedded UI build (run `make webui`); Go tests stay green without Bun")
	}
	ts := newSPATestServer(t)

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
}
