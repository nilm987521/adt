package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry is a single audit log record written in JSON Lines format.
type Entry struct {
	Ts        string `json:"ts"`
	Env       string `json:"env"`
	Cmd       string `json:"cmd"`
	SQL       string `json:"sql"`
	Rows      int    `json:"rows"`
	ElapsedMs int64  `json:"elapsed_ms"`
	Status    string `json:"status"` // "ok", "rejected", "db_error", "timeout"
	Error     string `json:"error,omitempty"`
}

// Write appends an audit entry to the log file in JSON Lines format.
// Creates parent directories if needed.
// If logPath is empty, Write is a no-op.
func Write(logPath string, entry Entry) error {
	if logPath == "" {
		return nil // no-op if not configured
	}

	// Expand ~ in path
	if len(logPath) >= 2 && logPath[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			logPath = filepath.Join(home, logPath[2:])
		}
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return err
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	entry.Ts = time.Now().Format(time.RFC3339)
	return json.NewEncoder(f).Encode(entry)
}
