# adt (Agentic DB Tool) — 開發規格

## 1. 專案概述

### 1.1 專案名稱

**adt**（Agentic DB Tool）

一個跨平台（macOS / Windows / Linux）的關聯式資料庫查詢 CLI 工具，支援 Oracle、PostgreSQL、MySQL 與 SQL Server，設計為供本機 AI agent（如 Claude Code）安全使用。

### 1.2 授權

**MIT License**

公開發佈於 GitHub，允許商業與非商業使用。

### 1.3 專案目標

- 在**既有資料庫帳號**（無法新增唯讀帳號）的限制下，於應用層自行擋下所有危險操作
- 確保 AI agent 無論如何都不會對資料庫造成寫入或破壞
- 提供跨平台單一 binary，無須安裝 Oracle Instant Client
- 為未來公開發佈設計，架構與命名從一開始就考慮開源使用者

### 1.4 使用者與威脅模型

**執行者**：AI agent，以使用者身分在本機執行 CLI

**目標使用者**：

- 階段一：開發者本人（內部使用）
- 階段二：公開發佈後，其他開發者在自己機器上與自己的 agent 使用

**主要風險**：

- Agent 幻覺產生錯誤 SQL（如無 WHERE 的 UPDATE、誤用的 DROP）
- Prompt injection 導致 agent 執行惡意操作
- Agent 為了「完成任務」跑出非預期的破壞性或高成本查詢
- Agent 讀取環境變數、設定檔，取得 DB 憑證

**非風險（已排除）**：

- 反編譯分析（使用者自己的 binary、自己的 DB）
- 惡意終端使用者（工具執行者 = 工具擁有者）
- 網路攻擊（本機工具，無對外服務）

### 1.5 核心設計原則

1. **應用層預設拒絕**：只允許明確安全的 SELECT 操作，其餘一律拒絕
2. **雙層防護**：應用層 SQL 驗證 + DB 層 READ ONLY transaction
3. **最小權限暴露給 agent**：只提供唯讀 subcommand，不給萬能 `exec`
4. **憑證隱藏**：密碼存於 OS keyring，agent 難以透過常見手段取得
5. **完整稽核**：所有執行記錄落檔，可追溯
6. **穩定對外介面**：error code、JSON 結構、config schema 都以版本化方式演進

---

## 2. 技術棧

| 項目 | 選擇 | 理由 |
|------|------|------|
| 語言 | Go 1.22+ | 跨平台單 binary、交叉編譯簡單、agent 熟悉 |
| CLI 框架 | cobra | 業界標準、生態完整、未來擴充彈性高 |
| 設定管理 | viper | 與 cobra 整合良好、支援多來源設定 |
| Oracle Driver | go-ora (`github.com/sijms/go-ora/v2`) | 純 Go 實作、免 Oracle Instant Client、支援 11g |
| PostgreSQL Driver | pgx/v5 (`github.com/jackc/pgx/v5/stdlib`) | 高效能、原生 `BEGIN READ ONLY` |
| MySQL Driver | go-sql-driver/mysql (`github.com/go-sql-driver/mysql`) | 業界標準純 Go 實作 |
| SQL Server Driver | go-mssqldb (`github.com/microsoft/go-mssqldb`) | Microsoft 官方 Go 驅動 |
| 憑證儲存 | `github.com/zalando/go-keyring` | 三平台 OS keyring 抽象化 |

### 2.1 目標 Oracle 版本

- **Oracle Database 11g Enterprise Edition Release 11.2.0.4.0 - 64bit Production**
- 影響設計的 11g 限制：
    - 不支援 `FETCH FIRST N ROWS ONLY`（12c+ 語法），需改用 `ROWNUM`
    - `ROWNUM` 需配合 subquery 才能正確處理 `ORDER BY`
- 預期相容性：go-ora 支援 11g ~ 23c，未來使用者若用在較新版本應可直接運作

---

## 3. 專案結構（Go 標準布局）

```
adt/
├── cmd/
│   └── adt/
│       └── main.go              # 進入點
├── internal/                    # 只有本專案能 import
│   ├── cli/                     # cobra command 定義
│   │   ├── root.go
│   │   ├── setup.go
│   │   ├── env.go
│   │   ├── query.go
│   │   ├── list_tables.go
│   │   ├── describe.go
│   │   ├── explain.go
│   │   └── sample.go
│   ├── config/                  # 設定檔讀寫、schema migration
│   ├── security/                # SQL 驗證、risk 判斷
│   ├── oracle/                  # go-ora 連線管理、READ ONLY tx
│   ├── keyring/                 # OS keyring 封裝
│   ├── audit/                   # 稽核 log 寫入
│   └── output/                  # JSON/table/CSV 格式化
├── LICENSE                      # MIT License
├── README.md
├── CHANGELOG.md
├── SECURITY.md                  # 安全政策、漏洞回報
├── CONTRIBUTING.md              # 貢獻指引
├── Makefile                     # 編譯與交叉編譯
├── .goreleaser.yaml             # 發版自動化
├── go.mod
└── go.sum
```

`internal/` 的 package 外部無法 import，避免未來被誤用為公開 API。有需要暴露的穩定 API 再搬到 `/pkg/`。

---

## 4. CLI 命令結構

```
adt
├── version                         # 顯示版本、build 時間、driver 版本
├── setup --env <name>              # 互動式儲存連線資訊與密碼
├── env
│   ├── list                        # 列出所有環境
│   └── current                     # 顯示目前 default_env
├── query <sql>                     # 執行 SELECT 查詢
├── list-tables [--schema <name>]   # 列出資料表
├── describe <table>                # 顯示資料表結構
├── explain <sql>                   # 執行 EXPLAIN PLAN（不實際執行）
└── sample <table> [-n <count>]     # 隨機取樣
```

### 4.1 全域 Flag

| Flag | 說明 | 預設值 |
|------|------|--------|
| `--env <name>` | 指定環境 | config 的 `default_env` |
| `--config <path>` | 指定設定檔路徑 | `~/.config/adt/config.yaml` |
| `--format <json\|table\|csv>` | 輸出格式 | `json` |
| `--limit <N>` | 覆蓋最大回傳筆數 | config 設定值 |
| `--timeout <duration>` | 覆蓋 query timeout | config 設定值 |
| `--dry-run` | 僅驗證 SQL，不實際執行 | false |
| `--confirm` | 對 production 環境明確確認 | 視環境設定 |

### 4.2 Exit Code

| Code | 意義 |
|------|------|
| 0 | 成功 |
| 1 | 使用者錯誤（SQL 語法錯、權限不足、找不到環境） |
| 2 | 安全拒絕（非 SELECT、多語句、PL/SQL block、超過硬上限、prod 未確認） |
| 3 | DB 連線或執行錯誤（timeout、網路斷線等） |

---

## 5. 設定檔

### 5.1 路徑（依平台 XDG 慣例）

- Linux / macOS：`~/.config/adt/config.yaml`
- Windows：`%APPDATA%\adt\config.yaml`
- 可用 `--config` 覆蓋
- 權限要求：Unix 系統 `chmod 600`，Windows 對應 ACL

### 5.2 結構

```yaml
config_version: 3               # Schema 版本；v3 新增 data masking 支援

default_env: oracle-dev

environments:
  oracle-dev:
    driver: oracle              # oracle | postgres | mysql | mssql
    user: dev_user
    host: dev-db.example.net
    port: 1521
    service: DEVDB              # Oracle 使用 service；其他 DB 使用 database

  pg-dev:
    driver: postgres
    user: dev_user
    host: pg.example.net
    port: 5432
    database: myapp

  oracle-prod:
    driver: oracle
    user: prod_user
    host: prod-db.example.net
    port: 1521
    service: PRODDB
    production: true            # 標記為正式環境
    require_confirmation: true  # 執行前需 --confirm flag
    max_rows: 500               # 覆蓋預設 row limit
    timeout: 15s                # 覆蓋預設 timeout
    mask_columns:               # 選填：此環境額外要遮罩的欄位（疊加至全域設定）
      - phone

defaults:
  max_rows: 1000
  timeout: 30s
  mask_columns:                 # 選填：全域遮罩欄位，套用於所有環境
    - id_number
    - email

audit:
  log_path: ~/.local/share/adt/audit.log
```

`mask_columns` 欄位名稱比對為 **case-insensitive**（不分大小寫）。實際生效的遮罩集合為全域 `defaults.mask_columns` 與各環境 `mask_columns` 的**聯集（union）**。被遮罩的欄位值在所有輸出格式（JSON、table、CSV）中均顯示為 `[REDACTED]`。

### 5.3 Schema Version 與遷移策略

- 第一版寫入 `config_version: 1`
- 未來 schema 有破壞性變更時 bump 版本號
- 啟動時偵測到舊版本自動提示，或提供 `adt config migrate` 指令
- 讀取時若缺少 `config_version` 欄位，視為 version 1 並警告
- v1→v2：自動遷移，新增多 DB driver 支援
- v2→v3：自動遷移，新增 data masking 支援（`mask_columns` 欄位）

### 5.4 環境命名規則

- **完全自訂**：`environments` 底下 key 可以是任意字串
- **建議格式**：
    - 只用小寫英數字和 `-` / `_`
    - 避免空格、特殊字元、中文、emoji
    - 範例：`fetnet-dev`、`clientb-uat`、`local-test`

### 5.5 設定優先序

Flag > 環境特定設定 > 全域 `defaults`

**刻意不支援從環境變數讀取 DB 連線資訊**（見第 6 節憑證管理）。

---

## 6. 憑證管理

### 6.1 方案：OS Keyring

- **macOS**：Keychain
- **Windows**：Credential Manager
- **Linux**：Secret Service（需 GNOME Keyring、KWallet 或 libsecret）

### 6.2 Entry 命名

- Service：`adt`
- Account：`oracle-password-<env_name>`
- 範例：
    - `adt:oracle-password-fetnet-dev`
    - `adt:oracle-password-fetnet-prod`

### 6.3 不使用環境變數

為防止 agent 透過 `printenv`、`/proc/self/environ`、log 誤印等手段取得密碼，**刻意不支援**從環境變數讀取 DB 密碼。

此設計決策會寫進 README 的「安全模型」章節，讓公開使用者理解取捨。

### 6.4 設定流程

```bash
adt setup --env fetnet-dev
# 互動式輸入 user / host / port / service（寫入 config.yaml）
# 互動式輸入 password（寫入 OS keyring）
```

重複執行相同 `--env` 會更新既有設定。

---

## 7. 安全架構

### 7.1 分層防禦

由 CLI 外層到 DB 層，共 4 道防線：

```
Agent 輸入 SQL
    ↓
[第 1 層] Tool 設計：只提供唯讀 subcommand，無寫入 tool
    ↓
[第 2 層] SQL 關鍵字驗證：白名單檢查
    ↓
[第 3 層] 自動注入 row limit + query timeout
    ↓
[第 4 層] SET TRANSACTION READ ONLY 包裝
    ↓
Oracle DB 執行
```

### 7.2 第 2 層：SQL 驗證規則

**第一版不使用完整 SQL parser**（Oracle 11g 特殊語法 parser 支援度差、維護成本高），採用**關鍵字白名單檢查**：

執行順序：

1. **預處理**：移除所有註解（`--` 單行、`/* */` 多行）
2. **預處理**：移除字串常數（`'...'`）避免誤判內文關鍵字
3. **檢查**：去除前導空白後，必須以 `SELECT` 或 `WITH`（CTE）開頭
4. **檢查**：不得含有額外的 `;` 後還有非空白內容（防多語句）
5. **檢查**：不得含 `INTO`（防 SELECT INTO 寫入）
6. **檢查**：不得含 `FOR UPDATE`（防 row lock）
7. **檢查**：不得含 PL/SQL block 關鍵字（`BEGIN`、`DECLARE`、`CALL`）

違反任一條 → 退出 code 2，錯誤訊息明確指出原因。

未來階段可能引入真正的 SQL parser 作為增強，但 READ ONLY transaction 始終是最終防線。

### 7.3 第 3 層：自動 Row Limit（11g 語法）

將 agent 的原始 SQL 包在 subquery 內：

```sql
SELECT * FROM (
    <agent 的原始 SQL>
) WHERE ROWNUM <= <max_rows>
```

此寫法在有無 `ORDER BY` 的情況下都正確（先排序再截取）。

### 7.4 第 4 層：READ ONLY Transaction

每次 query 執行流程：

```
1. 從 connection pool 取連線
2. 執行 SET TRANSACTION READ ONLY
3. 執行包裝後的 SELECT
4. COMMIT 或 ROLLBACK
5. 連線歸還 pool
```

任何 DML/DDL 即使繞過第 2 層驗證，也會在此層觸發 Oracle `ORA-01456` 錯誤被擋下。

### 7.5 Production 環境額外保護

當環境設定 `require_confirmation: true`：

- 執行任何會查詢 DB 的 subcommand 時必須加 `--confirm` flag
- 沒加 flag 直接拒絕（exit 2），錯誤訊息指出這是 production 環境
- 輸出 JSON 會包含 `"env": "<name>"`、`"production": true` 欄位，讓 agent context 持續意識到目前在 prod

---

## 8. 輸出格式

### 8.1 JSON（預設，agent 友善）

**成功 query**：

```json
{
  "env": "fetnet-dev",
  "production": false,
  "cmd": "query",
  "rows": [
    {"id": 1, "name": "Alice"},
    {"id": 2, "name": "Bob"}
  ],
  "row_count": 2,
  "truncated": false,
  "elapsed_ms": 123,
  "executed_sql": "SELECT * FROM (SELECT id, name FROM users) WHERE ROWNUM <= 1000"
}
```

**被安全層拒絕**：

```json
{
  "error": "sql_not_select",
  "message": "only SELECT or WITH queries are allowed",
  "original_sql": "DELETE FROM users"
}
```

**DB 錯誤**：

```json
{
  "error": "db_error",
  "message": "ORA-00942: table or view does not exist",
  "oracle_code": "ORA-00942"
}
```

### 8.2 Error Code 穩定性

`error` 欄位為**穩定的英文 key**，供 agent 或 script 判斷錯誤類型。

| Error Code | 意義 |
|------------|------|
| `sql_not_select` | SQL 不是 SELECT/WITH 開頭 |
| `multi_statement` | 含多語句 |
| `forbidden_keyword` | 含禁用關鍵字（INTO、FOR UPDATE、PL/SQL block 等） |
| `production_not_confirmed` | Production 環境未加 `--confirm` |
| `env_not_found` | 指定的環境不存在 |
| `credential_not_found` | Keyring 中找不到密碼 |
| `db_connection_failed` | DB 連線失敗 |
| `db_error` | Oracle 執行時錯誤（帶 `oracle_code`） |
| `timeout` | 查詢超時 |
| `row_limit_exceeded` | 超過硬上限（保留給未來） |

Error code 一旦發佈，視為公開 API 的一部分，破壞性變更需 bump major version。

### 8.3 Table 與 CSV

- `--format table`：對齊表格，供人類閱讀
- `--format csv`：標準 CSV，可重導到檔案

### 8.4 型別序列化

- Oracle `NUMBER` → JSON number（超過 JS 安全整數範圍時改為字串表示，JSON 欄位附註）
- Oracle `DATE` / `TIMESTAMP` → RFC3339 字串（如 `"2026-04-17T10:23:45+08:00"`）
- Oracle `CLOB` → 字串
- Oracle `BLOB` → base64 字串，並加註提示
- `NULL` → JSON `null`

---

## 9. 稽核 Log

### 9.1 格式：JSON Lines

每次執行寫一筆，不論成功失敗：

```json
{"ts":"2026-04-17T10:23:45+08:00","env":"fetnet-dev","cmd":"query","sql":"SELECT * FROM users WHERE id = 1","rows":1,"elapsed_ms":45,"status":"ok"}
{"ts":"2026-04-17T10:24:10+08:00","env":"fetnet-prod","cmd":"query","sql":"DELETE FROM users","rows":0,"elapsed_ms":2,"status":"rejected","error":"sql_not_select"}
```

### 9.2 記錄欄位

| 欄位 | 說明 |
|------|------|
| `ts` | RFC3339 時間戳 |
| `env` | 環境名稱 |
| `cmd` | subcommand 名稱 |
| `sql` | **SQL 全文**（記錄完整內容，便於除錯與追溯） |
| `rows` | 回傳筆數（rejected 時為 0） |
| `elapsed_ms` | 執行耗時（毫秒） |
| `status` | `ok` / `rejected` / `db_error` / `timeout` |
| `error` | 錯誤代碼（status 非 ok 時） |

### 9.3 路徑

- Linux / macOS 預設：`~/.local/share/adt/audit.log`
- Windows 預設：`%LOCALAPPDATA%\adt\audit.log`
- 可在 config 的 `audit.log_path` 覆蓋

### 9.4 Log Rotation

**第一版不實作**自動 rotation，使用者需自行處理（可用 `logrotate` 或定期手動歸檔）。README 會提示。

### 9.5 隱私提醒

因 SQL 可能含 `WHERE id_number = '...'` 之類敏感欄位，audit.log 本身即為敏感檔案，README 會提示使用者適當保護。

---

## 10. 編譯與發布

### 10.1 交叉編譯

```bash
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o dist/adt-darwin-arm64 ./cmd/adt

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o dist/adt-darwin-amd64 ./cmd/adt

# Windows
GOOS=windows GOARCH=amd64 go build -o dist/adt-windows-amd64.exe ./cmd/adt

# Linux
GOOS=linux GOARCH=amd64 go build -o dist/adt-linux-amd64 ./cmd/adt
GOOS=linux GOARCH=arm64 go build -o dist/adt-linux-arm64 ./cmd/adt
```

以 Makefile 封裝。

### 10.2 編譯優化

```bash
go build -ldflags "-s -w" -trimpath -o adt ./cmd/adt
```

- `-s -w`：去除除錯符號，縮小 binary
- `-trimpath`：移除編譯路徑資訊

### 10.3 版本資訊

編譯時注入版本：

```bash
go build -ldflags "
  -X main.version=$(git describe --tags --always)
  -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)
" -o adt ./cmd/adt
```

`adt version` subcommand 顯示版本、build 時間、Go 版本、go-ora 版本。

### 10.4 Semver

- 初期使用 `v0.x.x`（unstable，API 可能變動）
- 穩定後發 `v1.0.0`
- Git tag 驅動 goreleaser 自動發版

### 10.5 CI/CD（公開發佈階段）

- **GitHub Actions**：PR/push 跑 `go test`、`go vet`、`golangci-lint`
- **goreleaser**：推 tag 自動產生各平台 binary 並建立 GitHub Release
- **CHANGELOG.md**：由 conventional commits + goreleaser 自動產生

---

## 11. Agent 整合指引

### 11.1 CLAUDE.md 範例

部署時放在 project 或 `~/.claude/CLAUDE.md`：

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

## 12. 開發階段

### 階段 1：核心骨架（MVP，v0.1.0）

- [x] Cobra + viper 基礎架構，`/cmd` + `/internal` 布局
- [x] `setup` subcommand（keyring 寫入）
- [x] `env list` / `env current`
- [x] `query` subcommand 完整實作
    - [x] SQL 關鍵字驗證
    - [x] Row limit 自動注入
    - [x] READ ONLY transaction
    - [x] Timeout
    - [x] JSON 輸出
- [x] 稽核 log
- [x] Config schema version 支援
- [x] 三平台編譯 Makefile

### 階段 2：輔助 subcommand（v0.2.0）

- [x] `list-tables`
- [x] `describe`
- [x] `explain`
- [x] `sample`
- [x] Table / CSV 輸出格式
- [x] `--dry-run`

### 階段 3：公開發佈準備（v0.3.0 ~ v1.0.0）

- [x] README（含 Quick Start、asciinema demo）
- [x] SECURITY.md（威脅模型、漏洞回報）
- [x] CONTRIBUTING.md
- [x] LICENSE（MIT）
- [x] GitHub Actions CI
- [x] goreleaser 配置
- [x] CHANGELOG.md
- [x] 單元測試與整合測試
- [ ] 公開於 GitHub

### 階段 4：多資料庫支援（v0.4.0）

- [x] `db.Driver` interface + `QueryResult` 型別（`internal/db/driver.go`）
- [x] Oracle driver 搬移至 `internal/db/oracle/`，實作 `db.Driver`
- [x] PostgreSQL driver (`internal/db/postgres/`)
- [x] MySQL driver (`internal/db/mysql/`)
- [x] SQL Server driver (`internal/db/mssql/`)
- [x] `dbfactory.NewDriver` factory (`internal/dbfactory/factory.go`)
- [x] Config v2：`driver` + `database` 欄位、v1→v2 自動 migration
- [x] CLI commands 改用 `dbfactory.NewDriver`
- [x] `setup` subcommand 支援 driver 選擇與各 DB 預設 port
- [x] 各 driver 單元測試（`wrapWithRowLimit`、DSN 格式）

### 階段 5：推廣與強化（v1.x 之後）

- [ ] Homebrew tap
- [ ] Scoop / WinGet 支援
- [ ] 更嚴謹的 SQL parser 評估
- [ ] Schema 白名單
- [ ] Log rotation
- [ ] 更完整的型別序列化
- [ ] Shell completion 產生

---

## 13. 公開發佈 Checklist

發版前逐項確認：

**法律與授權**

- [x] LICENSE 檔存在且為 MIT
- [x] 所有依賴套件授權相容（Apache/MIT/BSD）
- [x] README 明確標示授權

**文件**

- [x] README 有 Quick Start、安裝、使用範例
- [x] SECURITY.md 說明威脅模型與漏洞回報流程
- [x] CHANGELOG.md 依 Keep a Changelog 格式
- [x] 所有 error code 有文件
- [x] Config schema 有文件

**程式碼品質**

- [x] 無 hardcode 的客戶 / 個人資訊
- [x] 無暴露個人路徑的 panic message
- [x] `go vet` / `golangci-lint` 無警告
- [x] 核心邏輯（SQL 驗證、READ ONLY tx）有單元測試

**發版機制**

- [x] goreleaser 設定完成，可自動產生三平台 binary
- [x] GitHub Actions 綠燈
- [x] Git tag semver 格式正確

**安全**

- [x] 無 commit 過的憑證或 secret
- [x] 無 telemetry 或對外連線（除 DB 本身）

---

## 14. 決策記錄（ADR 摘要）

| 決策 | 選擇 | 替代方案 | 主要理由 |
|------|------|----------|----------|
| 專案名稱 | `adt` (Agentic DB Tool) | mytool、oraq、orasafe | 簡短、描述準確、反映 agentic 設計初衷 |
| 授權 | MIT | Apache 2.0、AGPL | 最寬鬆、推廣阻力最小 |
| 介面形式 | CLI subcommand | MCP server | 簡單、agent 透過 shell 即可使用 |
| 語言 | Go | Rust | Oracle driver 有純 Go 實作，跨平台打包乾淨 |
| Oracle Driver | go-ora | godror | 不需 Oracle Instant Client，真正單 binary |
| 套件布局 | `/cmd` + `/internal` | 平面結構 | Go 社群標準，`internal/` 防止外部誤 import |
| SQL 驗證 | 關鍵字白名單 | 完整 parser | 11g 語法 parser 支援差，READ ONLY transaction 為可靠後援 |
| 密碼儲存 | OS keyring | 環境變數、設定檔、編譯注入 | Agent 實務上最難透過常見手段取得 |
| 多環境 | 自訂命名 + `production` 旗標 | 固定 dev/stg/prod | 使用者可能有多個客戶、非標準階段劃分 |
| Prod 保護 | `--confirm` flag | 互動確認、無保護 | 不破壞 agent 自動化，但強制 agent 明確意識 |
| 稽核 SQL | 完整記錄 | 遮蔽字串常數、僅 hash | 優先除錯與追溯能力 |
| Config schema | 帶 `config_version` | 無版本 | 未來變更可無痛遷移 |
| Error code | 穩定英文 key | 僅中文訊息 | 供 agent / script 判斷，也利於國際化 |

---

*文件版本：v1.0　　日期：2026-04-17*
