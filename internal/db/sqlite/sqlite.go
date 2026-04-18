// Package sqlite provides a SQLite database backend implementing db.Driver.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver via init()

	"github.com/nilm987521/adt/internal/db"
)

// DB manages a SQLite database connection and implements db.Driver.
type DB struct {
	conn *sql.DB
}

// Compile-time check: *DB must satisfy db.Driver.
var _ db.Driver = (*DB)(nil)

// New opens an existing SQLite database at dbPath in read-only mode.
// Read-only enforcement is applied at the OS/VFS level via the SQLite URI
// parameter ?mode=ro, which applies to every connection opened by the pool.
// The database file must already exist; New returns an error if it does not.
func New(dbPath string) (*DB, error) {
	if dbPath == "" {
		return nil, errors.New("sqlite: dbPath must not be empty")
	}

	// Use a SQLite URI with mode=ro so OS-level read-only enforcement covers
	// every connection opened by the pool, regardless of pool size.
	// filepath.ToSlash converts Windows backslashes to forward slashes for the URI.
	u := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(dbPath),
		RawQuery: "mode=ro",
	}

	conn, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("sqlite: failed to open connection: %w", err)
	}

	// Ping forces an actual connection, surfacing "file not found" at New() time
	// rather than at first query time. sql.Open is lazy and does not connect.
	if err = conn.PingContext(context.Background()); err != nil {
		_ = conn.Close() //nolint:errcheck // cleanup; returning the ping error is more important
		return nil, fmt.Errorf("sqlite: database not accessible: %w", err)
	}

	conn.SetMaxOpenConns(3)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(5 * time.Minute)

	return &DB{conn: conn}, nil
}

// Close releases the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// wrapWithRowLimit wraps SQL in a subquery with LIMIT.
// Example: SELECT * FROM (<original_sql>) AS _adt_sub LIMIT <maxRows>.
func wrapWithRowLimit(sqlStr string, maxRows int) string {
	return fmt.Sprintf("SELECT * FROM (\n    %s\n) AS _adt_sub LIMIT %d", sqlStr, maxRows)
}

// Query executes a read-only SELECT query with row limit wrapping.
// SQLite does not support ReadOnly transactions; PRAGMA query_only = ON is
// enforced at connection open time in New().
func (d *DB) Query(ctx context.Context, originalSQL string, maxRows int) (*db.QueryResult, error) {
	start := time.Now()
	wrappedSQL := wrapWithRowLimit(originalSQL, maxRows)

	rows, _, err := d.rawQueryWithArgs(ctx, wrappedSQL)
	if err != nil {
		return nil, err
	}

	elapsed := time.Since(start).Milliseconds()
	rowCount := len(rows)

	return &db.QueryResult{
		Rows:        rows,
		RowCount:    rowCount,
		Truncated:   rowCount == maxRows,
		ElapsedMs:   elapsed,
		ExecutedSQL: wrappedSQL,
	}, nil
}

// RawQuery executes rawSQL directly without row-limit wrapping.
// Returns rows as maps plus ordered column names.
func (d *DB) RawQuery(ctx context.Context, rawSQL string) ([]map[string]any, []string, error) {
	return d.rawQueryWithArgs(ctx, rawSQL)
}

// ExplainPlan executes EXPLAIN QUERY PLAN <sql> and returns the detail column
// from each output row. The original SQL is not executed.
func (d *DB) ExplainPlan(ctx context.Context, userSQL string) ([]string, error) {
	explainSQL := "EXPLAIN QUERY PLAN " + userSQL //nolint:gosec // userSQL has been validated as SELECT-only by the security layer before reaching this point

	sqlRows, err := d.conn.QueryContext(ctx, explainSQL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("sqlite: explain query plan timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("sqlite: EXPLAIN QUERY PLAN failed: %w", err)
	}
	defer sqlRows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	var lines []string

	for sqlRows.Next() {
		// EXPLAIN QUERY PLAN returns: id, parent, notused, detail
		var id, parent, notused int

		var detail string

		if err := sqlRows.Scan(&id, &parent, &notused, &detail); err != nil {
			return nil, fmt.Errorf("sqlite: failed to scan plan row: %w", err)
		}

		lines = append(lines, detail)
	}

	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: plan row iteration error: %w", err)
	}

	return lines, nil
}

// ListTables returns table and view metadata from sqlite_master.
// The schema parameter is ignored: SQLite has no multi-schema concept.
func (d *DB) ListTables(ctx context.Context, _ string) ([]map[string]any, []string, error) {
	const rawSQL = "SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name"

	return d.rawQueryWithArgs(ctx, rawSQL)
}

// DescribeTable executes PRAGMA table_info(<table>) and returns rows with the
// fixed column order [cid, name, type, notnull, dflt_value, pk].
// A non-existent table returns an empty slice without error (SQLite behaviour).
func (d *DB) DescribeTable(ctx context.Context, table string) ([]map[string]any, []string, error) {
	fixedCols := []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}
	// PRAGMA table_info does not support parameterized identifiers. The table
	// name is not validated by an upstream security layer for this command path;
	// callers should validate identifiers before calling DescribeTable.
	pragmaSQL := fmt.Sprintf("PRAGMA table_info(%s)", table) //nolint:gosec // PRAGMA identifiers cannot be parameterized; no upstream validation exists for this path

	sqlRows, err := d.conn.QueryContext(ctx, pragmaSQL)
	if err != nil {
		return nil, nil, fmt.Errorf("sqlite: PRAGMA table_info failed: %w", err)
	}
	defer sqlRows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	var results []map[string]any

	for sqlRows.Next() {
		var cid, notnull, pk int

		var name, colType string

		var dfltValue sql.NullString

		if err := sqlRows.Scan(&cid, &name, &colType, &notnull, &dfltValue, &pk); err != nil {
			return nil, nil, fmt.Errorf("sqlite: failed to scan table_info row: %w", err)
		}

		var dfltVal any

		if dfltValue.Valid {
			dfltVal = dfltValue.String
		}

		row := map[string]any{
			"cid":        cid,
			"name":       name,
			"type":       colType,
			"notnull":    notnull,
			"dflt_value": dfltVal,
			"pk":         pk,
		}
		results = append(results, row)
	}

	if err := sqlRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("sqlite: table_info row iteration error: %w", err)
	}

	return results, fixedCols, nil
}

// Sample returns a random sample of n rows from schema.table.
// If schema is non-empty, the table is qualified as schema.table.
// Table and schema identifiers are interpolated directly (validated by caller).
func (d *DB) Sample(ctx context.Context, schema, table string, n int) (*db.QueryResult, error) {
	qualifiedTable := table
	if schema != "" {
		qualifiedTable = schema + "." + table
	}

	// SQL identifiers cannot be parameterized. The table/schema names are not
	// validated by an upstream security layer for this command path; callers
	// should validate identifiers before calling Sample.
	sampleSQL := fmt.Sprintf("SELECT * FROM %s ORDER BY RANDOM() LIMIT %d", qualifiedTable, n) //nolint:gosec // identifiers cannot be parameterized; no upstream validation exists for this path

	rows, _, err := d.rawQueryWithArgs(ctx, sampleSQL)
	if err != nil {
		return nil, fmt.Errorf("sqlite: sample query failed: %w", err)
	}

	return &db.QueryResult{
		Rows:        rows,
		RowCount:    len(rows),
		Truncated:   len(rows) == n,
		ExecutedSQL: sampleSQL,
	}, nil
}

// rawQueryWithArgs executes a SQL statement with optional positional bind
// parameters, returning rows as maps plus ordered column names.
// SQLite does not support ReadOnly transactions; OS-level read-only enforcement
// is applied by the ?mode=ro URI parameter set in New().
func (d *DB) rawQueryWithArgs(ctx context.Context, rawSQL string, args ...any) ([]map[string]any, []string, error) {
	sqlRows, err := d.conn.QueryContext(ctx, rawSQL, args...)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("sqlite: query timeout: %w", ctx.Err())
		}

		return nil, nil, fmt.Errorf("sqlite: query execution failed: %w", err)
	}
	defer sqlRows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("sqlite: failed to get columns: %w", err)
	}

	var results []map[string]any

	for sqlRows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))

		for i := range vals {
			ptrs[i] = &vals[i]
		}

		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, fmt.Errorf("sqlite: failed to scan row: %w", err)
		}

		row := make(map[string]any, len(cols))

		for i, col := range cols {
			row[col] = vals[i]
		}

		results = append(results, row)
	}

	if err := sqlRows.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("sqlite: query timeout: %w", ctx.Err())
		}

		return nil, nil, fmt.Errorf("sqlite: row iteration error: %w", err)
	}

	return results, cols, nil
}
