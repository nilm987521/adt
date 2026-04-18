// Package cli provides CLI commands for adt.
package cli

import (
	"errors"
	"os"

	"github.com/nilm987521/adt/internal/audit"
	"github.com/nilm987521/adt/internal/config"
	"github.com/nilm987521/adt/internal/output"
	"github.com/nilm987521/adt/internal/security"
)

// handleValidationError handles a SQL validation error: prints JSON output,
// writes audit entry, and exits with code 2.
func handleValidationError(err error, originalSQL, envName, cmd string, cfg *config.Config) {
	var code, message string

	ve := &security.ValidationError{}
	if errors.As(err, &ve) {
		code = ve.Code
		message = ve.Message
	} else {
		code = "validation_error"
		message = err.Error()
	}

	_ = output.PrintJSON(output.ErrorOutput{
		Error:       code,
		Message:     message,
		OriginalSQL: originalSQL,
	})

	auditPath := cfg.Audit.LogPath
	if auditPath == "" {
		auditPath = config.DefaultAuditLogPath()
	}

	_ = audit.Write(auditPath, audit.Entry{
		Env:    envName,
		Cmd:    cmd,
		SQL:    originalSQL,
		Status: "rejected",
		Error:  code,
	})

	os.Exit(2)
}
