package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nilm987521/adt/internal/config"
	"github.com/nilm987521/adt/internal/keyring"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure a database environment",
	RunE:  runSetup,
}

func init() {
	RootCmd.AddCommand(setupCmd)
	setupCmd.Flags().StringP("env", "e", "", "environment name to configure (required)")
	_ = setupCmd.MarkFlagRequired("env")
}

const driverOracle = "oracle"

// defaultPortForDriver returns the conventional default port for the given driver name.
func defaultPortForDriver(driver string) string {
	switch driver {
	case "postgres":
		return "5432"
	case "mysql":
		return "3306"
	case "mssql":
		return "1433"
	default: // "oracle" and empty
		return "1521"
	}
}

func runSetup(cmd *cobra.Command, _ []string) error { //nolint:gocyclo,funlen // CLI command; complexity and length are inherent in sequential interactive prompts
	envName, _ := cmd.Flags().GetString("env")

	// 1. Load config (or create empty if not exists)
	cfgPath := config.DefaultConfigPath()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		// Create a minimal empty config if loading fails
		cfg = &config.Config{
			ConfigVersion: config.CurrentVersion,
			Environments:  make(map[string]config.Environment),
		}
	}

	if cfg.Environments == nil {
		cfg.Environments = make(map[string]config.Environment)
	}

	// 2. Show existing values as defaults if env already configured
	existing, hasExisting := cfg.Environments[envName]

	reader := bufio.NewReader(os.Stdin)

	prompt := func(label, defaultVal string) (string, error) {
		if defaultVal != "" {
			fmt.Printf("%s [%s]: ", label, defaultVal)
		} else {
			fmt.Printf("%s: ", label)
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return defaultVal, nil
		}

		return line, nil
	}

	fmt.Printf("Configuring environment: %s\n", envName)

	// 3. Prompt for driver
	defaultDriver := driverOracle
	if hasExisting && existing.EffectiveDriver() != "" {
		defaultDriver = existing.EffectiveDriver()
	}

	driver, err := prompt("Driver (oracle/postgres/mysql/mssql/sqlite)", defaultDriver)
	if err != nil {
		return err
	}

	driver = strings.ToLower(strings.TrimSpace(driver))

	switch driver {
	case driverOracle, "postgres", "mysql", "mssql", "sqlite":
		// valid
	default:
		return fmt.Errorf("unsupported driver %q: must be one of oracle, postgres, mysql, mssql, sqlite", driver)
	}

	// 4. Prompt for connection details
	var user, host, portStr, service, database, password string
	var port int

	if driver == "sqlite" {
		// SQLite only needs a file path — skip user, host, port, password
		defaultServiceOrDB := ""
		if hasExisting {
			defaultServiceOrDB = existing.Database
		}

		database, err = prompt("Database file path", defaultServiceOrDB)
		if err != nil {
			return err
		}
	} else {
		defaultUser := ""
		defaultHost := ""
		defaultPort := defaultPortForDriver(driver)
		defaultServiceOrDB := ""

		if hasExisting {
			defaultUser = existing.User
			defaultHost = existing.Host

			if existing.Port != 0 {
				defaultPort = strconv.Itoa(existing.Port)
			}

			if driver == driverOracle {
				defaultServiceOrDB = existing.Service
			} else {
				defaultServiceOrDB = existing.Database
			}
		}

		user, err = prompt("User", defaultUser)
		if err != nil {
			return err
		}

		host, err = prompt("Host", defaultHost)
		if err != nil {
			return err
		}

		portStr, err = prompt("Port", defaultPort)
		if err != nil {
			return err
		}

		port, err = strconv.Atoi(portStr)
		if err != nil {
			return fmt.Errorf("invalid port number %q: %w", portStr, err)
		}

		if driver == driverOracle {
			service, err = prompt("Service name", defaultServiceOrDB)
			if err != nil {
				return err
			}
		} else {
			database, err = prompt("Database name", defaultServiceOrDB)
			if err != nil {
				return err
			}
		}

		// 5. Prompt for password (hidden input via bufio.Scanner — golang.org/x/term not in go.mod)
		fmt.Print("Password (input will be visible): ")

		passwordLine, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}

		password = strings.TrimRight(passwordLine, "\r\n")
	}

	// 6. Write connection info to config
	env := config.Environment{
		Driver:   driver,
		User:     user,
		Host:     host,
		Port:     port,
		Service:  service,
		Database: database,
	}

	if hasExisting {
		// Preserve existing settings not being overwritten
		env.Production = existing.Production
		env.RequireConfirmation = existing.RequireConfirmation
		env.MaxRows = existing.MaxRows
		env.Timeout = existing.Timeout
	}

	cfg.Environments[envName] = env
	cfg.ConfigVersion = config.CurrentVersion

	if err := cfg.Save(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to save config: %v\n", err)
		os.Exit(1)
	}

	// 7. Write password to keyring (skip for sqlite — no password needed)
	if driver != "sqlite" {
		if err := keyring.Set(envName, password); err != nil {
			fmt.Fprintf(os.Stderr, "error: failed to save password to keyring: %v\n", err)
			os.Exit(1)
		}
	}

	// 8. Print success message
	fmt.Printf("Environment %q configured successfully (driver: %s).\n", envName, driver)

	return nil
}
