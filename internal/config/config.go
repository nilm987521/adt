package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"gopkg.in/yaml.v3"
)

// CurrentVersion is the latest config_version this binary understands.
const CurrentVersion = 1

// Environment holds connection settings for a single named Oracle environment.
type Environment struct {
	User                string        `yaml:"user"`
	Host                string        `yaml:"host"`
	Port                int           `yaml:"port"`
	Service             string        `yaml:"service"`
	Production          bool          `yaml:"production"`
	RequireConfirmation bool          `yaml:"require_confirmation"`
	MaxRows             int           `yaml:"max_rows"`
	Timeout             time.Duration `yaml:"timeout"`
}

// Config is the top-level configuration structure.
type Config struct {
	ConfigVersion int                    `yaml:"config_version"`
	DefaultEnv    string                 `yaml:"default_env"`
	Environments  map[string]Environment `yaml:"environments"`
	Defaults      struct {
		MaxRows int           `yaml:"max_rows"`
		Timeout time.Duration `yaml:"timeout"`
	} `yaml:"defaults"`
	Audit struct {
		LogPath string `yaml:"log_path"`
	} `yaml:"audit"`
}

// Load reads a YAML config file from path.
// It warns when config_version is missing (0) and applies sensible defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	if cfg.ConfigVersion == 0 {
		fmt.Fprintf(os.Stderr,
			"warning: config file %q is missing config_version field (expected %d). "+
				"Run `adt setup --migrate` to update your config.\n",
			path, CurrentVersion)
	}

	applyDefaults(&cfg)

	return &cfg, nil
}

// applyDefaults fills in zero-value fields with built-in defaults.
func applyDefaults(cfg *Config) {
	if cfg.Defaults.MaxRows == 0 {
		cfg.Defaults.MaxRows = 1000
	}
	if cfg.Defaults.Timeout == 0 {
		cfg.Defaults.Timeout = 30 * time.Second
	}
	if cfg.Audit.LogPath == "" {
		cfg.Audit.LogPath = DefaultAuditLogPath()
	}
}

// GetEnv returns the named environment or an error when it is not defined.
func (c *Config) GetEnv(name string) (*Environment, error) {
	if name == "" {
		name = c.DefaultEnv
	}
	if name == "" {
		return nil, fmt.Errorf("no environment specified and no default_env set in config")
	}
	env, ok := c.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment %q not found in config (available: %v)", name, envNames(c))
	}
	// Inherit global defaults for fields that are not set per-environment.
	if env.MaxRows == 0 {
		env.MaxRows = c.Defaults.MaxRows
	}
	if env.Timeout == 0 {
		env.Timeout = c.Defaults.Timeout
	}
	return &env, nil
}

// Save serialises the config to YAML and writes it to path.
// Parent directories are created as needed. On non-Windows systems the
// file is created with permission 0600 (owner-readable only).
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("serialising config: %w", err)
	}

	perm := os.FileMode(0600)
	if runtime.GOOS == "windows" {
		perm = 0666 // Windows ACLs handle access control; 0600 is not meaningful.
	}

	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("writing config file %q: %w", path, err)
	}

	return nil
}

// DefaultConfigPath returns the platform-appropriate default config file path.
//
//	Linux / macOS: ~/.config/adt/config.yaml
//	Windows:       %APPDATA%\adt\config.yaml
func DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "adt", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".config", "adt", "config.yaml")
}

// DefaultAuditLogPath returns the platform-appropriate default audit log path.
//
//	Linux / macOS: ~/.local/share/adt/audit.log
//	Windows:       %LOCALAPPDATA%\adt\audit.log
func DefaultAuditLogPath() string {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(localAppData, "adt", "audit.log")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "~"
	}
	return filepath.Join(home, ".local", "share", "adt", "audit.log")
}

// envNames returns a sorted slice of environment names for error messages.
func envNames(c *Config) []string {
	names := make([]string, 0, len(c.Environments))
	for k := range c.Environments {
		names = append(names, k)
	}
	return names
}
