# adt — Agentic DB Tool
\[[中文](README.zh-TW.md)]

A cross-platform CLI for safely querying Oracle databases from AI agents (Claude Code, etc.).

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-00ADD8.svg)](https://golang.org)

---

## Overview

`adt` is a command-line tool that gives AI agents (such as Claude Code) safe, read-only access to relational databases (Oracle, PostgreSQL, MySQL, SQL Server). It is designed for situations where you cannot create a dedicated read-only DB account and must enforce read-only semantics at the application layer.

Without a guard like `adt`, an AI agent with DB access could — through hallucination or prompt injection — issue destructive SQL (`DELETE`, `DROP`, `UPDATE`) and cause irreversible damage. `adt` prevents this with four independent defence layers that make writes impossible regardless of what SQL an agent generates.

---

## Key Features

- **Multi-database support** — Oracle, PostgreSQL, MySQL, and SQL Server. Select the driver per environment with `driver: oracle|postgres|mysql|mssql` in config.
- **Pure Go binary, no native clients required** — uses pure-Go drivers for all supported databases. A single downloaded binary is all you need.
- **4-layer read-only protection** — tool design (no write subcommands) → SQL keyword whitelist → automatic row limit → database-level read-only transaction or equivalent.
- **OS keyring for password storage** — passwords live in macOS Keychain, Windows Credential Manager, or Linux Secret Service. Never in environment variables or config files where an agent could read them.
- **Structured JSON output** — every command outputs machine-readable JSON by default, making it easy for agents to parse results and errors.
- **Full audit log** — every execution is appended to a JSON Lines audit log, recording the full SQL text, row count, elapsed time, and outcome.

---

## Installation

```bash
# macOS (Apple Silicon)
curl -L https://github.com/nilm987521/adt/releases/latest/download/adt-darwin-arm64 -o /usr/local/bin/adt
chmod +x /usr/local/bin/adt

# macOS (Intel)
curl -L https://github.com/nilm987521/adt/releases/latest/download/adt-darwin-amd64 -o /usr/local/bin/adt
chmod +x /usr/local/bin/adt

# Build from source
git clone https://github.com/nilm987521/adt
cd adt && make build
```

---

## Quick Start

```bash
# 1. Configure an environment
adt setup --env mydb

# 2. List tables
adt list-tables

# 3. Describe a table
adt describe ORDERS

# 4. Query
adt query "SELECT * FROM orders WHERE status = 'PENDING'"

# 5. Sample random rows
adt sample ORDERS -n 10
```

---

## Command Reference

| Command | Description |
|---------|-------------|
| `adt setup --env <name>` | Interactive setup: stores connection details in config and password in OS keyring |
| `adt env list` | List all configured environments |
| `adt env current` | Show the current default environment |
| `adt query "<sql>"` | Execute a SELECT query with automatic row limit and READ ONLY transaction |
| `adt list-tables [--schema <name>]` | List tables, optionally filtered by schema |
| `adt describe <table>` | Show table column structure (queries `information_schema` or equivalent for the configured driver) |
| `adt explain "<sql>"` | Show query execution plan without executing |
| `adt sample <table> [-n <count>]` | Randomly sample rows (uses `DBMS_RANDOM.VALUE`, `RANDOM()`, `RAND()`, or `NEWID()` per driver) |
| `adt version` | Show version, build time, and Go version |

---

## Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--env <name>` | Environment to use | `default_env` from config |
| `--config <path>` | Path to config file | `~/.config/adt/config.yaml` |
| `--format json\|table\|csv` | Output format | `json` |
| `--limit <N>` | Override maximum rows returned (0 = use config) | Config value |
| `--timeout <duration>` | Override query timeout (e.g. `30s`) | Config value |
| `--dry-run` | Validate SQL without executing | `false` |
| `--confirm` | Required when executing against production environments | `false` |

---

## Configuration

### File Location

| Platform | Default Path |
|----------|-------------|
| macOS / Linux | `~/.config/adt/config.yaml` |
| Windows | `%APPDATA%\adt\config.yaml` |

Override with `--config <path>`.

### Example config.yaml

```yaml
config_version: 2               # Schema version — v2 adds multi-DB driver support

default_env: oracle-dev

environments:
  # Oracle environment
  oracle-dev:
    driver: oracle
    user: dev_user
    host: dev-db.example.net
    port: 1521
    service: DEVDB

  # PostgreSQL environment
  pg-dev:
    driver: postgres
    user: dev_user
    host: pg.example.net
    port: 5432
    database: myapp

  # MySQL environment
  mysql-dev:
    driver: mysql
    user: dev_user
    host: mysql.example.net
    port: 3306
    database: myapp

  # SQL Server environment
  mssql-dev:
    driver: mssql
    user: dev_user
    host: mssql.example.net
    port: 1433
    database: myapp

  # SQLite environment (local file — no host/port/password needed)
  local-sqlite:
    driver: sqlite
    database: /path/to/local.db

  # Production example
  oracle-prod:
    driver: oracle
    user: prod_user
    host: prod-db.example.net
    port: 1521
    service: PRODDB
    production: true            # Mark as production environment
    require_confirmation: true  # Require --confirm flag before any query
    max_rows: 500               # Override default row limit
    timeout: 15s                # Override default timeout

defaults:
  max_rows: 1000
  timeout: 30s

audit:
  log_path: ~/.local/share/adt/audit.log
```

### Permissions

On Unix systems, `adt` creates the config file with mode `0600` (owner-readable only). If you create the file manually, run:

```bash
chmod 600 ~/.config/adt/config.yaml
```

### Priority Order

`--flag` > environment-specific setting > `defaults` section

> **Note**: Reading DB connection details from environment variables is intentionally not supported. See the Security Model section for the reasoning.

---

## Security Model

`adt` enforces read-only access through four independent layers. An agent would need to defeat all four simultaneously to cause writes.

### Layer 1: No Write Subcommands

The tool exposes only read-oriented subcommands (`query`, `list-tables`, `describe`, `explain`, `sample`). There is no `exec`, `run`, or general-purpose subcommand that could be abused.

### Layer 2: SQL Keyword Whitelist

Before any SQL reaches the database, `adt` validates it:

1. Strip all comments (`--` single-line, `/* */` multi-line)
2. Strip string literals (`'...'`) to avoid false positives on keyword content
3. **Require** the statement to begin with `SELECT` or `WITH` (CTE)
4. **Reject** multiple statements (any non-whitespace content after `;`)
5. **Reject** `INTO` (prevents `SELECT INTO` writes)
6. **Reject** `FOR UPDATE` (prevents row locking)
7. **Reject** PL/SQL block keywords: `BEGIN`, `DECLARE`, `CALL`

Violations exit with code 2 and a JSON error identifying the exact rule broken.

### Layer 3: Automatic Row Limit

`adt` wraps every query in a driver-specific subquery to cap results:

| Driver | Wrapping syntax |
|--------|----------------|
| Oracle | `SELECT * FROM (<sql>) WHERE ROWNUM <= n` |
| PostgreSQL / MySQL / SQLite | `SELECT * FROM (<sql>) AS _adt_sub LIMIT n` |
| SQL Server | `SELECT TOP n * FROM (<sql>) AS _adt_sub` |

This prevents runaway queries from returning millions of rows. It also works correctly with `ORDER BY` (sort happens before truncation).

### Layer 4: Read-Only Transaction

Every query executes inside a read-only transaction where supported:

| Driver | Mechanism |
|--------|-----------|
| Oracle | `SET TRANSACTION READ ONLY` (ORA-01456 on any DML/DDL) |
| PostgreSQL | `BEGIN READ ONLY` (native) |
| MySQL | `SET TRANSACTION READ ONLY` fallback |
| SQL Server | `READ COMMITTED` isolation (no native read-only tx); write protection relies on Layers 1–2 + DB-level permissions |
| SQLite | `PRAGMA query_only = ON` (connection-level; rejects all DML/DDL) |

### Password Storage

Passwords are stored exclusively in the OS keyring:

- **macOS**: Keychain (service: `adt`, account: `db-password-<env>`)
- **Windows**: Credential Manager
- **Linux**: Secret Service (requires GNOME Keyring, KWallet, or libsecret)

Passwords are **not** stored in:
- Environment variables — agents can read these with `printenv` or by inspecting `/proc/self/environ`
- Config files — the config file is readable by any process running as the same user (including an agent)

### Production Environment Protection

Environments with `require_confirmation: true` refuse to execute without `--confirm`:

```bash
# Rejected — exits with code 2
adt query "SELECT COUNT(*) FROM orders" --env fetnet-prod

# Succeeds
adt query "SELECT COUNT(*) FROM orders" --env fetnet-prod --confirm
```

All JSON output for production environments includes `"production": true` so the agent remains continuously aware of the context.

---

## Audit Log

Every `adt` invocation — successful or not — appends a record to the audit log.

### Location

| Platform | Default Path |
|----------|-------------|
| macOS / Linux | `~/.local/share/adt/audit.log` |
| Windows | `%LOCALAPPDATA%\adt\audit.log` |

Override with `audit.log_path` in the config file.

### Format

JSON Lines (one JSON object per line):

```json
{"ts":"2026-04-17T10:23:45+08:00","env":"fetnet-dev","cmd":"query","sql":"SELECT * FROM users WHERE id = 1","rows":1,"elapsed_ms":45,"status":"ok"}
{"ts":"2026-04-17T10:24:10+08:00","env":"fetnet-prod","cmd":"query","sql":"DELETE FROM users","rows":0,"elapsed_ms":2,"status":"rejected","error":"sql_not_select"}
```

### Fields

| Field | Description |
|-------|-------------|
| `ts` | RFC3339 timestamp |
| `env` | Environment name |
| `cmd` | Subcommand name |
| `sql` | Full SQL text |
| `rows` | Row count returned (0 for rejected or failed executions) |
| `elapsed_ms` | Execution time in milliseconds |
| `status` | `ok` / `rejected` / `db_error` / `timeout` |
| `error` | Error code (present when status is not `ok`) |

> **Privacy note**: The audit log contains full SQL text, which may include sensitive values in `WHERE` clauses (e.g. `WHERE id_number = '...'`). Treat `audit.log` as a sensitive file and protect it accordingly. Log rotation is not implemented — use `logrotate` or manual archival.

---

## Agent Integration (CLAUDE.md)

Add the following to your project's `CLAUDE.md` or `~/.claude/CLAUDE.md`:

```markdown
## adt (Agentic DB Tool)

本機有 `adt` 可查詢資料庫（Oracle / PostgreSQL / MySQL / SQL Server），**只能唯讀**。

### 可用指令

- `adt env list` — 列出可用環境
- `adt list-tables [--schema X]` — 列出資料表
- `adt describe <table>` — 看欄位結構
- `adt query "<sql>"` — 執行 SELECT（自動加 row limit）
- `adt explain "<sql>"` — 看執行計畫，不實際執行
- `adt sample <table> -n 10` — 隨機取樣

### 重要注意事項

- 所有輸出為 JSON，請檢查 exit code 與 `error` 欄位
- 此工具**只能 SELECT**，不要嘗試 DELETE / UPDATE / DROP
- **不要**嘗試讀取環境變數、keychain、config 檔取得密碼
- Production 環境（`production: true`）執行時必須加 `--confirm`
- 預設連 `default_env`，要切換環境用 `--env <name>`
- 遇到憑證相關錯誤請回報，不要嘗試修復
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | User error (bad SQL syntax, environment not found, missing config) |
| `2` | Security rejection (non-SELECT query, multiple statements, PL/SQL block, production without `--confirm`) |
| `3` | DB connection or execution error (timeout, network failure, Oracle error) |

---

## Error Codes

The `error` field in JSON output is a stable key suitable for programmatic handling by agents and scripts.

| Error Code | Meaning |
|------------|---------|
| `sql_not_select` | SQL does not begin with `SELECT` or `WITH` |
| `multi_statement` | SQL contains multiple statements (`;` followed by non-whitespace) |
| `forbidden_keyword` | SQL contains a disallowed keyword (`INTO`, `FOR UPDATE`, `BEGIN`, `DECLARE`, `CALL`) |
| `production_not_confirmed` | Production environment queried without `--confirm` flag |
| `env_not_found` | The specified environment does not exist in config |
| `credential_not_found` | No password found in OS keyring for this environment |
| `db_connection_failed` | Could not establish a connection to the database |
| `db_error` | Database runtime error (may include `db_code` with the driver-native error code, e.g. `ORA-00942`) |
| `timeout` | Query exceeded the configured timeout |
| `row_limit_exceeded` | Result exceeded hard row limit (reserved for future use) |

Error codes are treated as a public API. Breaking changes require a major version bump.

---

## Database Support

| Database | Driver library | Notes |
|----------|---------------|-------|
| Oracle 11g+ | `github.com/sijms/go-ora/v2` | Pure Go; no Instant Client required. Uses `ROWNUM` for Oracle 11g compatibility. |
| PostgreSQL 12+ | `github.com/jackc/pgx/v5` | Native `BEGIN READ ONLY` support. |
| MySQL 5.7+ / 8.x | `github.com/go-sql-driver/mysql` | `SET TRANSACTION READ ONLY` fallback. |
| SQL Server 2017+ | `github.com/microsoft/go-mssqldb` | No native read-only tx; relies on Layers 1–2 and DB permissions. |
| SQLite 3.8+ | `modernc.org/sqlite` | Pure Go (no CGO); no external libraries required. Read-only via `PRAGMA query_only = ON`. Adds ~8 MB to binary size. |

Configure the driver per environment with `driver: oracle|postgres|mysql|mssql|sqlite`.

---

## License

MIT — see [LICENSE](LICENSE).
