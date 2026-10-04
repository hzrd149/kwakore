package secretstore

import "context"

// serviceAvailable is always true: Credential Manager is part of every
// Windows session.
func serviceAvailable(context.Context) bool {
	return true
}
