package backend

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

func TestServiceDiscoveryCompletesAndFilters(t *testing.T) {
	resetLauncherState(t)
	old := subscribeDiscovery
	subscribeDiscovery = func(ctx context.Context, urls []string) (<-chan nostr.RelayEvent, <-chan struct{}, error) {
		events := make(chan nostr.RelayEvent)
		eose := make(chan struct{})
		go func() {
			events <- testNappEvent(nostr.Generate(), "notes", "Notes", 1)
			close(eose)
			<-ctx.Done()
			close(events)
		}()
		return events, eose, nil
	}
	t.Cleanup(func() { subscribeDiscovery = old })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	page, err := ServiceDiscover(ctx, "notes", true, 0, 100)
	if err != nil || !page.Complete || page.FetchedAt == nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Name != "Notes" {
		t.Fatalf("refresh: %+v %v", page, err)
	}
	filtered, err := ServiceDiscover(ctx, "no match", false, 0, 100)
	if err != nil || !filtered.Complete || filtered.FetchedAt == nil || filtered.Total != 0 {
		t.Fatalf("cached filter: %+v %v", filtered, err)
	}
}

func TestServiceInstalledCanonicalSafePages(t *testing.T) {
	pk := nostr.Generate().Public()
	old := state.InstalledNapps
	stateMu.Lock()
	state.InstalledNapps = map[string]Napp{
		"legacy-short": {ID: pk.Hex()[:16] + "~z", D: "z", Author: pk, Name: "Z\x00\u200b" + strings.Repeat("x", 300), CreatedAt: 7, EventID: "event-z", Paths: []NappPath{{Path: "private/path"}}, Servers: []string{"private.server"}},
		"root":         {ID: "root", Author: pk, Format: FormatNapplet, Kind: KindRootNapplet, Name: "Root", ArtifactHash: "hash-root", EventID: "event-root"},
	}
	stateMu.Unlock()
	t.Cleanup(func() { stateMu.Lock(); state.InstalledNapps = old; stateMu.Unlock() })

	first := ServiceInstalled(0, 1)
	if first.Total != 2 || first.NextOffset == nil || *first.NextOffset != 1 || len(first.Items) != 1 || first.Items[0].Address != "15129:"+pk.Hex()+":" {
		t.Fatalf("first page: %+v", first)
	}
	last := ServiceInstalled(1, 1)
	if last.Total != 2 || last.NextOffset != nil || len(last.Items) != 1 || last.Items[0].Address != "35130:"+pk.Hex()+":z" {
		t.Fatalf("last page: %+v", last)
	}
	if got := []rune(last.Items[0].Name); len(got) != 256 || strings.ContainsAny(last.Items[0].Name, "\x00\u200b") {
		t.Fatalf("unsanitized name: %q", last.Items[0].Name)
	}
	wire, err := json.Marshal(last)
	if err != nil || strings.Contains(string(wire), "private/path") || strings.Contains(string(wire), "private.server") || strings.Contains(string(wire), "legacy-short") || strings.Contains(string(wire), "updateAvailable") {
		t.Fatalf("unsafe result: %s %v", wire, err)
	}
}
