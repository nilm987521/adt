# Contributing to adt

Thank you for your interest in contributing to **adt** (Agentic DB Tool). This document covers everything you need to get a working development environment, understand the project layout, and submit a pull request.

---

## Prerequisites

| Requirement | Notes |
|-------------|-------|
| **Go 1.22+** | Required. Earlier versions are not tested. |
| **Git** | Standard version control. |
| **Oracle DB access** | Optional. Required only for integration tests. Unit tests run without a database. |
| **OS keyring** | Required for keyring-related tests. On Linux, ensure `libsecret` and a compatible keyring daemon (GNOME Keyring or KWallet) are running. |

---

## Development Setup

```bash
git clone https://github.com/nilm987521/adt.git
cd adt
go mod download    # fetch dependencies
make build         # verify the project compiles
make test          # run unit tests (no Oracle connection required)
```

If you have an Oracle database available and want to run integration tests:

```bash
# Set environment variables to enable integration tests
export ADT_TEST_ORACLE_DSN="user/pass@host:1521/service"
make test-integration
```

---

## Project Structure

```
adt/
├── cmd/adt/main.go          # entry point — wires CLI to internal packages
├── internal/
│   ├── cli/                 # cobra commands (query, schema, explain, auth, config)
│   ├── config/              # config file I/O (reads ~/.config/adt/config.yaml)
│   ├── security/            # SQL validation (whitelist, comment stripping, token checks)
│   ├── oracle/              # DB connection pool, query execution, READ ONLY transactions
│   ├── keyring/             # OS keyring wrapper (macOS / Windows / Linux)
│   ├── audit/               # audit log writer (JSON Lines)
│   └── output/              # result formatting (JSON, table, CSV)
├── Makefile
└── .goreleaser.yaml
```

All business logic lives under `internal/`. The `cmd/` layer only handles argument parsing and wiring. The `internal/` packages have **no public API** — breaking changes inside `internal/` are acceptable between minor versions without a deprecation period.

---

## Making Changes

### General rules

- All changes to public-facing behaviour (CLI flags, output format, JSON field names, exit codes) must include tests.
- Bug fixes should include a test that reproduces the bug before the fix is applied.
- New features should include both unit tests and, where applicable, integration tests.

### Security package

Changes to `internal/security/` are the most sensitive changes in the codebase. Every change must:

1. Update `validator_test.go` with test cases covering the new behaviour.
2. Include at least one test case for a bypass attempt that the change is meant to block.
3. Be conservative: **when in doubt, reject**. A false positive (rejecting a valid query) is always preferable to a false negative (passing a malicious statement).

### Before submitting

```bash
make test          # all unit tests must pass
go vet ./...       # no vet warnings
golangci-lint run  # no lint errors
```

If `golangci-lint` is not installed:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

---

## Submitting a Pull Request

1. **Fork** the repository on GitHub.
2. **Create a feature branch** from `main`:
   ```bash
   git checkout -b feat/my-feature
   ```
3. **Make your changes** with accompanying tests.
4. **Verify** everything passes:
   ```bash
   make test && go vet ./...
   ```
5. **Commit** using a conventional commit message (see below).
6. **Push** your branch and open a PR against `main` on the upstream repository.

### Conventional commit messages

Use one of these prefixes:

| Prefix | Use for |
|--------|---------|
| `feat:` | New user-facing feature |
| `fix:` | Bug fix |
| `docs:` | Documentation only |
| `test:` | Adding or fixing tests |
| `chore:` | Build system, dependencies, CI |
| `refactor:` | Internal restructuring with no behaviour change |
| `security:` | Security fix or hardening (use for `internal/security/` changes) |

Examples:

```
feat: add --format csv flag to query subcommand
fix: handle ORA-01013 user-requested cancel gracefully
security: reject SELECT ... INTO TABLE in SQL validator
test: add validator tests for PL/SQL EXECUTE IMMEDIATE bypass
```

### PR checklist

- [ ] Tests added or updated
- [ ] `make test` passes locally
- [ ] `go vet ./...` clean
- [ ] `golangci-lint run` clean
- [ ] Conventional commit message used
- [ ] No credentials or connection strings committed

---

## Security Considerations for Contributors

These are non-negotiable constraints, not style preferences. Please read them before making changes.

**Do not add environment variable password reading.**
adt intentionally does not read database passwords from environment variables. This is a deliberate design decision (environment variables are visible to other processes on the same host). Do not add `os.Getenv("ADT_PASSWORD")` or equivalent, even as an optional fallback.

**Do not add telemetry or outbound connections.**
adt must not make any network connection beyond the configured Oracle host. No analytics, no update checks, no error reporting services.

**Do not commit test credentials or connection strings.**
Use environment-gated test helpers. Integration tests must check for a test DSN environment variable and skip gracefully if it is not set:

```go
dsn := os.Getenv("ADT_TEST_ORACLE_DSN")
if dsn == "" {
    t.Skip("ADT_TEST_ORACLE_DSN not set; skipping integration test")
}
```

**Be conservative with SQL validation changes.**
The SQL validator in `internal/security/` is a defence layer, not a parser. If you are unsure whether a new construct should be allowed, default to rejecting it and open a discussion in the PR.

**Do not remove or weaken the READ ONLY transaction.**
The `SET TRANSACTION READ ONLY` in `internal/oracle/` is the last line of defence against writes reaching the database. Do not remove it, make it conditional, or replace it with advisory-only logic.

---

## Code Style

**Formatting:** Standard Go formatting enforced by `gofmt`. Run `gofmt -w .` before committing, or configure your editor to run it on save.

**Dependencies:** Do not add new external dependencies without first opening an issue for discussion. adt aims to have a small, auditable dependency tree.

**Error messages:** Should be lowercase with no trailing period, following standard Go convention. Example: `"connection profile not found"`, not `"Connection profile not found."`.

**JSON field names:** The `error` field and all top-level JSON output fields are **public API**. Once a field name appears in a released version, it must not be renamed. Add new fields rather than renaming existing ones.

**Internal package boundaries:** Packages in `internal/` should not import each other in cycles. The dependency direction is: `cli` → everything else; `oracle`, `security`, `keyring`, `audit`, `output` → `config` only.

---

## License

By contributing to adt, you agree that your contributions will be licensed under the **MIT License**, the same license that covers the project. See [LICENSE](LICENSE) for the full text.
