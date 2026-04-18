// Package oracle provides an Oracle database backend implementing db.Driver.
package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/nilm987521/adt/internal/db"
	_ "github.com/sijms/go-ora/v2" // register Oracle driver via init()
)

// DB manages an Oracle database connection and implements db.Driver.
type DB struct {
	conn *sql.DB
}

// Compile-time check: *DB must satisfy db.Driver.
var _ db.Driver = (*DB)(nil)

// New opens a new Oracle DB connection.
// DSN format: oracle://<user>:<password>@<host>:<port>/<service>
func New(user, password, host string, port int, service string) (*DB, error) {
	dsn := fmt.Sprintf("oracle://%s:%s@%s:%d/%s", user, password, host, port, service)

	conn, err := sql.Open("oracle", dsn)
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

// wrapWithRowLimit wraps SQL in a ROWNUM subquery for Oracle 11g compatibility.
// Example: SELECT * FROM (original_sql) WHERE ROWNUM <= maxRows.
func wrapWithRowLimit(sqlStr string, maxRows int) string {
	return fmt.Sprintf("SELECT * FROM (\n    %s\n) WHERE ROWNUM <= %d", sqlStr, maxRows)
}

// Query executes a read-only SELECT query with row limit and timeout.
// It wraps the SQL with ROWNUM and runs inside a READ ONLY transaction.
func (d *DB) Query(ctx context.Context, originalSQL string, maxRows int) (*db.QueryResult, error) { //nolint:gocyclo // sequential DB operation; complexity is inherent
	start := time.Now()
	wrappedSQL := wrapWithRowLimit(originalSQL, maxRows)

	// Use a connection explicitly to run SET TRANSACTION READ ONLY
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	// Begin READ ONLY transaction
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		// Fallback: try without ReadOnly hint (go-ora may handle it differently)
		// Try setting READ ONLY via SQL statement
		tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, execErr := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); execErr != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("failed to set transaction read only: %w", execErr)
		}
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback

	// Execute query
	sqlRows, err := tx.QueryContext(ctx, wrappedSQL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer sqlRows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	// Get column names
	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Scan rows
	var results []map[string]any

	for sqlRows.Next() {
		// Create scan targets
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

// RawQuery executes sql in a READ ONLY transaction without ROWNUM wrapping.
// Returns rows as maps, plus ordered column names.
// Use for internally generated SQL that already includes its own limits.
func (d *DB) RawQuery(ctx context.Context, rawSQL string) ([]map[string]any, []string, error) { //nolint:gocyclo // sequential DB operation; complexity is inherent
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		// Fallback: try without ReadOnly hint (go-ora may handle it differently)
		tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, execErr := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); execErr != nil {
			_ = tx.Rollback()
			return nil, nil, fmt.Errorf("failed to set read only: %w", execErr)
		}
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

// ExplainPlan executes EXPLAIN PLAN FOR <sql> and returns plan output lines.
// EXPLAIN PLAN writes to PLAN_TABLE so it cannot run in a READ ONLY transaction.
func (d *DB) ExplainPlan(ctx context.Context, userSQL string) ([]string, error) {
	stmtID := fmt.Sprintf("adt_%d", time.Now().UnixNano())

	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	// EXPLAIN PLAN writes to PLAN_TABLE — use a regular transaction.
	// Oracle does not support a bind parameter for the target SQL of EXPLAIN PLAN,
	// so userSQL must be concatenated. The SQL has already been validated by the
	// security layer (SELECT-only) before reaching this point.
	explainSQL := fmt.Sprintf("EXPLAIN PLAN SET STATEMENT_ID = '%s' FOR %s", stmtID, userSQL)
	if _, err := conn.ExecContext(ctx, explainSQL); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}

		return nil, fmt.Errorf("EXPLAIN PLAN failed: %w", err)
	}

	// Read the plan using DBMS_XPLAN.DISPLAY
	planSQL := fmt.Sprintf( //nolint:gosec // stmtID is a UUID generated internally, not user input
		"SELECT PLAN_TABLE_OUTPUT FROM TABLE(DBMS_XPLAN.DISPLAY('PLAN_TABLE','%s','ALL'))",
		stmtID,
	)

	rows, err := conn.QueryContext(ctx, planSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan output: %w", err)
	}
	defer rows.Close() //nolint:errcheck // cleanup; error not actionable in defer

	var lines []string

	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}

		lines = append(lines, line)
	}

	return lines, rows.Err()
}

// ListTables returns table metadata rows and ordered column names for the given schema.
// An empty schema queries USER_TABLES (current user); otherwise ALL_TABLES filtered by owner.
func (d *DB) ListTables(ctx context.Context, schema string) ([]map[string]any, []string, error) {
	var rawSQL string

	if schema == "" {
		rawSQL = "SELECT TABLE_NAME, NUM_ROWS, LAST_ANALYZED FROM USER_TABLES ORDER BY TABLE_NAME"
		return d.RawQuery(ctx, rawSQL)
	}

	rawSQL = "SELECT TABLE_NAME, NUM_ROWS, LAST_ANALYZED FROM ALL_TABLES WHERE OWNER = UPPER(:1) ORDER BY TABLE_NAME"

	// RawQuery does not support bind params, so we inline via a wrapper that calls the conn directly.
	return d.rawQueryWithArgs(ctx, rawSQL, schema)
}

// DescribeTable returns column metadata rows and ordered column names for the named table.
// table may be "SCHEMA.TABLE" or "TABLE" (uses USER_TAB_COLUMNS for the current user when no dot).
func (d *DB) DescribeTable(ctx context.Context, table string) ([]map[string]any, []string, error) {
	parts := strings.SplitN(strings.ToUpper(table), ".", 2)

	if len(parts) == 2 {
		// Fully-qualified: query ALL_TAB_COLUMNS with explicit owner.
		rawSQL := "SELECT COLUMN_ID, COLUMN_NAME, DATA_TYPE, DATA_LENGTH, DATA_PRECISION, DATA_SCALE, NULLABLE, DATA_DEFAULT " +
			"FROM ALL_TAB_COLUMNS " +
			"WHERE OWNER = UPPER(:1) AND TABLE_NAME = UPPER(:2) " +
			"ORDER BY COLUMN_ID"

		return d.rawQueryWithArgs(ctx, rawSQL, parts[0], parts[1])
	}

	// Unqualified: query USER_TAB_COLUMNS (current user only), mirrors ListTables pattern.
	rawSQL := "SELECT COLUMN_ID, COLUMN_NAME, DATA_TYPE, DATA_LENGTH, DATA_PRECISION, DATA_SCALE, NULLABLE, DATA_DEFAULT " +
		"FROM USER_TAB_COLUMNS " +
		"WHERE TABLE_NAME = UPPER(:1) " +
		"ORDER BY COLUMN_ID"

	return d.rawQueryWithArgs(ctx, rawSQL, parts[0])
}

// Sample returns a random sample of n rows from schema.table.
// Both schema and table must be upper-cased, alphanumeric Oracle identifiers (A-Z, 0-9, _, $, #).
// Table identifiers cannot be parameterized in Oracle SQL, so they are interpolated directly.
// The caller (internal/cli/sample.go) validates the table argument before this point.
func (d *DB) Sample(ctx context.Context, schema, table string, n int) (*db.QueryResult, error) {
	qualifiedTable := schema + "." + table
	sampleSQL := fmt.Sprintf(
		"SELECT * FROM (\n    SELECT * FROM %s\n    ORDER BY DBMS_RANDOM.VALUE\n) WHERE ROWNUM <= %d",
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
func (d *DB) rawQueryWithArgs(ctx context.Context, rawSQL string, args ...any) ([]map[string]any, []string, error) { //nolint:gocyclo // sequential DB operation; complexity is inherent
	conn, err := d.conn.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck // cleanup; error not actionable in defer

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, execErr := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); execErr != nil {
			_ = tx.Rollback()
			return nil, nil, fmt.Errorf("failed to set read only: %w", execErr)
		}
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
