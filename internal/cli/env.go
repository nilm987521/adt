package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nilm987521/adt/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage database environments",
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured database environments",
	RunE:  runEnvList,
}

var envCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current active database environment",
	RunE:  runEnvCurrent,
}

func init() {
	RootCmd.AddCommand(envCmd)
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envCurrentCmd)
}

// envEntry is the JSON representation of a single environment in env list output.
type envEntry struct {
	Name       string `json:"name"`
	User       string `json:"user"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Service    string `json:"service"`
	Production bool   `json:"production"`
	Default    bool   `json:"default"`
}

// envListOutput is the top-level JSON structure for env list.
type envListOutput struct {
	Environments []envEntry `json:"environments"`
}

func runEnvList(_ *cobra.Command, _ []string) error {
	cfgPath := config.DefaultConfigPath()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	entries := make([]envEntry, 0, len(cfg.Environments))
	for name, env := range cfg.Environments {
		entries = append(entries, envEntry{
			Name:       name,
			User:       env.User,
			Host:       env.Host,
			Port:       env.Port,
			Service:    env.Service,
			Production: env.Production,
			Default:    name == cfg.DefaultEnv,
		})
	}

	// Determine output format from root flag (accessed via viper)
	format := viper.GetString("format")
	if format == "" {
		format = "json"
	}

	switch format {
	case "table":
		return printEnvTable(entries, cfg.DefaultEnv)
	default:
		// JSON output
		out := envListOutput{Environments: entries}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")

		return enc.Encode(out)
	}
}

func printEnvTable(entries []envEntry, _ string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush() //nolint:errcheck // cleanup; error not actionable

	_, _ = fmt.Fprintln(w, "NAME\tUSER\tHOST\tPORT\tSERVICE\tPROD\tDEFAULT")
	_, _ = fmt.Fprintln(w, "----\t----\t----\t----\t-------\t----\t-------")

	for _, e := range entries {
		prod := "false"
		if e.Production {
			prod = "true"
		}

		def := "false"
		if e.Default {
			def = "true"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			e.Name, e.User, e.Host, e.Port, e.Service, prod, def)
	}

	return nil
}

// currentEnvOutput is the JSON structure for env current.
type currentEnvOutput struct {
	CurrentEnv string `json:"current_env"`
}

func runEnvCurrent(_ *cobra.Command, _ []string) error {
	cfgPath := config.DefaultConfigPath()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Prefer --env flag if set, otherwise use config default
	current := viper.GetString("env")
	if current == "" {
		current = cfg.DefaultEnv
	}

	out := currentEnvOutput{CurrentEnv: current}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return enc.Encode(out)
}
