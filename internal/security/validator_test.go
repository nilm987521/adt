package security

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) { //nolint:funlen // table-driven test; length is expected
	t.Parallel()

	tests := []struct {
		name        string
		sql         string
		wantErr     bool
		wantCode    string
		wantMsgPart string
	}{
		// Valid queries — should pass
		{
			name:    "valid SELECT",
			sql:     "SELECT * FROM employees",
			wantErr: false,
		},
		{
			name:    "valid SELECT with WHERE",
			sql:     "SELECT id, name FROM employees WHERE active = 1",
			wantErr: false,
		},
		{
			name:    "valid WITH CTE",
			sql:     "WITH cte AS (SELECT 1 FROM dual) SELECT * FROM cte",
			wantErr: false,
		},
		{
			name:    "leading whitespace SELECT",
			sql:     "   \n\t SELECT id FROM t",
			wantErr: false,
		},
		{
			name:    "case insensitive select lowercase",
			sql:     "select * from t",
			wantErr: false,
		},
		{
			name:    "case insensitive select uppercase",
			sql:     "SELECT * FROM t",
			wantErr: false,
		},
		{
			name:    "case insensitive select mixed",
			sql:     "Select * From t",
			wantErr: false,
		},
		{
			name:    "SELECT with BEGIN in string literal",
			sql:     "SELECT 'BEGIN' FROM dual",
			wantErr: false,
		},
		{
			name:    "SELECT with FOR UPDATE in string literal",
			sql:     "SELECT 'FOR UPDATE' FROM dual",
			wantErr: false,
		},
		{
			name:    "SELECT with INTO in string literal",
			sql:     "SELECT 'INTO' FROM dual",
			wantErr: false,
		},
		{
			name:    "keyword in single-line comment is ignored",
			sql:     "SELECT id FROM t -- BEGIN ignored",
			wantErr: false,
		},
		{
			name:    "keyword in block comment is ignored",
			sql:     "SELECT id FROM t /* DECLARE ignored */",
			wantErr: false,
		},
		{
			name:    "SELECT with semicolon and only whitespace after",
			sql:     "SELECT * FROM t;   ",
			wantErr: false,
		},

		// sql_not_select errors
		{
			name:     "INSERT rejected",
			sql:      "INSERT INTO employees VALUES (1, 'Alice')",
			wantErr:  true,
			wantCode: "sql_not_select",
		},
		{
			name:     "UPDATE rejected",
			sql:      "UPDATE employees SET name = 'Bob' WHERE id = 1",
			wantErr:  true,
			wantCode: "sql_not_select",
		},
		{
			name:     "DELETE rejected",
			sql:      "DELETE FROM employees WHERE id = 1",
			wantErr:  true,
			wantCode: "sql_not_select",
		},
		{
			name:     "DROP rejected",
			sql:      "DROP TABLE employees",
			wantErr:  true,
			wantCode: "sql_not_select",
		},
		{
			name:     "MERGE rejected",
			sql:      "MERGE INTO t USING s ON (t.id=s.id) WHEN MATCHED THEN UPDATE SET t.v=s.v",
			wantErr:  true,
			wantCode: "sql_not_select",
		},

		// multi_statement errors
		{
			name:     "multi-statement SELECT then DELETE",
			sql:      "SELECT * FROM t; DELETE FROM t",
			wantErr:  true,
			wantCode: "multi_statement",
		},
		{
			name:     "multi-statement two SELECTs",
			sql:      "SELECT 1; SELECT 2",
			wantErr:  true,
			wantCode: "multi_statement",
		},

		// forbidden_keyword errors
		{
			name:        "SELECT INTO forbidden",
			sql:         "SELECT * INTO #tmp FROM employees",
			wantErr:     true,
			wantCode:    "forbidden_keyword",
			wantMsgPart: "SELECT INTO",
		},
		{
			name:        "FOR UPDATE forbidden",
			sql:         "SELECT * FROM employees FOR UPDATE",
			wantErr:     true,
			wantCode:    "forbidden_keyword",
			wantMsgPart: "FOR UPDATE",
		},
		{
			name:     "FOR update case insensitive",
			sql:      "SELECT * FROM t FOR update",
			wantErr:  true,
			wantCode: "forbidden_keyword",
		},
		{
			name:     "DECLARE forbidden",
			sql:      "DECLARE v NUMBER; BEGIN SELECT 1 FROM dual; END;",
			wantErr:  true,
			wantCode: "sql_not_select",
		},
		{
			name:        "standalone DECLARE after SELECT",
			sql:         "SELECT DECLARE FROM t",
			wantErr:     true,
			wantCode:    "forbidden_keyword",
			wantMsgPart: "DECLARE",
		},
		{
			name:     "BEGIN forbidden",
			sql:      "SELECT 1; BEGIN NULL; END;",
			wantErr:  true,
			wantCode: "multi_statement",
		},
		{
			name:        "standalone BEGIN after SELECT",
			sql:         "SELECT BEGIN FROM t",
			wantErr:     true,
			wantCode:    "forbidden_keyword",
			wantMsgPart: "BEGIN",
		},
		{
			name:     "CALL forbidden",
			sql:      "SELECT 1 FROM dual; CALL my_proc()",
			wantErr:  true,
			wantCode: "multi_statement",
		},
		{
			name:        "standalone CALL after SELECT",
			sql:         "SELECT CALL FROM t",
			wantErr:     true,
			wantCode:    "forbidden_keyword",
			wantMsgPart: "CALL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.sql)

			if tt.wantErr { //nolint:nestif // test assertion logic; nested ifs are intentional
				if err == nil {
					t.Fatalf("expected error but got nil")
				}

				ve := &ValidationError{}

				ok := errors.As(err, &ve)
				if !ok {
					t.Fatalf("expected *ValidationError, got %T: %v", err, err)
				}

				if tt.wantCode != "" && ve.Code != tt.wantCode {
					t.Errorf("code = %q, want %q (msg: %s)", ve.Code, tt.wantCode, ve.Message)
				}

				if tt.wantMsgPart != "" && !contains(ve.Message, tt.wantMsgPart) {
					t.Errorf("message %q does not contain %q", ve.Message, tt.wantMsgPart)
				}
			} else if err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}

			return false
		}())
}
