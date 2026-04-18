// Package mysql provides a MySQL database backend implementing db.Driver.
// NOTE: This stub will be replaced by the full implementation once td-3398a5 is merged.
package mysql

import (
	"context"
	"errors"

	"github.com/nilm987521/adt/internal/db"
)

// errNotImplemented is returned by all stub methods.
var errNotImplemented = errors.New("mysql driver: not yet implemented")

// DB is a stub MySQL database connection.
type DB struct{}

// Compile-time check: *DB must satisfy db.Driver.
var _ db.Driver = (*DB)(nil)

// New constructs a MySQL DB connection stub.
func New(_, _, _ string, _ int, _ string) (*DB, error) {
	return nil, errNotImplemented
}

// Close is a stub.
func (d *DB) Close() error { return errNotImplemented }

// Query is a stub.
func (d *DB) Query(_ context.Context, _ string, _ int) (*db.QueryResult, error) {
	return nil, errNotImplemented
}

// RawQuery is a stub.
func (d *DB) RawQuery(_ context.Context, _ string) ([]map[string]any, []string, error) {
	return nil, nil, errNotImplemented
}

// ExplainPlan is a stub.
func (d *DB) ExplainPlan(_ context.Context, _ string) ([]string, error) {
	return nil, errNotImplemented
}

// ListTables is a stub.
func (d *DB) ListTables(_ context.Context, _ string) ([]map[string]any, []string, error) {
	return nil, nil, errNotImplemented
}

// DescribeTable is a stub.
func (d *DB) DescribeTable(_ context.Context, _ string) ([]map[string]any, []string, error) {
	return nil, nil, errNotImplemented
}

// Sample is a stub.
func (d *DB) Sample(_ context.Context, _, _ string, _ int) (*db.QueryResult, error) {
	return nil, errNotImplemented
}
