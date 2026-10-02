//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	verdanaDesktopID       = "com.verdana.Verdana.desktop"
	searchProviderFileName = "com.verdana.Verdana.search-provider.ini"
	searchServiceFileName  = "com.verdana.Verdana.SearchProvider.service"
)

func gnomeSearchSupported() bool {
	desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	return strings.Contains(desktop, "gnome")
}

func xdgDataHome() string {
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return base
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".local/share"
	}
	return filepath.Join(home, ".local", "share")
}

func gnomeSearchIntegrationPaths() (desktop, provider, service string) {
	base := xdgDataHome()
	return filepath.Join(base, "applications", verdanaDesktopID),
		filepath.Join(base, "gnome-shell", "search-providers", searchProviderFileName),
		filepath.Join(base, "dbus-1", "services", searchServiceFileName)
}

func setGNOMESearchIntegration(enabled bool, exe string) error {
	desktopPath, providerPath, servicePath := gnomeSearchIntegrationPaths()
	if !enabled {
		for _, path := range []string{desktopPath, providerPath, servicePath} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		refreshShortcutParent(filepath.Dir(desktopPath))
		return nil
	}

	desktop := fmt.Sprintf(`[Desktop Entry]
Version=1.0
Type=Application
Name=Verdana
Comment=Discover and run Nostr applications
Exec=%s
Icon=applications-internet
Terminal=false
Categories=Network;
Keywords=Nostr;Napp;Napplet;
`, quoteExecField(exe))
	provider := `[Shell Search Provider]
DesktopId=com.verdana.Verdana.desktop
BusName=com.verdana.Verdana.SearchProvider
ObjectPath=/com/verdana/Verdana/SearchProvider
Version=2
`
	service := fmt.Sprintf(`[D-BUS Service]
Name=com.verdana.Verdana.SearchProvider
Exec=%s --background
`, quoteExecField(exe))
	for _, file := range []struct {
		path string
		data string
	}{
		{desktopPath, desktop},
		{providerPath, provider},
		{servicePath, service},
	} {
		if err := writeAtomic(file.path, []byte(file.data), 0644); err != nil {
			return err
		}
	}
	refreshShortcutParent(filepath.Dir(desktopPath))
	return nil
}
