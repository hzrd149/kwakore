//go:build linux

package daemon

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func TestDecodeSocialRefreshSettingParams(t *testing.T) {
	field, value, err := decodeSettingParams([]byte(`{"field":"social_refresh","value":"30m"}`))
	if err != nil || field != "social_refresh" || value != "30m" {
		t.Fatalf("social_refresh 30m: %q, %#v, %v", field, value, err)
	}
	for _, bad := range []string{"null", "true", `["6h"]`, "5"} {
		_, _, err := decodeSettingParams([]byte(`{"field":"social_refresh","value":` + bad + `}`))
		if err == nil || err.Code != controlprotocol.InvalidParams {
			t.Fatalf("social_refresh accepted %s: %+v", bad, err)
		}
	}
}

func TestRPCSocialRefreshSetting(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	result, rpcErr, _ := rpcCall(t, reader, conn, "settings.set", `{"field":"social_refresh","value":"30m"}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"social_refresh":"30m"`)) {
		t.Fatalf("set social_refresh: %s %+v", result, rpcErr)
	}
	// the rejected value never reaches the wire
	_, rpcErr, raw := rpcCall(t, reader, conn, "settings.set", `{"field":"social_refresh","value":"999h"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.ConfigInvalid || strings.Contains(raw, "999h") {
		t.Fatalf("invalid social_refresh: %+v %s", rpcErr, raw)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "settings.clear", `{"field":"social_refresh"}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"social_refresh":"6h"`)) {
		t.Fatalf("clear social_refresh: %s %+v", result, rpcErr)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "settings.inspect", `{}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"social_refresh":"default"`)) {
		t.Fatalf("inspect social_refresh: %s %+v", result, rpcErr)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "service.diagnostics", `{}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"social_refresh":"6h"`)) {
		t.Fatalf("diagnostics social_refresh: %s %+v", result, rpcErr)
	}
}

func TestSocialRefreshSettingsChangedEvent(t *testing.T) {
	s, _, _, _ := rpcService(t)
	sub, err := s.events.subscribe([]string{backend.EventSettingsChanged})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.events.remove(sub) })
	if err := s.SetSetting("social_refresh", "30m"); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-sub.queue:
		if !slices.Equal(e.Fields, []string{"social_refresh"}) {
			t.Fatalf("settings.changed fields = %v", e.Fields)
		}
	case <-time.After(time.Second):
		t.Fatal("no settings.changed after setting social_refresh")
	}
	// the same value again changes nothing and queues nothing
	if err := s.SetSetting("social_refresh", "30m"); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-sub.queue:
		t.Fatalf("unchanged social_refresh queued %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestReloadWarningNamesSocialRefresh(t *testing.T) {
	err := errors.New("/home/u/.config/kwakore/config.json: social_refresh: use 0 to turn the periodic refresh off, or a duration from 15m to 168h such as 6h")
	if got := reloadWarning("/home/u/.config/kwakore/config.json", err); !strings.Contains(got, "invalid social_refresh") {
		t.Fatalf("warning = %q", got)
	}
}
