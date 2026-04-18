package mssql

import (
	"fmt"
	"testing"
)

func TestWrapWithRowLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		sql     string
		maxRows int
		want    string
	}{
		{
			name:    "simple select",
			sql:     "SELECT id, name FROM users",
			maxRows: 100,
			want:    "SELECT TOP 100 * FROM (\n    SELECT id, name FROM users\n) AS _adt_sub",
		},
		{
			name:    "select with order by",
			sql:     "SELECT * FROM users ORDER BY created_at DESC",
			maxRows: 50,
			want:    "SELECT TOP 50 * FROM (\n    SELECT * FROM users ORDER BY created_at DESC\n) AS _adt_sub",
		},
		{
			name:    "complex CTE",
			sql:     "WITH t AS (SELECT id FROM users) SELECT * FROM t",
			maxRows: 1000,
			want:    "SELECT TOP 1000 * FROM (\n    WITH t AS (SELECT id FROM users) SELECT * FROM t\n) AS _adt_sub",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := wrapWithRowLimit(tc.sql, tc.maxRows)
			if got != tc.want {
				t.Errorf("wrapWithRowLimit() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatDSN(t *testing.T) {
	t.Parallel()

	got := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s", "alice", "s3cr3t", "db.example.com", 1433, "mydb")
	want := "sqlserver://alice:s3cr3t@db.example.com:1433?database=mydb"

	if got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
}
