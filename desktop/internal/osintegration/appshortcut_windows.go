//go:build windows

package osintegration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"verdana/backend"

	"github.com/sergeymakinen/go-ico"
)

func SyncAppShortcuts(shortcuts []backend.AppShortcut, exe string) error {
	programs, err := userStartMenuPrograms()
	if err != nil {
		return err
	}
	dir := filepath.Join(programs, "Verdana Apps")
	icons := appShortcutIconDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	desiredLinks := make(map[string]bool, len(shortcuts))
	desiredIcons := make(map[string]bool, len(shortcuts))
	names := windowsShortcutNames(shortcuts)
	for i, shortcut := range shortcuts {
		key := appShortcutKey(shortcut.ID)
		iconPath := filepath.Join(icons, key+".ico")
		var icon bytes.Buffer
		if err := ico.Encode(&icon, appShortcutIcon(shortcut.Icon)); err != nil {
			return err
		}
		if err := writeAtomic(iconPath, icon.Bytes(), 0644); err != nil {
			return err
		}
		spec := appLnkSpec(dir, names[i], exe, iconPath, shortcut)
		if err := writeLnk(spec); err != nil {
			return fmt.Errorf("creating app shortcut failed: %w", err)
		}
		desiredLinks[spec.Path] = true
		desiredIcons[iconPath] = true
	}
	if err := removeStaleWindowsFiles(dir, ".lnk", desiredLinks); err != nil {
		return err
	}
	return removeStaleWindowsFiles(icons, ".ico", desiredIcons)
}

func removeStaleWindowsFiles(dir, suffix string, desired map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), suffix) && !desired[path] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
