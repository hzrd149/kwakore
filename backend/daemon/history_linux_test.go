//go:build linux

package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func TestRPCHistoryLocalAndStrictParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	address := "35129:" + strings.Repeat("a", 64) + ":unknown"
	for _, method := range []string{"napplet.versions", "napplet.snapshots"} {
		result, rpcErr, _ := rpcCall(t, reader, conn, method, `{"address":"`+address+`"}`)
		if rpcErr != nil {
			t.Fatalf("%s: %+v", method, rpcErr)
		}
		var page struct {
			Items           []any  `json:"items"`
			Total           int    `json:"total"`
			LookupStatus    string `json:"lookup_status"`
			HistoryComplete bool   `json:"history_complete"`
		}
		if json.Unmarshal(result, &page) != nil || page.Items == nil || len(page.Items) != 0 || page.Total != 0 || page.LookupStatus != "not_requested" || page.HistoryComplete {
			t.Fatalf("%s: %s", method, result)
		}
		for _, params := range []string{
			`{}`, `{"address":"bad"}`, `{"address":null}`, `{"address":"` + address + `","extra":1}`,
			`{"address":"` + address + `","refresh":null}`, `{"address":"` + address + `","refresh":"true"}`,
			`{"address":"` + address + `","offset":-1}`, `{"address":"` + address + `","limit":501}`,
			`{"address":"` + address + `","limit":0}`, `{"address":"` + address + `","limit":null}`,
			`{"address":"` + address + `","refresh":true,"refresh":false}`,
		} {
			if _, rpcErr, raw := rpcCall(t, reader, conn, method, params); rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams || strings.Contains(raw, "bad") {
				t.Fatalf("%s %s: %+v %s", method, params, rpcErr, raw)
			}
		}
	}
}

func TestRPCHistoryReturnsArchivedEventsWithPagination(t *testing.T) {
	_, reader, conn, paths := rpcService(t)
	sk := nostr.Generate()
	relay := &nostr.Relay{URL: "wss://history.example"}
	var events []nostr.Event
	for i := range 3 {
		evt := nostr.Event{Kind: backend.KindNapplet, CreatedAt: nostr.Timestamp(10 + i), Content: "history",
			Tags: nostr.Tags{{"d", "app"}, {"x", strings.Repeat("a", 64)}}}
		if err := evt.Sign(sk); err != nil {
			t.Fatal(err)
		}
		events = append(events, evt)
		backend.Sys().Pool.EventMiddleware(nostr.RelayEvent{Event: evt, Relay: relay})
	}
	address := "35129:" + sk.Public().Hex() + ":app"
	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.versions", `{"address":"`+address+`","offset":1,"limit":1}`)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	var page backend.ServiceHistoryPage
	if json.Unmarshal(result, &page) != nil || page.Total != 3 || len(page.Items) != 1 || page.Items[0].EventID != events[1].ID.Hex() || page.LatestObserved.EventID != events[2].ID.Hex() || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatalf("history result %s", result)
	}
	if strings.Contains(raw, paths.DataDir) || strings.Contains(raw, `"sig"`) || strings.Contains(raw, `"content"`) {
		t.Fatalf("history exposed internal data %s", raw)
	}
}
