# adt — Agentic DB Tool

專為 AI agent 設計的跨平台 Oracle 資料庫唯讀查詢 CLI 工具（相容 Claude Code 等）。

[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-00ADD8.svg)](https://golang.org)

---

## 簡介

`adt` 讓 AI agent（如 Claude Code）能安全地以唯讀方式存取 Oracle 資料庫。特別適合無法額外建立唯讀帳號、必須在應用層自行實施安全限制的場景。

若沒有 `adt` 這類守衛工具，擁有資料庫存取權的 AI agent 可能因幻覺或 prompt injection，執行 `DELETE`、`DROP`、`UPDATE` 等破壞性 SQL，造成無法復原的損失。`adt` 透過四道獨立防線，確保無論 agent 產生什麼 SQL，寫入操作都不可能成功。

---

## 主要特色

- **純 Go 單一 binary，免安裝 Oracle Instant Client** — 使用 `go-ora`（`github.com/sijms/go-ora/v2`）純 Go 驅動，下載單一 binary 即可使用。
- **四層唯讀防護** — 工具設計（無寫入 subcommand）→ SQL 關鍵字白名單 → 自動 ROWNUM 筆數限制 → DB 層 `SET TRANSACTION READ ONLY`。
- **OS Keyring 密碼管理** — 密碼儲存於 macOS Keychain、Windows Credential Manager 或 Linux Secret Service，不存於環境變數或設定檔，agent 無法輕易取得。
- **結構化 JSON 輸出** — 所有指令預設輸出機器可讀的 JSON，便於 agent 解析結果與錯誤。
- **完整稽核日誌** — 每次執行都寫入 JSON Lines 格式的稽核日誌，記錄完整 SQL、筆數、耗時與執行結果。

---

## 安裝

```bash
# macOS（Apple Silicon）
curl -L https://github.com/nilm987521/adt/releases/latest/download/adt-darwin-arm64 -o /usr/local/bin/adt
chmod +x /usr/local/bin/adt

# macOS（Intel）
curl -L https://github.com/nilm987521/adt/releases/latest/download/adt-darwin-amd64 -o /usr/local/bin/adt
chmod +x /usr/local/bin/adt

# 從原始碼編譯
git clone https://github.com/nilm987521/adt
cd adt && make build
```

---

## 快速開始

```bash
# 1. 設定資料庫環境
adt setup --env mydb

# 2. 列出資料表
adt list-tables

# 3. 查看資料表結構
adt describe ORDERS

# 4. 執行查詢
adt query "SELECT * FROM orders WHERE status = 'PENDING'"

# 5. 隨機取樣
adt sample ORDERS -n 10
```

---

## 指令說明

| 指令 | 說明 |
|------|------|
| `adt setup --env <name>` | 互動式設定：連線資訊寫入設定檔，密碼寫入 OS keyring |
| `adt env list` | 列出所有已設定的環境 |
| `adt env current` | 顯示目前預設環境 |
| `adt query "<sql>"` | 執行 SELECT 查詢（自動加上筆數限制與 READ ONLY transaction） |
| `adt list-tables [--schema <name>]` | 列出資料表，可指定 schema 篩選 |
| `adt describe <table>` | 顯示資料表欄位結構（查詢 `ALL_TAB_COLUMNS`） |
| `adt explain "<sql>"` | 顯示執行計畫（`EXPLAIN PLAN` + `DBMS_XPLAN`），不實際執行查詢 |
| `adt sample <table> [-n <count>]` | 使用 `DBMS_RANDOM.VALUE` 隨機取樣資料列 |
| `adt version` | 顯示版本、編譯時間與 Go 版本 |

---

## 全域 Flag

| Flag | 說明 | 預設值 |
|------|------|--------|
| `--env <name>` | 指定使用的環境 | 設定檔的 `default_env` |
| `--config <path>` | 指定設定檔路徑 | `~/.config/adt/config.yaml` |
| `--format json\|table\|csv` | 輸出格式 | `json` |
| `--limit <N>` | 覆蓋最大回傳筆數（0 = 使用設定值） | 設定值 |
| `--timeout <duration>` | 覆蓋查詢逾時（例如 `30s`） | 設定值 |
| `--dry-run` | 僅驗證 SQL，不實際執行 | `false` |
| `--confirm` | 對 production 環境執行時的明確確認 | `false` |

---

## 設定檔

### 路徑

| 平台 | 預設路徑 |
|------|---------|
| macOS / Linux | `~/.config/adt/config.yaml` |
| Windows | `%APPDATA%\adt\config.yaml` |

可用 `--config <path>` 覆蓋。

### 設定檔範例

```yaml
config_version: 1               # Schema 版本，未來變更用於 migration

default_env: fetnet-dev

environments:
  fetnet-dev:
    user: dev_user
    host: dev-db.fetnet.net
    port: 1521
    service: DEVDB

  fetnet-stg:
    user: stg_user
    host: stg-db.fetnet.net
    port: 1521
    service: STGDB

  fetnet-prod:
    user: prod_user
    host: prod-db.fetnet.net
    port: 1521
    service: PRODDB
    production: true            # 標記為正式環境
    require_confirmation: true  # 執行前需要 --confirm flag
    max_rows: 500               # 覆蓋預設筆數限制
    timeout: 15s                # 覆蓋預設逾時

defaults:
  max_rows: 1000
  timeout: 30s

audit:
  log_path: ~/.local/share/adt/audit.log
```

### 檔案權限

在 Unix 系統上，`adt` 建立設定檔時會設定 `0600`（僅擁有者可讀寫）。若手動建立，請執行：

```bash
chmod 600 ~/.config/adt/config.yaml
```

### 優先序

`--flag` > 環境特定設定 > `defaults` 全域設定

> **注意**：刻意不支援從環境變數讀取 DB 連線資訊。詳見安全架構說明。

---

## 安全架構

`adt` 透過四道獨立防線實施唯讀限制。agent 必須同時突破所有四道防線才可能造成寫入，實際上不可能發生。

### 第 1 層：工具設計

工具只提供讀取導向的 subcommand（`query`、`list-tables`、`describe`、`explain`、`sample`），不存在 `exec`、`run` 或任何可被濫用的萬能指令。

### 第 2 層：SQL 關鍵字白名單

SQL 送達資料庫前，`adt` 依序驗證：

1. 移除所有註解（`--` 單行、`/* */` 多行）
2. 移除字串常數（`'...'`），避免內文關鍵字觸發誤判
3. **要求**語句必須以 `SELECT` 或 `WITH`（CTE）開頭
4. **拒絕**多語句（`;` 後有非空白內容）
5. **拒絕** `INTO`（防止 `SELECT INTO` 寫入）
6. **拒絕** `FOR UPDATE`（防止 row lock）
7. **拒絕** PL/SQL block 關鍵字：`BEGIN`、`DECLARE`、`CALL`

違反任一規則 → exit code 2，JSON 錯誤訊息明確指出觸發原因。

### 第 3 層：自動 ROWNUM 筆數限制

`adt` 將每個查詢包在 subquery 內加上上限（相容 Oracle 11g 語法）：

```sql
SELECT * FROM (
    <你的原始 SQL>
) WHERE ROWNUM <= <max_rows>
```

防止 agent 觸發返回數百萬筆的全表掃描，且正確處理含 `ORDER BY` 的查詢（先排序再截取）。

### 第 4 層：SET TRANSACTION READ ONLY

每次查詢都在 `SET TRANSACTION READ ONLY` transaction 內執行。即使 SQL 繞過了第 2 層驗證，Oracle 本身也會以 `ORA-01456` 拒絕任何 DML 或 DDL，在資料庫層提供硬性保障。

### 密碼管理

密碼僅儲存於 OS keyring：

- **macOS**：Keychain（service: `adt`，account: `oracle-password-<env>`）
- **Windows**：Credential Manager
- **Linux**：Secret Service（需 GNOME Keyring、KWallet 或 libsecret）

密碼**不**儲存於：
- 環境變數 — agent 可透過 `printenv` 或 `/proc/self/environ` 讀取
- 設定檔 — 設定檔可被同一使用者下的任何程序（包括 agent）讀取

### Production 環境額外保護

設定 `require_confirmation: true` 的環境，未加 `--confirm` 時拒絕執行：

```bash
# 被拒絕 — exit code 2
adt query "SELECT COUNT(*) FROM orders" --env fetnet-prod

# 正常執行
adt query "SELECT COUNT(*) FROM orders" --env fetnet-prod --confirm
```

所有 production 環境的 JSON 輸出都包含 `"production": true`，讓 agent 持續意識到目前在正式環境。

---

## 稽核日誌

每次 `adt` 執行（無論成功或失敗）都會在稽核日誌追加一筆記錄。

### 路徑

| 平台 | 預設路徑 |
|------|---------|
| macOS / Linux | `~/.local/share/adt/audit.log` |
| Windows | `%LOCALAPPDATA%\adt\audit.log` |

可在設定檔的 `audit.log_path` 覆蓋。

### 格式

JSON Lines（每行一個 JSON 物件）：

```json
{"ts":"2026-04-17T10:23:45+08:00","env":"fetnet-dev","cmd":"query","sql":"SELECT * FROM users WHERE id = 1","rows":1,"elapsed_ms":45,"status":"ok"}
{"ts":"2026-04-17T10:24:10+08:00","env":"fetnet-prod","cmd":"query","sql":"DELETE FROM users","rows":0,"elapsed_ms":2,"status":"rejected","error":"sql_not_select"}
```

### 欄位說明

| 欄位 | 說明 |
|------|------|
| `ts` | RFC3339 時間戳 |
| `env` | 環境名稱 |
| `cmd` | 執行的 subcommand |
| `sql` | SQL 全文 |
| `rows` | 回傳筆數（被拒絕或失敗時為 0） |
| `elapsed_ms` | 執行耗時（毫秒） |
| `status` | `ok` / `rejected` / `db_error` / `timeout` |
| `error` | 錯誤代碼（status 非 ok 時出現） |

> **隱私提醒**：稽核日誌記錄完整 SQL 文字，其中 `WHERE` 條件可能包含敏感值（如 `WHERE id_number = '...'`）。請將 `audit.log` 視為敏感檔案並妥善保護。本工具未內建 log rotation，可使用 `logrotate` 或手動定期歸檔。

---

## AI Agent 整合（CLAUDE.md）

將以下內容加入你的專案 `CLAUDE.md` 或 `~/.claude/CLAUDE.md`：

```markdown
## adt (Agentic DB Tool)

本機有 `adt` 可查詢 Oracle DB，**只能唯讀**。

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

## Exit Code

| Code | 說明 |
|------|------|
| `0` | 成功 |
| `1` | 使用者錯誤（SQL 語法錯、找不到環境、設定缺失） |
| `2` | 安全拒絕（非 SELECT 查詢、多語句、PL/SQL block、production 未加 `--confirm`） |
| `3` | DB 連線或執行錯誤（逾時、網路中斷、Oracle 錯誤） |

---

## 錯誤代碼

JSON 輸出中的 `error` 欄位為穩定的英文 key，供 agent 或 script 程式化判斷錯誤類型。

| 錯誤代碼 | 說明 |
|----------|------|
| `sql_not_select` | SQL 不以 `SELECT` 或 `WITH` 開頭 |
| `multi_statement` | SQL 含多語句（`;` 後有非空白內容） |
| `forbidden_keyword` | SQL 含禁用關鍵字（`INTO`、`FOR UPDATE`、`BEGIN`、`DECLARE`、`CALL`） |
| `production_not_confirmed` | 對 production 環境執行時未加 `--confirm` |
| `env_not_found` | 指定的環境不存在於設定檔 |
| `credential_not_found` | OS keyring 中找不到該環境的密碼 |
| `db_connection_failed` | 無法建立 Oracle 資料庫連線 |
| `db_error` | Oracle 執行時錯誤（同時包含 `oracle_code` 欄位，如 `ORA-00942`） |
| `timeout` | 查詢超過設定的逾時時間 |
| `row_limit_exceeded` | 超過硬上限筆數（保留給未來使用） |

錯誤代碼視為公開 API 的一部分。破壞性變更需 bump major version。

---

## Oracle 版本支援

- **主要目標版本**：Oracle 11g（11.2.0.4.0 — 64-bit）
- **相容版本**：Oracle 12c 至 23c
- 使用 `ROWNUM` 分頁，而非 `FETCH FIRST N ROWS ONLY`（11g 不支援此語法）
- 驅動：`go-ora`（`github.com/sijms/go-ora/v2`）— 純 Go 實作，無需 Oracle Instant Client

---

## 授權

MIT — 詳見 [LICENSE](LICENSE)。
