package serviceconfig

import (
	"os"
	"testing"
)

func TestDesktopIntegrationDefaultsAndOverrides(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Effective().DesktopEntries || !m.Effective().GNOMESearch {
		t.Fatal("desktop integrations must default on")
	}
	for _, field := range []string{"desktop_entries", "gnome_search"} {
		if err := m.SetOverride(field, false); err != nil {
			t.Fatal(err)
		}
	}
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Effective().DesktopEntries || m.Effective().GNOMESearch {
		t.Fatal("false overrides were lost")
	}
	if err := m.ClearOverride("desktop_entries"); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearOverride("gnome_search"); err != nil {
		t.Fatal(err)
	}
	if !m.Effective().DesktopEntries || !m.Effective().GNOMESearch {
		t.Fatal("clear did not restore defaults")
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"desktop_entries":false,"gnome_search":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(); err != nil {
		t.Fatal(err)
	}
	if m.Effective().DesktopEntries || m.Effective().GNOMESearch {
		t.Fatal("declarative false ignored")
	}
}
