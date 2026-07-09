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

// TestLoadOrCreateConfigGeneratesSecretsInTokenMode proves a first run in token
// mode mints an operator token + HMAC secret and persists them (0600), and that
// numeric limit/LLM defaults are filled.
func TestLoadOrCreateConfigGeneratesSecretsInTokenMode(t *testing.T) {
	home := t.TempDir()
	cfg, path, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("loadOrCreateConfig: %v", err)
	}
	if cfg.Auth.Mode != "token" {
		t.Fatalf("Auth.Mode = %q, want token (default)", cfg.Auth.Mode)
	}
	if cfg.Auth.OperatorToken == "" || cfg.Auth.Secret == "" {
		t.Fatal("token mode must generate operatorToken and secret")
	}
	if cfg.Auth.OperatorToken == cfg.Auth.Secret {
		t.Fatal("operatorToken and secret must be independently generated")
	}
	if cfg.Auth.InviteTTLSeconds != 86400 {
		t.Errorf("InviteTTLSeconds = %d, want 86400", cfg.Auth.InviteTTLSeconds)
	}
	if cfg.Limits.RequestsPerSecond != 10 || cfg.Limits.Burst != 20 ||
		cfg.Limits.MaxConcurrentIngests != 1 || cfg.Limits.IngestTimeoutSeconds != 600 {
		t.Errorf("limits defaults wrong: %+v", cfg.Limits)
	}
	if cfg.Log.Level != "info" || cfg.LLM.MaxTokensCap != 4096 {
		t.Errorf("log/llm defaults wrong: log=%+v llm.cap=%d", cfg.Log, cfg.LLM.MaxTokensCap)
	}
	// Reload must be stable — same secrets, no rewrite churn.
	cfg2, _, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg2.Auth.OperatorToken != cfg.Auth.OperatorToken || cfg2.Auth.Secret != cfg.Auth.Secret {
		t.Fatal("secrets changed on reload; they must be stable once written")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config perm = %o, want 600 (holds secrets)", perm)
	}
}

// TestApplyConfigDefaultsBackfillsOldFile proves a pre-Block-14 config (no auth/
// limits sections) still loads: defaults fill in and secrets are generated.
func TestApplyConfigDefaultsBackfillsOldFile(t *testing.T) {
	home := t.TempDir()
	old := `{"testdb":"/x.db","blobs":"/b","catalog":"/c.json","prompts":"/p"}`
	path := filepath.Join(home, "config", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatalf("loadOrCreateConfig on old file: %v", err)
	}
	if cfg.TestDB != "/x.db" {
		t.Errorf("existing value lost: TestDB = %q", cfg.TestDB)
	}
	if cfg.Auth.Mode != "token" || cfg.Auth.OperatorToken == "" {
		t.Error("old file must gain token-mode defaults + generated secrets")
	}
}

// TestNoneModeGeneratesNoSecrets proves auth.mode:none neither needs nor mints
// secrets (the trusted-localhost / test posture).
func TestNoneModeGeneratesNoSecrets(t *testing.T) {
	home := t.TempDir()
	seed := Config{Auth: AuthConfig{Mode: "none"}}
	if err := writeConfig(filepath.Join(home, "config", "config.json"), seed); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadOrCreateConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.OperatorToken != "" || cfg.Auth.Secret != "" {
		t.Fatal("none mode must not generate secrets")
	}
}
