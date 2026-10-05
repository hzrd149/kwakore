//go:build windows

package osintegration

import (
	"fmt"
	"os"
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
	dir := filepath.Join(programs, "Verdana Discover")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	desired := make(map[string]bool, len(napplets))
	names := windowsShortcutNames(napplets)
	for i, napplet := range napplets {
		spec := searchLnkSpec(dir, names[i], exe, napplet)
		if err := writeLnk(spec); err != nil {
			return fmt.Errorf("creating search shortcut failed: %w", err)
		}
		desired[spec.Path] = true
	}
	return removeStaleWindowsFiles(dir, ".lnk", desired)
}
