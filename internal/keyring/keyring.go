package keyring

import (
	"fmt"

	gokeyring "github.com/zalando/go-keyring"
)

const service = "adt"

func accountKey(envName string) string {
	return "oracle-password-" + envName
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
