//go:build linux

package osintegration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"verdana/backend"
)

const (
	verdanaDesktopID       = "com.verdana.Verdana.desktop"
	searchProviderFileName = "com.verdana.Verdana.search-provider.ini"
	searchServiceFileName  = "com.verdana.Verdana.SearchProvider.service"
)

func GnomeSearchSupported() bool {
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
		gnomeSearchProviderPath(),
		filepath.Join(base, "dbus-1", "services", searchServiceFileName)
}

// GNOME Shell intentionally looks for search providers in XDG_DATA_DIRS, not
// XDG_DATA_HOME. Prefer the first per-user data directory the session already
// treats as a system data directory (normally Flatpak's user export directory).
func gnomeSearchProviderPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	for _, dir := range filepath.SplitList(dirs) {
		dir = filepath.Clean(dir)
		rel, err := filepath.Rel(home, dir)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join(dir, "gnome-shell", "search-providers", searchProviderFileName)
		}
	}
	return ""
}

func SetGNOMESearchIntegration(enabled bool, exe string) error {
	desktopPath, providerPath, servicePath := gnomeSearchIntegrationPaths()
	legacyProviderPath := filepath.Join(xdgDataHome(), "gnome-shell", "search-providers", searchProviderFileName)
	if !enabled {
		for _, path := range []string{desktopPath, providerPath, legacyProviderPath, servicePath} {
			if path == "" {
				continue
			}
			err := os.Remove(path)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if err == nil && searchDebugEnabled() {
				log.Info().Str("path", path).Msg("removed GNOME search integration file")
			}
		}
		RefreshShortcutParent(filepath.Dir(desktopPath))
		return nil
	}
	if providerPath == "" {
		return errors.New("GNOME search needs a user-writable directory in XDG_DATA_DIRS")
	}
	// quoted before the write loop, so a refused exe leaves no partial set
	quotedExe, err := quoteExecField(exe)
	if err != nil {
		return fmt.Errorf("GNOME search integration not written: %w", err)
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
`, quotedExe)
	provider := `[Shell Search Provider]
DesktopId=com.verdana.Verdana.desktop
BusName=com.verdana.Verdana.SearchProvider
ObjectPath=/com/verdana/Verdana/SearchProvider
Version=2
`
	service := fmt.Sprintf(`[D-BUS Service]
Name=com.verdana.Verdana.SearchProvider
Exec=%s --background
`, quotedExe)
	for _, file := range []struct {
		kind string
		path string
		data string
	}{
		{"desktop entry", desktopPath, desktop},
		{"search provider", providerPath, provider},
		{"D-Bus service", servicePath, service},
	} {
		if err := writeAtomic(file.path, []byte(file.data), 0644); err != nil {
			return err
		}
		if searchDebugEnabled() {
			log.Info().Str("kind", file.kind).Str("path", file.path).Str("executable", exe).
				Msg("installed GNOME search integration file")
		}
	}
	if legacyProviderPath != providerPath {
		if err := os.Remove(legacyProviderPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	RefreshShortcutParent(filepath.Dir(desktopPath))
	return nil
}

// GNOME queries the live provider, so it needs no indexed launcher entries.
func SyncSearchNapplets([]backend.AppShortcut, string) error { return nil }
