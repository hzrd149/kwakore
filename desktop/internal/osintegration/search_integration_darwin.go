//go:build darwin

package osintegration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"verdana/backend"
)

// SyncSearchNapplets writes tiny application bundles below ~/Applications.
// Spotlight indexes application bundles automatically; activating one routes
// the discovered napplet, as its launch token, through Verdana's
// trial-aware launch path.
func SyncSearchNapplets(napplets []backend.AppShortcut, exe string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Applications", "Verdana Discover")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	desired := make(map[string]bool, len(napplets))
	nameCounts := make(map[string]int, len(napplets))
	for _, napplet := range napplets {
		nameCounts[strings.ToLower(darwinShortcutName(napplet.Name))]++
	}
	for _, napplet := range napplets {
		name := darwinShortcutName(napplet.Name)
		key := appShortcutKey(napplet.ID)
		if nameCounts[strings.ToLower(name)] > 1 {
			name += " (" + key[:6] + ")"
		}
		appDir := filepath.Join(dir, name+".app")
		macOSDir := filepath.Join(appDir, "Contents", "MacOS")
		if err := os.MkdirAll(macOSDir, 0755); err != nil {
			return err
		}
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>launch</string>
<key>CFBundleIdentifier</key><string>com.verdana.search.%s</string>
<key>CFBundleName</key><string>%s</string>
<key>CFBundleDisplayName</key><string>%s</string>
<key>CFBundleGetInfoString</key><string>%s</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSUIElement</key><true/>
</dict></plist>
`, key, xmlEscape(name), xmlEscape(name), xmlEscape(appShortcutText(napplet.Description)))
		if err := writeAtomic(filepath.Join(appDir, "Contents", "Info.plist"), []byte(plist), 0644); err != nil {
			return err
		}
		script := "#!/bin/sh\nexec " + shellQuote(exe) + " --background --try-napplet " + shellQuote(napplet.Token) + "\n"
		if err := writeAtomic(filepath.Join(macOSDir, "launch"), []byte(script), 0755); err != nil {
			return err
		}
		desired[appDir] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".app") && !desired[path] {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	// Spotlight normally notices Applications changes itself. mdimport is a
	// best-effort nudge so a freshly completed Discovery appears promptly.
	_ = exec.Command("/usr/bin/mdimport", dir).Run()
	return nil
}
