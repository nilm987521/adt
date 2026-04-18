package oracle

import "testing"

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
			want:    "SELECT * FROM (\n    SELECT id, name FROM users\n) WHERE ROWNUM <= 100",
		},
		{
			name:    "select with order by",
			sql:     "SELECT * FROM users ORDER BY created_at DESC",
			maxRows: 50,
			want:    "SELECT * FROM (\n    SELECT * FROM users ORDER BY created_at DESC\n) WHERE ROWNUM <= 50",
		},
		{
			name:    "complex CTE",
			sql:     "WITH t AS (SELECT id FROM users) SELECT * FROM t",
			maxRows: 1000,
			want:    "SELECT * FROM (\n    WITH t AS (SELECT id FROM users) SELECT * FROM t\n) WHERE ROWNUM <= 1000",
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
