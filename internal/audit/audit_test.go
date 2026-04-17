package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWrite_CreatesFileAndAppendsEntries(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "sub", "audit.log")

	e1 := Entry{Env: "test", Cmd: "query", SQL: "SELECT 1 FROM DUAL", Rows: 1, ElapsedMs: 10, Status: "ok"}
	e2 := Entry{Env: "test", Cmd: "query", SQL: "DELETE FROM x", Rows: 0, ElapsedMs: 2, Status: "rejected", Error: "sql_not_select"}

	if err := Write(logPath, e1); err != nil {
		t.Fatalf("Write e1: %v", err)
	}
	if err := Write(logPath, e2); err != nil {
		t.Fatalf("Write e2: %v", err)
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var entries []Entry
	for scanner.Scan() {
		var e Entry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Status != "ok" {
		t.Errorf("entry 0 status = %q, want ok", entries[0].Status)
	}
	if entries[1].Error != "sql_not_select" {
		t.Errorf("entry 1 error = %q, want sql_not_select", entries[1].Error)
	}
	if entries[0].Ts == "" {
		t.Error("Ts not set on entry 0")
	}
	if entries[1].Ts == "" {
		t.Error("Ts not set on entry 1")
	}
}

func TestWrite_TsIsRFC3339(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	before := time.Now().UTC().Truncate(time.Second)
	if err := Write(logPath, Entry{Env: "x", Cmd: "query", SQL: "SELECT 1", Status: "ok"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	f, _ := os.Open(logPath)
	defer f.Close()
	var e Entry
	if err := json.NewDecoder(f).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ts, err := time.Parse(time.RFC3339, e.Ts)
	if err != nil {
		t.Fatalf("Ts %q is not RFC3339: %v", e.Ts, err)
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Ts %v is outside expected range [%v, %v]", ts, before, after)
	}
}

func TestWrite_TildePath(t *testing.T) {
	// ~/... path should expand and write
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	// Use a unique subpath to avoid polluting actual home dir
	rel := filepath.Join(".adt-test-audit", "audit_tilde_test.log")
	absPath := filepath.Join(home, rel)
	tildePath := "~/" + rel

	defer os.RemoveAll(filepath.Join(home, ".adt-test-audit"))

	if err := Write(tildePath, Entry{Env: "tilde", Cmd: "query", SQL: "SELECT 1", Status: "ok"}); err != nil {
		t.Fatalf("Write with tilde path: %v", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		t.Errorf("file not found at expanded path %s: %v", absPath, err)
	}
}

func TestWrite_EmptyPath(t *testing.T) {
	// empty logPath → no error, no file created
	err := Write("", Entry{Env: "x", Cmd: "query", SQL: "SELECT 1", Status: "ok"})
	if err != nil {
		t.Errorf("Write with empty path should be a no-op, got error: %v", err)
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "a", "b", "c", "d", "audit.log")

	if err := Write(logPath, Entry{Env: "deep", Cmd: "query", SQL: "SELECT 1", Status: "ok"}); err != nil {
		t.Fatalf("Write with deep nested path: %v", err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Errorf("file not created at deep path: %v", err)
	}
}

func TestWrite_AppendsBetweenCalls(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	for i := 0; i < 5; i++ {
		e := Entry{Env: "test", Cmd: "query", SQL: "SELECT 1", Rows: i, ElapsedMs: int64(i * 10), Status: "ok"}
		if err := Write(logPath, e); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}
}

func TestWrite_FilePermissions(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	if err := Write(logPath, Entry{Env: "x", Cmd: "query", SQL: "SELECT 1", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestWrite_AllFields(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	e := Entry{
		Env:       "prod",
		Cmd:       "query",
		SQL:       "SELECT * FROM orders",
		Rows:      42,
		ElapsedMs: 150,
		Status:    "ok",
		Error:     "",
	}
	if err := Write(logPath, e); err != nil {
		t.Fatal(err)
	}

	f, _ := os.Open(logPath)
	defer f.Close()
	var got Entry
	if err := json.NewDecoder(f).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Env != "prod" {
		t.Errorf("Env = %q, want prod", got.Env)
	}
	if got.SQL != "SELECT * FROM orders" {
		t.Errorf("SQL = %q", got.SQL)
	}
	if got.Rows != 42 {
		t.Errorf("Rows = %d, want 42", got.Rows)
	}
	if got.ElapsedMs != 150 {
		t.Errorf("ElapsedMs = %d, want 150", got.ElapsedMs)
	}
}

func TestWrite_RejectedEntryHasError(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	e := Entry{Env: "dev", Cmd: "query", SQL: "DROP TABLE x", Rows: 0, Status: "rejected", Error: "sql_not_select"}
	if err := Write(logPath, e); err != nil {
		t.Fatal(err)
	}

	f, _ := os.Open(logPath)
	defer f.Close()
	var got Entry
	if err := json.NewDecoder(f).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "rejected" {
		t.Errorf("Status = %q, want rejected", got.Status)
	}
	if got.Error != "sql_not_select" {
		t.Errorf("Error = %q, want sql_not_select", got.Error)
	}
}
