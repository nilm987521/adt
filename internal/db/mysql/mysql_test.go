package mysql

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
			want:    "SELECT * FROM (\n    SELECT id, name FROM users\n) AS _adt_sub LIMIT 100",
		},
		{
			name:    "select with order by",
			sql:     "SELECT * FROM users ORDER BY created_at DESC",
			maxRows: 50,
			want:    "SELECT * FROM (\n    SELECT * FROM users ORDER BY created_at DESC\n) AS _adt_sub LIMIT 50",
		},
		{
			name:    "complex CTE",
			sql:     "WITH t AS (SELECT id FROM users) SELECT * FROM t",
			maxRows: 1000,
			want:    "SELECT * FROM (\n    WITH t AS (SELECT id FROM users) SELECT * FROM t\n) AS _adt_sub LIMIT 1000",
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

	got := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", "alice", "s3cr3t", "db.example.com", 3306, "mydb")
	want := "alice:s3cr3t@tcp(db.example.com:3306)/mydb?parseTime=true"

	if got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
}
