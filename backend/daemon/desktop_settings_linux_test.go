//go:build linux

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kwakore/backend/desktopentry"
)

func TestDecodeDesktopIntegrationSettingParams(t *testing.T) {
	for _, field := range []string{"desktop_entries", "gnome_search"} {
		params, _ := json.Marshal(map[string]any{"field": field, "value": false})
		gotField, value, err := decodeSettingParams(params)
		if err != nil || gotField != field || value != false {
			t.Fatalf("%s: %s, %v, %v", field, gotField, value, err)
		}
		for _, bad := range []string{"null", `"false"`, "[]"} {
			params = []byte(`{"field":"` + field + `","value":` + bad + `}`)
			if _, _, err := decodeSettingParams(params); err == nil {
				t.Fatalf("%s accepted %s", field, bad)
			}
		}
	}
}

func TestServiceDesktopSettingsRemoveIntegrations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "share"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(home, "exports", "share")+":/usr/share")
	cli := filepath.Join(home, "kwakore")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	previous := nativeEntryCLIPath
	nativeEntryCLIPath = func() string { return cli }
	t.Cleanup(func() { nativeEntryCLIPath = previous })
	s, err := Open(daemonPaths(t), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.searchHost.SetGNOMESearchIntegration(true); err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(home, "exports", "share", "gnome-shell", "search-providers", "org.kwakore.Search.search-provider.ini")
	if _, err := os.Stat(provider); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("gnome_search", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(provider); !os.IsNotExist(err) {
		t.Fatalf("provider remains: %v", err)
	}
	apps := filepath.Join(home, "share", "applications")
	entry := filepath.Join(apps, desktopentry.FileName("35129:"+"0000000000000000000000000000000000000000000000000000000000000000"+":notes"))
	if err := os.MkdirAll(apps, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("[Desktop Entry]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("desktop_entries", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entry); !os.IsNotExist(err) {
		t.Fatalf("entry remains: %v", err)
	}
}
