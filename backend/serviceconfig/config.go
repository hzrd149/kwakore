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
	dataDir := filepath.Join(dataRoot, "kwakore")
	return Paths{ConfigFile: filepath.Join(configRoot, "kwakore", "config.json"), DataDir: dataDir, OverrideFile: filepath.Join(dataDir, "settings-overrides.json")}, nil
}

type Config struct {
	Relays               *[]string `json:"relays,omitempty"`
	BlossomServers       *[]string `json:"blossom_servers,omitempty"`
	DiscoverOnUserRelays *bool     `json:"discover_on_user_relays,omitempty"`
}

type Effective struct {
	Relays               []string `json:"relays"`
	BlossomServers       []string `json:"blossom_servers"`
	DiscoverOnUserRelays bool     `json:"discover_on_user_relays"`
}

func Defaults() Effective {
	return Effective{Relays: []string{"wss://relay.nostrapps.com", "wss://relay.nostrapps.com/public"}, BlossomServers: []string{"https://relay.nostrapps.com", "https://nostr.download"}, DiscoverOnUserRelays: true}
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
	}
	return v
}

func validate(c Config) error {
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
			if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (field.scheme == "wss" && u.Scheme != "wss") || (field.scheme == "https" && u.Scheme != "https" && u.Scheme != "http") {
				return fmt.Errorf("%s: invalid URL %q", field.name, raw)
			}
			if seen[raw] {
				return fmt.Errorf("%s: duplicate URL %q", field.name, raw)
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
	for key := range raw {
		if key != "relays" && key != "blossom_servers" && key != "discover_on_user_relays" {
			return Config{}, fmt.Errorf("%s: unknown setting %q", path, key)
		}
	}
	if err := checkDuplicateKeys(b); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
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
	mu        sync.RWMutex
	paths     Paths
	file      Config
	override  Config
	effective Effective
}

func Load(paths Paths) (*Manager, error) {
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

func (m *Manager) Reload() error {
	file, err := read(m.paths.ConfigFile)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.file = file
	m.effective = merge(file, m.override)
	return nil
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

func FieldName(name string) bool {
	return strings.Contains(" relays blossom_servers discover_on_user_relays ", " "+name+" ")
}
