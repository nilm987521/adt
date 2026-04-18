package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nilm987521/adt/internal/audit"
	"github.com/nilm987521/adt/internal/config"
	"github.com/nilm987521/adt/internal/dbfactory"
	"github.com/nilm987521/adt/internal/keyring"
	"github.com/nilm987521/adt/internal/output"
	"github.com/nilm987521/adt/internal/security"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var queryCmd = &cobra.Command{
	Use:   "query <sql>",
	Short: "Execute a SELECT query against Oracle",
	Args:  cobra.ExactArgs(1),
	Run:   runQuery,
}

func init() {
	RootCmd.AddCommand(queryCmd)
}

func runQuery(_ *cobra.Command, args []string) { //nolint:gocyclo,funlen // CLI command; complexity is inherent in sequential validation steps
	originalSQL := args[0]

	// 1. Load config — read flag values via viper (bound in root.go via BindPFlags)
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

	// 3. Production check — require --confirm flag for production environments
	confirmFlag := viper.GetBool("confirm")
	if env.RequireConfirmation && !confirmFlag {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "production_not_confirmed",
			Message: fmt.Sprintf("environment %q requires --confirm flag (production environment)", envName),
		})
		// Audit rejected execution
		auditPath := cfg.Audit.LogPath
		if auditPath == "" {
			auditPath = config.DefaultAuditLogPath()
		}

		_ = audit.Write(auditPath, audit.Entry{
			Env:    envName,
			Cmd:    "query",
			SQL:    originalSQL,
			Status: "rejected",
			Error:  "production_not_confirmed",
		})

		os.Exit(2)
	}

	// 4. Validate SQL (SELECT-only enforcement via security package)
	if err := security.Validate(originalSQL); err != nil {
		handleValidationError(err, originalSQL, envName, "query", cfg)
	}

	// 5. Dry-run: SQL has been validated — exit before touching credentials or DB
	if viper.GetBool("dry-run") {
		maskCols := env.MaskColumns
		if maskCols == nil {
			maskCols = []string{}
		}

		_ = output.PrintJSON(map[string]any{
			"dry_run":      true,
			"env":          envName,
			"production":   env.Production,
			"sql_valid":    true,
			"sql":          originalSQL,
			"mask_columns": maskCols,
			"message":      "SQL validated successfully, dry-run mode — not executed",
		})

		os.Exit(0)
	}

	// 6. Retrieve password from system keyring (sqlite uses empty password)
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

	// 7. Resolve maxRows: config defaults → env override → --limit flag
	maxRows := cfg.Defaults.MaxRows
	if maxRows == 0 {
		maxRows = 1000
	}

	if env.MaxRows > 0 {
		maxRows = env.MaxRows
	}

	if limitFlag := viper.GetInt("limit"); limitFlag > 0 {
		maxRows = limitFlag
	}

	// 8. Resolve queryTimeout: config defaults → env override → --timeout flag
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

	// 9. Connect to Oracle DB
	db, err := dbfactory.NewDriver(env, password)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "db_connection_failed",
			Message: err.Error(),
		})

		os.Exit(3)
	}
	defer db.Close() //nolint:errcheck // cleanup; error not actionable

	// 10. Execute query with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	result, err := db.Query(ctx, originalSQL, maxRows)

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
			Cmd:    "query",
			SQL:    originalSQL,
			Status: status,
			Error:  errCode,
		})

		os.Exit(3) //nolint:gocritic // exitAfterDefer: intentional; cancel() not needed after fatal DB error
	}

	// 11. Serialize rows to JSON-safe types, apply masking, and output result
	serializedRows := output.SerializeRows(result.Rows)
	serializedRows = output.MaskRows(serializedRows, env.MaskColumns)
	out := output.QueryOutput{
		Env:         envName,
		Production:  env.Production,
		Cmd:         "query",
		Rows:        serializedRows,
		RowCount:    result.RowCount,
		Truncated:   result.Truncated,
		ElapsedMs:   result.ElapsedMs,
		ExecutedSQL: result.ExecutedSQL,
	}
	_ = output.PrintJSON(out)

	// 12. Write success audit entry
	_ = audit.Write(auditPath, audit.Entry{
		Env:       envName,
		Cmd:       "query",
		SQL:       originalSQL,
		Rows:      result.RowCount,
		ElapsedMs: result.ElapsedMs,
		Status:    "ok",
	})
}
