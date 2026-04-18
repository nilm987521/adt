// Package db defines the Driver interface and associated types that all
// database backends must implement.
package db

import "context"

// Driver is the interface that all database backends must implement.
// Implementations are expected to be safe for concurrent use.
type Driver interface {
	// Query runs a user-supplied SELECT with automatic row-limit wrapping
	// inside a read-only transaction.
	Query(ctx context.Context, sql string, maxRows int) (*QueryResult, error)

	// RawQuery runs an internally generated SQL statement without row-limit
	// wrapping. Returns rows, ordered column names, and any error.
	// Used by metadata helpers.
	RawQuery(ctx context.Context, sql string) ([]map[string]any, []string, error)

	// ExplainPlan returns the query plan for the given SQL without executing it.
	// Each element of the returned slice is one line of the plan output.
	ExplainPlan(ctx context.Context, sql string) ([]string, error)

	// ListTables returns table metadata rows and ordered column names for the
	// given schema. An empty schema defaults to the current user/database.
	ListTables(ctx context.Context, schema string) ([]map[string]any, []string, error)

	// DescribeTable returns column metadata rows and ordered column names for
	// the named table.
	DescribeTable(ctx context.Context, table string) ([]map[string]any, []string, error)

	// Sample returns a random sample of n rows from schema.table.
	// An empty schema defaults to the current user/database.
	Sample(ctx context.Context, schema, table string, n int) (*QueryResult, error)

	// Close releases the underlying connection pool.
	Close() error
}

// QueryResult holds the output of a user-facing query operation.
type QueryResult struct {
	Rows        []map[string]any
	RowCount    int
	Truncated   bool // true if RowCount == maxRows (result may be cut off)
	ElapsedMs   int64
	ExecutedSQL string // actual SQL sent to the database (with limit wrapper)
}
