# Multi-DB Support Design

**Date:** 2026-04-18
**Status:** Approved
**Scope:** Add PostgreSQL, MySQL, and Microsoft SQL Server support to `adt` alongside the existing Oracle driver.

---

## 1. Overview

`adt` currently supports Oracle only. This design adds support for three additional databases — PostgreSQL, MySQL, and MSSQL — while keeping the CLI interface unchanged for agents and users. All DB-specific logic is encapsulated behind a common `Driver` interface; the CLI layer does not need to know which database it is talking to.

### Target databases

| Driver key | Database |
|------------|----------|
| `oracle` | Oracle Database (existing) |
| `postgres` | PostgreSQL |
| `mysql` | MySQL / MariaDB |
| `mssql` | Microsoft SQL Server |

---

## 2. Architecture

### 2.1 New directory layout

```
internal/
├── db/
│   ├── driver.go          # Driver interface, QueryResult, shared types
│   ├── factory.go         # NewDriver(env, password) factory
│   ├── oracle/
│   │   └── oracle.go      # Moved from internal/oracle/, implements Driver
│   ├── postgres/
│   │   └── postgres.go
│   ├── mysql/
│   │   └── mysql.go
│   └── mssql/
│       └── mssql.go
├── cli/                   # Import path updated; logic unchanged
├── config/                # driver + database fields added
├── security/              # Unchanged
├── keyring/               # Entry prefix updated
├── audit/                 # Unchanged
└── output/                # Unchanged
```

The existing `internal/oracle/` package is moved into `internal/db/oracle/` and refactored to implement the new interface. No other CLI logic changes.

### 2.2 Driver interface

```go
// internal/db/driver.go

package db

import "context"

// Driver is the common interface every database backend must implement.
type Driver interface {
    // Query runs a user-supplied SELECT with automatic row-limit wrapping
    // inside a read-only transaction.
    Query(ctx context.Context, sql string, maxRows int) (*QueryResult, error)

    // RawQuery runs an internally generated SQL statement without row-limit
    // wrapping. Used by ListTables, DescribeTable, and similar helpers.
    RawQuery(ctx context.Context, sql string) ([]map[string]any, []string, error)

    // ExplainPlan returns the query plan for the given SQL without executing it.
    ExplainPlan(ctx context.Context, sql string) ([]string, error)

    // ListTables returns table metadata for the given schema (empty = current user).
    ListTables(ctx context.Context, schema string) ([]map[string]any, []string, error)

    // DescribeTable returns column metadata for the named table.
    DescribeTable(ctx context.Context, table string) ([]map[string]any, []string, error)

    // Close releases the underlying connection pool.
    Close() error
}

// QueryResult holds the output of a Query call.
type QueryResult struct {
    Rows        []map[string]any
    RowCount    int
    Truncated   bool   // true if rowCount == maxRows (result may be cut off)
    ElapsedMs   int64
    ExecutedSQL string // actual SQL sent to the database (with limit wrapper)
}
```

### 2.3 Factory

```go
// internal/db/factory.go

// NewDriver constructs the appropriate Driver implementation based on env.Driver.
// Supported values: "oracle", "postgres", "mysql", "mssql".
// An empty Driver field defaults to "oracle" for backwards compatibility.
func NewDriver(env *config.Environment, password string) (Driver, error)
```

---

## 3. Config changes

### 3.1 Schema version bump: v1 → v2

`config_version` is bumped to 2. The two new fields added to `Environment`:

```go
type Environment struct {
    Driver   string `yaml:"driver"`   // "oracle"|"postgres"|"mysql"|"mssql"; default "oracle"
    Database string `yaml:"database"` // used by postgres/mysql/mssql; ignored for oracle
    // ... all existing fields unchanged
}
```

### 3.2 Example config (v2)

```yaml
config_version: 2

default_env: my-oracle

environments:
  my-oracle:
    driver: oracle
    user: dev_user
    host: db.example.com
    port: 1521
    service: DEVDB

  my-postgres:
    driver: postgres
    user: pg_user
    host: pg.example.com
    port: 5432
    database: mydb

  my-mysql:
    driver: mysql
    user: mysql_user
    host: mysql.example.com
    port: 3306
    database: mydb

  my-mssql:
    driver: mssql
    user: sa
    host: mssql.example.com
    port: 1433
    database: mydb
```

### 3.3 Backward compatibility

If `driver` is absent or empty, it defaults to `"oracle"`. Existing v1 configs continue to work without any changes by the user.

### 3.4 Automatic v1 → v2 migration

When `adt` starts and detects `config_version: 1`:

1. All environments get `driver: oracle` injected.
2. Keyring entries are renamed from `oracle-password-<env>` to `db-password-<env>`.
3. `config_version` is written as `2`.
4. A one-time notice is printed to stderr explaining the migration.

No manual `adt config migrate` command is needed.

---

## 4. Keyring changes

| Version | Entry key format |
|---------|-----------------|
| v1 | `adt` / `oracle-password-<env>` |
| v2 | `adt` / `db-password-<env>` |

Migration renames entries automatically during the v1→v2 upgrade path. `setup` always writes `db-password-<env>` going forward.

---

## 5. DB-specific behaviour

### 5.1 Row-limit wrapping

Each driver wraps the user SQL differently to enforce `maxRows`:

| Driver | Wrapping strategy |
|--------|------------------|
| Oracle | `SELECT * FROM (<sql>) WHERE ROWNUM <= N` (11g-compatible subquery) |
| PostgreSQL | `SELECT * FROM (<sql>) AS _adt_sub LIMIT N` |
| MySQL | `SELECT * FROM (<sql>) AS _adt_sub LIMIT N` |
| MSSQL | `SELECT TOP N * FROM (<sql>) AS _adt_sub` |

### 5.2 Read-only transaction

| Driver | Mechanism |
|--------|-----------|
| Oracle | `SET TRANSACTION READ ONLY` (ORA-01456 is the final safeguard) |
| PostgreSQL | `BEGIN READ ONLY` (native; enforced by the database) |
| MySQL | `SET TRANSACTION READ ONLY` before `START TRANSACTION` |
| MSSQL | No native read-only transaction. Rely on application-layer SQL validation + `READ COMMITTED` isolation. The security layer (exit code 2) is the primary safeguard. |

### 5.3 Explain plan

| Driver | Implementation |
|--------|---------------|
| Oracle | `EXPLAIN PLAN SET STATEMENT_ID='...' FOR <sql>` then `DBMS_XPLAN.DISPLAY` |
| PostgreSQL | `EXPLAIN <sql>` — returns result set directly |
| MySQL | `EXPLAIN <sql>` — returns result set directly |
| MSSQL | `SET SHOWPLAN_TEXT ON` → execute → `SET SHOWPLAN_TEXT OFF` |

All drivers return `[]string` (one line per plan row) so the CLI output is uniform.

### 5.4 List tables

Each driver queries its own system catalog but returns the same column set: `TABLE_NAME`, `TABLE_TYPE`, `OWNER`/`TABLE_SCHEMA`.

| Driver | Source |
|--------|--------|
| Oracle | `ALL_TABLES` (filtered by `OWNER` when schema is given) |
| PostgreSQL | `information_schema.tables` |
| MySQL | `information_schema.TABLES` |
| MSSQL | `INFORMATION_SCHEMA.TABLES` |

### 5.5 Describe table

Same approach — each driver queries its system catalog and normalises the output to: `COLUMN_NAME`, `DATA_TYPE`, `NULLABLE`, `DATA_DEFAULT`.

### 5.6 DSN formats

| Driver | DSN |
|--------|-----|
| Oracle | `oracle://user:pass@host:port/service` |
| PostgreSQL | `postgres://user:pass@host:port/database` |
| MySQL | `user:pass@tcp(host:port)/database` |
| MSSQL | `sqlserver://user:pass@host:port?database=database` |

---

## 6. Go dependencies

| Driver | Package | Notes |
|--------|---------|-------|
| Oracle | `github.com/sijms/go-ora/v2` | Existing; pure Go |
| PostgreSQL | `github.com/jackc/pgx/v5/stdlib` | Pure Go; recommended over `lib/pq` |
| MySQL | `github.com/go-sql-driver/mysql` | Industry standard; pure Go |
| MSSQL | `github.com/microsoft/go-mssqldb` | Microsoft-maintained; pure Go |

All four are pure Go. The single-binary, no-native-dependency constraint is preserved.

---

## 7. `setup` subcommand changes

`adt setup --env <name>` gains two new interactive prompts:

1. **Driver** — select from `oracle` / `postgres` / `mysql` / `mssql` (default: `oracle`)
2. **Database / Service** — if driver is `oracle`, prompt for `service`; otherwise prompt for `database`

Port defaults change per driver: Oracle→1521, PostgreSQL→5432, MySQL→3306, MSSQL→1433.

---

## 8. Security model — unchanged constraints

The existing four-layer defence applies to all drivers:

1. Tool design: read-only subcommands only
2. SQL keyword validation (`security.Validate`) — DB-agnostic, unchanged
3. Automatic row-limit injection — per-driver wrapping (Section 5.1)
4. Read-only transaction — per-driver mechanism (Section 5.2)

MSSQL is the only driver without a native read-only transaction. Layer 2 (keyword validation) and exit code 2 are the primary safeguards. This limitation is documented in the README security model section.

---

## 9. Error codes — additions

Two new error codes added to the stable `error` field:

| Error Code | Meaning |
|------------|---------|
| `unsupported_driver` | `driver` value in config is not one of the four supported values |
| `driver_not_configured` | Required field missing for the selected driver (e.g. `database` absent for postgres) |

---

## 10. Implementation phases

### Phase 1 — Interface & Oracle migration
- Define `internal/db/driver.go` (interface + QueryResult)
- Move `internal/oracle/` → `internal/db/oracle/`, implement Driver interface
- Add `internal/db/factory.go`
- Update all CLI import paths
- All existing tests pass

### Phase 2 — Config v2 & migration
- Add `Driver` + `Database` fields to `config.Environment`
- Implement v1→v2 auto-migration (driver inject + keyring rename)
- Update `setup` subcommand

### Phase 3 — PostgreSQL driver
- Implement `internal/db/postgres/`
- Add `pgx/v5` dependency

### Phase 4 — MySQL driver
- Implement `internal/db/mysql/`
- Add `go-sql-driver/mysql` dependency

### Phase 5 — MSSQL driver
- Implement `internal/db/mssql/`
- Add `go-mssqldb` dependency
- Document MSSQL read-only limitation in README

### Phase 6 — Tests & docs
- Unit tests for each driver's `wrapWithRowLimit`, `ListTables`, `DescribeTable`
- Integration test stubs (skipped unless `ADT_TEST_<DRIVER>_DSN` env var is set)
- Update README, CHANGELOG, spec.md

---

*Document version: 1.0 — 2026-04-18*
