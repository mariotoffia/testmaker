package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadOrCreateConfigCreatesDefaults proves a first run writes a config file
// with defaults rooted under the home, and that secrets default to empty (env).
func TestLoadOrCreateConfigCreatesDefaults(t *testing.T) {
	home := t.TempDir()
	cfg, path, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("loadOrCreateConfig: %v", err)
	}
	if path != filepath.Join(home, "config", "config.json") {
		t.Fatalf("config path = %s", path)
	}
	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("config file not created at %s: %v", path, serr)
	}
	if want := filepath.Join(home, "data", "testmaker.db"); cfg.TestDB != want {
		t.Errorf("TestDB = %q, want %q", cfg.TestDB, want)
	}
	if want := filepath.Join(home, "data", "blobs"); cfg.Blobs != want {
		t.Errorf("Blobs = %q, want %q", cfg.Blobs, want)
	}
	if want := filepath.Join(home, "data", "catalog", "sources.json"); cfg.Catalog != want {
		t.Errorf("Catalog = %q, want %q", cfg.Catalog, want)
	}
	if cfg.LLM.BaseURL != "" {
		t.Errorf("LLM.BaseURL = %q, want empty (environment fallback)", cfg.LLM.BaseURL)
	}
}

// TestLoadOrCreateConfigReadsExisting proves an existing config file is read, not
// overwritten with defaults.
func TestLoadOrCreateConfigReadsExisting(t *testing.T) {
	home := t.TempDir()
	custom := Config{TestDB: "/custom/x.db", Blobs: "/custom/blobs", Catalog: "/custom/c.json", Prompts: "/custom/p"}
	if err := writeConfig(filepath.Join(home, "config", "config.json"), custom); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	cfg, _, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("loadOrCreateConfig: %v", err)
	}
	if cfg.TestDB != "/custom/x.db" {
		t.Errorf("TestDB = %q, want the file's value", cfg.TestDB)
	}
}

// TestLoadOrCreateConfigRejectsMalformed proves a corrupt config is an error, not
// silently overwritten.
func TestLoadOrCreateConfigRejectsMalformed(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadOrCreateConfig(home); err == nil {
		t.Fatal("malformed config accepted; want an error")
	}
}

// TestOpenTestDBCreatesParentDir proves a file-backed sqlite db opens even when
// its parent directory does not exist yet — the config-driven server must be
// self-sufficient (running the binary directly, not only via `make serve`).
func TestOpenTestDBCreatesParentDir(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "nested", "sub", "testmaker.db")
	db, err := openTestDB(dsn)
	if err != nil {
		t.Fatalf("openTestDB(%q): %v", dsn, err)
	}
	defer func() { _ = db.close() }()
	if _, err := os.Stat(dsn); err != nil {
		t.Fatalf("db file not created at %s: %v", dsn, err)
	}
}

// TestTestmakerHomeUsesEnv proves TESTMAKER_HOME overrides the default home.
func TestTestmakerHomeUsesEnv(t *testing.T) {
	t.Setenv("TESTMAKER_HOME", "/opt/tm")
	home, err := testmakerHome()
	if err != nil {
		t.Fatalf("testmakerHome: %v", err)
	}
	if home != "/opt/tm" {
		t.Errorf("home = %q, want /opt/tm", home)
	}
}
