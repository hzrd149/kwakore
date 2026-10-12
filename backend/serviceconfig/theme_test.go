package serviceconfig

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestThemeDefaultFileOverrideAndClear(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().Theme, m.Sources()["theme"]; got != "system" || src != "default" {
		t.Fatalf("default theme = %q from %q", got, src)
	}

	if err := os.WriteFile(p.ConfigFile, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().Theme, m.Sources()["theme"]; got != "dark" || src != "config" {
		t.Fatalf("file theme = %q from %q", got, src)
	}

	if err := m.SetOverride("theme", "light"); err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().Theme, m.Sources()["theme"]; got != "light" || src != "override" {
		t.Fatalf("override = %q from %q", got, src)
	}
	// the override survives a restart on the same paths
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().Theme, m.Sources()["theme"]; got != "light" || src != "override" {
		t.Fatalf("after restart = %q from %q", got, src)
	}
	if err := m.ClearOverride("theme"); err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().Theme, m.Sources()["theme"]; got != "dark" || src != "config" {
		t.Fatalf("after clear = %q from %q", got, src)
	}
	if !FieldName("theme") {
		t.Fatal("FieldName(theme) = false")
	}
	if secretField("theme") {
		t.Fatal("theme treated as a secret field")
	}
}

func TestThemeRejectedInFile(t *testing.T) {
	for _, raw := range []string{`""`, `"Dark"`, `"blue"`, `5`, `null`} {
		p := testPaths(t)
		if err := os.WriteFile(p.ConfigFile, []byte(`{"theme":`+raw+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil {
			t.Fatalf("file theme %s accepted", raw)
		}
		// only the string values are checked: a digit in the temp path
		// would make the number look echoed
		if value := strings.Trim(raw, `"`); strings.HasPrefix(raw, `"`) && value != "" && strings.Contains(err.Error(), value) {
			t.Fatalf("load error repeats the value %s: %v", raw, err)
		}
	}
}

func TestThemeRejectedOverrideKeepsDiskAndSnapshot(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("theme", "dark"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p.OverrideFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{"", "blue", 5, []string{"dark"}} {
		err := m.SetOverride("theme", value)
		if err == nil {
			t.Fatalf("override %#v accepted", value)
		}
		if s, ok := value.(string); ok && s != "" && strings.Contains(err.Error(), s) {
			t.Fatalf("override error repeats the value: %v", err)
		}
		after, _ := os.ReadFile(p.OverrideFile)
		if !bytes.Equal(before, after) || m.Effective().Theme != "dark" {
			t.Fatalf("rejected override %#v changed disk or snapshot", value)
		}
	}
}
