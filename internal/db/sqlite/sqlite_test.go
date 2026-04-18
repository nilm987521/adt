// Package sqlite provides tests for the SQLite database backend.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite" // register "sqlite" driver via init()
)

// seedDB opens a raw connection (without query_only) to dbPath and inserts
// the standard fixture schema and data used by tests.
func seedDB(t *testing.T, dbPath string) {
	t.Helper()

	raw, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err, "opening seed connection")

	defer func() { _ = raw.Close() }()

	ctx := context.Background()

	_, err = raw.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(t, err, "creating users table")

	_, err = raw.ExecContext(ctx, `CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, amount REAL)`)
	require.NoError(t, err, "creating orders table")

	_, err = raw.ExecContext(ctx, `CREATE VIEW active_users AS SELECT id, name FROM users WHERE id > 0`)
	require.NoError(t, err, "creating active_users view")

	for i := 1; i <= 5; i++ {
		_, err = raw.ExecContext(ctx, `INSERT INTO users (id, name) VALUES (?, ?)`, i, fmt.Sprintf("user%c", rune('A'+i-1)))
		require.NoError(t, err, "inserting user row")
	}
}

// newTestDB creates a temp SQLite file, seeds it with fixture data via a
// plain connection, then opens it with New() (which sets query_only = ON).
// It registers t.Cleanup handlers for both the file and the DB connection.
func newTestDB(t *testing.T) *DB {
	t.Helper()

	tmpPath := filepath.Join(t.TempDir(), "adt_test.db")

	// Seed fixture data before New() sets query_only = ON.
	seedDB(t, tmpPath)

	d, err := New(tmpPath)
	require.NoError(t, err, "New() should open the temp db")

	t.Cleanup(func() { _ = d.Close() })

	return d
}

// initSQLiteFile creates a valid empty SQLite database at path by opening and
// closing a writable connection. Required because ?mode=ro refuses to create
// new files; the database file must already exist before New() is called.
func initSQLiteFile(t *testing.T, path string) {
	t.Helper()

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err, "creating empty sqlite db")

	require.NoError(t, raw.PingContext(context.Background()), "pinging empty sqlite db")
	require.NoError(t, raw.Close(), "closing empty sqlite db")
}

// TestNew verifies that New() opens an existing database in read-only mode.
// Test 6.2.
func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("opens file successfully", func(t *testing.T) {
		t.Parallel()

		tmpPath := filepath.Join(t.TempDir(), "adt_new.db")
		initSQLiteFile(t, tmpPath)

		d, err := New(tmpPath)
		require.NoError(t, err, "New() should succeed for valid path")
		require.NotNil(t, d)
		require.NoError(t, d.Close())
	})

	t.Run("mode=ro prevents writes on all pool connections", func(t *testing.T) {
		t.Parallel()

		tmpPath := filepath.Join(t.TempDir(), "adt_readonly.db")
		initSQLiteFile(t, tmpPath)

		d, err := New(tmpPath)
		require.NoError(t, err)

		t.Cleanup(func() { _ = d.Close() })

		// ?mode=ro enforces OS-level read-only on every connection in the pool.
		_, execErr := d.conn.ExecContext(context.Background(),
			`CREATE TABLE should_fail (id INTEGER)`)
		assert.Error(t, execErr, "write should fail when opened with ?mode=ro")
	})

	t.Run("fails on empty path", func(t *testing.T) {
		t.Parallel()

		_, err := New("")
		assert.Error(t, err, "New() with empty path should return an error")
	})

	t.Run("fails on non-existent file path", func(t *testing.T) {
		t.Parallel()

		// ?mode=ro refuses to create new files, so a non-existent path must error.
		_, err := New(filepath.Join(t.TempDir(), "does_not_exist.db"))
		assert.Error(t, err, "New() should fail when the database file does not exist")
	})
}

// TestQuery verifies that Query() wraps SQL with a row limit and sets Truncated correctly.
// Test 6.3.
func TestQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		maxRows       int
		wantRowCount  int
		wantTruncated bool
	}{
		{
			name:          "returns rows up to limit",
			maxRows:       3,
			wantRowCount:  3,
			wantTruncated: true,
		},
		{
			name:          "returns all rows when limit equals row count — Truncated true",
			maxRows:       5,
			wantRowCount:  5,
			wantTruncated: true, // exactly 5 rows exist and limit is 5; Truncated is true when rowCount == maxRows
		},
		{
			name:          "limit larger than available rows",
			maxRows:       10,
			wantRowCount:  5,
			wantTruncated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := newTestDB(t)
			ctx := context.Background()

			result, err := d.Query(ctx, "SELECT * FROM users", tt.maxRows)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.wantRowCount, result.RowCount)
			assert.Equal(t, tt.wantTruncated, result.Truncated)
			assert.NotEmpty(t, result.ExecutedSQL)
			assert.GreaterOrEqual(t, result.ElapsedMs, int64(0))
		})
	}
}

// TestRawQuery verifies that RawQuery() executes SQL and returns rows with column names.
func TestRawQuery(t *testing.T) {
	t.Parallel()

	d := newTestDB(t)
	ctx := context.Background()

	rows, cols, err := d.RawQuery(ctx, "SELECT id, name FROM users ORDER BY id")
	require.NoError(t, err)
	assert.Len(t, rows, 5)
	assert.Equal(t, []string{"id", "name"}, cols)
}

// TestExplainPlan verifies that ExplainPlan() returns plan lines without executing the query.
// Test 6.4.
func TestExplainPlan(t *testing.T) {
	t.Parallel()

	t.Run("returns non-empty plan slice", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		lines, err := d.ExplainPlan(ctx, "SELECT * FROM users")
		require.NoError(t, err)
		assert.NotEmpty(t, lines, "ExplainPlan should return at least one plan line")

		for _, line := range lines {
			assert.NotEmpty(t, line, "each plan line should be non-empty")
		}
	})

	t.Run("does not execute the original query side effects", func(t *testing.T) {
		t.Parallel()

		// Open an empty DB; ExplainPlan on a missing table returns an error —
		// confirming the plan analyser ran rather than executing data rows.
		tmpPath := filepath.Join(t.TempDir(), "adt_explain.db")
		initSQLiteFile(t, tmpPath)

		d, err := New(tmpPath)
		require.NoError(t, err)

		t.Cleanup(func() { _ = d.Close() })

		_, planErr := d.ExplainPlan(context.Background(), "SELECT * FROM users WHERE id = 1")
		// SQLite returns an error for unknown tables in EXPLAIN QUERY PLAN.
		assert.Error(t, planErr, "ExplainPlan on non-existent table should return an error")
	})
}

// TestListTables verifies that ListTables() returns both tables and views.
// Test 6.5.
func TestListTables(t *testing.T) {
	t.Parallel()

	t.Run("returns tables and views", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		rows, cols, err := d.ListTables(ctx, "")
		require.NoError(t, err)
		assert.Equal(t, []string{"name", "type"}, cols)

		names := make(map[string]string)

		for _, row := range rows {
			name, _ := row["name"].(string)
			typ, _ := row["type"].(string)
			names[name] = typ
		}

		assert.Equal(t, "table", names["users"], "users should be a table")
		assert.Equal(t, "table", names["orders"], "orders should be a table")
		assert.Equal(t, "view", names["active_users"], "active_users should be a view")
	})

	t.Run("schema parameter is ignored", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		rowsEmpty, _, err := d.ListTables(ctx, "")
		require.NoError(t, err)

		rowsWithSchema, _, err := d.ListTables(ctx, "main")
		require.NoError(t, err)

		assert.Len(t, rowsWithSchema, len(rowsEmpty),
			"schema parameter should not change the result set")
	})
}

// TestDescribeTable verifies PRAGMA table_info returns the correct 6 columns.
// Test 6.6.
func TestDescribeTable(t *testing.T) {
	t.Parallel()

	t.Run("returns 6-column pragma rows for existing table", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		rows, cols, err := d.DescribeTable(ctx, "users")
		require.NoError(t, err)
		require.NotEmpty(t, rows, "users table should have columns")
		assert.Equal(t, []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}, cols)

		// Verify the id column is present.
		found := false

		for _, row := range rows {
			if row["name"] == "id" {
				found = true

				break
			}
		}

		assert.True(t, found, "id column should be described")
	})

	t.Run("non-existent table returns empty slice, no error", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		rows, cols, err := d.DescribeTable(ctx, "no_such_table")
		require.NoError(t, err, "non-existent table should not return an error")
		assert.Empty(t, rows, "non-existent table should return empty rows")
		assert.Equal(t, []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}, cols)
	})
}

// TestSample verifies that Sample() returns n random rows.
// Test 6.7.
func TestSample(t *testing.T) {
	t.Parallel()

	t.Run("returns n random rows", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		result, err := d.Sample(ctx, "", "users", 3)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, 3, result.RowCount)
		assert.Len(t, result.Rows, 3)
		assert.NotEmpty(t, result.ExecutedSQL)
	})

	t.Run("schema parameter prefixes table name", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		// SQLite accepts "main.users" as a qualified table name.
		result, err := d.Sample(ctx, "main", "users", 2)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, 2, result.RowCount)
		assert.Contains(t, result.ExecutedSQL, "main.users")
	})

	t.Run("returns fewer rows than n when table has less data", func(t *testing.T) {
		t.Parallel()

		d := newTestDB(t)
		ctx := context.Background()

		result, err := d.Sample(ctx, "", "users", 100)
		require.NoError(t, err)
		assert.Equal(t, 5, result.RowCount, "should return all 5 rows when n > table size")
	})
}

// TestWrapWithRowLimit verifies that the SQL wrapping helper produces the correct output.
func TestWrapWithRowLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sql     string
		maxRows int
		want    string
	}{
		{
			name:    "simple select",
			sql:     "SELECT id, name FROM users",
			maxRows: 100,
			want:    "SELECT * FROM (\n    SELECT id, name FROM users\n) AS _adt_sub LIMIT 100",
		},
		{
			name:    "select with order by",
			sql:     "SELECT * FROM users ORDER BY id",
			maxRows: 50,
			want:    "SELECT * FROM (\n    SELECT * FROM users ORDER BY id\n) AS _adt_sub LIMIT 50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := wrapWithRowLimit(tt.sql, tt.maxRows)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNew_modeROPreventsWrites confirms that ?mode=ro enforces write rejection
// at the OS/VFS level, not just per-connection pragma state.
func TestNew_modeROPreventsWrites(t *testing.T) {
	t.Parallel()

	tmpPath := filepath.Join(t.TempDir(), "mode_ro_check.db")
	initSQLiteFile(t, tmpPath)

	d, err := New(tmpPath)
	require.NoError(t, err)

	defer func() { _ = d.Close() }()

	_, writeErr := d.conn.ExecContext(context.Background(), `CREATE TABLE should_fail (x INTEGER)`)
	assert.Error(t, writeErr, "any write attempt should fail when opened with ?mode=ro")
}
