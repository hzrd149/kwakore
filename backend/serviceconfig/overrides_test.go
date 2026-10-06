package serviceconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOverridePrecedenceAndRestart(t *testing.T) {
	p := testPaths(t)
	original := []byte(`{"relays":["wss://file.example"],"blossom_servers":["https://file.example"],"discover_on_user_relays":true}`)
	if err := os.WriteFile(p.ConfigFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("load created override file: %v", err)
	}
	if err := m.SetOverride("relays", []string{}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("blossom_servers", []string{"https://override.example"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("discover_on_user_relays", false); err != nil {
		t.Fatal(err)
	}
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	got := m.Effective()
	if len(got.Relays) != 0 || !reflect.DeepEqual(got.BlossomServers, []string{"https://override.example"}) || got.DiscoverOnUserRelays {
		t.Fatalf("override presence lost on restart: %+v", got)
	}
	if got, err := os.ReadFile(p.ConfigFile); err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("declarative config changed: %q, %v", got, err)
	}
	if got := m.ConfiguredBlossomServers(); !reflect.DeepEqual(got, []string{"https://override.example"}) {
		t.Fatalf("configured servers: %v", got)
	}
	servers := m.ConfiguredBlossomServers()
	servers[0] = "https://tampered.example"
	if m.ConfiguredBlossomServers()[0] != "https://override.example" {
		t.Fatal("configured server list aliases manager state")
	}
}

func TestOverrideClearPreservesOtherFields(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://file.example"],"blossom_servers":["https://file.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("relays", []string{"wss://override.example"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("blossom_servers", []string{}); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearOverride("relays"); err != nil {
		t.Fatal(err)
	}
	got := m.Effective()
	if !reflect.DeepEqual(got.Relays, []string{"wss://file.example"}) || len(got.BlossomServers) != 0 {
		t.Fatalf("clearing relays changed other fields: %+v", got)
	}
	if err := m.ClearOverride("blossom_servers"); err != nil {
		t.Fatal(err)
	}
	if got := m.ConfiguredBlossomServers(); !reflect.DeepEqual(got, []string{"https://file.example"}) {
		t.Fatalf("file servers not restored: %v", got)
	}
}

func TestOverrideUnsupportedField(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"update_preference", "login", "relays "} {
		if err := m.SetOverride(field, true); err == nil || !strings.Contains(err.Error(), "unsupported setting") {
			t.Fatalf("SetOverride(%q) = %v", field, err)
		}
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("unsupported mutation wrote file: %v", err)
	}
}

func TestOverrideCreatesPrivateDataDirOnMutation(t *testing.T) {
	root := t.TempDir()
	p := Paths{ConfigFile: filepath.Join(root, "config.json"), DataDir: filepath.Join(root, "data"), OverrideFile: filepath.Join(root, "data", "settings-overrides.json")}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.DataDir); !os.IsNotExist(err) {
		t.Fatalf("load created data directory: %v", err)
	}
	if err := m.SetOverride("relays", []string{}); err != nil {
		t.Fatalf("first mutation failed to create data directory: %v", err)
	}
	for _, path := range []string{p.DataDir, p.OverrideFile} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0700)
		if path == p.OverrideFile {
			want = 0600
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}
