# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `query` subcommand: execute SELECT queries with automatic ROWNUM row limit and READ ONLY transaction
- `setup` subcommand: interactive configuration with OS keyring password storage
- `env list` / `env current` subcommands: manage database environments
- `list-tables` subcommand: list tables with optional `--schema` filter
- `describe` subcommand: show table column structure from ALL_TAB_COLUMNS
- `explain` subcommand: show execution plan via EXPLAIN PLAN + DBMS_XPLAN
- `sample` subcommand: randomly sample rows using DBMS_RANDOM.VALUE (Oracle 11g compatible)
- `version` subcommand: show version, build time, Go version
- 4-layer SQL read-only protection (tool design → keyword whitelist → row limit → READ ONLY tx)
- JSON output format (default, agent-friendly) with stable error codes
- Table and CSV output formats (`--format table|csv`)
- `--dry-run` flag: validate SQL without executing
- `--confirm` flag: required for environments with `require_confirmation: true`
- OS keyring integration (macOS Keychain, Windows Credential Manager, Linux Secret Service)
- Audit log in JSON Lines format (`~/.local/share/adt/audit.log`)
- Cross-platform config file (`~/.config/adt/config.yaml`) with `config_version` schema versioning
- Cross-compilation Makefile for darwin/linux/windows (amd64 + arm64)
- Oracle 11g compatibility: ROWNUM-based pagination instead of FETCH FIRST N ROWS

[Unreleased]: https://github.com/nilm987521/adt/commits/main
