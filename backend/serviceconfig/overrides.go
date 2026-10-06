package serviceconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"verdana/backend/fileutil"
)

var writeAtomic = fileutil.WriteFileAtomic

func privateDataDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
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
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
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
	return m.change(field, value, false)
}

func (m *Manager) ClearOverride(field string) error {
	return m.change(field, nil, true)
}

func (m *Manager) change(field string, value any, clear bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !FieldName(field) {
		return fmt.Errorf("unsupported setting %q", field)
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
		case "discover_on_user_relays":
			v, ok := value.(bool)
			if !ok {
				return fmt.Errorf("%s: expected boolean", field)
			}
			next.DiscoverOnUserRelays = &v
		}
	} else {
		switch field {
		case "relays":
			next.Relays = nil
		case "blossom_servers":
			next.BlossomServers = nil
		case "discover_on_user_relays":
			next.DiscoverOnUserRelays = nil
		}
	}
	if err := validate(next); err != nil {
		return err
	}
	if filepath.Dir(m.paths.OverrideFile) != m.paths.DataDir {
		return errors.New("override path must be in data directory")
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
		if observed, readErr := read(m.paths.OverrideFile); readErr == nil {
			m.override = observed
			m.effective = merge(m.file, observed)
		}
		return err
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
	return n
}
