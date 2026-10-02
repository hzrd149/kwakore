//go:build linux

package osintegration

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxAutostart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if AutostartEnabled() {
		t.Fatal("autostart begins enabled")
	}
	if err := SetAutostart(true, "/opt/Verdana App/verdana"); err != nil {
		t.Fatal(err)
	}
	if !AutostartEnabled() {
		t.Fatal("autostart was not enabled")
	}
	raw, err := os.ReadFile(autostartPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `Exec="/opt/Verdana App/verdana" --background`) {
		t.Fatalf("unexpected desktop entry:\n%s", raw)
	}
	if err := SetAutostart(false, ""); err != nil {
		t.Fatal(err)
	}
	if AutostartEnabled() {
		t.Fatal("autostart remains enabled")
	}
}
