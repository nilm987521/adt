// Package mssql provides a Microsoft SQL Server database backend implementing db.Driver.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb" // register sqlserver driver via init()
	"github.com/nilm987521/adt/internal/db"
)

// DB manages a SQL Server database connection and implements db.Driver.
type DB struct {
	conn *sql.DB
}

// Compile-time check: *DB must satisfy db.Driver.
var _ db.Driver = (*DB)(nil)

// New opens a new SQL Server DB connection.
// DSN format: sqlserver://<user>:<password>@<host>:<port>?database=<database>
func New(user, password, host string, port int, database string) (*DB, error) {
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s", user, password, host, port, database)

	conn, err := sql.Open("sqlserver", dsn)
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

// wrapWithRowLimit wraps SQL in a TOP subquery for SQL Server.
// Example: SELECT TOP n * FROM (original_sql) AS _adt_sub.
func wrapWithRowLimit(sqlStr string, maxRows int) string {
	return fmt.Sprintf("SELECT TOP %d * FROM (\n    %s\n) AS _adt_sub", maxRows, sqlStr)
}

// Query executes a read-only SELECT query with row limit and timeout.
// It wraps the SQL with TOP and runs inside a READ COMMITTED transaction.
func (d *DB) Query(ctx context.Context, originalSQL string, maxRows int) (*db.QueryResult, error) { //nolint:gocyclo // sequential DB operation; complexity is inherent
	start := time.Now()
	wrappedSQL := wrapWithRowLimit(originalSQL, maxRows)

	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

	if _, err := tx.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"); err != nil {
		_ = tx.Rollback()

		return nil, fmt.Errorf("failed to set isolation level: %w", err)
	}

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

// RawQuery executes sql in a READ COMMITTED transaction without row-limit wrapping.
// Returns rows as maps, plus ordered column names.
// Use for internally generated SQL that already includes its own limits.
func (d *DB) RawQuery(ctx context.Context, rawSQL string) ([]map[string]any, []string, error) { //nolint:gocyclo // sequential DB operation; complexity is inherent
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

	if _, err := tx.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"); err != nil {
		_ = tx.Rollback()

		return nil, nil, fmt.Errorf("failed to set isolation level: %w", err)
	}

	sqlRows, err := tx.QueryContext(ctx, rawSQL)
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

// ExplainPlan returns the query execution plan for the given SQL without executing it.
// It uses SET SHOWPLAN_TEXT ON to obtain the plan as a result set.
// Each element of the returned slice is one line of the plan output.
func (d *DB) ExplainPlan(ctx context.Context, userSQL string) ([]string, error) {
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	if _, err := conn.ExecContext(ctx, "SET SHOWPLAN_TEXT ON"); err != nil {
		return nil, fmt.Errorf("failed to enable showplan: %w", err)
	}

	// When SHOWPLAN_TEXT is ON, executing a statement returns the plan instead of running it.
	rows, err := conn.QueryContext(ctx, userSQL)
	if err != nil {
		_, _ = conn.ExecContext(ctx, "SET SHOWPLAN_TEXT OFF") // best-effort cleanup

		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("explain plan failed: %w", err)
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

	if _, err := conn.ExecContext(ctx, "SET SHOWPLAN_TEXT OFF"); err != nil {
		return nil, fmt.Errorf("failed to disable showplan: %w", err)
	}

	return lines, nil
}

// ListTables returns table metadata rows and ordered column names for the given schema.
// An empty schema queries INFORMATION_SCHEMA.TABLES for the current schema; otherwise
// the schema name is used as a filter.
func (d *DB) ListTables(ctx context.Context, schema string) ([]map[string]any, []string, error) {
	if schema == "" {
		rawSQL := "SELECT TABLE_NAME, TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = SCHEMA_NAME() ORDER BY TABLE_NAME"

		return d.RawQuery(ctx, rawSQL)
	}

	rawSQL := "SELECT TABLE_NAME, TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = @p1 ORDER BY TABLE_NAME"

	return d.rawQueryWithArgs(ctx, rawSQL, schema)
}

// DescribeTable returns column metadata rows and ordered column names for the named table.
// table may be "SCHEMA.TABLE" or "TABLE" (uses SCHEMA_NAME() for the current schema when no dot).
func (d *DB) DescribeTable(ctx context.Context, table string) ([]map[string]any, []string, error) {
	parts := strings.SplitN(table, ".", 2)

	if len(parts) == 2 {
		rawSQL := "SELECT ORDINAL_POSITION, COLUMN_NAME, DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, NUMERIC_PRECISION, NUMERIC_SCALE, IS_NULLABLE, COLUMN_DEFAULT " +
			"FROM INFORMATION_SCHEMA.COLUMNS " +
			"WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2 " +
			"ORDER BY ORDINAL_POSITION"

		return d.rawQueryWithArgs(ctx, rawSQL, parts[0], parts[1])
	}

	rawSQL := "SELECT ORDINAL_POSITION, COLUMN_NAME, DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, NUMERIC_PRECISION, NUMERIC_SCALE, IS_NULLABLE, COLUMN_DEFAULT " +
		"FROM INFORMATION_SCHEMA.COLUMNS " +
		"WHERE TABLE_SCHEMA = SCHEMA_NAME() AND TABLE_NAME = @p1 " +
		"ORDER BY ORDINAL_POSITION"

	return d.rawQueryWithArgs(ctx, rawSQL, parts[0])
}

// Sample returns a random sample of n rows from schema.table.
// Both schema and table are interpolated directly into SQL as they are structural identifiers
// that cannot be parameterized. The caller validates these arguments before this point.
func (d *DB) Sample(ctx context.Context, schema, table string, n int) (*db.QueryResult, error) {
	qualifiedTable := schema + "." + table
	sampleSQL := fmt.Sprintf(
		"SELECT TOP %d * FROM (\n    SELECT * FROM %s ORDER BY NEWID()\n) AS _adt_sample",
		n, qualifiedTable,
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

// rawQueryWithArgs executes a SQL statement with positional bind parameters inside a
// READ COMMITTED transaction, returning rows as maps plus ordered column names.
func (d *DB) rawQueryWithArgs(ctx context.Context, rawSQL string, args ...any) ([]map[string]any, []string, error) { //nolint:gocyclo // sequential DB operation; complexity is inherent
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

	if _, err := tx.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"); err != nil {
		_ = tx.Rollback()

		return nil, nil, fmt.Errorf("failed to set isolation level: %w", err)
	}

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
