package security

import (
	"regexp"
	"strings"
)

// ValidationError represents a SQL validation failure with a code and message.
type ValidationError struct {
	Code    string // "sql_not_select", "multi_statement", "forbidden_keyword"
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// precompiled regexps
var (
	reLineComment   = regexp.MustCompile(`--[^\n]*`)
	reBlockComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reStringLiteral = regexp.MustCompile(`'(?:''|[^'])*'`)

	reWordINTO      = regexp.MustCompile(`(?i)\bINTO\b`)
	reForUpdate     = regexp.MustCompile(`(?i)\bFOR\s+UPDATE\b`)
	reWordBEGIN     = regexp.MustCompile(`(?i)\bBEGIN\b`)
	reDECLARE       = regexp.MustCompile(`(?i)\bDECLARE\b`)
	reCALL          = regexp.MustCompile(`(?i)\bCALL\b`)

	reSemicolon = regexp.MustCompile(`;\s*\S`)
)

// preprocess removes comments and string literals from sql to avoid false
// positives when scanning for forbidden keywords.
func preprocess(sql string) string {
	// 1. Strip single-line comments
	sql = reLineComment.ReplaceAllString(sql, "")
	// 2. Strip multi-line comments
	sql = reBlockComment.ReplaceAllString(sql, "")
	// 3. Replace string literals with empty placeholder so keywords inside
	//    strings don't trigger validation rules.
	sql = reStringLiteral.ReplaceAllString(sql, "''")
	return sql
}

// firstToken returns the first whitespace-delimited token from s (uppercased).
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	idx := strings.IndexAny(s, " \t\r\n")
	if idx == -1 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(s[:idx])
}

// Validate checks if sql is a safe read-only query.
// Returns *ValidationError on rejection, nil on pass.
func Validate(sql string) error {
	processed := preprocess(sql)

	// 4. First token must be SELECT or WITH
	tok := firstToken(processed)
	if tok != "SELECT" && tok != "WITH" {
		return &ValidationError{
			Code:    "sql_not_select",
			Message: "only SELECT or WITH queries are allowed",
		}
	}

	// 5. Multi-statement: semicolon followed by non-whitespace
	if reSemicolon.MatchString(processed) {
		return &ValidationError{
			Code:    "multi_statement",
			Message: "multiple statements are not allowed",
		}
	}

	// 6. INTO as whole word
	if reWordINTO.MatchString(processed) {
		return &ValidationError{
			Code:    "forbidden_keyword",
			Message: "SELECT INTO is not allowed",
		}
	}

	// 7. FOR UPDATE
	if reForUpdate.MatchString(processed) {
		return &ValidationError{
			Code:    "forbidden_keyword",
			Message: "FOR UPDATE is not allowed",
		}
	}

	// 8. PL/SQL blocks
	if reWordBEGIN.MatchString(processed) {
		return &ValidationError{
			Code:    "forbidden_keyword",
			Message: "PL/SQL blocks are not allowed (found: BEGIN)",
		}
	}
	if reDECLARE.MatchString(processed) {
		return &ValidationError{
			Code:    "forbidden_keyword",
			Message: "PL/SQL blocks are not allowed (found: DECLARE)",
		}
	}
	if reCALL.MatchString(processed) {
		return &ValidationError{
			Code:    "forbidden_keyword",
			Message: "PL/SQL blocks are not allowed (found: CALL)",
		}
	}

	return nil
}
