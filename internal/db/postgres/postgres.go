// Package postgres provides a PostgreSQL database backend implementing db.Driver.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register "pgx" driver via init()

	"github.com/nilm987521/adt/internal/db"
)

// DB manages a PostgreSQL database connection and implements db.Driver.
type DB struct {
	conn *sql.DB
}

// Compile-time check: *DB must satisfy db.Driver.
var _ db.Driver = (*DB)(nil)

// New opens a new PostgreSQL DB connection.
// DSN format: postgres://<user>:<password>@<host>:<port>/<database>
func New(user, password, host string, port int, database string) (*DB, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s", user, password, host, port, database)

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
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

// wrapWithRowLimit wraps SQL in a subquery with LIMIT for PostgreSQL.
// Example: SELECT * FROM (<original_sql>) AS _adt_sub LIMIT <maxRows>.
func wrapWithRowLimit(sqlStr string, maxRows int) string {
	return fmt.Sprintf("SELECT * FROM (\n    %s\n) AS _adt_sub LIMIT %d", sqlStr, maxRows)
}

// Query executes a read-only SELECT query with row limit.
// It wraps the SQL with LIMIT and runs inside a READ ONLY transaction.
func (d *DB) Query(ctx context.Context, originalSQL string, maxRows int) (*db.QueryResult, error) {
	start := time.Now()
	wrappedSQL := wrapWithRowLimit(originalSQL, maxRows)

	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

	sqlRows, err := tx.QueryContext(ctx, wrappedSQL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer sqlRows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any

	for sqlRows.Next() {
		values := make([]any, len(cols))

		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := sqlRows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}

		results = append(results, row)
	}

	if err := sqlRows.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	elapsed := time.Since(start).Milliseconds()
	rowCount := len(results)

	return &db.QueryResult{
		Rows:        results,
		RowCount:    rowCount,
		Truncated:   rowCount == maxRows,
		ElapsedMs:   elapsed,
		ExecutedSQL: wrappedSQL,
	}, nil
}

// RawQuery executes sql in a READ ONLY transaction without LIMIT wrapping.
// Returns rows as maps, plus ordered column names.
// Use for internally generated SQL that already includes its own limits.
func (d *DB) RawQuery(ctx context.Context, rawSQL string) ([]map[string]any, []string, error) {
	return d.rawQueryWithArgs(ctx, rawSQL)
}

// ExplainPlan executes EXPLAIN <sql> and returns plan output lines.
// PostgreSQL natively returns plan rows as a result set in a read-only transaction.
func (d *DB) ExplainPlan(ctx context.Context, userSQL string) ([]string, error) {
	explainSQL := "EXPLAIN " + userSQL //nolint:gosec // userSQL has been validated as SELECT-only by the security layer before reaching this point

	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

	rows, err := tx.QueryContext(ctx, explainSQL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("EXPLAIN failed: %w", err)
	}
	defer rows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	var lines []string

	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, fmt.Errorf("failed to scan plan row: %w", err)
		}

		lines = append(lines, line)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plan row iteration error: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return lines, nil
}

// ListTables returns table metadata rows and ordered column names for the given schema.
// An empty schema queries information_schema.tables for the current schema.
func (d *DB) ListTables(ctx context.Context, schema string) ([]map[string]any, []string, error) {
	if schema == "" {
		rawSQL := "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = current_schema() ORDER BY table_name"
		return d.RawQuery(ctx, rawSQL)
	}

	rawSQL := "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = $1 ORDER BY table_name"

	return d.rawQueryWithArgs(ctx, rawSQL, schema)
}

// DescribeTable returns column metadata rows and ordered column names for the named table.
// table may be "schema.table" or "table" (uses current_schema() when no dot).
func (d *DB) DescribeTable(ctx context.Context, table string) ([]map[string]any, []string, error) {
	parts := strings.SplitN(table, ".", 2)

	if len(parts) == 2 {
		rawSQL := "SELECT ordinal_position, column_name, data_type, character_maximum_length, numeric_precision, numeric_scale, is_nullable, column_default " +
			"FROM information_schema.columns " +
			"WHERE table_schema = $1 AND table_name = $2 " +
			"ORDER BY ordinal_position"

		return d.rawQueryWithArgs(ctx, rawSQL, parts[0], parts[1])
	}

	rawSQL := "SELECT ordinal_position, column_name, data_type, character_maximum_length, numeric_precision, numeric_scale, is_nullable, column_default " +
		"FROM information_schema.columns " +
		"WHERE table_schema = current_schema() AND table_name = $1 " +
		"ORDER BY ordinal_position"

	return d.rawQueryWithArgs(ctx, rawSQL, parts[0])
}

// Sample returns a random sample of n rows from schema.table.
// Both schema and table are interpolated directly (validated by caller).
// Table identifiers cannot be parameterized in SQL, so they are interpolated directly.
func (d *DB) Sample(ctx context.Context, schema, table string, n int) (*db.QueryResult, error) {
	qualifiedTable := schema + "." + table
	sampleSQL := fmt.Sprintf(
		"SELECT * FROM (\n    SELECT * FROM %s ORDER BY RANDOM()\n) AS _adt_sample LIMIT %d",
		qualifiedTable, n,
	)

	rows, _, err := d.RawQuery(ctx, sampleSQL)
	if err != nil {
		return nil, fmt.Errorf("sample query failed: %w", err)
	}

	return &db.QueryResult{
		Rows:        rows,
		RowCount:    len(rows),
		Truncated:   len(rows) == n,
		ExecutedSQL: sampleSQL,
	}, nil
}

// rawQueryWithArgs executes a SQL statement with optional positional bind parameters
// inside a READ ONLY transaction, returning rows as maps plus ordered column names.
func (d *DB) rawQueryWithArgs(ctx context.Context, rawSQL string, args ...any) ([]map[string]any, []string, error) {
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

	sqlRows, err := tx.QueryContext(ctx, rawSQL, args...)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer sqlRows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any

	for sqlRows.Next() {
		vals := make([]any, len(cols))

		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}

		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}

		results = append(results, row)
	}

	if err := sqlRows.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, nil, fmt.Errorf("row iteration error: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}

	return results, cols, nil
}
