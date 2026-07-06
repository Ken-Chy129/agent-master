// Package config loads and persists the daemon's configuration under
// ~/.agent-master/config.json. On first run it generates a fresh auth token.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DefaultPort is the daemon's default listen port.
	DefaultPort = 8888
	// DefaultHost binds all interfaces; keep the daemon on a trusted network
	// (e.g. a Tailscale tailnet) rather than the public internet.
	DefaultHost = "0.0.0.0"

	dirName    = ".agent-master"
	configName = "config.json"
	dbName     = "agent-master.db"
)

// Config is the persisted daemon configuration.
type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// Token authenticates clients. Treat it as a secret.
	Token string `json:"token"`
	// ClaudeBin optionally overrides the claude binary path. Empty = look it
	// up on PATH at run time.
	ClaudeBin string `json:"claude_bin,omitempty"`
	// WorkspaceRoots optionally whitelists the directories a session may run
	// in. Empty = no restriction (v1 default).
	WorkspaceRoots []string `json:"workspace_roots,omitempty"`
	// AllowedOrigins optionally restricts CORS to specific origins. Empty =
	// allow any origin (the token is the real guard).
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

// Dir returns the config directory (~/.agent-master).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName), nil
}

// Path returns the config file path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configName), nil
}

// DBPath returns the SQLite database path.
func DBPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dbName), nil
}

// Default builds a config with a freshly generated token.
func Default() (*Config, error) {
	tok, err := generateToken()
	if err != nil {
		return nil, err
	}
	return &Config{Host: DefaultHost, Port: DefaultPort, Token: tok}, nil
}

// Load reads the config, creating a default one (with a fresh token) on first
// run and healing a missing token on older files.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg, err := Default()
		if err != nil {
			return nil, err
		}
		if err := cfg.Save(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.applyDefaults()

	if cfg.Token == "" {
		if cfg.Token, err = generateToken(); err != nil {
			return nil, err
		}
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

// Save writes the config with 0600 perms (it holds the token).
func (c *Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, configName), data, 0o600)
}

// Addr returns the host:port listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) applyDefaults() {
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
