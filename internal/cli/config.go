package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/nilm987521/adt/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage adt configuration",
	Long:  "Commands for managing the adt configuration file.",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a config.yaml template",
	Long: `Generate a config.yaml template containing annotated sections for all supported
database drivers. Use --db to limit output to specific drivers.`,
	RunE: runConfigInit,
}

func init() {
	RootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configInitCmd)

	configInitCmd.Flags().String("output", "./config.yaml", "output file path")
	configInitCmd.Flags().StringArray("db", nil, "database driver(s) to include (oracle, postgres, mysql, mssql, sqlite); may be repeated or comma-separated")
	configInitCmd.Flags().Bool("force", false, "overwrite existing file")
}

func runConfigInit(cmd *cobra.Command, _ []string) error {
	outPath, err := cmd.Flags().GetString("output")
	if err != nil {
		return err
	}

	rawDBs, err := cmd.Flags().GetStringArray("db")
	if err != nil {
		return err
	}

	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		return err
	}

	dbs, err := resolveDBs(rawDBs)
	if err != nil {
		return err
	}

	// Check if the file already exists.
	if _, statErr := os.Stat(outPath); statErr == nil {
		if !force {
			return fmt.Errorf("file %q already exists; use --force to overwrite", outPath)
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "warning: overwriting existing file %q\n", outPath) //nolint:errcheck // stderr write; error is non-actionable
	}

	content := config.BuildTemplate(dbs)

	perm := os.FileMode(0o600)
	if runtime.GOOS == "windows" {
		perm = 0o666
	}

	if err := os.WriteFile(outPath, []byte(content), perm); err != nil {
		return fmt.Errorf("writing template to %q: %w", outPath, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "config template written to %q\n", outPath) //nolint:errcheck // stdout write; error is non-actionable

	return nil
}

// resolveDBs expands and validates the --db flag values (supports comma-separated).
// Returns nil if input is empty, which signals BuildTemplate to include all drivers.
func resolveDBs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil // BuildTemplate(nil) returns all drivers
	}

	// Expand comma-separated values.
	var expanded []string

	for _, v := range raw {
		for part := range strings.SplitSeq(v, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			if part != "" {
				expanded = append(expanded, part)
			}
		}
	}

	// Validate each value.
	valid := make(map[string]bool, len(config.ValidDrivers))
	for _, d := range config.ValidDrivers {
		valid[d] = true
	}

	for _, d := range expanded {
		if !valid[d] {
			return nil, fmt.Errorf("unknown driver %q for --db flag; valid values: %s",
				d, strings.Join(config.ValidDrivers, ", "))
		}
	}

	return expanded, nil
}
