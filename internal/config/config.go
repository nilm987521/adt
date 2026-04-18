// Package config provides configuration loading and management for adt.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nilm987521/adt/internal/keyring"
)

// CurrentVersion is the latest config_version this binary understands.
const CurrentVersion = 3

// Environment holds connection settings for a single named database environment.
type Environment struct {
	Driver              string        `yaml:"driver"`   // "oracle"|"postgres"|"mysql"|"mssql"; default "oracle"
	Database            string        `yaml:"database"` // used by postgres/mysql/mssql; oracle uses Service
	User                string        `yaml:"user"`
	Host                string        `yaml:"host"`
	Port                int           `yaml:"port"`
	Service             string        `yaml:"service"`
	Production          bool          `yaml:"production"`
	RequireConfirmation bool          `yaml:"require_confirmation"`
	MaxRows             int           `yaml:"max_rows"`
	Timeout             time.Duration `yaml:"timeout"`
	MaskColumns         []string      `yaml:"mask_columns"` // columns to redact in output; merged with global defaults
}

// EffectiveMaskColumns returns the union of globalMask and the environment's own MaskColumns,
// with all values uppercased for case-insensitive comparison.
// Duplicates (e.g. the same column in both global and env lists) are deduplicated.
func (e Environment) EffectiveMaskColumns(globalMask []string) []string {
	seen := make(map[string]bool, len(globalMask)+len(e.MaskColumns))
	result := make([]string, 0, len(globalMask)+len(e.MaskColumns))

	for _, col := range globalMask {
		upper := strings.ToUpper(col)
		if !seen[upper] {
			seen[upper] = true
			result = append(result, upper)
		}
	}

	for _, col := range e.MaskColumns {
		upper := strings.ToUpper(col)
		if !seen[upper] {
			seen[upper] = true
			result = append(result, upper)
		}
	}

	return result
}

// EffectiveDriver returns the driver name, defaulting to "oracle" for backwards compatibility.
func (e Environment) EffectiveDriver() string {
	if e.Driver == "" {
		return "oracle"
	}

	return e.Driver
}

// Config is the top-level configuration structure.
type Config struct {
	ConfigVersion int                    `yaml:"config_version"`
	DefaultEnv    string                 `yaml:"default_env"`
	Environments  map[string]Environment `yaml:"environments"`
	Defaults      struct {
		MaxRows     int           `yaml:"max_rows"`
		Timeout     time.Duration `yaml:"timeout"`
		MaskColumns []string      `yaml:"mask_columns"`
	} `yaml:"defaults"`
	Audit struct {
		LogPath string `yaml:"log_path"`
	} `yaml:"audit"`
}

// Load reads a YAML config file from path.
// It warns when config_version is missing (0) and applies sensible defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path from internal config resolution, not user input
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

	if cfg.ConfigVersion == 1 {
		if err := migrateV1ToV2(&cfg, path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: config migration failed: %v\n", err)
		}
	}

	if cfg.ConfigVersion == 2 {
		if err := migrateV2ToV3(&cfg, path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: config migration failed: %v\n", err)
		}
	}

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
		return nil, errors.New("no environment specified and no default_env set in config")
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

	// Compute effective mask columns: union of global and per-environment lists.
	env.MaskColumns = env.EffectiveMaskColumns(c.Defaults.MaskColumns)

	return &env, nil
}

// migrateV1ToV2 upgrades a v1 config in-place:
// - sets Driver: "oracle" on every environment that lacks a driver
// - migrates each environment's keyring entry (oracle-password-* → db-password-*)
// - bumps config_version to 2 and saves
// Prints a one-time notice to stderr.
func migrateV1ToV2(cfg *Config, path string) error {
	for name, env := range cfg.Environments {
		if env.Driver == "" {
			env.Driver = "oracle"
			cfg.Environments[name] = env
		}

		if err := keyring.MigrateOracleKey(name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: keyring migration for env %q: %v\n", name, err)
		}
	}

	cfg.ConfigVersion = 2

	if err := cfg.Save(path); err != nil {
		return fmt.Errorf("save migrated config: %w", err)
	}

	fmt.Fprintln(os.Stderr,
		"notice: config migrated from v1 to v2 (multi-DB support). "+
			"All environments set to driver: oracle. "+
			"Keyring entries updated to db-password-* format.")

	return nil
}

// migrateV2ToV3 upgrades a v2 config in-place:
// - ensures Defaults.MaskColumns is initialised to an empty slice if nil
// - bumps config_version to 3 and saves
// Prints a one-time notice to stderr.
func migrateV2ToV3(cfg *Config, path string) error {
	if cfg.Defaults.MaskColumns == nil {
		cfg.Defaults.MaskColumns = []string{}
	}

	cfg.ConfigVersion = 3

	if err := cfg.Save(path); err != nil {
		return fmt.Errorf("save migrated config: %w", err)
	}

	fmt.Fprintln(os.Stderr,
		"notice: config migrated from v2 to v3 (data masking support added).")

	return nil
}

// Save serialises the config to YAML and writes it to path.
// Parent directories are created as needed. On non-Windows systems the
// file is created with permission 0600 (owner-readable only).
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("serialising config: %w", err)
	}

	perm := os.FileMode(0o600)
	if runtime.GOOS == "windows" {
		perm = 0o666 // Windows ACLs handle access control; 0600 is not meaningful.
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

	sort.Strings(names)

	return names
}
