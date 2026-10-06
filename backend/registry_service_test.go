package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

func resetServiceCatalog(t *testing.T) {
	t.Helper()
	discoverMu.Lock()
	oldCatalog, oldFetched := catalog, catalogFetched
	catalog, catalogFetched = nil, nil
	discoverMu.Unlock()
	t.Cleanup(func() {
		discoverMu.Lock()
		catalog, catalogFetched = oldCatalog, oldFetched
		discoverMu.Unlock()
	})
}

func TestServiceDiscoveryCompletesAndFilters(t *testing.T) {
	resetLauncherState(t)
	resetServiceCatalog(t)
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

func TestServiceDiscoveryEmptyCachedUnavailable(t *testing.T) {
	resetLauncherState(t)
	resetServiceCatalog(t)
	stateMu.Lock()
	state.Relays = []string{}
	stateMu.Unlock()
	page, err := ServiceDiscover(t.Context(), "", false, 0, 100)
	if err != nil || page.Complete || page.FetchedAt != nil || page.Total != 0 || page.Items == nil {
		t.Fatalf("uncached: %+v %v", page, err)
	}
	if _, err := ServiceDiscover(t.Context(), "", true, 0, 100); !errors.Is(err, ErrDiscoveryUnavailable) {
		t.Fatalf("no relay error: %v", err)
	}
}

func TestServiceDiscoverySupersededRefresh(t *testing.T) {
	resetLauncherState(t)
	resetServiceCatalog(t)
	old := subscribeDiscovery
	var calls atomic.Int32
	started := make(chan struct{})
	subscribeDiscovery = func(ctx context.Context, urls []string) (<-chan nostr.RelayEvent, <-chan struct{}, error) {
		if calls.Add(1) == 1 {
			close(started)
			return make(chan nostr.RelayEvent), make(chan struct{}), nil
		}
		events := make(chan nostr.RelayEvent)
		close(events)
		return events, make(chan struct{}), nil
	}
	t.Cleanup(func() { subscribeDiscovery = old })
	firstErr := make(chan error, 1)
	go func() { _, err := ServiceDiscover(t.Context(), "", true, 0, 100); firstErr <- err }()
	<-started
	page, err := ServiceDiscover(t.Context(), "", true, 0, 100)
	if err != nil || !page.Complete || page.FetchedAt == nil || page.Total != 0 {
		t.Fatalf("second refresh: %+v %v", page, err)
	}
	select {
	case err := <-firstErr:
		if !errors.Is(err, ErrDiscoveryConflict) {
			t.Fatalf("first refresh: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("superseded refresh did not return")
	}
}

func TestServiceDiscoveryQueryPagination(t *testing.T) {
	resetServiceCatalog(t)
	pk := nostr.Generate().Public()
	now := time.Now().UTC()
	discoverMu.Lock()
	catalog = []Napp{
		{D: "z", Author: pk, Name: "Notes Z", EventID: "z"},
		{D: "a", Author: pk, Name: "Notes A", EventID: "a"},
		{D: "x", Author: pk, Name: "Other", EventID: "x"},
	}
	catalogFetched = &now
	discoverMu.Unlock()
	page, err := ServiceDiscover(t.Context(), "NOTES", false, 0, 1)
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Address != "35130:"+pk.Hex()+":a" || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatalf("first matching page: %+v %v", page, err)
	}
	page, err = ServiceDiscover(t.Context(), "notes", false, 1, 1)
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Address != "35130:"+pk.Hex()+":z" || page.NextOffset != nil {
		t.Fatalf("last matching page: %+v %v", page, err)
	}
}

func TestServiceDiscoveryTimeoutKeepsCache(t *testing.T) {
	resetLauncherState(t)
	resetServiceCatalog(t)
	now := time.Now().UTC()
	discoverMu.Lock()
	catalog = []Napp{{D: "saved", Author: nostr.Generate().Public(), Name: "Saved"}}
	catalogFetched = &now
	discoverMu.Unlock()
	old := subscribeDiscovery
	subscribeDiscovery = func(ctx context.Context, urls []string) (<-chan nostr.RelayEvent, <-chan struct{}, error) {
		return make(chan nostr.RelayEvent), make(chan struct{}), nil
	}
	t.Cleanup(func() { subscribeDiscovery = old })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := ServiceDiscover(ctx, "", true, 0, 100); !errors.Is(err, ErrDiscoveryTimeout) {
		t.Fatalf("refresh timeout: %v", err)
	}
	page, err := ServiceDiscover(t.Context(), "", false, 0, 100)
	if err != nil || !page.Complete || page.Total != 1 || page.Items[0].Name != "Saved" {
		t.Fatalf("cached catalog lost: %+v %v", page, err)
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

func TestServiceInstallCanonicalAddress(t *testing.T) {
	pk := nostr.Generate().Public().Hex()
	valid := "15129:" + pk + ":"
	if _, err := ParseCanonicalServiceAddress(valid); err != nil {
		t.Fatalf("canonical root refused: %v", err)
	}
	for _, input := range []string{" " + valid, "15129:" + strings.ToUpper(pk) + ":", "15129:" + pk, "35129:" + pk[:16] + ":app", strings.Repeat("x", 4097)} {
		if _, err := ParseCanonicalServiceAddress(input); err == nil {
			t.Fatalf("accepted noncanonical address %q", input)
		}
	}
}
