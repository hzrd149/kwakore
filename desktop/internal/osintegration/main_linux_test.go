//go:build linux

package osintegration

import (
	"os"
	"testing"
)

// TestMain keeps update-desktop-database off PATH. RefreshShortcutParent
// starts it without waiting, and it writes a cache into the test's
// applications dir while t.TempDir is being removed, which fails the
// cleanup at random.
func TestMain(m *testing.M) {
	os.Setenv("PATH", "")
	os.Exit(m.Run())
}
