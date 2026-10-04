//go:build linux

package osintegration

import (
	"fmt"
	"os"
	"path/filepath"

	"verdana/backend/fileutil"
)

func autostartPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "autostart", "verdana.desktop")
}

func AutostartEnabled() bool {
	_, err := os.Stat(autostartPath())
	return err == nil
}

func SetAutostart(enabled bool, exe string) error {
	path := autostartPath()
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Verdana
Comment=Run Verdana in the background
Exec=%s --background
Terminal=false
X-GNOME-Autostart-enabled=true
`, quoteExecField(exe))
	return fileutil.WriteFileAtomic(path, []byte(data), 0644)
}
