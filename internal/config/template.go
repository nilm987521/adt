package config

import "strings"

// ValidDrivers is the list of driver names accepted by the --db flag.
var ValidDrivers = []string{"oracle", "postgres", "mysql", "mssql", "sqlite"}

const templateHeader = `config_version: 3

# required: name of the default environment to use when --env flag is not specified
default_env: ""

environments:
`

const templateFooter = `
defaults:
  max_rows: 1000   # optional: global row limit applied to all environments (default: 1000)
  timeout: 30s     # optional: global query timeout for all environments (default: 30s)
  mask_columns: [] # optional: column names to redact in query output (case-insensitive)

audit:
  log_path: ""     # optional: path for the audit log file (default: ~/.local/share/adt/audit.log)
`

const oracleTemplate = `  # Oracle — https://www.oracle.com/database/
  # my-oracle:
  #   driver: oracle        # required: database driver
  #   host: ""              # required: database hostname or IP address
  #   port: 1521            # optional: Oracle listener port (default: 1521)
  #   service: ""           # required: Oracle Service Name (see: SELECT value FROM v$parameter WHERE name='service_names')
  #   user: ""              # required: database username
  #   # password is stored in the system keyring — run: adt setup --env my-oracle
  #   # tip: set ADT_PASSWORD=<password> env var to override keyring in non-interactive environments
  #   production: false           # optional: mark as production to enable safety prompts (default: false)
  #   require_confirmation: false # optional: always prompt before executing queries (default: false)
  #   max_rows: 0                 # optional: per-environment row limit (0 = inherit from defaults)
  #   timeout: 0s                 # optional: per-environment query timeout (0 = inherit from defaults)
  #   mask_columns: []            # optional: columns to redact in query output (merged with global list)
`

const postgresTemplate = `  # PostgreSQL — https://www.postgresql.org/
  # my-postgres:
  #   driver: postgres      # required: database driver
  #   host: ""              # required: database hostname or IP address
  #   port: 5432            # optional: PostgreSQL port (default: 5432)
  #   database: ""          # required: database name
  #   user: ""              # required: database username
  #   # password is stored in the system keyring — run: adt setup --env my-postgres
  #   # tip: set ADT_PASSWORD=<password> env var to override keyring in non-interactive environments
  #   production: false           # optional: mark as production to enable safety prompts (default: false)
  #   require_confirmation: false # optional: always prompt before executing queries (default: false)
  #   max_rows: 0                 # optional: per-environment row limit (0 = inherit from defaults)
  #   timeout: 0s                 # optional: per-environment query timeout (0 = inherit from defaults)
  #   mask_columns: []            # optional: columns to redact in query output (merged with global list)
`

const mysqlTemplate = `  # MySQL / MariaDB — https://www.mysql.com/
  # my-mysql:
  #   driver: mysql         # required: database driver (use "mysql" for MariaDB too)
  #   host: ""              # required: database hostname or IP address
  #   port: 3306            # optional: MySQL/MariaDB port (default: 3306)
  #   database: ""          # required: database name (schema name)
  #   user: ""              # required: database username
  #   # password is stored in the system keyring — run: adt setup --env my-mysql
  #   # tip: set ADT_PASSWORD=<password> env var to override keyring in non-interactive environments
  #   production: false           # optional: mark as production to enable safety prompts (default: false)
  #   require_confirmation: false # optional: always prompt before executing queries (default: false)
  #   max_rows: 0                 # optional: per-environment row limit (0 = inherit from defaults)
  #   timeout: 0s                 # optional: per-environment query timeout (0 = inherit from defaults)
  #   mask_columns: []            # optional: columns to redact in query output (merged with global list)
`

const mssqlTemplate = `  # Microsoft SQL Server — https://www.microsoft.com/sql-server/
  # my-mssql:
  #   driver: mssql         # required: database driver
  #   host: ""              # required: database hostname or IP address (e.g. sqlserver.example.com)
  #   port: 1433            # optional: SQL Server port (default: 1433)
  #   database: ""          # required: database name (catalog)
  #   user: ""              # required: database username (SQL Server auth) or domain\user (Windows auth)
  #   # password is stored in the system keyring — run: adt setup --env my-mssql
  #   # tip: set ADT_PASSWORD=<password> env var to override keyring in non-interactive environments
  #   production: false           # optional: mark as production to enable safety prompts (default: false)
  #   require_confirmation: false # optional: always prompt before executing queries (default: false)
  #   max_rows: 0                 # optional: per-environment row limit (0 = inherit from defaults)
  #   timeout: 0s                 # optional: per-environment query timeout (0 = inherit from defaults)
  #   mask_columns: []            # optional: columns to redact in query output (merged with global list)
`

const sqliteTemplate = `  # SQLite — https://www.sqlite.org/
  # my-sqlite:
  #   driver: sqlite        # required: database driver
  #   database: ""          # required: path to SQLite database file (e.g. ./data/myapp.db or :memory:)
  #   production: false           # optional: mark as production to enable safety prompts (default: false)
  #   require_confirmation: false # optional: always prompt before executing queries (default: false)
  #   max_rows: 0                 # optional: per-environment row limit (0 = inherit from defaults)
  #   timeout: 0s                 # optional: per-environment query timeout (0 = inherit from defaults)
  #   mask_columns: []            # optional: columns to redact in query output (merged with global list)
`

var driverTemplates = map[string]string{
	"oracle":   oracleTemplate,
	"postgres": postgresTemplate,
	"mysql":    mysqlTemplate,
	"mssql":    mssqlTemplate,
	"sqlite":   sqliteTemplate,
}

// BuildTemplate returns a complete config.yaml template containing the
// sections for the requested database drivers. If dbs is nil or empty,
// all five driver sections are included.
func BuildTemplate(dbs []string) string {
	if len(dbs) == 0 {
		dbs = ValidDrivers
	}

	var sb strings.Builder

	sb.WriteString(templateHeader)

	for _, driver := range dbs {
		if tmpl, ok := driverTemplates[driver]; ok {
			sb.WriteString(tmpl)
		}
	}

	sb.WriteString(templateFooter)

	return sb.String()
}
