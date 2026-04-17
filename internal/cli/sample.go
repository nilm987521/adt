package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nilm987521/adt/internal/audit"
	"github.com/nilm987521/adt/internal/config"
	"github.com/nilm987521/adt/internal/keyring"
	"github.com/nilm987521/adt/internal/oracle"
	"github.com/nilm987521/adt/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var sampleCmd = &cobra.Command{
	Use:   "sample <table>",
	Short: "Randomly sample rows from a table",
	Args:  cobra.ExactArgs(1),
	Run:   runSample,
}

var sampleN int

func init() {
	RootCmd.AddCommand(sampleCmd)
	sampleCmd.Flags().IntVarP(&sampleN, "count", "n", 10, "number of rows to sample")
}

func runSample(cmd *cobra.Command, args []string) {
	tableArg := args[0]

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
		auditPath := cfg.Audit.LogPath
		if auditPath == "" {
			auditPath = config.DefaultAuditLogPath()
		}
		_ = audit.Write(auditPath, audit.Entry{
			Env:    envName,
			Cmd:    "sample",
			SQL:    fmt.Sprintf("SAMPLE %s", tableArg),
			Status: "rejected",
			Error:  "production_not_confirmed",
		})
		os.Exit(2)
	}

	// 4. Retrieve password from system keyring
	password, err := keyring.Get(envName)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "credential_not_found",
			Message: err.Error(),
		})
		os.Exit(1)
	}

	// 5. Resolve timeout: --timeout flag → env.Timeout → cfg.Defaults.Timeout → 30s
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

	// 6. Resolve effective n: min(sampleN, maxRows limit)
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

	effectiveN := sampleN
	if effectiveN > maxRows {
		effectiveN = maxRows
	}

	// 7. Parse table argument: SCHEMA.TABLE or TABLE (use env.User as schema)
	parts := strings.SplitN(strings.ToUpper(tableArg), ".", 2)
	var schema, table string
	if len(parts) == 2 {
		schema = parts[0]
		table = parts[1]
	} else {
		schema = strings.ToUpper(env.User)
		table = parts[0]
	}
	qualifiedTable := fmt.Sprintf("%s.%s", schema, table)

	// 8. Build random sample SQL using DBMS_RANDOM.VALUE (Oracle 11g compatible)
	sampleSQL := fmt.Sprintf(
		"SELECT * FROM (\n    SELECT * FROM %s\n    ORDER BY DBMS_RANDOM.VALUE\n) WHERE ROWNUM <= %d",
		qualifiedTable, effectiveN,
	)

	// 9. Connect to Oracle DB
	db, err := oracle.New(env.User, password, env.Host, env.Port, env.Service)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "db_connection_failed",
			Message: err.Error(),
		})
		os.Exit(3)
	}
	defer db.Close()

	// 10. Execute query with timeout context using RawQuery (no double ROWNUM wrap)
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	rows, _, err := db.RawQuery(ctx, sampleSQL)

	auditPath := cfg.Audit.LogPath
	if auditPath == "" {
		auditPath = config.DefaultAuditLogPath()
	}

	if err != nil {
		errMsg := err.Error()
		status := "db_error"
		errCode := "db_error"
		if ctx.Err() != nil {
			status = "timeout"
			errCode = "timeout"
		}
		oraCode := output.ExtractOracleCode(errMsg)
		_ = output.PrintJSON(output.ErrorOutput{
			Error:      errCode,
			Message:    errMsg,
			OracleCode: oraCode,
		})
		_ = audit.Write(auditPath, audit.Entry{
			Env:    envName,
			Cmd:    "sample",
			SQL:    sampleSQL,
			Status: status,
			Error:  errCode,
		})
		os.Exit(3)
	}

	elapsedMs := time.Since(start).Milliseconds()

	// 11. Output result
	serializedRows := output.SerializeRows(rows)
	out := map[string]interface{}{
		"env":        envName,
		"production": env.Production,
		"cmd":        "sample",
		"table":      qualifiedTable,
		"rows":       serializedRows,
		"row_count":  len(rows),
		"elapsed_ms": elapsedMs,
	}
	_ = output.PrintJSON(out)

	// 12. Write success audit entry
	_ = audit.Write(auditPath, audit.Entry{
		Env:       envName,
		Cmd:       "sample",
		SQL:       sampleSQL,
		Rows:      len(rows),
		ElapsedMs: elapsedMs,
		Status:    "ok",
	})
}
