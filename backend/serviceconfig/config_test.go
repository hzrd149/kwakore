package serviceconfig

import (
	"os"
	"path/filepath"
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

func TestConfigRejectsMalformed(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"unknown", `{"private_key":"secret"}`, "private_key"},
		{"duplicate", `{"relays":[],"relays":[]}`, "duplicate"},
		{"trailing", `{} {}`, "trailing"},
		{"bad URL", `{"relays":["https://relay.example"]}`, "relays"},
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
