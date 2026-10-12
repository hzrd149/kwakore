//go:build linux

package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"fiatjaf.com/nostr"
	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func TestRPCExactInstallAndOfflineRollback(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	var hits atomic.Int64
	documents := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if data, ok := documents[strings.TrimPrefix(r.URL.Path, "/")]; ok {
			w.Write(data)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	value, _ := json.Marshal([]string{server.URL})
	if _, err, _ := rpcCall(t, reader, conn, "settings.set", `{"field":"blossom_servers","value":`+string(value)+`}`); err != nil {
		t.Fatal(err)
	}
	sk := nostr.Generate()
	var events []nostr.Event
	for i, code := range []string{"<!doctype html><title>Old</title>", "<!doctype html><title>New</title>"} {
		sum := sha256.Sum256([]byte(code))
		hash := hex.EncodeToString(sum[:])
		documents[hash] = []byte(code)
		evt := nostr.Event{Kind: backend.KindNapplet, CreatedAt: nostr.Timestamp(10 + i), Content: "A test napplet",
			Tags: nostr.Tags{{"d", "versions"}, {"x", hash}}}
		if err := evt.Sign(sk); err != nil {
			t.Fatal(err)
		}
		events = append(events, evt)
		backend.Sys().Pool.EventMiddleware(nostr.RelayEvent{Event: evt, Relay: &nostr.Relay{URL: "wss://history.example"}})
	}
	address := "35129:" + sk.Public().Hex() + ":versions"
	params := func(id string) string { return `{"address":"` + address + `","event_id":"` + id + `"}` }
	result, rpcErr, _ := rpcCall(t, reader, conn, "napplet.install", params(events[0].ID.Hex()))
	if rpcErr != nil || !strings.Contains(string(result), events[0].ID.Hex()) || hits.Load() != 1 {
		t.Fatalf("exact install %s %+v", result, rpcErr)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "napplet.select", params(events[1].ID.Hex()))
	if rpcErr != nil || !strings.Contains(string(result), `"data_reset":true`) || !strings.Contains(string(result), `"direction":"forward"`) {
		t.Fatalf("forward %s %+v", result, rpcErr)
	}
	before := hits.Load()
	result, rpcErr, _ = rpcCall(t, reader, conn, "napplet.select", params(events[0].ID.Hex()))
	if rpcErr != nil || hits.Load() != before || !strings.Contains(string(result), `"direction":"backward"`) || !strings.Contains(string(result), `"pinned":true`) {
		t.Fatalf("cached rollback %s %+v", result, rpcErr)
	}
	result, rpcErr, _ = rpcCall(t, reader, conn, "napplet.detail", `{"address":"`+address+`"}`)
	if rpcErr != nil || !strings.Contains(string(result), `"pinned":true`) {
		t.Fatalf("pinned detail %s %+v", result, rpcErr)
	}
}

func TestRPCSelectionStrictParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	for _, method := range []string{"napplet.select", "napplet.install"} {
		for _, raw := range []string{
			`{"address":"` + address + `","event_id":null}`,
			`{"address":"` + address + `","event_id":"short"}`,
			`{"address":"` + address + `","event_id":"` + strings.Repeat("B", 64) + `"}`,
			`{"address":"` + address + `","event_id":1}`,
			`{"address":"` + address + `","event_id":"` + strings.Repeat("b", 64) + `","extra":true}`,
		} {
			if _, err, _ := rpcCall(t, reader, conn, method, raw); err == nil || err.Code != controlprotocol.InvalidParams {
				t.Fatalf("%s %s: %+v", method, raw, err)
			}
		}
	}
	if _, err, _ := rpcCall(t, reader, conn, "napplet.select", `{"address":"`+address+`"}`); err == nil || err.Code != controlprotocol.InvalidParams {
		t.Fatalf("missing event id: %+v", err)
	}
}
