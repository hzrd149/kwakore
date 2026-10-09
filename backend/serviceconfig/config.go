// Package serviceconfig loads the non-secret configuration of the Linux service.
package serviceconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxConfigBytes = 1 << 20

type Paths struct {
	ConfigFile   string
	DataDir      string
	OverrideFile string
}

func ResolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	configRoot := os.Getenv("XDG_CONFIG_HOME")
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
	}
	dataRoot := os.Getenv("XDG_DATA_HOME")
	if dataRoot == "" {
		dataRoot = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(configRoot) || !filepath.IsAbs(dataRoot) {
		return Paths{}, errors.New("XDG_CONFIG_HOME and XDG_DATA_HOME must be absolute paths")
	}
	configFile := filepath.Join(configRoot, "kwakore", "config.json")
	// KWAKORE_CONFIG_FILE names the file directly, so a system-managed file
	// does not have to move XDG_CONFIG_HOME, which napplet windows inherit.
	if override := os.Getenv("KWAKORE_CONFIG_FILE"); override != "" {
		if !filepath.IsAbs(override) {
			return Paths{}, errors.New("KWAKORE_CONFIG_FILE must be an absolute path")
		}
		configFile = override
	}
	dataDir := filepath.Join(dataRoot, "kwakore")
	return Paths{ConfigFile: configFile, DataDir: dataDir, OverrideFile: filepath.Join(dataDir, "settings-overrides.json")}, nil
}

type Config struct {
	Relays               *[]string `json:"relays,omitempty"`
	BlossomServers       *[]string `json:"blossom_servers,omitempty"`
	DiscoverOnUserRelays *bool     `json:"discover_on_user_relays,omitempty"`
	DesktopEntries       *bool     `json:"desktop_entries,omitempty"`
	GNOMESearch          *bool     `json:"gnome_search,omitempty"`
	Signer               *Signer   `json:"signer,omitempty"`
}

type Signer struct {
	Mode   string `json:"mode"`
	Relay  string `json:"relay,omitempty"`
	Socket string `json:"socket,omitempty"`
}

type Effective struct {
	Relays               []string `json:"relays"`
	BlossomServers       []string `json:"blossom_servers"`
	DiscoverOnUserRelays bool     `json:"discover_on_user_relays"`
	DesktopEntries       bool     `json:"desktop_entries"`
	GNOMESearch          bool     `json:"gnome_search"`
	Signer               Signer   `json:"signer"`
}

func Defaults() Effective {
	return Effective{Relays: []string{"wss://relay.nostrapps.com", "wss://relay.nostrapps.com/public"}, BlossomServers: []string{"https://relay.nostrapps.com", "https://nostr.download"}, DiscoverOnUserRelays: true, DesktopEntries: true, GNOMESearch: true, Signer: Signer{Mode: "none"}}
}

func merge(file, override Config) Effective {
	v := Defaults()
	for _, c := range []Config{file, override} {
		if c.Relays != nil {
			v.Relays = append([]string{}, (*c.Relays)...)
		}
		if c.BlossomServers != nil {
			v.BlossomServers = append([]string{}, (*c.BlossomServers)...)
		}
		if c.DiscoverOnUserRelays != nil {
			v.DiscoverOnUserRelays = *c.DiscoverOnUserRelays
		}
		if c.DesktopEntries != nil {
			v.DesktopEntries = *c.DesktopEntries
		}
		if c.GNOMESearch != nil {
			v.GNOMESearch = *c.GNOMESearch
		}
		if c.Signer != nil {
			v.Signer = *c.Signer
		}
	}
	return v
}

func validateMerged(file, override Config) error {
	v := merge(file, override)
	return validate(Config{Relays: &v.Relays, BlossomServers: &v.BlossomServers, DiscoverOnUserRelays: &v.DiscoverOnUserRelays, DesktopEntries: &v.DesktopEntries, GNOMESearch: &v.GNOMESearch, Signer: &v.Signer})
}

func validate(c Config) error {
	if c.Signer != nil {
		s := *c.Signer
		if s.Mode != "none" && s.Mode != "nsec" && s.Mode != "bunker" && s.Mode != "system" {
			return errors.New("signer: invalid mode")
		}
		if s.Mode != "system" && s.Socket != "" {
			return errors.New("signer: socket is only valid for system")
		}
		if s.Socket != "" && (!filepath.IsAbs(s.Socket) || filepath.Clean(s.Socket) != s.Socket || len(s.Socket) > 107) {
			return errors.New("signer: invalid socket; use an absolute, clean path")
		}
		if s.Mode != "bunker" && s.Relay != "" {
			return errors.New("signer: relay is only valid for bunker")
		}
		if s.Mode == "bunker" {
			u, err := url.Parse(s.Relay)
			if err != nil || u.Scheme != "wss" || u.Hostname() == "" || u.Hostname() != strings.ToLower(u.Hostname()) || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" || u.String() != s.Relay || strings.HasSuffix(u.Path, "/") {
				return errors.New("signer: invalid relay; use a canonical wss URL")
			}
		}
	}
	for _, field := range []struct {
		name   string
		values *[]string
		scheme string
	}{{"relays", c.Relays, "wss"}, {"blossom_servers", c.BlossomServers, "https"}} {
		if field.values == nil {
			continue
		}
		seen := map[string]bool{}
		for _, raw := range *field.values {
			u, err := url.Parse(raw)
			if err != nil || u.Hostname() == "" || u.Hostname() != strings.ToLower(u.Hostname()) || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" || u.String() != raw || (field.scheme == "wss" && (u.Scheme != "wss" || strings.HasSuffix(u.Path, "/"))) || (field.scheme == "https" && u.Scheme != "https" && u.Scheme != "http") {
				if field.name == "relays" {
					return fmt.Errorf("%s: invalid URL %q; use a canonical wss:// URL with a host", field.name, raw)
				}
				return fmt.Errorf("%s: invalid URL %q; use an http:// or https:// server URL with a host", field.name, raw)
			}
			if seen[raw] {
				return fmt.Errorf("%s: duplicate URL %q; remove the duplicate", field.name, raw)
			}
			seen[raw] = true
		}
	}
	return nil
}

func read(path string) (Config, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if len(b) > maxConfigBytes {
		return Config{}, fmt.Errorf("%s: file exceeds 1 MiB", path)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := rejectSecretFields(raw); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	for key := range raw {
		if key != "relays" && key != "blossom_servers" && key != "discover_on_user_relays" && key != "desktop_entries" && key != "gnome_search" && key != "signer" {
			return Config{}, fmt.Errorf("%s: unknown setting %q; remove it or use relays, blossom_servers, discover_on_user_relays, desktop_entries, or gnome_search", path, key)
		}
		if bytes.Equal(bytes.TrimSpace(raw[key]), []byte("null")) {
			return Config{}, fmt.Errorf("%s: %s must not be null; omit the setting to use its default", path, key)
		}
	}
	if err := checkDuplicateKeys(b); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if signerRaw, ok := raw["signer"]; ok && len(signerRaw) > 0 && signerRaw[0] == '{' {
		if err := checkDuplicateKeys(signerRaw); err != nil {
			return Config{}, fmt.Errorf("%s: signer: %w", path, err)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Config{}, fmt.Errorf("%s: trailing JSON", path)
	}
	if err := validate(c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func rejectSecretFields(raw map[string]json.RawMessage) error {
	for key, value := range raw {
		if secretField(key) {
			return fmt.Errorf("%s: secret field is forbidden", safeSecretField(key, "file"))
		}
		if key != "signer" {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(value, &nested) != nil {
			continue
		}
		for name := range nested {
			if secretField(name) {
				return fmt.Errorf("signer.%s: secret field is forbidden", safeSecretField(name, "field"))
			}
		}
	}
	return nil
}

func safeSecretField(name, fallback string) string {
	switch name {
	case "secret", "nsec", "private_key", "client_key", "login", "password", "credential", "bunker_url", "auth_token":
		return name
	default:
		return fallback
	}
}

func secretField(name string) bool {
	s := strings.ToLower(name)
	for _, fragment := range []string{"secret", "nsec", "key", "login", "token", "password", "credential", "bunker_url", "auth"} {
		if strings.Contains(s, fragment) {
			return true
		}
	}
	return false
}

func checkDuplicateKeys(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	t, err := d.Token()
	if err != nil {
		return err
	}
	if t != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := t.(string)
		if !ok {
			return errors.New("expected setting name")
		}
		if seen[key] {
			return fmt.Errorf("duplicate setting %q", key)
		}
		seen[key] = true
		var discard json.RawMessage
		if err := d.Decode(&discard); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

type Manager struct {
	mu             sync.RWMutex
	paths          Paths
	file           Config
	override       Config
	effective      Effective
	persistenceErr error
}

func Load(paths Paths) (*Manager, error) {
	if filepath.Dir(paths.OverrideFile) != paths.DataDir || filepath.Base(paths.OverrideFile) != "settings-overrides.json" {
		return nil, errors.New("override path must be settings-overrides.json in data directory")
	}
	if err := safeDataPath(paths.DataDir); err != nil {
		return nil, err
	}
	file, err := read(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Lstat(paths.DataDir); statErr == nil && info.IsDir() {
		if err := privateDataDir(paths.DataDir); err != nil {
			return nil, err
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	} else if statErr == nil {
		return nil, fmt.Errorf("%s: data path must be a directory", paths.DataDir)
	}
	if err := privateFile(paths.OverrideFile); err != nil {
		return nil, err
	}
	override, err := read(paths.OverrideFile)
	if err != nil {
		return nil, err
	}
	if err := validateMerged(file, override); err != nil {
		return nil, fmt.Errorf("%s: %w", paths.OverrideFile, err)
	}
	return &Manager{paths: paths, file: file, override: override, effective: merge(file, override)}, nil
}

func (m *Manager) Effective() Effective {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v := m.effective
	v.Relays = append([]string{}, v.Relays...)
	v.BlossomServers = append([]string{}, v.BlossomServers...)
	return v
}

// Sources reports which layer currently supplies each non-secret setting.
func (m *Manager) Sources() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sourcesLocked()
}

// Inspect returns values and their sources from one configuration snapshot.
func (m *Manager) Inspect() (Effective, map[string]string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v := m.effective
	v.Relays = append([]string{}, v.Relays...)
	v.BlossomServers = append([]string{}, v.BlossomServers...)
	return v, m.sourcesLocked()
}

func (m *Manager) sourcesLocked() map[string]string {
	fields := map[string]struct{ file, override bool }{
		"relays":                  {m.file.Relays != nil, m.override.Relays != nil},
		"blossom_servers":         {m.file.BlossomServers != nil, m.override.BlossomServers != nil},
		"discover_on_user_relays": {m.file.DiscoverOnUserRelays != nil, m.override.DiscoverOnUserRelays != nil},
		"desktop_entries":         {m.file.DesktopEntries != nil, m.override.DesktopEntries != nil},
		"gnome_search":            {m.file.GNOMESearch != nil, m.override.GNOMESearch != nil},
		"signer":                  {m.file.Signer != nil, m.override.Signer != nil},
	}
	out := make(map[string]string, len(fields))
	for field, layers := range fields {
		source := "default"
		if layers.file {
			source = "config"
		}
		if layers.override {
			source = "override"
		}
		out[field] = source
	}
	return out
}

func (m *Manager) Reload() error {
	file, err := read(m.paths.ConfigFile)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.persistenceErr != nil {
		return fmt.Errorf("override persistence unhealthy: %w", m.persistenceErr)
	}
	if err := validateMerged(file, m.override); err != nil {
		return err
	}
	m.file = file
	m.effective = merge(file, m.override)
	return nil
}

// PersistenceError reports when the override file could not be reconciled after
// a failed write. Restarting after repairing the file restores normal writes.
func (m *Manager) PersistenceError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.persistenceErr
}

func (m *Manager) ConfiguredBlossomServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.override.BlossomServers != nil {
		return append([]string{}, (*m.override.BlossomServers)...)
	}
	if m.file.BlossomServers != nil {
		return append([]string{}, (*m.file.BlossomServers)...)
	}
	return nil
}

func (m *Manager) Paths() Paths { return m.paths }

// DefaultSystemSignerSocket is where the system signer listens when no
// configuration names a socket.
const DefaultSystemSignerSocket = "/run/nostr-signer.sock"

// SystemSignerSocket is the socket the system signer mode connects to: the
// effective one, else the configuration file's (so that an explicit switch
// back to system without a socket keeps the administrator's choice), else
// the default.
func (m *Manager) SystemSignerSocket() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.effective.Signer.Mode == "system" && m.effective.Signer.Socket != "" {
		return m.effective.Signer.Socket
	}
	return m.fileSystemSignerSocketLocked()
}

// FileSystemSignerSocket is the configuration file's system signer socket,
// else the default: what a switch to system without a socket connects to.
func (m *Manager) FileSystemSignerSocket() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fileSystemSignerSocketLocked()
}

func (m *Manager) fileSystemSignerSocketLocked() string {
	if m.file.Signer != nil && m.file.Signer.Socket != "" {
		return m.file.Signer.Socket
	}
	return DefaultSystemSignerSocket
}

func FieldName(name string) bool {
	return strings.Contains(" relays blossom_servers discover_on_user_relays desktop_entries gnome_search ", " "+name+" ")
}
