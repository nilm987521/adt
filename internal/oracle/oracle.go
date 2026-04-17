package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/sijms/go-ora/v2"
)

// QueryResult holds the result of a query execution.
type QueryResult struct {
	Rows        []map[string]interface{}
	RowCount    int
	Truncated   bool  // true if result may have been cut off (rowCount == maxRows)
	ElapsedMs   int64
	ExecutedSQL string // the actual SQL sent to Oracle (with ROWNUM wrapper)
}

// DB manages an Oracle database connection.
type DB struct {
	db *sql.DB
}

// New opens a new Oracle DB connection.
// DSN format: oracle://<user>:<password>@<host>:<port>/<service>
func New(user, password, host string, port int, service string) (*DB, error) {
	dsn := fmt.Sprintf("oracle://%s:%s@%s:%d/%s", user, password, host, port, service)
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection: %w", err)
	}
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &DB{db: db}, nil
}

// Close releases the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// WrapWithRowLimit wraps SQL in a ROWNUM subquery for Oracle 11g compatibility.
// Example: SELECT * FROM (original_sql) WHERE ROWNUM <= maxRows
func WrapWithRowLimit(sql string, maxRows int) string {
	return fmt.Sprintf("SELECT * FROM (\n    %s\n) WHERE ROWNUM <= %d", sql, maxRows)
}

// Query executes a read-only SELECT query with row limit and timeout.
// It wraps the SQL with ROWNUM and runs inside a READ ONLY transaction.
func (d *DB) Query(ctx context.Context, originalSQL string, maxRows int) (*QueryResult, error) {
	start := time.Now()
	wrappedSQL := WrapWithRowLimit(originalSQL, maxRows)

	// Use a connection explicitly to run SET TRANSACTION READ ONLY
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close()

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
			tx.Rollback()
			return nil, fmt.Errorf("failed to set transaction read only: %w", execErr)
		}
	}
	defer tx.Rollback()

	// Execute query
	sqlRows, err := tx.QueryContext(ctx, wrappedSQL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}
		return nil, err
	}
	defer sqlRows.Close()

	// Get column names
	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Scan rows
	var results []map[string]interface{}
	for sqlRows.Next() {
		// Create scan targets
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := sqlRows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		row := make(map[string]interface{}, len(cols))
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

	tx.Commit()

	elapsed := time.Since(start).Milliseconds()
	rowCount := len(results)

	return &QueryResult{
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
func (d *DB) RawQuery(ctx context.Context, rawSQL string) ([]map[string]interface{}, []string, error) {
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close()

	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		// Fallback: try without ReadOnly hint (go-ora may handle it differently)
		tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
		}
		if _, execErr := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); execErr != nil {
			tx.Rollback()
			return nil, nil, fmt.Errorf("failed to set read only: %w", execErr)
		}
	}
	defer tx.Rollback()

	sqlRows, err := tx.QueryContext(ctx, rawSQL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}
		return nil, nil, err
	}
	defer sqlRows.Close()

	cols, err := sqlRows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var results []map[string]interface{}
	for sqlRows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := sqlRows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make(map[string]interface{}, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}
	if err := sqlRows.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}
		return nil, nil, err
	}
	tx.Commit()
	return results, cols, nil
}

// ExplainPlan executes EXPLAIN PLAN FOR <sql> and returns plan output lines.
// EXPLAIN PLAN writes to PLAN_TABLE so it cannot run in a READ ONLY transaction.
func (d *DB) ExplainPlan(ctx context.Context, userSQL string) ([]string, error) {
	stmtID := fmt.Sprintf("adt_%d", time.Now().UnixNano()%10000000)

	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close()

	// EXPLAIN PLAN writes to PLAN_TABLE — use a regular transaction
	explainSQL := fmt.Sprintf("EXPLAIN PLAN SET STATEMENT_ID = '%s' FOR %s", stmtID, userSQL)
	if _, err := conn.ExecContext(ctx, explainSQL); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timeout: %w", ctx.Err())
		}
		return nil, fmt.Errorf("EXPLAIN PLAN failed: %w", err)
	}

	// Read the plan using DBMS_XPLAN.DISPLAY
	planSQL := fmt.Sprintf(
		"SELECT PLAN_TABLE_OUTPUT FROM TABLE(DBMS_XPLAN.DISPLAY('PLAN_TABLE','%s','ALL'))",
		stmtID,
	)
	rows, err := conn.QueryContext(ctx, planSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to read plan output: %w", err)
	}
	defer rows.Close()

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
