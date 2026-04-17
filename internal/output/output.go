package output

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// QueryOutput is the JSON structure for a successful query.
type QueryOutput struct {
	Env         string                   `json:"env"`
	Production  bool                     `json:"production"`
	Cmd         string                   `json:"cmd"`
	Rows        []map[string]interface{} `json:"rows"`
	RowCount    int                      `json:"row_count"`
	Truncated   bool                     `json:"truncated"`
	ElapsedMs   int64                    `json:"elapsed_ms"`
	ExecutedSQL string                   `json:"executed_sql"`
}

// ErrorOutput is the JSON structure for errors.
type ErrorOutput struct {
	Error       string `json:"error"`
	Message     string `json:"message"`
	OriginalSQL string `json:"original_sql,omitempty"`
	OracleCode  string `json:"oracle_code,omitempty"`
}

// PrintJSON prints v as indented JSON to stdout.
func PrintJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// PrintTable prints headers and rows as a simple text table using tabwriter.
// Separator dashes reflect the actual max column width across headers and data.
func PrintTable(headers []string, rows [][]string) error {
	// Compute max width per column (headers and data)
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Header
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Separator using actual max widths
	seps := make([]string, len(headers))
	for i, width := range widths {
		seps[i] = strings.Repeat("-", width)
	}
	fmt.Fprintln(w, strings.Join(seps, "\t"))

	// Data rows
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

// PrintCSV prints headers and rows in RFC 4180 CSV format to stdout.
func PrintCSV(headers []string, rows [][]string) error {
	w := csv.NewWriter(os.Stdout)
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// QueryResultToTable converts a slice of map-based rows (from oracle.QueryResult.Rows)
// into ordered headers and string rows suitable for PrintTable/PrintCSV.
// Column order is sorted alphabetically for determinism.
func QueryResultToTable(rows []map[string]interface{}) (headers []string, tableRows [][]string) {
	if len(rows) == 0 {
		return nil, nil
	}

	// Collect and sort column names from first row
	for k := range rows[0] {
		headers = append(headers, k)
	}
	sort.Strings(headers)

	// Convert each row to a string slice in the same column order
	tableRows = make([][]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(headers))
		for j, h := range headers {
			cells[j] = valueToString(row[h])
		}
		tableRows[i] = cells
	}
	return headers, tableRows
}

// valueToString converts an interface{} oracle value to a display string.
func valueToString(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return "<BLOB>"
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// Print dispatches output to the appropriate formatter based on format string.
// For JSON: data is marshalled directly.
// For table/csv: headers and rows are used.
// If headers is nil (e.g., called from JSON-only context), falls back to JSON.
func Print(format string, jsonData interface{}, headers []string, rows [][]string) error {
	switch format {
	case "table":
		if headers == nil {
			return PrintJSON(jsonData)
		}
		return PrintTable(headers, rows)
	case "csv":
		if headers == nil {
			return PrintJSON(jsonData)
		}
		return PrintCSV(headers, rows)
	default: // "json" or anything else
		return PrintJSON(jsonData)
	}
}

// SerializeRows converts raw Oracle scan values to JSON-safe types:
//   - time.Time → RFC3339 string
//   - []byte    → base64 string
//   - nil       → nil (JSON null)
//   - others    → as-is
func SerializeRows(rows []map[string]interface{}) []map[string]interface{} {
	if rows == nil {
		return []map[string]interface{}{}
	}
	result := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		newRow := make(map[string]interface{}, len(row))
		for k, v := range row {
			switch val := v.(type) {
			case time.Time:
				newRow[k] = val.Format(time.RFC3339)
			case []byte:
				newRow[k] = base64.StdEncoding.EncodeToString(val)
			case nil:
				newRow[k] = nil
			default:
				newRow[k] = val
			}
		}
		result[i] = newRow
	}
	return result
}

// ExtractOracleCode extracts "ORA-NNNNN" from an error message.
func ExtractOracleCode(errMsg string) string {
	re := regexp.MustCompile(`ORA-\d+`)
	return re.FindString(errMsg)
}
