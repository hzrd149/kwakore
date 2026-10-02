//go:build linux

package osintegration

import (
	"os"
	"strings"
	"testing"
)

func TestGNOMESearchSupported(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "ubuntu:GNOME")
	if !GnomeSearchSupported() {
		t.Fatal("GNOME desktop was not detected")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "KDE")
	if GnomeSearchSupported() {
		t.Fatal("non-GNOME desktop was detected as GNOME")
	}
}

func TestSetGNOMESearchIntegrationCreatesAndRemovesFiles(t *testing.T) {
	home := t.TempDir()
	dataHome := home + "/data-home"
	dataDir := home + "/flatpak-exports"
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_DATA_DIRS", dataDir+":/usr/share")
	if err := SetGNOMESearchIntegration(true, "/opt/Verdana App/verdana"); err != nil {
		t.Fatal(err)
	}
	desktop, provider, service := gnomeSearchIntegrationPaths()
	if strings.HasPrefix(provider, dataHome) {
		t.Fatalf("provider was written under XDG_DATA_HOME, which GNOME Shell does not scan: %s", provider)
	}
	if !strings.HasPrefix(provider, dataDir) {
		t.Fatalf("provider was not written under a user XDG_DATA_DIR: %s", provider)
	}
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

	if err := SetGNOMESearchIntegration(false, ""); err != nil {
		t.Fatal(err)
	}
	for path := range checks {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("integration file remains at %s: %v", path, err)
		}
	}
}

func TestGNOMESearchIntegrationNeedsUserDataDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_DATA_DIRS", "/usr/local/share:/usr/share")
	if err := SetGNOMESearchIntegration(true, "/opt/verdana"); err == nil {
		t.Fatal("integration succeeded without a user-writable XDG_DATA_DIR")
	}
}
