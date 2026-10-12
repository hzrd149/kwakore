//go:build linux

package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fiatjaf.com/nostr"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func TestRPCUpdatesCheckParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	result, rpcErr, _ := rpcCall(t, reader, conn, "napplet.updates.check", `{}`)
	if rpcErr != nil || string(result) != `{"items":[],"complete":true}` {
		t.Fatalf("empty install: %s %+v", result, rpcErr)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "napplet.updates.check", "")
	if rpcErr != nil || string(result) != `{"items":[],"complete":true}` {
		t.Fatalf("no params: %s %+v", result, rpcErr)
	}
	for _, params := range []string{`{"address":"not-an-address"}`, `{"address":5}`, `{"address":null}`, `{"extra":1}`, `[]`} {
		_, rpcErr, raw := rpcCall(t, reader, conn, "napplet.updates.check", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams || strings.Contains(raw, "not-an-address") {
			t.Fatalf("%s: %+v %s", params, rpcErr, raw)
		}
	}
	missing := `{"address":"35129:` + strings.Repeat("a", 64) + `:missing"}`
	if _, rpcErr, _ := rpcCall(t, reader, conn, "napplet.updates.check", missing); rpcErr == nil || rpcErr.Code != controlprotocol.NotFound {
		t.Fatalf("unknown address: %+v", rpcErr)
	}
}

func TestRPCDetailParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	for _, params := range []string{`{}`, "", `{"address":"bad"}`, `{"address":"35129:` + strings.Repeat("a", 64) + `:x","extra":1}`} {
		if _, rpcErr, _ := rpcCall(t, reader, conn, "napplet.detail", params); rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("%q: %+v", params, rpcErr)
		}
	}
	missing := `{"address":"35129:` + strings.Repeat("a", 64) + `:missing"}`
	if _, rpcErr, _ := rpcCall(t, reader, conn, "napplet.detail", missing); rpcErr == nil || rpcErr.Code != controlprotocol.NotFound {
		t.Fatalf("unknown address: %+v", rpcErr)
	}
}

func TestRPCInstalledItemsCarryUpdateAvailable(t *testing.T) {
	paths := daemonPaths(t)
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	key := nostr.MustSecretKeyFromHex(strings.Repeat("0", 63) + "2")
	napp := backend.Napp{D: "notes", Name: "Notes", Author: key.Public(), Kind: backend.KindNapplet, Format: backend.FormatNapplet, EventID: strings.Repeat("e", 64), CreatedAt: 10}
	// a napplet record is keyed by its address
	napp.ID = napp.Address()
	data, err := json.Marshal(backend.AppState{InstalledNapps: map[string]backend.Napp{napp.ID: napp}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.DataDir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, reader, conn, _ := rpcServiceAt(t, paths)
	result, rpcErr, _ := rpcCall(t, reader, conn, "napplet.installed", `{}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"update_available":null`)) || !bytes.Contains(result, []byte(`"total":1`)) {
		t.Fatalf("installed: %s %+v", result, rpcErr)
	}
	// the detail of the installed napplet answers from the record
	result, rpcErr, _ = rpcCall(t, reader, conn, "napplet.detail", `{"address":"`+napp.Address()+`"}`)
	if rpcErr != nil || !bytes.Contains(result, []byte(`"installed":true`)) || bytes.Contains(result, []byte(paths.DataDir)) {
		t.Fatalf("detail: %s %+v", result, rpcErr)
	}
}
