package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the persistent configuration for `testmaker -serve`. It is loaded from
// $TESTMAKER_HOME/config/config.json and created there with defaults on first run.
// Mutable state (the sqlite db, the blob store) and the seed catalogue/prompts
// default to per-user paths under the home, never the working directory, so an
// installed binary run from anywhere is self-contained.
type Config struct {
	TestDB  string     `json:"testdb"`
	Blobs   string     `json:"blobs"`
	Catalog string     `json:"catalog"`
	Prompts string     `json:"prompts"`
	LLM     LLMConfig  `json:"llm"`
	Auth    AuthConfig `json:"auth"`
}

// LLMConfig configures the optional LLM backend used by the ingest-llm endpoint.
// An empty BaseURL means "use the TESTMAKER_LLM_* environment", so an API key can
// stay in the environment instead of on disk.
type LLMConfig struct {
	BaseURL    string `json:"baseURL"`
	APIKey     string `json:"apiKey"`
	Model      string `json:"model"`
	AuthScheme string `json:"authScheme"`
}

// AuthConfig configures delivery-surface access control (ADR-0006). Zero value
// = auth off, which is what tests construct; loadOrCreateConfig defaults Mode
// to "token" for real deployments (Task 4 in PLAN.md).
type AuthConfig struct {
	Mode             string `json:"mode"`
	OperatorToken    string `json:"operatorToken"`
	Secret           string `json:"secret"`
	InviteTTLSeconds int    `json:"inviteTTLSeconds"`
}

// testmakerHome resolves the per-user home directory: $TESTMAKER_HOME if set, else
// ~/.testmaker.
func testmakerHome() (string, error) {
	if h := os.Getenv("TESTMAKER_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".testmaker"), nil
}

// defaultConfig returns the built-in defaults, all rooted under home/data.
func defaultConfig(home string) Config {
	data := filepath.Join(home, "data")
	return Config{
		TestDB:  filepath.Join(data, "testmaker.db"),
		Blobs:   filepath.Join(data, "blobs"),
		Catalog: filepath.Join(data, "catalog", "sources.json"),
		Prompts: filepath.Join(data, "prompts"),
	}
}

// loadOrCreateConfig reads $home/config/config.json, or writes it with defaults
// when it does not exist (returning those). A malformed file is an error — it is
// never silently overwritten. The returned path is the file it read or created.
func loadOrCreateConfig(home string) (Config, string, error) {
	path := filepath.Join(home, "config", "config.json")
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		var cfg Config
		if uerr := json.Unmarshal(b, &cfg); uerr != nil {
			return Config{}, path, fmt.Errorf("parse config %s: %w", path, uerr)
		}
		return cfg, path, nil
	case errors.Is(err, os.ErrNotExist):
		cfg := defaultConfig(home)
		if werr := writeConfig(path, cfg); werr != nil {
			return Config{}, path, werr
		}
		return cfg, path, nil
	default:
		return Config{}, path, fmt.Errorf("read config %s: %w", path, err)
	}
}

// writeConfig writes cfg as indented JSON, creating the config directory. The file
// is 0600 because it may hold an LLM API key.
func writeConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// serveWithConfig loads (or creates) the config, lets explicit CLI flags override
// it via ov, and runs the delivery server on addr. It is the composition root for
// the server's persistent configuration.
func serveWithConfig(addr string, ov func(*Config)) error {
	home, err := testmakerHome()
	if err != nil {
		return err
	}
	cfg, path, err := loadOrCreateConfig(home)
	if err != nil {
		return err
	}
	ov(&cfg)
	fmt.Fprintf(os.Stderr, "testmaker: config %s\n", path)
	return runServer(addr, cfg)
}
