//go:build windows

package osintegration

import (
	"bytes"
	"path/filepath"
	"verdana/backend"

	"github.com/sergeymakinen/go-ico"
)

func SyncAppShortcuts(shortcuts []backend.AppShortcut, exe string) error {
	programs, err := userStartMenuPrograms()
	if err != nil {
		return err
	}
	return syncAppLinks(filepath.Join(programs, "Verdana Apps"), appShortcutIconDir(), shortcuts, exe, writeAppIcon, writeLnk)
}

func writeAppIcon(path string, shortcut backend.AppShortcut) error {
	var icon bytes.Buffer
	if err := ico.Encode(&icon, appShortcutIcon(shortcut.Icon)); err != nil {
		return err
	}
	return writeAtomic(path, icon.Bytes(), 0644)
}
