package serviceconfig

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestSyncIDDefaultFileOverrideAndClear(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Effective().SyncID; got != "" {
		t.Fatalf("default sync_id = %q, want empty", got)
	}
	if got := m.Sources()["sync_id"]; got != "default" {
		t.Fatalf("default source = %q", got)
	}
	// clearing an absent override never creates the override file
	if err := m.ClearOverride("sync_id"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("clear created override file: %v", err)
	}

	if err := os.WriteFile(p.ConfigFile, []byte(`{"sync_id":"laptops"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Effective().SyncID; got != "laptops" {
		t.Fatalf("file sync_id = %q", got)
	}
	if got := m.Sources()["sync_id"]; got != "config" {
		t.Fatalf("file source = %q", got)
	}

	if err := m.SetOverride("sync_id", "desk"); err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SyncID, m.Sources()["sync_id"]; got != "desk" || src != "override" {
		t.Fatalf("override = %q from %q", got, src)
	}
	// the override survives a restart on the same paths
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SyncID, m.Sources()["sync_id"]; got != "desk" || src != "override" {
		t.Fatalf("after restart = %q from %q", got, src)
	}
	if err := m.ClearOverride("sync_id"); err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SyncID, m.Sources()["sync_id"]; got != "laptops" || src != "config" {
		t.Fatalf("after clear = %q from %q", got, src)
	}
	// an empty override turns sync off over the file value
	if err := m.SetOverride("sync_id", ""); err != nil {
		t.Fatal(err)
	}
	if got, src := m.Effective().SyncID, m.Sources()["sync_id"]; got != "" || src != "override" {
		t.Fatalf("empty override = %q from %q", got, src)
	}
}

func TestSyncIDAcceptedValues(t *testing.T) {
	for _, value := range []string{"a", strings.Repeat("x", 64), "kwakore-installed_1.~!"} {
		p := testPaths(t)
		m, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.SetOverride("sync_id", value); err != nil {
			t.Fatalf("override %q rejected: %v", value, err)
		}
		if got := m.Effective().SyncID; got != value {
			t.Fatalf("override %q stored as %q", value, got)
		}
		body := []byte(`{"sync_id":` + quoteJSON(t, value) + `}`)
		if err := os.WriteFile(p.ConfigFile, body, 0600); err != nil {
			t.Fatal(err)
		}
		if err := m.ClearOverride("sync_id"); err != nil {
			t.Fatal(err)
		}
		if err := m.Reload(); err != nil {
			t.Fatalf("file %q rejected: %v", value, err)
		}
		if got := m.Effective().SyncID; got != value {
			t.Fatalf("file %q read as %q", value, got)
		}
	}
}

func TestSyncIDRejectedValuesNeverEchoed(t *testing.T) {
	for _, value := range []string{strings.Repeat("y", 65), "has space", "tab\t", "é", "\x7f"} {
		p := testPaths(t)
		m, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.SetOverride("sync_id", "keep"); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(p.OverrideFile)
		if err != nil {
			t.Fatal(err)
		}
		err = m.SetOverride("sync_id", value)
		if err == nil {
			t.Fatalf("override %q accepted", value)
		}
		if strings.Contains(err.Error(), value) {
			t.Fatalf("override error repeats the value: %v", err)
		}
		after, _ := os.ReadFile(p.OverrideFile)
		if !bytes.Equal(before, after) || m.Effective().SyncID != "keep" {
			t.Fatalf("rejected override %q changed disk or snapshot", value)
		}

		if err := os.WriteFile(p.ConfigFile, []byte(`{"sync_id":`+quoteJSON(t, value)+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		err = m.Reload()
		if err == nil {
			t.Fatalf("file %q accepted by reload", value)
		}
		if strings.Contains(err.Error(), value) {
			t.Fatalf("reload error repeats the value: %v", err)
		}
		_, err = Load(p)
		if err == nil {
			t.Fatalf("file %q accepted by load", value)
		}
		if strings.Contains(err.Error(), value) {
			t.Fatalf("load error repeats the value: %v", err)
		}
	}
}

func TestSyncIDRejectsWrongTypes(t *testing.T) {
	for _, body := range []string{`{"sync_id":5}`, `{"sync_id":null}`, `{"sync_id":["x"]}`} {
		p := testPaths(t)
		if err := os.WriteFile(p.ConfigFile, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatalf("%s accepted", body)
		}
	}
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{5, []string{"x"}, true, nil} {
		if err := m.SetOverride("sync_id", value); err == nil {
			t.Fatalf("override %#v accepted", value)
		}
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("rejected overrides created the file: %v", err)
	}
	if !FieldName("sync_id") {
		t.Fatal("FieldName(sync_id) = false")
	}
	if secretField("sync_id") {
		t.Fatal("sync_id treated as a secret field")
	}
}

func quoteJSON(t *testing.T, s string) string {
	t.Helper()
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte("0123456789abcdef"[r>>4])
			b.WriteByte("0123456789abcdef"[r&0xf])
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
