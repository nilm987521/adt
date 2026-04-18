// Package keyring provides system keyring integration for storing database credentials.
package keyring

import (
	"fmt"

	gokeyring "github.com/zalando/go-keyring"
)

const service = "adt"

func accountKey(envName string) string {
	return "db-password-" + envName
}

// Set stores the password for the given environment in the system keyring.
func Set(envName, password string) error {
	return gokeyring.Set(service, accountKey(envName), password)
}

// Get retrieves the password for the given environment from the system keyring.
// Returns an error with message "credential not found for env: <envName>" if missing.
func Get(envName string) (string, error) {
	pw, err := gokeyring.Get(service, accountKey(envName))
	if err != nil {
		return "", fmt.Errorf("credential not found for env %q: %w", envName, err)
	}

	return pw, nil
}

// Delete removes the password for the given environment from the system keyring.
func Delete(envName string) error {
	return gokeyring.Delete(service, accountKey(envName))
}

// MigrateOracleKey migrates a v1 keyring entry (oracle-password-<env>) to the
// v2 format (db-password-<env>). It reads the old entry, writes the new one,
// then deletes the old one. Returns nil if the old entry does not exist
// (idempotent — safe to call multiple times).
func MigrateOracleKey(envName string) error {
	oldKey := "oracle-password-" + envName

	pw, err := gokeyring.Get(service, oldKey)
	if err != nil {
		// Old entry doesn't exist — nothing to migrate. Any error here means
		// the key is absent, so treat as a no-op.
		return nil //nolint:nilerr // absence of old key is not an error condition
	}

	if err := gokeyring.Set(service, accountKey(envName), pw); err != nil {
		return fmt.Errorf("migrate keyring entry for %q: write new key: %w", envName, err)
	}

	if err := gokeyring.Delete(service, oldKey); err != nil {
		// Non-fatal: new key is already written; old key cleanup failure is acceptable
		return fmt.Errorf("migrate keyring entry for %q: delete old key: %w", envName, err)
	}

	return nil
}
