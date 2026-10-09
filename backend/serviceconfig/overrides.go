package serviceconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"kwakore/backend/fileutil"
)

var writeAtomic = fileutil.WriteFileAtomic

func safeDataPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s: data directory must be absolute", path)
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinked data path is unsafe", current)
		}
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	return nil
}

func privateDataDir(path string) error {
	if err := safeDataPath(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("%s: data directory must be a private 0700 directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("%s: data directory must be owned by current user", path)
	}
	return nil
}

func privateFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("%s: override file must be a private 0600 regular file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("%s: override file must be owned by current user", path)
	}
	return nil
}

// SetOverride persists one supported non-secret setting.
func (m *Manager) SetOverride(field string, value any) error {
	return m.change(field, value, false, false)
}

func (m *Manager) ClearOverride(field string) error {
	return m.change(field, nil, true, false)
}

// SetSignerOverride is reserved for the explicit signer command.
func (m *Manager) SetSignerOverride(signer Signer) error {
	return m.change("signer", signer, false, true)
}

func (m *Manager) change(field string, value any, clear, signerAllowed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !FieldName(field) && !(signerAllowed && field == "signer") {
		return fmt.Errorf("unsupported setting %q", field)
	}
	if m.persistenceErr != nil {
		return fmt.Errorf("%s: override persistence unhealthy: %w", field, m.persistenceErr)
	}
	if clear {
		switch field {
		case "relays":
			if m.override.Relays == nil {
				return nil
			}
		case "blossom_servers":
			if m.override.BlossomServers == nil {
				return nil
			}
		case "discover_on_user_relays":
			if m.override.DiscoverOnUserRelays == nil {
				return nil
			}
		case "desktop_entries":
			if m.override.DesktopEntries == nil {
				return nil
			}
		case "gnome_search":
			if m.override.GNOMESearch == nil {
				return nil
			}
		}
	}
	next := cloneConfig(m.override)
	if !clear {
		switch field {
		case "relays", "blossom_servers":
			v, ok := value.([]string)
			if !ok {
				return fmt.Errorf("%s: expected array of URLs", field)
			}
			copyValue := append([]string{}, v...)
			if field == "relays" {
				next.Relays = &copyValue
			} else {
				next.BlossomServers = &copyValue
			}
		case "discover_on_user_relays", "desktop_entries", "gnome_search":
			v, ok := value.(bool)
			if !ok {
				return fmt.Errorf("%s: expected boolean", field)
			}
			switch field {
			case "desktop_entries":
				next.DesktopEntries = &v
			case "gnome_search":
				next.GNOMESearch = &v
			default:
				next.DiscoverOnUserRelays = &v
			}
		case "signer":
			v, ok := value.(Signer)
			if !ok {
				return errors.New("signer: invalid value")
			}
			next.Signer = &v
		}
	} else {
		switch field {
		case "relays":
			next.Relays = nil
		case "blossom_servers":
			next.BlossomServers = nil
		case "discover_on_user_relays":
			next.DiscoverOnUserRelays = nil
		case "desktop_entries":
			next.DesktopEntries = nil
		case "gnome_search":
			next.GNOMESearch = nil
		}
	}
	if err := validateMerged(m.file, next); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if filepath.Dir(m.paths.OverrideFile) != m.paths.DataDir {
		return errors.New("override path must be in data directory")
	}
	if err := safeDataPath(m.paths.DataDir); err != nil {
		return err
	}
	if err := os.MkdirAll(m.paths.DataDir, 0700); err != nil {
		return fmt.Errorf("%s: create data directory: %w", m.paths.DataDir, err)
	}
	if err := privateDataDir(m.paths.DataDir); err != nil {
		return err
	}
	if err := privateFile(m.paths.OverrideFile); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := writeAtomic(m.paths.OverrideFile, b, 0600); err != nil {
		// The rename may have succeeded before a later sync error. Read the
		// observed file so the in-memory snapshot never claims stale values.
		if readErr := privateDataDir(m.paths.DataDir); readErr != nil {
			m.persistenceErr = readErr
			return fmt.Errorf("%s: persist override: %w; reconciliation failed: %v", field, err, readErr)
		}
		if readErr := privateFile(m.paths.OverrideFile); readErr != nil {
			m.persistenceErr = readErr
			return fmt.Errorf("%s: persist override: %w; reconciliation failed: %v", field, err, readErr)
		}
		observed, readErr := read(m.paths.OverrideFile)
		if readErr == nil {
			readErr = validateMerged(m.file, observed)
		}
		if readErr == nil {
			m.override = observed
			m.effective = merge(m.file, observed)
		} else {
			m.persistenceErr = readErr
			return fmt.Errorf("%s: persist override: %w; reconciliation failed: %v", field, err, readErr)
		}
		return fmt.Errorf("%s: persist override: %w", field, err)
	}
	m.override = next
	m.effective = merge(m.file, next)
	return nil
}

func cloneConfig(c Config) Config {
	n := c
	if c.Relays != nil {
		v := append([]string{}, (*c.Relays)...)
		n.Relays = &v
	}
	if c.BlossomServers != nil {
		v := append([]string{}, (*c.BlossomServers)...)
		n.BlossomServers = &v
	}
	if c.DiscoverOnUserRelays != nil {
		v := *c.DiscoverOnUserRelays
		n.DiscoverOnUserRelays = &v
	}
	if c.DesktopEntries != nil {
		v := *c.DesktopEntries
		n.DesktopEntries = &v
	}
	if c.GNOMESearch != nil {
		v := *c.GNOMESearch
		n.GNOMESearch = &v
	}
	if c.Signer != nil {
		v := *c.Signer
		n.Signer = &v
	}
	return n
}
