package secretstore

import (
	"context"
	"os"
)

// serviceAvailable reports whether the security tool go-keyring drives is
// there; the login keychain itself always exists for a logged-in user.
func serviceAvailable(context.Context) bool {
	_, err := os.Stat("/usr/bin/security")
	return err == nil
}
