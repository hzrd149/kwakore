//go:build linux

package main

import (
	"os"
	"strings"
	"testing"
)

func TestGNOMESearchSupported(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "ubuntu:GNOME")
	if !gnomeSearchSupported() {
		t.Fatal("GNOME desktop was not detected")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	if gnomeSearchSupported() {
		t.Fatal("non-GNOME desktop was detected as GNOME")
	}
}

func TestSetGNOMESearchIntegrationCreatesAndRemovesFiles(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := setGNOMESearchIntegration(true, "/opt/Verdana App/verdana"); err != nil {
		t.Fatal(err)
	}
	desktop, provider, service := gnomeSearchIntegrationPaths()
	checks := map[string]string{
		desktop:  `Exec="/opt/Verdana App/verdana"`,
		provider: "BusName=com.verdana.Verdana.SearchProvider",
		service:  `Exec="/opt/Verdana App/verdana" --background`,
	}
	for path, want := range checks {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s does not contain %q:\n%s", path, want, raw)
		}
	}

	if err := setGNOMESearchIntegration(false, ""); err != nil {
		t.Fatal(err)
	}
	for path := range checks {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("integration file remains at %s: %v", path, err)
		}
	}
}
