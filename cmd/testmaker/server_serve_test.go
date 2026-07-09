package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// quietHandler builds the delivery handler the way runServer does but with a
// discard logger, so a test can exercise the real -serve wiring (auth, limits,
// middleware) without binding a port or spraying request logs.
func quietHandler(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	handler, closeFn, err := buildDeliveryHandler(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("buildDeliveryHandler: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

// TestServeEnforcesAuthFromTokenConfig proves the production -serve wiring
// enforces auth when the config is in token mode — the path was previously built
// without authCfg, so it silently ran auth-off. It also proves auth.mode "none"
// (what `-auth none` sets) reaches the surface and leaves it open.
func TestServeEnforcesAuthFromTokenConfig(t *testing.T) {
	base := func() Config {
		return Config{
			TestDB:  "memory",
			Blobs:   "memory",
			Catalog: filepath.Join(t.TempDir(), "absent.json"), // absent ⇒ empty catalogue
		}
	}

	// token mode: an operator endpoint is refused without a token, accepted with it.
	tokCfg := base()
	tokCfg.Auth = AuthConfig{Mode: "token", OperatorToken: "op-secret-token", Secret: "hmac-secret"}
	ts := quietHandler(t, tokCfg)

	resp := get(t, ts, "/api/sources")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /api/sources without token = %d, want 401 (token-mode config must enforce auth)", resp.StatusCode)
	}

	resp = authGet(t, ts.URL+"/api/sources", "op-secret-token")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sources with operator token = %d, want 200", resp.StatusCode)
	}

	// none mode (the -auth none escape hatch) reaches the surface: no token needed.
	noneCfg := base()
	noneCfg.Auth = AuthConfig{Mode: "none"}
	tsOpen := quietHandler(t, noneCfg)
	resp = get(t, tsOpen, "/api/sources")
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("none-mode GET /api/sources = %d, want 200 (auth off)", resp.StatusCode)
	}
}
