//go:build linux

package daemon

import (
	"context"
	"os"
	"testing"
)

// TestMain keeps the background social refresh out of every daemon test.
// Open runs on the default configuration, whose relays are real, and the
// install, update and selection hooks fetch social data only while the
// refresh loop runs; with startSocialRefresh a no-op no daemon test
// (TestInstalledSmokeSeed included) ever fetches social data. Tests that
// check the wiring swap in a recorder and restore this no-op.
func TestMain(m *testing.M) {
	startSocialRefresh = noSocialRefresh
	os.Exit(m.Run())
}

func noSocialRefresh(context.Context) func() { return func() {} }
