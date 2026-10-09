//go:build linux

package linuxhost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kwakore/backend/desktopentry"
	"kwakore/backend/fileutil"
)

const (
	searchBus      = "org.kwakore.SearchProvider"
	searchPath     = "/org/kwakore/SearchProvider"
	searchDesktop  = "org.kwakore.Search.desktop"
	searchProvider = "org.kwakore.Search.search-provider.ini"
)

func (*Host) GNOMESearchSupported() bool { return true }

// GNOME Shell scans XDG_DATA_DIRS for provider metadata, whereas the desktop
// and D-Bus service files can be installed under XDG_DATA_HOME.
func gnomeSearchPaths() (desktop, provider, service string, err error) {
	apps, err := desktopentry.ApplicationsDir()
	if err != nil {
		return "", "", "", err
	}
	dataHome := filepath.Dir(apps)
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", err
	}
	for _, dir := range filepath.SplitList(os.Getenv("XDG_DATA_DIRS")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		rel, e := filepath.Rel(home, dir)
		if e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			provider = filepath.Join(dir, "gnome-shell", "search-providers", searchProvider)
			break
		}
	}
	return filepath.Join(apps, searchDesktop), provider,
		filepath.Join(dataHome, "dbus-1", "services", searchBus+".service"), nil
}

func packagedSearchProvider() bool {
	for _, dir := range filepath.SplitList(os.Getenv("XDG_DATA_DIRS")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, "gnome-shell", "search-providers", searchProvider)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func (h *Host) SetGNOMESearchIntegration(enabled bool) error {
	desktop, provider, service, err := gnomeSearchPaths()
	if err != nil {
		return err
	}
	paths := []string{desktop, provider, service}
	if !enabled {
		var errs []error
		for _, path := range paths {
			if path != "" {
				if e := os.Remove(path); e != nil && !errors.Is(e, os.ErrNotExist) {
					errs = append(errs, e)
				}
			}
		}
		return errors.Join(errs...)
	}
	if provider == "" && packagedSearchProvider() {
		return nil
	}
	if provider == "" {
		return errors.New("GNOME search requires a user directory in XDG_DATA_DIRS")
	}
	// The provider desktop file uses a fixed command; author text never reaches it.
	execLine, err := desktopentry.QuoteExecutable(h.CLI)
	if err != nil {
		return err
	}
	files := []struct{ path, content string }{
		{desktop, "[Desktop Entry]\nType=Application\nName=Kwakore\nComment=Discover Nostr napplets\nExec=" + execLine + " discover\nTerminal=true\nCategories=Network;\n"},
		{provider, "[Shell Search Provider]\nDesktopId=" + searchDesktop + "\nBusName=" + searchBus + "\nObjectPath=" + searchPath + "\nVersion=2\n"},
		{service, "[D-BUS Service]\nName=" + searchBus + "\nExec=" + execLine + " status\n"},
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.path), 0700); err != nil {
			return err
		}
		if err := fileutil.WriteFileAtomic(f.path, []byte(f.content), 0644); err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
	}
	return nil
}
