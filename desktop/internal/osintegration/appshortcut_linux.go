//go:build linux

package osintegration

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"verdana/backend"
)

// SyncAppShortcuts writes one .desktop entry per shortcut and removes the
// managed entries no longer wanted. The napp id only ever reaches the file as
// shortcut.Token (an encoded launch token): the raw id carries the author's d
// tag, and a newline in it would otherwise add keys such as a second Exec=.
// The file name stays keyed on the raw id, so existing installs keep theirs.
// An entry that cannot be written is skipped (see writeShortcutEntries).
func SyncAppShortcuts(shortcuts []backend.AppShortcut, exe string) error {
	// The executable is quoted before the first write, so a refused one
	// leaves the installed shortcuts as they were.
	quotedExe, err := quoteExecField(exe)
	if err != nil {
		return fmt.Errorf("app shortcut executable: %w", err)
	}
	dir := applicationsDir()
	icons := appShortcutIconDir()
	entries := make([]shortcutEntry, len(shortcuts))
	for i, shortcut := range shortcuts {
		key := appShortcutKey(shortcut.ID)
		iconPath := filepath.Join(icons, key+".png")
		path := filepath.Join(dir, appShortcutPrefix+key+".desktop")
		entries[i] = shortcutEntry{id: shortcut.ID, paths: []string{path, iconPath}, write: func() error {
			return writeDesktopEntry(path, iconPath, quotedExe, shortcut)
		}}
	}
	desired, writeErr := writeShortcutEntries("app shortcut", entries)
	if err := removeStaleAppShortcutFiles(dir, ".desktop", desired); err != nil {
		return err
	}
	if err := removeStaleAppShortcutFiles(icons, ".png", desired); err != nil {
		return err
	}
	RefreshShortcutParent(dir)
	return writeErr
}

// writeDesktopEntry writes the icon and the .desktop entry for one shortcut.
func writeDesktopEntry(path, iconPath, quotedExe string, shortcut backend.AppShortcut) error {
	quotedToken, err := quoteExecField(shortcut.Token)
	if err != nil {
		return fmt.Errorf("app shortcut token: %w", err)
	}
	var icon bytes.Buffer
	if err := png.Encode(&icon, appShortcutIcon(shortcut.Icon)); err != nil {
		return err
	}
	if err := writeAtomic(iconPath, icon.Bytes(), 0644); err != nil {
		return err
	}
	data := fmt.Sprintf(`[Desktop Entry]
Version=1.0
Type=Application
Name=%s
Comment=%s
Exec=%s --background --launch-napp %s
Icon=%s
Terminal=false
Categories=Network;
X-Verdana-Napp-ID=%s
`, appShortcutText(shortcut.Name), appShortcutText(shortcut.Description), quotedExe, quotedToken, iconPath, shortcut.Token)
	return writeAtomic(path, []byte(data), 0644)
}

func removeStaleAppShortcutFiles(dir, suffix string, desired map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	icons := dir == appShortcutIconDir()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		if !icons && !strings.HasPrefix(entry.Name(), appShortcutPrefix) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !desired[path] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
