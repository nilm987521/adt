package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// RootCmd is the root cobra command for adt.
var RootCmd = &cobra.Command{
	Use:   "adt",
	Short: "Agentic DB Tool — safe read-only database CLI for AI agents",
	Long: `adt is a cross-platform database query CLI designed for safe use by AI agents.
It supports Oracle, PostgreSQL, MySQL, SQL Server, and SQLite.
It enforces SELECT-only queries, row limits, and audit logging.`,
}

// Global flags accessed by subcommands via viper bindings.
var (
	cfgFile string
	envName string
	format  string
	limit   int
	timeout string
	dryRun  bool
	confirm bool
)

// Execute runs the root command and exits on error.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/adt/config.yaml)")
	RootCmd.PersistentFlags().StringVar(&envName, "env", "", "environment name (default: config default_env)")
	RootCmd.PersistentFlags().StringVar(&format, "format", "json", "output format: json|table|csv")
	RootCmd.PersistentFlags().IntVar(&limit, "limit", 0, "override max rows (0 = use config)")
	RootCmd.PersistentFlags().StringVar(&timeout, "timeout", "", "override query timeout (e.g. 30s)")
	RootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "validate SQL without executing")
	RootCmd.PersistentFlags().BoolVar(&confirm, "confirm", false, "confirm execution on production environments")

	if err := viper.BindPFlags(RootCmd.PersistentFlags()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to bind persistent flags: %v\n", err)
	}

	RootCmd.AddCommand(versionCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(home + "/.config/adt")
		}

		viper.AddConfigPath("$HOME/.config/adt")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()

	// Silently ignore missing config file — config may not exist yet.
	_ = viper.ReadInConfig()
}
