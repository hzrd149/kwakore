//go:build windows

package osintegration

import (
	"path/filepath"
	"verdana/backend"
)

// SyncSearchNapplets mirrors the discovery catalog into a dedicated Start
// menu folder. Windows Search indexes these links and activating one sends the
// napplet through the trial-aware discovery path.
func SyncSearchNapplets(napplets []backend.AppShortcut, exe string) error {
	programs, err := userStartMenuPrograms()
	if err != nil {
		return err
	}
	return syncSearchLinks(filepath.Join(programs, "Verdana Discover"), napplets, exe, writeLnk)
}
