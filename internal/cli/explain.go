package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nilm987521/adt/internal/audit"
	"github.com/nilm987521/adt/internal/config"
	"github.com/nilm987521/adt/internal/keyring"
	"github.com/nilm987521/adt/internal/oracle"
	"github.com/nilm987521/adt/internal/output"
	"github.com/nilm987521/adt/internal/security"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var explainCmd = &cobra.Command{
	Use:   "explain <sql>",
	Short: "Show execution plan for a SELECT query (does not execute the query)",
	Args:  cobra.ExactArgs(1),
	Run:   runExplain,
}

func init() {
	RootCmd.AddCommand(explainCmd)
}

func runExplain(_ *cobra.Command, args []string) { //nolint:gocyclo,funlen // CLI command; complexity is inherent in sequential validation steps
	originalSQL := args[0]

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
			Cmd:    "explain",
			SQL:    originalSQL,
			Status: "rejected",
			Error:  "production_not_confirmed",
		})

		os.Exit(2)
	}

	// 4. Validate SQL (SELECT-only enforcement)
	if err := security.Validate(originalSQL); err != nil {
		handleValidationError(err, originalSQL, envName, "explain", cfg)
	}

	// 5. Retrieve password from system keyring
	password, err := keyring.Get(envName)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "credential_not_found",
			Message: err.Error(),
		})

		os.Exit(1)
	}

	// 6. Resolve timeout: --timeout flag → env.Timeout → cfg.Defaults.Timeout → 30s
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

	// 7. Connect to Oracle DB
	db, err := oracle.New(env.User, password, env.Host, env.Port, env.Service)
	if err != nil {
		_ = output.PrintJSON(output.ErrorOutput{
			Error:   "db_connection_failed",
			Message: err.Error(),
		})

		os.Exit(3)
	}
	defer db.Close() //nolint:errcheck // cleanup; error not actionable

	// 8. Execute EXPLAIN PLAN with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	planLines, err := db.ExplainPlan(ctx, originalSQL)

	// Audit SQL is the EXPLAIN PLAN form
	auditSQL := "EXPLAIN PLAN FOR " + originalSQL

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
			Cmd:    "explain",
			SQL:    auditSQL,
			Status: status,
			Error:  errCode,
		})

		os.Exit(3) //nolint:gocritic // exitAfterDefer: intentional; cancel() not needed after fatal DB error
	}

	// 9. Output result
	format := viper.GetString("format")
	if format == "table" || format == "csv" {
		for _, line := range planLines {
			fmt.Println(line)
		}
	} else {
		out := map[string]any{
			"env":          envName,
			"production":   env.Production,
			"cmd":          "explain",
			"original_sql": originalSQL,
			"plan":         planLines,
			"plan_lines":   len(planLines),
		}
		_ = output.PrintJSON(out)
	}

	// 10. Write success audit entry
	_ = audit.Write(auditPath, audit.Entry{
		Env:    envName,
		Cmd:    "explain",
		SQL:    auditSQL,
		Status: "ok",
	})
}
