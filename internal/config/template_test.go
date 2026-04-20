package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestBuildTemplateAllDrivers verifies that BuildTemplate with no filter
// returns a template containing all five driver sections.
func TestBuildTemplateAllDrivers(t *testing.T) {
	t.Parallel()

	out := BuildTemplate(nil)

	for _, driver := range []string{"oracle", "postgres", "mysql", "mssql", "sqlite"} {
		if !strings.Contains(out, "driver: "+driver) {
			t.Errorf("BuildTemplate(nil) missing driver section for %q", driver)
		}
	}
}

// TestBuildTemplateOracleOnly verifies oracle-specific fields.
func TestBuildTemplateOracleOnly(t *testing.T) {
	t.Parallel()

	out := BuildTemplate([]string{"oracle"})

	if !strings.Contains(out, "driver: oracle") {
		t.Error("oracle template missing driver field")
	}

	if !strings.Contains(out, "port: 1521") {
		t.Error("oracle template missing port 1521")
	}

	if !strings.Contains(out, "service:") {
		t.Error("oracle template missing service field")
	}

	// Must NOT contain other drivers
	for _, other := range []string{"postgres", "mysql", "mssql", "sqlite"} {
		if strings.Contains(out, "driver: "+other) {
			t.Errorf("oracle-only template unexpectedly contains driver %q", other)
		}
	}
}

// TestBuildTemplatePostgresOnly verifies postgres-specific fields.
func TestBuildTemplatePostgresOnly(t *testing.T) {
	t.Parallel()

	out := BuildTemplate([]string{"postgres"})

	if !strings.Contains(out, "driver: postgres") {
		t.Error("postgres template missing driver field")
	}

	if !strings.Contains(out, "port: 5432") {
		t.Error("postgres template missing port 5432")
	}

	if !strings.Contains(out, "database:") {
		t.Error("postgres template missing database field")
	}
}

// TestBuildTemplateMySQLOnly verifies mysql-specific fields.
func TestBuildTemplateMySQLOnly(t *testing.T) {
	t.Parallel()

	out := BuildTemplate([]string{"mysql"})

	if !strings.Contains(out, "driver: mysql") {
		t.Error("mysql template missing driver field")
	}

	if !strings.Contains(out, "port: 3306") {
		t.Error("mysql template missing port 3306")
	}

	if !strings.Contains(out, "database:") {
		t.Error("mysql template missing database field")
	}
}

// TestBuildTemplateMSSQLOnly verifies mssql-specific fields.
func TestBuildTemplateMSSQLOnly(t *testing.T) {
	t.Parallel()

	out := BuildTemplate([]string{"mssql"})

	if !strings.Contains(out, "driver: mssql") {
		t.Error("mssql template missing driver field")
	}

	if !strings.Contains(out, "port: 1433") {
		t.Error("mssql template missing port 1433")
	}

	if !strings.Contains(out, "database:") {
		t.Error("mssql template missing database field")
	}
}

// TestBuildTemplateSQLiteOnly verifies sqlite-specific fields.
func TestBuildTemplateSQLiteOnly(t *testing.T) {
	t.Parallel()

	out := BuildTemplate([]string{"sqlite"})

	if !strings.Contains(out, "driver: sqlite") {
		t.Error("sqlite template missing driver field")
	}

	if !strings.Contains(out, "database:") {
		t.Error("sqlite template missing database field (file path)")
	}

	// SQLite must NOT contain host or port (no network fields)
	if strings.Contains(out, "host:") {
		t.Error("sqlite template must not contain host field")
	}

	if strings.Contains(out, "port:") {
		t.Error("sqlite template must not contain port field")
	}
}

// TestBuildTemplateMultipleDrivers verifies filtering with multiple drivers.
func TestBuildTemplateMultipleDrivers(t *testing.T) {
	t.Parallel()

	out := BuildTemplate([]string{"postgres", "mysql"})

	if !strings.Contains(out, "driver: postgres") {
		t.Error("expected postgres section")
	}

	if !strings.Contains(out, "driver: mysql") {
		t.Error("expected mysql section")
	}

	// Must NOT contain unselected drivers
	for _, absent := range []string{"oracle", "mssql", "sqlite"} {
		if strings.Contains(out, "driver: "+absent) {
			t.Errorf("unexpected driver section %q in postgres+mysql template", absent)
		}
	}
}

// TestBuildTemplateParseableByLoader verifies the full template is valid YAML
// parseable by the config loader without errors.
func TestBuildTemplateParseableByLoader(t *testing.T) {
	t.Parallel()

	out := BuildTemplate(nil)

	var cfg Config
	if err := yaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("template YAML is not parseable: %v", err)
	}
}

// TestBuildTemplateRequiredAnnotations verifies required fields carry "required" comment.
func TestBuildTemplateRequiredAnnotations(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"oracle", "postgres", "mysql", "mssql", "sqlite"} {
		out := BuildTemplate([]string{driver})

		if !strings.Contains(strings.ToLower(out), "required") {
			t.Errorf("driver %q template has no 'required' annotation", driver)
		}
	}
}

// TestBuildTemplateOptionalAnnotations verifies optional fields carry "optional" comment.
func TestBuildTemplateOptionalAnnotations(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"oracle", "postgres", "mysql", "mssql", "sqlite"} {
		out := BuildTemplate([]string{driver})

		if !strings.Contains(strings.ToLower(out), "optional") {
			t.Errorf("driver %q template has no 'optional' annotation", driver)
		}
	}
}

// TestBuildTemplatePasswordNote verifies each driver template references ADT_PASSWORD.
func TestBuildTemplatePasswordNote(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"oracle", "postgres", "mysql", "mssql"} {
		out := BuildTemplate([]string{driver})

		if !strings.Contains(out, "ADT_PASSWORD") {
			t.Errorf("driver %q template missing ADT_PASSWORD reference", driver)
		}
	}
}

// TestBuildTemplateEmptySliceEqualsAllDrivers verifies BuildTemplate([]string{}) == BuildTemplate(nil).
func TestBuildTemplateEmptySliceEqualsAllDrivers(t *testing.T) {
	t.Parallel()

	nilResult := BuildTemplate(nil)
	emptyResult := BuildTemplate([]string{})

	if nilResult != emptyResult {
		t.Error("BuildTemplate(nil) and BuildTemplate([]string{}) should produce identical output")
	}
}
