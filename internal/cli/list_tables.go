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

var listTablesCmd = &cobra.Command{
	Use:   "list-tables",
	Short: "List tables in the database",
	Run:   runListTables,
}

var schemaFlag string

func init() {
	RootCmd.AddCommand(listTablesCmd)
	listTablesCmd.Flags().StringVar(&schemaFlag, "schema", "", "schema/owner name (default: current user)")
}

// tableRow is the JSON representation of a single table entry.
type tableRow struct {
	Name         string  `json:"name"`
	NumRows      *int64  `json:"num_rows"`
	LastAnalyzed *string `json:"last_analyzed"`
}

// listTablesOutput is the JSON output structure for list-tables.
type listTablesOutput struct {
	Env        string     `json:"env"`
	Production bool       `json:"production"`
	Cmd        string     `json:"cmd"`
	Schema     string     `json:"schema"`
	Tables     []tableRow `json:"tables"`
	TableCount int        `json:"table_count"`
}

func runListTables(_ *cobra.Command, _ []string) { //nolint:gocyclo,funlen // CLI command; complexity is inherent in sequential validation steps
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

	// 6. Build SQL query
	schema := schemaFlag

	var sql string

	if schema == "" {
		schema = env.User
		sql = "SELECT TABLE_NAME, NUM_ROWS, LAST_ANALYZED FROM USER_TABLES ORDER BY TABLE_NAME"
	} else {
		sql = fmt.Sprintf(
			"SELECT TABLE_NAME, NUM_ROWS, LAST_ANALYZED FROM ALL_TABLES WHERE OWNER = UPPER('%s') ORDER BY TABLE_NAME",
			strings.ToUpper(schema),
		)
	}

	schemaUpper := strings.ToUpper(schema)

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

	result, err := db.Query(ctx, sql, 5000)

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
			Cmd:    "list-tables",
			SQL:    sql,
			Status: status,
			Error:  errCode,
		})

		os.Exit(3) //nolint:gocritic // exitAfterDefer: intentional; cancel() not needed after fatal DB error
	}

	// 9. Apply masking then map result rows to typed structs
	maskedRawRows := output.MaskRows(result.Rows, env.MaskColumns)

	tables := make([]tableRow, 0, len(maskedRawRows))
	for _, row := range maskedRawRows {
		tr := tableRow{}

		if v, ok := row["TABLE_NAME"]; ok && v != nil {
			if s, ok := v.(string); ok {
				tr.Name = s
			}
		}

		if v, ok := row["NUM_ROWS"]; ok && v != nil {
			switch n := v.(type) {
			case int64:
				tr.NumRows = &n
			case float64:
				i := int64(n)
				tr.NumRows = &i
			}
		}

		if v, ok := row["LAST_ANALYZED"]; ok && v != nil {
			switch t := v.(type) {
			case time.Time:
				s := t.Format(time.RFC3339)
				tr.LastAnalyzed = &s
			case string:
				tr.LastAnalyzed = &t
			}
		}

		tables = append(tables, tr)
	}

	// 10. Output result
	format := viper.GetString("format")
	if format == "table" {
		headers := []string{"TABLE_NAME", "NUM_ROWS", "LAST_ANALYZED"}

		rows := make([][]string, len(tables))
		for i, t := range tables {
			numRowsStr := "NULL"
			if t.NumRows != nil {
				numRowsStr = strconv.FormatInt(*t.NumRows, 10)
			}

			lastAnalyzedStr := "NULL"
			if t.LastAnalyzed != nil {
				lastAnalyzedStr = *t.LastAnalyzed
			}

			rows[i] = []string{t.Name, numRowsStr, lastAnalyzedStr}
		}

		_ = output.PrintTable(headers, rows)
	} else {
		out := listTablesOutput{
			Env:        envName,
			Production: env.Production,
			Cmd:        "list-tables",
			Schema:     schemaUpper,
			Tables:     tables,
			TableCount: len(tables),
		}
		_ = output.PrintJSON(out)
	}

	// 11. Write success audit entry
	_ = audit.Write(auditPath, audit.Entry{
		Env:       envName,
		Cmd:       "list-tables",
		SQL:       sql,
		Rows:      result.RowCount,
		ElapsedMs: result.ElapsedMs,
		Status:    "ok",
	})
}
