//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"verdana/backend"
)

// syncSearchNapplets mirrors the discovery catalog into a dedicated Start
// menu folder. Windows Search indexes these links and activating one sends the
// napplet through the trial-aware discovery path.
func syncSearchNapplets(napplets []backend.AppShortcut, exe string) error {
	programs, err := userStartMenuPrograms()
	if err != nil {
		return err
	}
	dir := filepath.Join(programs, "Verdana Discover")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	desired := make(map[string]bool, len(napplets))
	nameCounts := make(map[string]int, len(napplets))
	for _, napplet := range napplets {
		nameCounts[strings.ToLower(windowsShortcutName(napplet.Name))]++
	}
	for _, napplet := range napplets {
		name := windowsShortcutName(napplet.Name)
		key := appShortcutKey(napplet.ID)
		if nameCounts[strings.ToLower(name)] > 1 {
			name += " (" + key[:6] + ")"
		}
		path := filepath.Join(dir, name+".lnk")
		arguments := `--background --try-napplet "` + strings.ReplaceAll(napplet.ID, `"`, `\"`) + `"`
		ps := fmt.Sprintf(
			`$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut(%s); $s.TargetPath = %s; $s.Arguments = %s; $s.Description = %s; $s.Save()`,
			psSingleQuote(path), psSingleQuote(exe), psSingleQuote(arguments),
			psSingleQuote(appShortcutText(napplet.Description)),
		)
		if out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput(); err != nil {
			return fmt.Errorf("creating search shortcut failed: %v: %s", err, out)
		}
		desired[path] = true
	}
	return removeStaleWindowsFiles(dir, ".lnk", desired)
}
