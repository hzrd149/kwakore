//go:build linux

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func TestDecodeThemeSettingParams(t *testing.T) {
	field, value, err := decodeSettingParams([]byte(`{"field":"theme","value":"dark"}`))
	if err != nil || field != "theme" || value != "dark" {
		t.Fatalf("theme dark: %q, %#v, %v", field, value, err)
	}
	for _, bad := range []string{"null", "true", `["dark"]`, "5"} {
		_, _, err := decodeSettingParams([]byte(`{"field":"theme","value":` + bad + `}`))
		if err == nil || err.Code != controlprotocol.InvalidParams {
			t.Fatalf("theme accepted %s: %+v", bad, err)
		}
	}
}

func TestRPCThemeSetting(t *testing.T) {
	s, reader, conn, _ := rpcService(t)
	result, rpcErr, _ := rpcCall(t, reader, conn, "settings.set", `{"field":"theme","value":"dark"}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"theme":"dark"`)) {
		t.Fatalf("set theme: %s %+v", result, rpcErr)
	}
	if got := backend.ThemeName(); got != backend.ThemeDark {
		t.Fatalf("window theme after set dark = %q", got)
	}
	// the rejected value never reaches the wire
	_, rpcErr, raw := rpcCall(t, reader, conn, "settings.set", `{"field":"theme","value":"blue"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.ConfigInvalid || strings.Contains(raw, "blue") {
		t.Fatalf("invalid theme: %+v %s", rpcErr, raw)
	}
	if got := backend.ThemeName(); got != backend.ThemeDark {
		t.Fatalf("a rejected theme changed the window theme to %q", got)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "settings.clear", `{"field":"theme"}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"theme":"system"`)) {
		t.Fatalf("clear theme: %s %+v", result, rpcErr)
	}
	if s.systemDark.Load() {
		t.Fatal("systemDark set without a watch")
	}
	if got := backend.ThemeName(); got != backend.ThemeLight {
		t.Fatalf("system theme without a desktop preference = %q", got)
	}

	// "system" follows the desktop preference
	s.setSystemDark(true)
	if got := backend.ThemeName(); got != backend.ThemeDark {
		t.Fatalf("system theme with a dark desktop = %q", got)
	}
	s.setSystemDark(false)
	if got := backend.ThemeName(); got != backend.ThemeLight {
		t.Fatalf("system theme with a light desktop = %q", got)
	}
	// a forced theme ignores it
	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatal(err)
	}
	s.setSystemDark(true)
	if got := backend.ThemeName(); got != backend.ThemeLight {
		t.Fatalf("light theme with a dark desktop = %q", got)
	}
	// and picks it up again when cleared
	if err := s.ClearSetting("theme"); err != nil {
		t.Fatal(err)
	}
	if got := backend.ThemeName(); got != backend.ThemeDark {
		t.Fatalf("cleared theme with a dark desktop = %q", got)
	}
}

func TestReloadWarningNamesTheme(t *testing.T) {
	err := errors.New("/home/u/.config/kwakore/config.json: theme: use system, light or dark")
	if got := reloadWarning("/home/u/.config/kwakore/config.json", err); !strings.Contains(got, "invalid theme") {
		t.Fatalf("warning = %q", got)
	}
}

func TestRPCDiagnosticsInstallSyncOff(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	result, rpcErr, _ := rpcCall(t, reader, conn, "service.diagnostics", `{}`)
	if rpcErr != nil {
		t.Fatalf("diagnostics: %+v", rpcErr)
	}
	var got struct {
		InstallSync json.RawMessage `json:"install_sync"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	want := `{"enabled":false,"last_attempt":null,"last_success":null,"last_result":null,"napplets":0}`
	if string(got.InstallSync) != want {
		t.Fatalf("install_sync = %s, want %s", got.InstallSync, want)
	}
}
