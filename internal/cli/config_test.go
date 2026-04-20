package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra" //nolint:depguard // test helper uses cobra directly
)

// executeConfigInit builds a standalone cobra.Command (bypassing cobra's global
// OnInitialize / viper machinery) and calls runConfigInit directly.
// This avoids the global-viper data race that occurs when parallel tests all
// trigger cobra's OnInitialize hook concurrently.
func executeConfigInit(t *testing.T, args ...string) (stderr string, err error) {
	t.Helper()

	var stderrBuf bytes.Buffer

	cmd := &cobra.Command{Use: "init", RunE: runConfigInit, SilenceUsage: true, SilenceErrors: true}
	cmd.Flags().String("output", "./config.yaml", "output file path")
	cmd.Flags().StringArray("db", nil, "database driver(s) to include")
	cmd.Flags().Bool("force", false, "overwrite existing file")
	cmd.SetErr(&stderrBuf)
	cmd.SetOut(&bytes.Buffer{})

	// Parse flags and call runConfigInit directly — no cobra OnInitialize triggered.
	if parseErr := cmd.ParseFlags(args); parseErr != nil {
		return "", parseErr
	}

	err = runConfigInit(cmd, nil)

	return stderrBuf.String(), err
}

// TestConfigInitDefaultPath verifies that running with no flags writes to ./config.yaml.
//
//nolint:paralleltest // t.Chdir requires non-parallel execution
func TestConfigInitDefaultPath(t *testing.T) {
	tmp := t.TempDir()

	t.Chdir(tmp)

	_, err := executeConfigInit(t)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, "config.yaml")); err != nil {
		t.Errorf("expected config.yaml to exist in working dir: %v", err)
	}
}

// TestConfigInitCustomOutput verifies --output writes to the specified path.
func TestConfigInitCustomOutput(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "myconfig.yaml")

	_, err := executeConfigInit(t, "--output", outPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("expected output file to exist at %s: %v", outPath, err)
	}
}

// TestConfigInitFileExistsNoForce verifies that an existing file without --force causes an error.
func TestConfigInitFileExistsNoForce(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "config.yaml")

	if err := os.WriteFile(outPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := executeConfigInit(t, "--output", outPath)
	if err == nil {
		t.Fatal("expected error when file exists without --force, got nil")
	}
}

// TestConfigInitFileExistsWithForce verifies that --force overwrites and prints a warning.
func TestConfigInitFileExistsWithForce(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "config.yaml")

	if err := os.WriteFile(outPath, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	stderr, err := executeConfigInit(t, "--output", outPath, "--force")
	if err != nil {
		t.Fatalf("expected no error with --force, got %v", err)
	}

	content, err := os.ReadFile(outPath) //nolint:gosec // test file read
	if err != nil {
		t.Fatal(err)
	}

	if string(content) == "existing" {
		t.Error("file was not overwritten by --force")
	}

	if !strings.Contains(stderr, "warning") && !strings.Contains(strings.ToLower(stderr), "overwrit") {
		t.Errorf("expected warning in stderr, got: %q", stderr)
	}
}

// TestConfigInitInvalidDB verifies that an invalid --db value causes an error.
func TestConfigInitInvalidDB(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "config.yaml")

	_, err := executeConfigInit(t, "--output", outPath, "--db", "unknown")
	if err == nil {
		t.Fatal("expected error for invalid --db value, got nil")
	}
}

// TestConfigInitDBFilter verifies that --db limits output to the specified driver.
func TestConfigInitDBFilter(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "config.yaml")

	_, err := executeConfigInit(t, "--output", outPath, "--db", "postgres")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	content, err := os.ReadFile(outPath) //nolint:gosec // test file read
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "driver: postgres") {
		t.Error("expected postgres section in output")
	}

	for _, absent := range []string{"driver: oracle", "driver: mysql", "driver: mssql", "driver: sqlite"} {
		if strings.Contains(string(content), absent) {
			t.Errorf("unexpected %q in postgres-only output", absent)
		}
	}
}

// TestConfigInitDBFilterCommaSeparated verifies comma-separated --db values.
func TestConfigInitDBFilterCommaSeparated(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "config.yaml")

	_, err := executeConfigInit(t, "--output", outPath, "--db", "postgres,mysql")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	content, err := os.ReadFile(outPath) //nolint:gosec // test file read
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "driver: postgres") {
		t.Error("expected postgres section")
	}

	if !strings.Contains(string(content), "driver: mysql") {
		t.Error("expected mysql section")
	}

	for _, absent := range []string{"driver: oracle", "driver: mssql", "driver: sqlite"} {
		if strings.Contains(string(content), absent) {
			t.Errorf("unexpected %q in postgres,mysql output", absent)
		}
	}
}

// TestConfigInitDBFilterMultipleFlags verifies multiple --db flags.
func TestConfigInitDBFilterMultipleFlags(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "config.yaml")

	_, err := executeConfigInit(t, "--output", outPath, "--db", "postgres", "--db", "mysql")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	content, err := os.ReadFile(outPath) //nolint:gosec // test file read
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(content), "driver: postgres") {
		t.Error("expected postgres section")
	}

	if !strings.Contains(string(content), "driver: mysql") {
		t.Error("expected mysql section")
	}
}

// TestConfigInitOutputContainsAllDriversByDefault verifies the full template is output with no --db.
func TestConfigInitOutputContainsAllDriversByDefault(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	outPath := filepath.Join(tmp, "config.yaml")

	_, err := executeConfigInit(t, "--output", outPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	content, err := os.ReadFile(outPath) //nolint:gosec // test file read
	if err != nil {
		t.Fatal(err)
	}

	for _, driver := range []string{"oracle", "postgres", "mysql", "mssql", "sqlite"} {
		if !strings.Contains(string(content), "driver: "+driver) {
			t.Errorf("expected driver section for %q in default output", driver)
		}
	}
}
