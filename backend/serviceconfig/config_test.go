package serviceconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	return Paths{ConfigFile: filepath.Join(root, "config.json"), DataDir: data, OverrideFile: filepath.Join(data, "settings-overrides.json")}
}

func TestConfigDefaultsAndPresence(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("missing config was created: %v", err)
	}
	if !m.Effective().DiscoverOnUserRelays {
		t.Fatal("default discovery disabled")
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":[],"discover_on_user_relays":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	got := m.Effective()
	if len(got.Relays) != 0 || got.DiscoverOnUserRelays {
		t.Fatalf("presence lost: %+v", got)
	}
}

func TestConfigSignerNonSecretAndRejectsCredentials(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"signer":{"mode":"nsec"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil || m.Effective().Signer.Mode != "nsec" {
		t.Fatalf("signer config: %+v %v", m, err)
	}
	for _, body := range []string{
		`{"signer":{"mode":"nsec","secret":"secret-sentinel"}}`,
		`{"signer":{"mode":"none","client_key":"secret-sentinel"}}`,
		`{"private_key":"secret-sentinel"}`,
		`{"signer":{"mode":"bunker","relay":"wss://relay.example/?token=secret-sentinel"}}`,
		`{"signer":{"mode":"nsec","mode":"none","secret":"secret-sentinel"}}`,
		`{"signer":null}`,
	} {
		if err := os.WriteFile(p.ConfigFile, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		err := m.Reload()
		if err == nil || strings.Contains(err.Error(), "secret-sentinel") || m.Effective().Signer.Mode != "nsec" {
			t.Fatalf("unsafe reload: %v", err)
		}
	}
}

func TestSecretFieldOverrideRejectsBeforeValueFormatting(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.OverrideFile, []byte(`{"signer":{"mode":"nsec","secret":"private-sentinel"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil || strings.Contains(err.Error(), "private-sentinel") || !strings.Contains(err.Error(), "signer.secret") {
		t.Fatalf("override leaked or accepted: %v", err)
	}
}

func TestConfigRejectsMalformed(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"unknown", `{"private_key":"secret"}`, "private_key"},
		{"duplicate", `{"relays":[],"relays":[]}`, "duplicate"},
		{"trailing", `{} {}`, "trailing"},
		{"bad URL", `{"relays":["https://relay.example"]}`, "relays"},
		{"null list", `{"relays":null}`, "relays"},
		{"null boolean", `{"discover_on_user_relays":null}`, "discover_on_user_relays"},
		{"noncanonical relay", `{"relays":["wss://relay.example/"]}`, "relays"},
		{"blossom query", `{"blossom_servers":["https://blossom.example/?token=x"]}`, "blossom_servers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testPaths(t)
			if err := os.WriteFile(p.ConfigFile, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}

func TestConfigRejectsOversizedFileAndRelativeRoots(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(strings.Repeat(" ", maxConfigBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("oversized config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	t.Setenv("XDG_DATA_HOME", filepath.Dir(p.DataDir))
	if _, err := ResolvePaths(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative config root: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(p.ConfigFile))
	t.Setenv("XDG_DATA_HOME", "relative-data")
	if _, err := ResolvePaths(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative data root: %v", err)
	}
}

func TestOverridePrecedenceAndClear(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"discover_on_user_relays":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("discover_on_user_relays", true); err != nil {
		t.Fatal(err)
	}
	if !m.Effective().DiscoverOnUserRelays {
		t.Fatal("override did not win")
	}
	m2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !m2.Effective().DiscoverOnUserRelays {
		t.Fatal("override did not survive restart")
	}
	if err := m2.ClearOverride("discover_on_user_relays"); err != nil {
		t.Fatal(err)
	}
	if m2.Effective().DiscoverOnUserRelays {
		t.Fatal("file value not restored")
	}
	if err := m2.SetOverride("login", "secret"); err == nil {
		t.Fatal("unsupported setting accepted")
	}
}

func TestConfigReloadKeepsFieldOverridesAndRejectsInvalidFile(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://first.example"],"blossom_servers":["https://first.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("relays", []string{"wss://override.example"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://second.example"],"blossom_servers":["https://second.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := m.Effective(); got.Relays[0] != "wss://override.example" || got.BlossomServers[0] != "https://second.example" {
		t.Fatalf("reload lost precedence: %+v", got)
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"blossom_servers":["ftp://invalid.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	if got := m.Effective(); got.Relays[0] != "wss://override.example" || got.BlossomServers[0] != "https://second.example" {
		t.Fatalf("invalid reload changed snapshot: %+v", got)
	}
}

func TestReloadRejectsWholeCandidateThenRestoresDefaultsWithOverride(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://first.example"],"blossom_servers":["https://first.example"],"discover_on_user_relays":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("relays", []string{"wss://override.example"}); err != nil {
		t.Fatal(err)
	}
	before := m.Effective()
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://new.example"],"blossom_servers":["ftp://invalid.example"],"discover_on_user_relays":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := m.Effective(); !reflect.DeepEqual(got, before) {
		t.Fatalf("file edit changed live settings without reload: %+v", got)
	}
	if err := m.Reload(); err == nil || !strings.Contains(err.Error(), "blossom_servers") {
		t.Fatalf("invalid candidate: %v", err)
	}
	if got := m.Effective(); !reflect.DeepEqual(got, before) {
		t.Fatalf("invalid candidate partially applied: %+v", got)
	}
	if err := os.Remove(p.ConfigFile); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := m.Effective(); !reflect.DeepEqual(got.Relays, []string{"wss://override.example"}) || !reflect.DeepEqual(got.BlossomServers, Defaults().BlossomServers) || !got.DiscoverOnUserRelays {
		t.Fatalf("missing file did not reveal defaults under override: %+v", got)
	}
}
