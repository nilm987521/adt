# Security Policy

## Supported Versions

adt is currently in active development. Only the **latest released version** receives security fixes.

| Version | Supported |
|---------|-----------|
| 0.x.x (latest) | Yes |
| Older 0.x.x | No |

---

## Threat Model

adt is a CLI tool designed to be invoked by AI agents querying an Oracle database. Its security model is built around the reality that an AI agent is an untrusted caller: it may hallucinate SQL, follow injected instructions, or issue queries that consume excessive resources.

### What adt protects against

**AI agent hallucination producing destructive SQL**
An agent may generate `DELETE`, `UPDATE`, or `DROP` statements — with or without a `WHERE` clause. adt rejects any statement whose first meaningful token is not `SELECT` or `WITH`.

**Prompt injection causing SQL execution**
An attacker who can influence the text fed to the agent may attempt to inject a DML or DDL statement. The SQL whitelist (see Security Architecture below) validates the query before it reaches the database, regardless of how it was constructed.

**Agent reading credentials through common means**
adt stores database passwords exclusively in the OS keyring (macOS Keychain, Windows Credential Manager, Linux Secret Service). Passwords are never written to config files, log files, or environment variables. An agent that reads `~/.config/adt/config.yaml` or inspects the process environment will not find a usable password.

**Accidental large result sets consuming excessive DB resources**
Every query is automatically wrapped in a `ROWNUM <= N` guard (configurable, default 500). Runaway queries that would return millions of rows are truncated at the database level before data is transferred.

---

### What adt does NOT protect against

The following are explicitly **out of scope**:

| Scenario | Reason |
|----------|--------|
| A malicious actor who controls the machine running adt | The tool owner is the tool executor. Physical or OS-level access grants full control by definition. |
| Reverse engineering the binary | adt is open source (MIT licensed). There are no secrets in the binary. |
| Network-level attacks | adt is a local CLI tool. It makes no external connections beyond the configured Oracle host. |
| Oracle server security | adt is a database client. Security controls on the Oracle server itself — authentication, authorisation, auditing, network ACLs — are the user's responsibility. |

---

## Security Architecture

adt applies four independent layers of defence. A query must pass all four before any data is returned.

### Layer 1 — Tool design: read-only subcommands only

adt exposes only read-oriented subcommands (`query`, `schema`, `explain`). There is no general `exec` or `run` subcommand that accepts arbitrary SQL. An agent cannot invoke a write operation simply by passing different arguments.

### Layer 2 — SQL whitelist validation

Before sending any SQL to Oracle, adt:

1. Strips all SQL comments (`--` line comments and `/* */` block comments).
2. Strips all string literals, replacing them with placeholders (prevents comment injection inside quoted values).
3. Checks that the **first token** of the normalised statement is `SELECT` or `WITH`.
4. Rejects statements containing `INTO` (blocks `SELECT INTO`, `INSERT INTO`).
5. Rejects statements containing `FOR UPDATE` (blocks pessimistic locking).
6. Rejects PL/SQL block markers (`BEGIN`, `DECLARE`, `EXECUTE IMMEDIATE`).

This validation is implemented in `internal/security/` and has its own unit test suite (`validator_test.go`).

### Layer 3 — Automatic row limit

Every query is rewritten to include a `WHERE ROWNUM <= N` constraint (or wrapped in a subquery when needed) before execution. This prevents a query that unexpectedly matches millions of rows from transferring unbounded data and exhausting memory or DB resources. The limit is configurable per connection profile but cannot be disabled without an explicit `--limit 0` override that requires `--confirm`.

### Layer 4 — READ ONLY transaction

Every query executes inside an Oracle `SET TRANSACTION READ ONLY` session. If any of the above layers fail or are somehow bypassed, Oracle itself will reject any write operation with `ORA-01456: may not perform insert/delete/update operation inside a READ ONLY transaction`. This is an Oracle-enforced hard stop, not a software check.

---

## Credential Security

Database passwords are stored exclusively in the **OS keyring**:

| Platform | Storage |
|----------|---------|
| macOS | Keychain |
| Windows | Credential Manager |
| Linux | Secret Service API (libsecret / GNOME Keyring / KWallet) |

The configuration file (`~/.config/adt/config.yaml` or platform equivalent) contains only connection metadata: host, port, username, and service name. It never contains a password.

adt intentionally does **not** read database passwords from environment variables. This is a design decision, not an oversight. Environment variables are visible to other processes on the same machine and are trivially exfiltrated by a compromised agent.

To add or rotate a password, use `adt auth set <profile>`.

---

## Audit Log Privacy

adt writes a structured audit log (JSON Lines format) containing:

- Timestamp
- Connection profile name
- Full SQL text (after validation, before execution)
- Row count returned
- Execution duration
- Whether the query was rejected and why

The **full SQL text** may contain sensitive values — for example, `WHERE ssn = '123-45-6789'` or `WHERE account_id = 99`. The audit log is intended for operational visibility and security review, not for sharing.

**Recommendation:** Apply appropriate filesystem permissions to the audit log directory. On Unix systems, `chmod 600` on the log file and `chmod 700` on the log directory limits access to the owning user.

The audit log location is printed by `adt config show`.

---

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Public issues are visible to everyone immediately, before a fix is available. Follow responsible disclosure instead.

### Preferred channel

Open a **GitHub Security Advisory** (private, visible only to maintainers):

> https://github.com/adt-tool/adt/security/advisories/new

### Alternative

Email the maintainer directly with the subject line **"adt security vulnerability"**. Contact details are in the repository's `go.mod` module path or the GitHub profile.

### What to include

- Description of the vulnerability and its impact
- Steps to reproduce (minimal reproducer preferred)
- Affected version(s)
- Any suggested fix or mitigation, if you have one

### Response timeline

- **Acknowledgement:** within 3 business days
- **Status update:** within 10 business days
- **Fix timeline:** depends on severity; critical issues are prioritised

### Disclosure process

We follow **coordinated responsible disclosure**:

1. Vulnerability reported privately.
2. Fix developed and tested.
3. Private advisory updated with CVE (if applicable).
4. Fix released.
5. Public advisory published with credit to the reporter (unless anonymity is requested).

We will not take legal action against researchers who report vulnerabilities in good faith and follow this process.
