//go:build darwin

package osintegration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"verdana/backend"

	"github.com/jackmordaunt/icns/v3"
)

func SyncAppShortcuts(shortcuts []backend.AppShortcut, exe string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Applications", "Verdana Apps")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	names := darwinShortcutNames(shortcuts)
	entries := make([]shortcutEntry, len(shortcuts))
	for i, shortcut := range shortcuts {
		appDir := filepath.Join(dir, names[i]+".app")
		entries[i] = shortcutEntry{id: shortcut.ID, paths: []string{appDir}, write: func() error {
			return writeAppBundle(appDir, names[i], exe, shortcut)
		}}
	}
	desired, writeErr := writeShortcutEntries("app shortcut", entries)
	existing, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range existing {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".app") && !desired[path] {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return writeErr
}

// writeAppBundle writes the application bundle for one installed napp.
func writeAppBundle(appDir, name, exe string, shortcut backend.AppShortcut) error {
	key := appShortcutKey(shortcut.ID)
	macOSDir := filepath.Join(appDir, "Contents", "MacOS")
	resourcesDir := filepath.Join(appDir, "Contents", "Resources")
	if err := os.MkdirAll(macOSDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(resourcesDir, 0755); err != nil {
		return err
	}
	var icon bytes.Buffer
	if err := icns.Encode(&icon, appShortcutIcon(shortcut.Icon)); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(resourcesDir, "AppIcon.icns"), icon.Bytes(), 0644); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>launch</string>
<key>CFBundleIdentifier</key><string>com.verdana.napp.%s</string>
<key>CFBundleName</key><string>%s</string>
<key>CFBundleDisplayName</key><string>%s</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleIconFile</key><string>AppIcon.icns</string>
<key>LSUIElement</key><true/>
</dict></plist>
`, key, xmlEscape(name), xmlEscape(name))
	if err := writeAtomic(filepath.Join(appDir, "Contents", "Info.plist"), []byte(plist), 0644); err != nil {
		return err
	}
	script := "#!/bin/sh\nexec " + shellQuote(exe) + " --background --launch-napp " + shellQuote(shortcut.Token) + "\n"
	return writeAtomic(filepath.Join(macOSDir, "launch"), []byte(script), 0755)
}
