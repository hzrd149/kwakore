//go:build linux

package daemon

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"kwakore/backend/controlprotocol"
)

func TestDecodeSyncIDSettingParams(t *testing.T) {
	for raw, want := range map[string]string{`"laptops"`: "laptops", `""`: ""} {
		field, value, err := decodeSettingParams([]byte(`{"field":"sync_id","value":` + raw + `}`))
		if err != nil || field != "sync_id" || value != want {
			t.Fatalf("%s: %q, %#v, %v", raw, field, value, err)
		}
	}
	for _, bad := range []string{"null", "5", "true", `["x"]`, `{"a":1}`} {
		_, _, err := decodeSettingParams([]byte(`{"field":"sync_id","value":` + bad + `}`))
		if err == nil || err.Code != controlprotocol.InvalidParams {
			t.Fatalf("sync_id accepted %s: %+v", bad, err)
		}
	}
}

func TestRPCSyncIDSetting(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	result, rpcErr, _ := rpcCall(t, reader, conn, "settings.set", `{"field":"sync_id","value":"laptops"}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"sync_id":"laptops"`)) {
		t.Fatalf("set sync_id: %s %+v", result, rpcErr)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "settings.inspect", `{}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"sync_id":"override"`)) {
		t.Fatalf("inspect sync_id: %s %+v", result, rpcErr)
	}
	// the rejected value never reaches the wire
	_, rpcErr, raw := rpcCall(t, reader, conn, "settings.set", `{"field":"sync_id","value":"bad value"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.ConfigInvalid || strings.Contains(raw, "bad value") {
		t.Fatalf("invalid sync_id: %+v %s", rpcErr, raw)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "settings.clear", `{"field":"sync_id"}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"sync_id":""`)) {
		t.Fatalf("clear sync_id: %s %+v", result, rpcErr)
	}
}

func TestReloadWarningNamesSyncID(t *testing.T) {
	err := errors.New("/home/u/.config/kwakore/config.json: sync_id: use 1 to 64 printable ASCII characters without spaces, or an empty string to turn sync off")
	if got := reloadWarning("/home/u/.config/kwakore/config.json", err); !strings.Contains(got, "invalid sync_id") {
		t.Fatalf("warning = %q", got)
	}
}

func TestServiceSyncIDSettingStartsAndStopsCleanly(t *testing.T) {
	s, err := Open(daemonPaths(t), "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if s.installSyncWait == nil {
		t.Fatal("no sync worker started")
	}
	// no signer is set, so the passes these kick return at once
	if err := s.SetSetting("sync_id", "laptops"); err != nil {
		t.Fatal(err)
	}
	if got := s.Manager().Effective().SyncID; got != "laptops" {
		t.Fatalf("sync_id = %q", got)
	}
	if err := s.ClearSetting("sync_id"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
}
