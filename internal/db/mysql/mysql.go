// Package mysql provides a MySQL database backend implementing db.Driver.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // register MySQL driver via init()
	"github.com/nilm987521/adt/internal/db"
)

// DB manages a MySQL database connection and implements db.Driver.
type DB struct {
	conn *sql.DB
}

// Compile-time check: *DB must satisfy db.Driver.
var _ db.Driver = (*DB)(nil)

// New opens a new MySQL DB connection.
// DSN format: user:password@tcp(host:port)/database?parseTime=true.
func New(user, password, host string, port int, database string) (*DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", user, password, host, port, database)

	conn, err := sql.Open("mysql", dsn)
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

// wrapWithRowLimit wraps SQL in a subquery with LIMIT for MySQL.
// Example: SELECT * FROM (<original_sql>) AS _adt_sub LIMIT maxRows.
func wrapWithRowLimit(sqlStr string, maxRows int) string {
	return fmt.Sprintf("SELECT * FROM (\n    %s\n) AS _adt_sub LIMIT %d", sqlStr, maxRows)
}

// beginReadOnlyTx starts a read-only transaction on the given connection.
// MySQL supports START TRANSACTION READ ONLY via sql.TxOptions; if that fails,
// a regular transaction is started and SET TRANSACTION READ ONLY is issued.
func beginReadOnlyTx(ctx context.Context, conn *sql.Conn) (*sql.Tx, error) {
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		// Fallback: begin a regular transaction then set read-only via SQL.
		tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, execErr := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); execErr != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("failed to set read only: %w", execErr)
		}
	}

	return tx, nil
}

// Query executes a read-only SELECT query with row limit and timing.
// It wraps the SQL with LIMIT and runs inside a READ ONLY transaction.
func (d *DB) Query(ctx context.Context, originalSQL string, maxRows int) (*db.QueryResult, error) {
	start := time.Now()
	wrappedSQL := wrapWithRowLimit(originalSQL, maxRows)

	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := beginReadOnlyTx(ctx, conn)
	if err != nil {
		return nil, err
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

// RawQuery executes sql in a READ ONLY transaction without row-limit wrapping.
// Returns rows as maps, plus ordered column names.
// Use for internally generated SQL that already includes its own limits.
func (d *DB) RawQuery(ctx context.Context, rawSQL string) ([]map[string]any, []string, error) {
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := beginReadOnlyTx(ctx, conn)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

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

// ExplainPlan executes EXPLAIN <sql> and returns each result row as a
// tab-separated string. MySQL's EXPLAIN returns a result set so we convert
// each row to a formatted line.
func (d *DB) ExplainPlan(ctx context.Context, userSQL string) ([]string, error) {
	// MySQL EXPLAIN returns a result set — use a regular (non-read-only) connection
	// because some MySQL versions disallow EXPLAIN inside a READ ONLY transaction.
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	// userSQL has been validated as SELECT-only by the security layer before
	// reaching this point, so direct concatenation is acceptable here.
	explainSQL := "EXPLAIN " + userSQL //nolint:gosec // userSQL validated upstream as SELECT-only

	rows, err := conn.QueryContext(ctx, explainSQL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("EXPLAIN failed: %w", err)
	}
	defer rows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get EXPLAIN columns: %w", err)
	}

	var lines []string

	for rows.Next() {
		vals := make([]any, len(cols))

		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("failed to scan EXPLAIN row: %w", err)
		}

		parts := make([]string, len(cols))

		for i, v := range vals {
			if v == nil {
				parts[i] = "NULL"
			} else {
				parts[i] = fmt.Sprintf("%v", v)
			}
		}

		lines = append(lines, strings.Join(parts, "\t"))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("EXPLAIN row iteration error: %w", err)
	}

	return lines, nil
}

// ListTables returns table metadata rows and ordered column names for the given schema.
// An empty schema queries information_schema for the current database.
func (d *DB) ListTables(ctx context.Context, schema string) ([]map[string]any, []string, error) {
	if schema == "" {
		rawSQL := "SELECT TABLE_NAME, TABLE_TYPE, TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() ORDER BY TABLE_NAME"
		return d.RawQuery(ctx, rawSQL)
	}

	rawSQL := "SELECT TABLE_NAME, TABLE_TYPE, TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME"

	return d.rawQueryWithArgs(ctx, rawSQL, schema)
}

// DescribeTable returns column metadata rows and ordered column names for the named table.
// table may be "schema.table" or "table" (uses DATABASE() when no dot is present).
func (d *DB) DescribeTable(ctx context.Context, table string) ([]map[string]any, []string, error) {
	parts := strings.SplitN(table, ".", 2)

	if len(parts) == 2 {
		// Fully-qualified: schema.table
		rawSQL := "SELECT ORDINAL_POSITION, COLUMN_NAME, DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, NUMERIC_PRECISION, NUMERIC_SCALE, IS_NULLABLE, COLUMN_DEFAULT " +
			"FROM information_schema.COLUMNS " +
			"WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? " +
			"ORDER BY ORDINAL_POSITION"

		return d.rawQueryWithArgs(ctx, rawSQL, parts[0], parts[1])
	}

	// Unqualified: use current database.
	rawSQL := "SELECT ORDINAL_POSITION, COLUMN_NAME, DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, NUMERIC_PRECISION, NUMERIC_SCALE, IS_NULLABLE, COLUMN_DEFAULT " +
		"FROM information_schema.COLUMNS " +
		"WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? " +
		"ORDER BY ORDINAL_POSITION"

	return d.rawQueryWithArgs(ctx, rawSQL, parts[0])
}

// Sample returns a random sample of n rows from schema.table.
// Table identifiers cannot be parameterized in MySQL SQL, so they are
// interpolated directly. The caller validates schema and table before this point.
func (d *DB) Sample(ctx context.Context, schema, table string, n int) (*db.QueryResult, error) {
	qualifiedTable := schema + "." + table
	sampleSQL := fmt.Sprintf(
		"SELECT * FROM (\n    SELECT * FROM %s ORDER BY RAND()\n) AS _adt_sample LIMIT %d",
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

// rawQueryWithArgs executes a SQL statement with positional bind parameters inside a READ ONLY
// transaction, returning rows as maps plus ordered column names.
func (d *DB) rawQueryWithArgs(ctx context.Context, rawSQL string, args ...any) ([]map[string]any, []string, error) {
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := beginReadOnlyTx(ctx, conn)
	if err != nil {
		return nil, nil, err
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
