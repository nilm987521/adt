package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/adt-tool/adt/internal/config"
	"github.com/adt-tool/adt/internal/keyring"
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

func runSetup(cmd *cobra.Command, args []string) error {
	envName, _ := cmd.Flags().GetString("env")

	// 1. Load config (or create empty if not exists)
	cfgPath := config.DefaultConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		// Create a minimal empty config if loading fails
		cfg = &config.Config{
			ConfigVersion: 1,
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

	// 3. Prompt for connection details
	defaultUser := ""
	defaultHost := ""
	defaultPort := "1521"
	defaultService := ""

	if hasExisting {
		defaultUser = existing.User
		defaultHost = existing.Host
		if existing.Port != 0 {
			defaultPort = strconv.Itoa(existing.Port)
		}
		defaultService = existing.Service
	}

	user, err := prompt("User", defaultUser)
	if err != nil {
		return err
	}

	host, err := prompt("Host", defaultHost)
	if err != nil {
		return err
	}

	portStr, err := prompt("Port", defaultPort)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid port number %q: %w", portStr, err)
	}

	service, err := prompt("Service name", defaultService)
	if err != nil {
		return err
	}

	// 4. Prompt for password (hidden input via bufio.Scanner — golang.org/x/term not in go.mod)
	fmt.Print("Password (input will be visible): ")
	passwordLine, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}
	password := strings.TrimRight(passwordLine, "\r\n")

	// 5. Write connection info to config
	env := config.Environment{
		User:    user,
		Host:    host,
		Port:    port,
		Service: service,
	}
	if hasExisting {
		// Preserve existing settings not being overwritten
		env.Production = existing.Production
		env.RequireConfirmation = existing.RequireConfirmation
		env.MaxRows = existing.MaxRows
		env.Timeout = existing.Timeout
	}
	cfg.Environments[envName] = env

	if err := cfg.Save(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to save config: %v\n", err)
		os.Exit(1)
	}

	// 6. Write password to keyring
	if err := keyring.Set(envName, password); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to save password to keyring: %v\n", err)
		os.Exit(1)
	}

	// 7. Print success message
	fmt.Printf("Environment %q configured successfully.\n", envName)
	return nil
}
