// Package cli provides CLI commands for adt.
package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nilm987521/adt/internal/audit"
	"github.com/nilm987521/adt/internal/config"
	"github.com/nilm987521/adt/internal/dbfactory"
	"github.com/nilm987521/adt/internal/keyring"
	"github.com/nilm987521/adt/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var describeCmd = &cobra.Command{
	Use:   "describe <table>",
	Short: "Show the structure of a database table",
	Args:  cobra.ExactArgs(1),
	Run:   runDescribe,
}

func init() {
	RootCmd.AddCommand(describeCmd)
}

// columnRow is the JSON representation of a single column entry.
type columnRow struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Length    int64  `json:"length"`
	Precision any    `json:"precision"`
	Scale     any    `json:"scale"`
	Nullable  bool   `json:"nullable"`
	Default   any    `json:"default"`
}

// describeOutput is the JSON output structure for describe.
type describeOutput struct {
	Env         string      `json:"env"`
	Production  bool        `json:"production"`
	Cmd         string      `json:"cmd"`
	Table       string      `json:"table"`
	Schema      string      `json:"schema"`
	Columns     []columnRow `json:"columns"`
	ColumnCount int         `json:"column_count"`
}

// parseTableArg splits "SCHEMA.TABLE" or "TABLE" into (schema, table).
func parseTableArg(arg, defaultSchema string) (schema, table string) {
	parts := strings.SplitN(strings.ToUpper(arg), ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}

	return strings.ToUpper(defaultSchema), strings.ToUpper(arg)
}

const (
	statusDBError = "db_error"
	statusTimeout = "timeout"
	formatTable   = "table"
	nullStr       = "NULL"
)

func runDescribe(_ *cobra.Command, args []string) { //nolint:gocyclo,funlen // CLI command; complexity is inherent in sequential validation steps
	// 1. Load config
	cfgPath := viper.GetString("config")
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "config_error",
			Message: err.Error(),
		})

		os.Exit(1)
	}

	// 2. Resolve environment name
	envName := viper.GetString("env")
	if envName == "" {
		envName = cfg.DefaultEnv
	}

	env, err := cfg.GetEnv(envName)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "env_not_found",
			Message: fmt.Sprintf("environment %q not found", envName),
		})

		os.Exit(1)
	}

	// 3. Production confirmation check
	if env.RequireConfirmation && !viper.GetBool("confirm") {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "production_not_confirmed",
			Message: fmt.Sprintf("environment %q requires --confirm flag (production environment)", envName),
		})

		os.Exit(2)
	}

	// 4. Retrieve password from system keyring (sqlite uses empty password)
	var password string
	if env.EffectiveDriver() != "sqlite" {
		password, err = keyring.Get(envName)
		if err != nil {
			_ = output.PrintJSON(output.ErrorOutput{
				Error:   "credential_not_found",
				Message: err.Error(),
			})

			os.Exit(1)
		}
	}

	// 5. Resolve timeout
	queryTimeout := cfg.Defaults.Timeout
	if queryTimeout == 0 {
		queryTimeout = 30 * time.Second
	}

	if env.Timeout > 0 {
		queryTimeout = env.Timeout
	}

	if timeoutFlag := viper.GetString("timeout"); timeoutFlag != "" {
		if d, parseErr := time.ParseDuration(timeoutFlag); parseErr == nil {
			queryTimeout = d
		}
	}

	// 6. Parse table argument and build SQL
	schema, tableName := parseTableArg(args[0], env.User)
	sql := fmt.Sprintf(
		"SELECT COLUMN_ID, COLUMN_NAME, DATA_TYPE, DATA_LENGTH, DATA_PRECISION, DATA_SCALE, NULLABLE, DATA_DEFAULT "+
			"FROM ALL_TAB_COLUMNS "+
			"WHERE OWNER = UPPER('%s') AND TABLE_NAME = UPPER('%s') "+
			"ORDER BY COLUMN_ID",
		schema, tableName,
	)

	// 7. Connect to Oracle DB
	db, err := dbfactory.NewDriver(env, password)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "db_connection_failed",
			Message: err.Error(),
		})

		os.Exit(3)
	}
	defer db.Close() //nolint:errcheck // cleanup; error not actionable

	// 8. Execute query with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	result, err := db.Query(ctx, sql, 1000)

	auditPath := cfg.Audit.LogPath
	if auditPath == "" {
		auditPath = config.DefaultAuditLogPath()
	}

	if err != nil {
		errMsg := err.Error()
		status := statusDBError
		errCode := statusDBError

		if ctx.Err() != nil {
			status = statusTimeout
			errCode = statusTimeout
		}

		oraCode := output.ExtractOracleCode(errMsg)
		_ = output.PrintJSON(output.ErrorOutput{
			Error:      errCode,
			Message:    errMsg,
			OracleCode: oraCode,
		})
		_ = audit.Write(auditPath, audit.Entry{
			Env:    envName,
			Cmd:    "describe",
			SQL:    sql,
			Status: status,
			Error:  errCode,
		})

		os.Exit(3) //nolint:gocritic // exitAfterDefer: intentional; cancel() not needed after fatal DB error
	}

	// 9. Apply masking then map result rows to typed structs
	maskedRawRows := output.MaskRows(result.Rows, env.MaskColumns)

	columns := make([]columnRow, 0, len(maskedRawRows))
	for _, row := range maskedRawRows {
		cr := columnRow{}

		if v, ok := row["COLUMN_ID"]; ok && v != nil {
			switch n := v.(type) {
			case int64:
				cr.ID = int(n)
			case float64:
				cr.ID = int(n)
			case int:
				cr.ID = n
			}
		}

		if v, ok := row["COLUMN_NAME"]; ok && v != nil {
			if s, ok := v.(string); ok {
				cr.Name = s
			}
		}

		if v, ok := row["DATA_TYPE"]; ok && v != nil {
			if s, ok := v.(string); ok {
				cr.Type = s
			}
		}

		if v, ok := row["DATA_LENGTH"]; ok && v != nil {
			switch n := v.(type) {
			case int64:
				cr.Length = n
			case float64:
				cr.Length = int64(n)
			case int:
				cr.Length = int64(n)
			}
		}

		if v, ok := row["DATA_PRECISION"]; ok && v != nil {
			switch n := v.(type) {
			case int64:
				cr.Precision = n
			case float64:
				cr.Precision = int64(n)
			case int:
				cr.Precision = n
			}
		} else {
			cr.Precision = nil
		}

		if v, ok := row["DATA_SCALE"]; ok && v != nil {
			switch n := v.(type) {
			case int64:
				cr.Scale = n
			case float64:
				cr.Scale = int64(n)
			case int:
				cr.Scale = n
			}
		} else {
			cr.Scale = nil
		}

		if v, ok := row["NULLABLE"]; ok && v != nil {
			if s, ok := v.(string); ok {
				cr.Nullable = s != "N"
			}
		}

		if v, ok := row["DATA_DEFAULT"]; ok && v != nil {
			switch d := v.(type) {
			case string:
				cr.Default = d
			case []byte:
				cr.Default = string(d)
			default:
				cr.Default = v
			}
		} else {
			cr.Default = nil
		}

		columns = append(columns, cr)
	}

	// 10. Output result
	format := viper.GetString("format")
	if format == formatTable { //nolint:nestif // output formatting block; nesting is intentional
		headers := []string{"ID", "NAME", "TYPE", "LENGTH", "PREC", "SCALE", "NULLABLE", "DEFAULT"}

		rows := make([][]string, len(columns))
		for i, c := range columns {
			precStr := nullStr
			if c.Precision != nil {
				precStr = fmt.Sprintf("%v", c.Precision)
			}

			scaleStr := nullStr
			if c.Scale != nil {
				scaleStr = fmt.Sprintf("%v", c.Scale)
			}

			defaultStr := nullStr
			if c.Default != nil {
				defaultStr = fmt.Sprintf("%v", c.Default)
			}

			nullableStr := "Y"
			if !c.Nullable {
				nullableStr = "N"
			}

			rows[i] = []string{
				strconv.Itoa(c.ID),
				c.Name,
				c.Type,
				strconv.FormatInt(c.Length, 10),
				precStr,
				scaleStr,
				nullableStr,
				defaultStr,
			}
		}

		_ = output.PrintTable(headers, rows)
	} else {
		out := describeOutput{
			Env:         envName,
			Production:  env.Production,
			Cmd:         "describe",
			Table:       tableName,
			Schema:      schema,
			Columns:     columns,
			ColumnCount: len(columns),
		}
		_ = output.PrintJSON(out)
	}

	// 11. Write success audit entry
	_ = audit.Write(auditPath, audit.Entry{
		Env:       envName,
		Cmd:       "describe",
		SQL:       sql,
		Rows:      result.RowCount,
		ElapsedMs: result.ElapsedMs,
		Status:    "ok",
	})
}
