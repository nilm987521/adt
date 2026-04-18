package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const driverOracle = "oracle"

const validConfigContent = `
config_version: 3
default_env: test-env
environments:
  test-env:
    driver: oracle
    user: testuser
    host: localhost
    port: 1521
    service: TESTDB
    production: false
  prod-env:
    driver: oracle
    user: produser
    host: prodhost
    port: 1521
    service: PRODDB
    production: true
    max_rows: 100
    timeout: 10s
defaults:
  max_rows: 500
  timeout: 20s
audit:
  log_path: /tmp/test-audit.log
`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	tmp := t.TempDir()

	cfgPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return cfgPath
}

func TestLoadValidConfig(t *testing.T) {
	t.Parallel()

	cfgPath := writeTempConfig(t, validConfigContent)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DefaultEnv != "test-env" {
		t.Errorf("DefaultEnv = %q, want %q", cfg.DefaultEnv, "test-env")
	}

	if cfg.Defaults.MaxRows != 500 {
		t.Errorf("Defaults.MaxRows = %d, want 500", cfg.Defaults.MaxRows)
	}

	if cfg.Defaults.Timeout != 20*time.Second {
		t.Errorf("Defaults.Timeout = %v, want 20s", cfg.Defaults.Timeout)
	}

	if cfg.Audit.LogPath != "/tmp/test-audit.log" {
		t.Errorf("Audit.LogPath = %q, want /tmp/test-audit.log", cfg.Audit.LogPath)
	}

	if cfg.ConfigVersion != 3 {
		t.Errorf("ConfigVersion = %d, want 3", cfg.ConfigVersion)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadMissingConfigVersion(t *testing.T) {
	t.Parallel()

	// config_version missing → treated as v1 (0), no error returned
	content := `
default_env: test-env
environments:
  test-env:
    user: testuser
    host: localhost
    port: 1521
    service: TESTDB
defaults:
  max_rows: 200
  timeout: 15s
`
	cfgPath := writeTempConfig(t, content)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() with missing config_version error = %v", err)
	}

	if cfg.DefaultEnv != "test-env" {
		t.Errorf("DefaultEnv = %q, want %q", cfg.DefaultEnv, "test-env")
	}
	// ConfigVersion should be 0 when not specified
	if cfg.ConfigVersion != 0 {
		t.Errorf("ConfigVersion = %d, want 0", cfg.ConfigVersion)
	}
}

func TestLoadAppliesDefaultMaxRows(t *testing.T) {
	t.Parallel()

	// When max_rows not set in defaults, applyDefaults fills in 1000
	content := `
config_version: 3
default_env: test-env
environments:
  test-env:
    driver: oracle
    user: testuser
    host: localhost
    port: 1521
    service: TESTDB
`
	cfgPath := writeTempConfig(t, content)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Defaults.MaxRows != 1000 {
		t.Errorf("Defaults.MaxRows = %d, want 1000 (built-in default)", cfg.Defaults.MaxRows)
	}

	if cfg.Defaults.Timeout != 30*time.Second {
		t.Errorf("Defaults.Timeout = %v, want 30s (built-in default)", cfg.Defaults.Timeout)
	}
}

func TestGetEnvFound(t *testing.T) {
	t.Parallel()

	cfgPath := writeTempConfig(t, validConfigContent)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	env, err := cfg.GetEnv("test-env")
	if err != nil {
		t.Fatalf("GetEnv() error = %v", err)
	}

	if env.User != "testuser" {
		t.Errorf("User = %q, want testuser", env.User)
	}

	if env.Host != "localhost" {
		t.Errorf("Host = %q, want localhost", env.Host)
	}

	if env.Port != 1521 {
		t.Errorf("Port = %d, want 1521", env.Port)
	}

	if env.Service != "TESTDB" {
		t.Errorf("Service = %q, want TESTDB", env.Service)
	}
}

func TestGetEnvNotFound(t *testing.T) {
	t.Parallel()

	cfgPath := writeTempConfig(t, validConfigContent)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cfg.GetEnv("nonexistent-env")
	if err == nil {
		t.Fatal("expected error for nonexistent env, got nil")
	}
}

func TestGetEnvDefaultEnv(t *testing.T) {
	t.Parallel()

	// Empty name falls back to DefaultEnv
	cfgPath := writeTempConfig(t, validConfigContent)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	env, err := cfg.GetEnv("")
	if err != nil {
		t.Fatalf("GetEnv(\"\") error = %v", err)
	}

	if env.User != "testuser" {
		t.Errorf("User = %q, want testuser (default env)", env.User)
	}
}

func TestGetEnvNoDefaultSet(t *testing.T) {
	t.Parallel()

	content := `
config_version: 3
environments:
  test-env:
    driver: oracle
    user: testuser
    host: localhost
    port: 1521
    service: TESTDB
`
	cfgPath := writeTempConfig(t, content)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cfg.GetEnv("")
	if err == nil {
		t.Fatal("expected error when no env specified and no default_env, got nil")
	}
}

func TestGetEnvInheritsDefaultMaxRows(t *testing.T) {
	t.Parallel()

	// When env.MaxRows == 0, should inherit from cfg.Defaults.MaxRows
	cfgPath := writeTempConfig(t, validConfigContent)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	env, err := cfg.GetEnv("test-env")
	if err != nil {
		t.Fatal(err)
	}
	// test-env has no max_rows set, should inherit 500 from defaults
	if env.MaxRows != 500 {
		t.Errorf("env.MaxRows = %d, want 500 (inherited from defaults)", env.MaxRows)
	}

	if env.Timeout != 20*time.Second {
		t.Errorf("env.Timeout = %v, want 20s (inherited from defaults)", env.Timeout)
	}
}

func TestGetEnvDoesNotInheritWhenSet(t *testing.T) {
	t.Parallel()

	// prod-env has its own max_rows and timeout; should NOT be overridden
	cfgPath := writeTempConfig(t, validConfigContent)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	env, err := cfg.GetEnv("prod-env")
	if err != nil {
		t.Fatal(err)
	}

	if env.MaxRows != 100 {
		t.Errorf("env.MaxRows = %d, want 100 (env-specific)", env.MaxRows)
	}

	if env.Timeout != 10*time.Second {
		t.Errorf("env.Timeout = %v, want 10s (env-specific)", env.Timeout)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()

	p := DefaultConfigPath()
	if p == "" {
		t.Fatal("DefaultConfigPath() returned empty string")
	}

	if !strings.HasSuffix(p, "config.yaml") {
		t.Errorf("DefaultConfigPath() = %q, expected to end with config.yaml", p)
	}
}

func TestDefaultAuditLogPath(t *testing.T) {
	t.Parallel()

	p := DefaultAuditLogPath()
	if p == "" {
		t.Fatal("DefaultAuditLogPath() returned empty string")
	}

	if !strings.HasSuffix(p, "audit.log") {
		t.Errorf("DefaultAuditLogPath() = %q, expected to end with audit.log", p)
	}
}

func TestSaveAndReload(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "subdir", "config.yaml")

	original := &Config{
		ConfigVersion: 3,
		DefaultEnv:    "roundtrip-env",
		Environments: map[string]Environment{
			"roundtrip-env": {
				User:       "rtuser",
				Host:       "rthost",
				Port:       5432,
				Service:    "RTDB",
				Production: false,
			},
		},
	}
	original.Defaults.MaxRows = 250
	original.Defaults.Timeout = 45 * time.Second
	original.Audit.LogPath = "/tmp/rt-audit.log"

	if err := original.Save(cfgPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file permissions (non-Windows)
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Logf("file permissions = %o (may differ on Windows)", info.Mode().Perm())
	}

	reloaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}

	if reloaded.DefaultEnv != original.DefaultEnv {
		t.Errorf("DefaultEnv = %q, want %q", reloaded.DefaultEnv, original.DefaultEnv)
	}

	if reloaded.Defaults.MaxRows != original.Defaults.MaxRows {
		t.Errorf("Defaults.MaxRows = %d, want %d", reloaded.Defaults.MaxRows, original.Defaults.MaxRows)
	}

	if reloaded.Defaults.Timeout != original.Defaults.Timeout {
		t.Errorf("Defaults.Timeout = %v, want %v", reloaded.Defaults.Timeout, original.Defaults.Timeout)
	}

	if reloaded.Audit.LogPath != original.Audit.LogPath {
		t.Errorf("Audit.LogPath = %q, want %q", reloaded.Audit.LogPath, original.Audit.LogPath)
	}

	env, err := reloaded.GetEnv("roundtrip-env")
	if err != nil {
		t.Fatalf("GetEnv() after reload error = %v", err)
	}

	if env.User != "rtuser" {
		t.Errorf("env.User = %q, want rtuser", env.User)
	}

	if env.Port != 5432 {
		t.Errorf("env.Port = %d, want 5432", env.Port)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "a", "b", "c", "config.yaml")
	cfg := &Config{ConfigVersion: 3, DefaultEnv: "x"}
	cfg.Defaults.MaxRows = 100

	cfg.Defaults.Timeout = 10 * time.Second
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	t.Parallel()

	// Use a tab character in a mapping where spaces are required — yaml.v3 returns an error
	cfgPath := writeTempConfig(t, "key:\n\t- bad indentation with tab")

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestEffectiveDriverEmpty(t *testing.T) {
	t.Parallel()

	env := Environment{}
	if got := env.EffectiveDriver(); got != driverOracle {
		t.Errorf("EffectiveDriver() = %q, want %q", got, driverOracle)
	}
}

func TestEffectiveDriverSet(t *testing.T) {
	t.Parallel()

	cases := []struct {
		driver string
	}{
		{"postgres"},
		{"mysql"},
		{"mssql"},
		{driverOracle},
	}
	for _, tc := range cases {
		env := Environment{Driver: tc.driver}
		if got := env.EffectiveDriver(); got != tc.driver {
			t.Errorf("EffectiveDriver() = %q, want %q", got, tc.driver)
		}
	}
}

func TestMigrateV1ToV2(t *testing.T) {
	t.Parallel()

	// A v1 config without driver fields should be migrated:
	// - Driver set to "oracle" on all environments
	// - config_version bumped to 2
	// - file saved
	v1Content := `
config_version: 1
default_env: dev
environments:
  dev:
    user: devuser
    host: localhost
    port: 1521
    service: DEVDB
  staging:
    user: staginguser
    host: staginghost
    port: 1521
    service: STAGINGDB
defaults:
  max_rows: 500
  timeout: 20s
audit:
  log_path: /tmp/migrate-test-audit.log
`
	cfgPath := writeTempConfig(t, v1Content)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// After load, both v1→v2 and v2→v3 migrations should have run: version bumped to 3
	if cfg.ConfigVersion != 3 {
		t.Errorf("ConfigVersion = %d, want 3 after v1→v2 and v2→v3 migrations", cfg.ConfigVersion)
	}

	// All environments should have Driver set to "oracle"
	for name, env := range cfg.Environments {
		if env.Driver != driverOracle {
			t.Errorf("env %q: Driver = %q, want %q", name, env.Driver, driverOracle)
		}
	}

	// The file on disk should also reflect the migration
	reloaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() after migration error = %v", err)
	}

	if reloaded.ConfigVersion != 3 {
		t.Errorf("reloaded ConfigVersion = %d, want 3", reloaded.ConfigVersion)
	}
}

func TestLoadV2WithPostgresDriver(t *testing.T) {
	t.Parallel()

	v2Content := `
config_version: 2
default_env: pg-env
environments:
  pg-env:
    driver: postgres
    database: myapp
    user: pguser
    host: pghost
    port: 5432
defaults:
  max_rows: 200
  timeout: 15s
audit:
  log_path: /tmp/pg-audit.log
`
	cfgPath := writeTempConfig(t, v2Content)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	env, err := cfg.GetEnv("pg-env")
	if err != nil {
		t.Fatalf("GetEnv() error = %v", err)
	}

	if env.Driver != "postgres" {
		t.Errorf("Driver = %q, want %q", env.Driver, "postgres")
	}

	if env.Database != "myapp" {
		t.Errorf("Database = %q, want %q", env.Database, "myapp")
	}

	if env.EffectiveDriver() != "postgres" {
		t.Errorf("EffectiveDriver() = %q, want %q", env.EffectiveDriver(), "postgres")
	}

	// v2 config triggers migration to v3 on load
	if cfg.ConfigVersion != 3 {
		t.Errorf("ConfigVersion = %d, want 3 (migrated from v2)", cfg.ConfigVersion)
	}
}

// --- EffectiveMaskColumns ---

func TestEffectiveMaskColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		globalMask []string
		envMask    []string
		want       []string
	}{
		{
			name:       "union of global and env",
			globalMask: []string{"email"},
			envMask:    []string{"phone"},
			want:       []string{"EMAIL", "PHONE"},
		},
		{
			name:       "global only, env empty",
			globalMask: []string{"email"},
			envMask:    []string{},
			want:       []string{"EMAIL"},
		},
		{
			name:       "global empty, env only",
			globalMask: []string{},
			envMask:    []string{"id_number"},
			want:       []string{"ID_NUMBER"},
		},
		{
			name:       "both empty",
			globalMask: []string{},
			envMask:    []string{},
			want:       []string{},
		},
		{
			name:       "case insensitive normalisation",
			globalMask: []string{"Email"},
			envMask:    []string{"PHONE"},
			want:       []string{"EMAIL", "PHONE"},
		},
		{
			name:       "global nil, env nil",
			globalMask: nil,
			envMask:    nil,
			want:       []string{},
		},
		{
			name:       "global nil, env set",
			globalMask: nil,
			envMask:    []string{"ssn"},
			want:       []string{"SSN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := Environment{MaskColumns: tt.envMask}
			got := env.EffectiveMaskColumns(tt.globalMask)

			if len(got) != len(tt.want) {
				t.Fatalf("EffectiveMaskColumns() len=%d, want %d; got %v, want %v",
					len(got), len(tt.want), got, tt.want)
			}

			// Build sets for comparison (order may vary)
			gotSet := make(map[string]bool, len(got))
			for _, v := range got {
				gotSet[v] = true
			}

			for _, w := range tt.want {
				if !gotSet[w] {
					t.Errorf("EffectiveMaskColumns() missing %q; got %v, want %v", w, got, tt.want)
				}
			}
		})
	}
}

// --- Config v2→v3 migration ---

func TestMigrateV2ToV3(t *testing.T) {
	t.Parallel()

	v2Content := `
config_version: 2
default_env: dev
environments:
  dev:
    driver: oracle
    user: devuser
    host: localhost
    port: 1521
    service: DEVDB
defaults:
  max_rows: 500
  timeout: 20s
audit:
  log_path: /tmp/migrate-v3-test-audit.log
`
	cfgPath := writeTempConfig(t, v2Content)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// After load, migration should bump version to 3
	if cfg.ConfigVersion != 3 {
		t.Errorf("ConfigVersion = %d, want 3 after v2→v3 migration", cfg.ConfigVersion)
	}

	// MaskColumns should be empty slice, not nil
	if cfg.Defaults.MaskColumns == nil {
		t.Errorf("Defaults.MaskColumns should be empty slice after migration, got nil")
	}

	if len(cfg.Defaults.MaskColumns) != 0 {
		t.Errorf("Defaults.MaskColumns = %v, want empty slice", cfg.Defaults.MaskColumns)
	}

	// The file on disk should also reflect version 3
	reloaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() after v2→v3 migration error = %v", err)
	}

	if reloaded.ConfigVersion != 3 {
		t.Errorf("reloaded ConfigVersion = %d, want 3", reloaded.ConfigVersion)
	}
}

// TestGetEnvEffectiveMaskColumns verifies that GetEnv populates MaskColumns
// using the union of global and per-environment mask columns.
func TestGetEnvEffectiveMaskColumns(t *testing.T) {
	t.Parallel()

	content := `
config_version: 3
default_env: dev
environments:
  dev:
    driver: oracle
    user: devuser
    host: localhost
    port: 1521
    service: DEVDB
    mask_columns:
      - phone
  prod:
    driver: oracle
    user: produser
    host: prodhost
    port: 1521
    service: PRODDB
defaults:
  max_rows: 500
  timeout: 20s
  mask_columns:
    - email
`
	cfgPath := writeTempConfig(t, content)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// dev env: global["email"] + env["phone"] → ["EMAIL", "PHONE"]
	devEnv, err := cfg.GetEnv("dev")
	if err != nil {
		t.Fatalf("GetEnv(dev) error = %v", err)
	}

	if len(devEnv.MaskColumns) != 2 {
		t.Fatalf("dev env MaskColumns len=%d, want 2; got %v", len(devEnv.MaskColumns), devEnv.MaskColumns)
	}

	devSet := make(map[string]bool)
	for _, v := range devEnv.MaskColumns {
		devSet[v] = true
	}

	if !devSet["EMAIL"] || !devSet["PHONE"] {
		t.Errorf("dev env MaskColumns = %v, want [EMAIL PHONE]", devEnv.MaskColumns)
	}

	// prod env: global["email"] + env[] → ["EMAIL"]
	prodEnv, err := cfg.GetEnv("prod")
	if err != nil {
		t.Fatalf("GetEnv(prod) error = %v", err)
	}

	if len(prodEnv.MaskColumns) != 1 || prodEnv.MaskColumns[0] != "EMAIL" {
		t.Errorf("prod env MaskColumns = %v, want [EMAIL]", prodEnv.MaskColumns)
	}
}
