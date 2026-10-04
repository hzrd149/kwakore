//go:build darwin

package osintegration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"verdana/backend/fileutil"
)

func autostartPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.verdana.launcher.plist")
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
	data := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.verdana.launcher</string>
<key>ProgramArguments</key><array><string>%s</string><string>--background</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`, xmlEscapeAutostart(exe))
	return fileutil.WriteFileAtomic(path, []byte(data), 0644)
}

func xmlEscapeAutostart(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(value)
}
