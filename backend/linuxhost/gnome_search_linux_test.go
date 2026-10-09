//go:build linux

package linuxhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGNOMESearchRegistrationAndRemoval(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "share")
	export := filepath.Join(home, "exports", "share")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", export+":/usr/share")
	cli := filepath.Join(home, "bin", "kwakore")
	if err := os.MkdirAll(filepath.Dir(cli), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	h := &Host{CLI: cli}
	if err := h.SetGNOMESearchIntegration(true); err != nil {
		t.Fatal(err)
	}
	desktop, provider, service, err := gnomeSearchPaths()
	if err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]string{
		desktop:  "Exec=\"" + cli + "\" discover",
		provider: "BusName=" + searchBus,
		service:  "Exec=\"" + cli + "\" status",
	} {
		body, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(body), expected) {
			t.Fatalf("%s: %v: %s", path, err, body)
		}
	}
	if err := h.SetGNOMESearchIntegration(false); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{desktop, provider, service} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", path, err)
		}
	}
}
