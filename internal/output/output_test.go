package output

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

const redacted = "[REDACTED]"

// captureStdout redirects os.Stdout during f() and returns the captured output.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	old := os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	os.Stdout = w

	f()
	w.Close() //nolint:errcheck,gosec // pipe write end; error not actionable

	os.Stdout = old

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}

// --- SerializeRows ---

func TestSerializeRows_TimeToRFC3339(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	rows := []map[string]any{{"ts": ts}}
	result := SerializeRows(rows)

	got, ok := result[0]["ts"].(string)
	if !ok {
		t.Fatal("expected string for time.Time value")
	}

	if !strings.HasPrefix(got, "2026-04-17") {
		t.Errorf("unexpected RFC3339: %s", got)
	}
}

func TestSerializeRows_BlobToBase64(t *testing.T) {
	t.Parallel()

	data := []byte("hello world")
	rows := []map[string]any{{"blob": data}}
	result := SerializeRows(rows)

	got, ok := result[0]["blob"].(string)
	if !ok {
		t.Fatal("expected string for []byte value")
	}

	decoded, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("base64 decode error: %v", err)
	}

	if string(decoded) != "hello world" {
		t.Errorf("decoded blob = %q, want %q", string(decoded), "hello world")
	}
}

func TestSerializeRows_NilPassthrough(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"field": nil}}

	result := SerializeRows(rows)
	if result[0]["field"] != nil {
		t.Errorf("nil value should remain nil, got %v", result[0]["field"])
	}
}

func TestSerializeRows_NilInput(t *testing.T) {
	t.Parallel()

	result := SerializeRows(nil)
	if result == nil {
		t.Fatal("SerializeRows(nil) returned nil, want empty slice")
	}

	if len(result) != 0 {
		t.Errorf("SerializeRows(nil) returned len=%d, want 0", len(result))
	}
}

func TestSerializeRows_StringPassthrough(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"name": "Alice"}}
	result := SerializeRows(rows)

	got, ok := result[0]["name"].(string)
	if !ok {
		t.Fatal("expected string")
	}

	if got != "Alice" {
		t.Errorf("got %q, want Alice", got)
	}
}

func TestSerializeRows_IntPassthrough(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"count": 42}}
	result := SerializeRows(rows)

	got, ok := result[0]["count"].(int)
	if !ok {
		t.Fatalf("expected int, got %T", result[0]["count"])
	}

	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestSerializeRows_MultipleRows(t *testing.T) {
	t.Parallel()

	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC)
	rows := []map[string]any{
		{"ts": ts1, "name": "first"},
		{"ts": ts2, "name": "second"},
	}

	result := SerializeRows(rows)
	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}

	ts0, _ := result[0]["ts"].(string)
	if !strings.HasPrefix(ts0, "2026-01-01") {
		t.Errorf("row 0 ts = %v", result[0]["ts"])
	}

	ts1Str, _ := result[1]["ts"].(string)
	if !strings.HasPrefix(ts1Str, "2026-06-15") {
		t.Errorf("row 1 ts = %v", result[1]["ts"])
	}
}

func TestSerializeRows_EmptySlice(t *testing.T) {
	t.Parallel()

	result := SerializeRows([]map[string]any{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d rows", len(result))
	}
}

// --- QueryResultToTable ---

func TestQueryResultToTable_Deterministic(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"z_col": "z", "a_col": "a", "m_col": "m"},
	}

	headers, tableRows := QueryResultToTable(rows)
	if len(headers) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(headers))
	}

	if headers[0] != "a_col" || headers[1] != "m_col" || headers[2] != "z_col" {
		t.Errorf("headers not sorted alphabetically: %v", headers)
	}

	if len(tableRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(tableRows))
	}

	if tableRows[0][0] != "a" || tableRows[0][1] != "m" || tableRows[0][2] != "z" {
		t.Errorf("values in wrong order: %v", tableRows[0])
	}
}

func TestQueryResultToTable_NilValues(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"col": nil},
	}

	headers, tableRows := QueryResultToTable(rows)
	if len(headers) != 1 {
		t.Fatalf("expected 1 header, got %d", len(headers))
	}

	if tableRows[0][0] != "NULL" {
		t.Errorf("nil value should render as NULL, got %q", tableRows[0][0])
	}
}

func TestQueryResultToTable_Empty(t *testing.T) {
	t.Parallel()

	headers, tableRows := QueryResultToTable([]map[string]any{})
	if headers != nil {
		t.Errorf("expected nil headers for empty input, got %v", headers)
	}

	if tableRows != nil {
		t.Errorf("expected nil tableRows for empty input, got %v", tableRows)
	}
}

func TestQueryResultToTable_NilInput(t *testing.T) {
	t.Parallel()

	headers, tableRows := QueryResultToTable(nil)
	if headers != nil {
		t.Errorf("expected nil headers for nil input, got %v", headers)
	}

	if tableRows != nil {
		t.Errorf("expected nil tableRows for nil input, got %v", tableRows)
	}
}

func TestQueryResultToTable_TimeValue(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	rows := []map[string]any{{"ts": ts}}

	headers, tableRows := QueryResultToTable(rows)
	if headers[0] != "ts" {
		t.Errorf("header = %q, want ts", headers[0])
	}

	if !strings.HasPrefix(tableRows[0][0], "2026-04-17") {
		t.Errorf("time value = %q, expected RFC3339 with 2026-04-17 prefix", tableRows[0][0])
	}
}

func TestQueryResultToTable_BlobValue(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"data": []byte("binary")}}

	_, tableRows := QueryResultToTable(rows)
	if tableRows[0][0] != "<BLOB>" {
		t.Errorf("blob value = %q, want <BLOB>", tableRows[0][0])
	}
}

func TestQueryResultToTable_MultipleRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	}

	headers, tableRows := QueryResultToTable(rows)
	if len(headers) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(headers))
	}

	if len(tableRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tableRows))
	}
	// headers sorted: id, name
	if headers[0] != "id" || headers[1] != "name" {
		t.Errorf("headers = %v, expected [id name]", headers)
	}
}

// --- PrintCSV ---

func TestPrintCSV_ProperQuoting(t *testing.T) { //nolint:paralleltest // captureStdout mutates os.Stdout (global); parallel would cause data race
	output := captureStdout(t, func() {
		headers := []string{"name", "value"}

		rows := [][]string{
			{"Alice", "hello, world"},
			{"Bob", `say "hi"`},
		}
		if err := PrintCSV(headers, rows); err != nil {
			t.Errorf("PrintCSV error: %v", err)
		}
	})
	if !strings.Contains(output, `"hello, world"`) {
		t.Errorf("CSV did not quote field with comma: %s", output)
	}

	if !strings.Contains(output, `"say ""hi"""`) {
		t.Errorf("CSV did not quote field with double-quote: %s", output)
	}
}

func TestPrintCSV_HeadersWritten(t *testing.T) { //nolint:paralleltest // captureStdout mutates os.Stdout (global); parallel would cause data race
	output := captureStdout(t, func() {
		_ = PrintCSV([]string{"col_a", "col_b"}, [][]string{{"v1", "v2"}})
	})
	if !strings.Contains(output, "col_a") || !strings.Contains(output, "col_b") {
		t.Errorf("headers not found in CSV output: %s", output)
	}
}

func TestPrintCSV_EmptyRows(t *testing.T) { //nolint:paralleltest // captureStdout mutates os.Stdout (global); parallel would cause data race
	output := captureStdout(t, func() {
		_ = PrintCSV([]string{"h1", "h2"}, [][]string{})
	})
	if !strings.Contains(output, "h1") {
		t.Errorf("headers should still appear with no data rows: %s", output)
	}
}

// --- PrintTable ---

func TestPrintTable_ContainsHeaders(t *testing.T) { //nolint:paralleltest // captureStdout mutates os.Stdout (global); parallel would cause data race
	output := captureStdout(t, func() {
		_ = PrintTable([]string{"NAME", "VALUE"}, [][]string{{"Alice", "42"}})
	})
	if !strings.Contains(output, "NAME") || !strings.Contains(output, "VALUE") {
		t.Errorf("table output missing headers: %s", output)
	}

	if !strings.Contains(output, "Alice") {
		t.Errorf("table output missing data: %s", output)
	}
}

func TestPrintTable_ContainsSeparator(t *testing.T) { //nolint:paralleltest // captureStdout mutates os.Stdout (global); parallel would cause data race
	output := captureStdout(t, func() {
		_ = PrintTable([]string{"NAME"}, [][]string{{"Alice"}})
	})
	if !strings.Contains(output, "----") {
		t.Errorf("table output missing separator dashes: %s", output)
	}
}

// --- MaskRows ---

func TestMaskRows_CaseInsensitiveMaskColsUppercase(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"email": "alice@example.com", "name": "Alice"}}
	result := MaskRows(rows, []string{"EMAIL"})

	if result[0]["email"] != redacted {
		t.Errorf("email should be [REDACTED], got %v", result[0]["email"])
	}

	if result[0]["name"] != "Alice" {
		t.Errorf("name should be unchanged, got %v", result[0]["name"])
	}
}

func TestMaskRows_CaseInsensitiveMaskColsLowercase(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"EMAIL": "alice@example.com", "name": "Alice"}}
	result := MaskRows(rows, []string{"EMAIL"})

	if result[0]["EMAIL"] != redacted {
		t.Errorf("EMAIL should be [REDACTED], got %v", result[0]["EMAIL"])
	}
}

func TestMaskRows_EmptyMaskCols(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"email": "alice@example.com", "age": 30}}
	result := MaskRows(rows, []string{})

	if result[0]["email"] != "alice@example.com" {
		t.Errorf("email should be unchanged with empty maskCols, got %v", result[0]["email"])
	}

	if result[0]["age"] != 30 {
		t.Errorf("age should be unchanged, got %v", result[0]["age"])
	}
}

func TestMaskRows_NilMaskCols(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"email": "alice@example.com"}}
	result := MaskRows(rows, nil)

	if result[0]["email"] != "alice@example.com" {
		t.Errorf("email should be unchanged with nil maskCols, got %v", result[0]["email"])
	}
}

func TestMaskRows_MultipleColumns(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"email": "alice@example.com", "phone": "555-1234", "name": "Alice"}}
	result := MaskRows(rows, []string{"EMAIL", "PHONE"})

	if result[0]["email"] != redacted {
		t.Errorf("email should be [REDACTED], got %v", result[0]["email"])
	}

	if result[0]["phone"] != redacted {
		t.Errorf("phone should be [REDACTED], got %v", result[0]["phone"])
	}

	if result[0]["name"] != "Alice" {
		t.Errorf("name should be unchanged, got %v", result[0]["name"])
	}
}

func TestMaskRows_NilValueMasked(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"email": nil, "name": "Alice"}}
	result := MaskRows(rows, []string{"EMAIL"})

	if result[0]["email"] != redacted {
		t.Errorf("nil email should become [REDACTED], got %v", result[0]["email"])
	}
}

func TestMaskRows_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := map[string]any{"email": "alice@example.com", "name": "Alice"}
	rows := []map[string]any{original}
	_ = MaskRows(rows, []string{"EMAIL"})

	// original map must not be mutated
	if original["email"] != "alice@example.com" {
		t.Errorf("original map was mutated: email = %v", original["email"])
	}
}

func TestMaskRows_DoesNotMutateInputSlice(t *testing.T) {
	t.Parallel()

	row := map[string]any{"email": "alice@example.com"}
	rows := []map[string]any{row}
	result := MaskRows(rows, []string{"EMAIL"})

	// result slice is distinct from input
	if &rows[0] == &result[0] {
		t.Error("result slice shares pointer with input slice")
	}
}

func TestMaskRows_EmptyInput(t *testing.T) {
	t.Parallel()

	result := MaskRows([]map[string]any{}, []string{"EMAIL"})
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d rows", len(result))
	}
}

func TestMaskRows_MultipleRows(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"email": "alice@example.com", "id": 1},
		{"email": "bob@example.com", "id": 2},
	}
	result := MaskRows(rows, []string{"EMAIL"})

	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}

	if result[0]["email"] != redacted || result[1]["email"] != redacted {
		t.Errorf("email in both rows should be [REDACTED], got %v, %v", result[0]["email"], result[1]["email"])
	}

	if result[0]["id"] != 1 || result[1]["id"] != 2 {
		t.Errorf("id values should be unchanged")
	}
}
