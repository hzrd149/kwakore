package backend

import (
	"os"
	"path/filepath"
	"testing"

	"verdana/backend/serviceconfig"
)

func TestServiceBlossomTrustUsesOnlyConfiguredServers(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	paths := serviceconfig.Paths{ConfigFile: filepath.Join(root, "config.json"), DataDir: data, OverrideFile: filepath.Join(data, "settings-overrides.json")}
	m, err := serviceconfig.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	previous := serviceConfig
	serviceConfig = m
	t.Cleanup(func() { serviceConfig = previous })
	if got := userBlobServers(); len(got) != 0 {
		t.Fatalf("defaults were trusted: %v", got)
	}
	if err := m.SetOverride("blossom_servers", []string{"https://trusted.example"}); err != nil {
		t.Fatal(err)
	}
	if got := userBlobServers(); len(got) != 1 || !got["https://trusted.example"] {
		t.Fatalf("override trust: %v", got)
	}
	if err := m.ClearOverride("blossom_servers"); err != nil {
		t.Fatal(err)
	}
	if got := userBlobServers(); len(got) != 0 {
		t.Fatalf("cleared override retained trust: %v", got)
	}
}
