package serviceconfig

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSocialRefreshInterval(t *testing.T) {
	accepted := map[string]time.Duration{
		"0":              0,
		"0s":             0,
		"0h0m":           0,
		"6h":             6 * time.Hour,
		"15m":            15 * time.Minute,
		"168h":           168 * time.Hour,
		"1h30m":          90 * time.Minute,
		"1.5h":           90 * time.Minute,
		"10080m":         168 * time.Hour,
		"900000000000ns": 15 * time.Minute,
	}
	for s, want := range accepted {
		got, ok := SocialRefreshInterval(s)
		if !ok || got != want {
			t.Errorf("SocialRefreshInterval(%q) = %v, %v; want %v, true", s, got, ok, want)
		}
	}
	rejected := []string{
		// empty and whitespace
		"", " 6h", "6h ",
		// out of range
		"14m59s", "168h1s", "169h", "1ns",
		// wrong units or case
		"7d", "1w", "6H", "6µs",
		// signs
		"-6h", "+6h",
		// shape
		"6", "h", "00", "1e3h", ".5h", "6h-",
		// overflow
		"9999999999999999999h",
		// 33 bytes
		strings.Repeat("0", 31) + "6h",
	}
	for _, s := range rejected {
		if d, ok := SocialRefreshInterval(s); ok {
			t.Errorf("SocialRefreshInterval(%q) = %v, true; want rejected", s, d)
		}
	}
	// 32 bytes is still allowed when the value is in range
	if _, ok := SocialRefreshInterval(strings.Repeat("0", 30) + "6h"); !ok {
		t.Error("a 32-byte in-range value was rejected")
	}
}

func TestSocialRefreshDefaultFileOverrideAndClear(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SocialRefresh, m.Sources()["social_refresh"]; got != "6h" || src != "default" {
		t.Fatalf("default social_refresh = %q from %q", got, src)
	}
	if got := Defaults().SocialRefresh; got != "6h" {
		t.Fatalf("Defaults().SocialRefresh = %q", got)
	}

	if err := os.WriteFile(p.ConfigFile, []byte(`{"social_refresh":"12h"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SocialRefresh, m.Sources()["social_refresh"]; got != "12h" || src != "config" {
		t.Fatalf("file social_refresh = %q from %q", got, src)
	}

	if err := m.SetOverride("social_refresh", "0"); err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SocialRefresh, m.Sources()["social_refresh"]; got != "0" || src != "override" {
		t.Fatalf("override = %q from %q", got, src)
	}
	// the override survives a restart on the same paths
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SocialRefresh, m.Sources()["social_refresh"]; got != "0" || src != "override" {
		t.Fatalf("after restart = %q from %q", got, src)
	}
	if v, src := m.Inspect(); v.SocialRefresh != "0" || src["social_refresh"] != "override" {
		t.Fatalf("inspect = %q from %q", v.SocialRefresh, src["social_refresh"])
	}
	if err := m.ClearOverride("social_refresh"); err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SocialRefresh, m.Sources()["social_refresh"]; got != "12h" || src != "config" {
		t.Fatalf("after clear = %q from %q", got, src)
	}
	// clearing an absent override is a no-op
	if err := m.ClearOverride("social_refresh"); err != nil {
		t.Fatal(err)
	}
	if !FieldName("social_refresh") {
		t.Fatal("FieldName(social_refresh) = false")
	}
	if secretField("social_refresh") {
		t.Fatal("social_refresh treated as a secret field")
	}
}

func TestSocialRefreshRejectedInFile(t *testing.T) {
	for _, raw := range []string{`"999h"`, `"14m"`, `"7d"`, `""`, `6`, `null`, `["6h"]`} {
		p := testPaths(t)
		if err := os.WriteFile(p.ConfigFile, []byte(`{"social_refresh":`+raw+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil {
			t.Fatalf("file social_refresh %s accepted", raw)
		}
		// only the string values are checked: a digit in the temp path
		// would make the number look echoed
		if value := strings.Trim(raw, `"`); strings.HasPrefix(raw, `"`) && value != "" && strings.Contains(err.Error(), value) {
			t.Fatalf("load error repeats the value %s: %v", raw, err)
		}
	}
}

func TestSocialRefreshRejectedOverrideKeepsDiskAndSnapshot(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("social_refresh", "30m"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p.OverrideFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{"", "999h", "14m", "7d", "-6h", "1w", 5, []string{"6h"}} {
		err := m.SetOverride("social_refresh", value)
		if err == nil {
			t.Fatalf("override %#v accepted", value)
		}
		if s, ok := value.(string); ok && s != "" && strings.Contains(err.Error(), s) {
			t.Fatalf("override error repeats the value: %v", err)
		}
		after, _ := os.ReadFile(p.OverrideFile)
		if !bytes.Equal(before, after) || m.Effective().SocialRefresh != "30m" {
			t.Fatalf("rejected override %#v changed disk or snapshot", value)
		}
	}
}
