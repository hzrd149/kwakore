package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/sdk"
	cache_memory "fiatjaf.com/nostr/sdk/cache/memory"
	"kwakore/backend/eventdb"
)

func withManifestHistory(t *testing.T) *manifestArchive {
	t.Helper()
	a, err := openManifestArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := manifestHistory.Swap(a)
	t.Cleanup(func() {
		manifestHistory.Store(old)
		a.db.Close()
	})
	return a
}

func historySnapshot(t *testing.T, sk nostr.SecretKey, address, content string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	return signedWith(t, sk, KindSnapshotNapplet, nostr.Tags{
		{"x", testArtifactOf(content)}, {"title", content}, {"a", address}, {"A", address},
	}, content, at)
}

func TestManifestHistoryRetainsEveryAuthenticPublication(t *testing.T) {
	newUpdateRig(t)
	a := withManifestHistory(t)
	sk := nostr.Generate()
	low, high := tiedPair(t, sk)
	bad := invalidNapplet(t, sk, "app", 200)
	old := validNapplet(t, sk, "app", "old", 10)
	forged := high
	forged.ID = nostr.ID{}
	wrongSignature := low
	wrongSignature.Sig[0] ^= 1
	normalNote := signedWith(t, sk, 1, nil, "not a manifest", 300)
	winners := latestByAddress{}
	for _, evt := range []nostr.Event{high, low, bad, old, forged, wrongSignature, normalNote} {
		winners.add(evt)
	}
	page, err := ServiceHistory(context.Background(), eventAddress(old), false, false, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || len(page.Items) != 2 || page.Items[0].EventID != bad.ID.Hex() || page.Items[1].EventID != low.ID.Hex() || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatalf("first page %+v", page)
	}
	if page.Items[0].Valid || page.Items[0].Reason == "" || page.LatestObserved == nil || page.LatestObserved.EventID != bad.ID.Hex() || page.HistoryComplete || page.LookupStatus != "not_requested" {
		t.Fatalf("invalid head %+v", page)
	}
	page, err = ServiceHistory(context.Background(), eventAddress(old), false, false, 2, 2)
	if err != nil || len(page.Items) != 2 || page.Items[0].EventID != high.ID.Hex() || page.Items[1].EventID != old.ID.Hex() || page.NextOffset != nil || page.LatestObserved.EventID != bad.ID.Hex() {
		t.Fatalf("second page %+v %v", page, err)
	}
	for _, evt := range []nostr.Event{forged, normalNote} {
		if record, err := a.get(evt.ID); err != nil || record != nil {
			t.Fatalf("archived rejected event: %+v %v", record, err)
		}
	}
	if got := winners[eventAddress(old)]; got.ID != bad.ID {
		t.Fatal("archiving changed latest selection")
	}
}

func TestManifestHistoryReopenAndProvenance(t *testing.T) {
	dir := t.TempDir()
	a, err := openManifestArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	evt := validNapplet(t, nostr.Generate(), "app", "same bytes", 10)
	for i, relay := range []string{"wss://two.example", "wss://one.example", "wss://two.example", "wss://user:secret@private.example", "not a relay"} {
		if err := a.observe(evt, relay, int64(100+i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.db.Close(); err != nil {
		t.Fatal(err)
	}
	a, err = openManifestArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	r, err := a.get(evt.ID)
	if err != nil || r == nil || r.FirstSeen != 100 || r.LastSeen != 104 || !reflect.DeepEqual(r.Relays, []string{"wss://one.example", "wss://two.example"}) {
		t.Fatalf("record %+v %v", r, err)
	}
	if !r.Event.CheckID() || !r.Event.VerifySignature() || r.Event.ID != evt.ID || r.Event.Content != evt.Content {
		t.Fatal("signed event was not preserved")
	}
	rows, total, _, err := a.page(eventAddress(evt), false, 0, 100)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("reopened index %d %+v %v", total, rows, err)
	}
	info, err := os.Stat(filepath.Join(dir, "manifest-history.db"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("archive permissions %v %v", info, err)
	}
}

func TestManifestHistoryBackfillsCacheBeforeReplacement(t *testing.T) {
	dir := t.TempDir()
	store, closeStore, err := eventdb.Open(filepath.Join(dir, "events"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	sk := nostr.Generate()
	old := validNapplet(t, sk, "app", "old", 10)
	newer := validNapplet(t, sk, "app", "new", 20)
	for _, evt := range []nostr.Event{old, newer} {
		if err := store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	a, err := openManifestArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.db.Close()
	if err := a.backfill(store); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceEvent(newer); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteEvent(old.ID); err != nil {
		t.Fatal(err)
	}
	rows, total, _, err := a.page(eventAddress(old), false, 0, 100)
	if err != nil || total != 2 || len(rows) != 2 || rows[1].Event.ID != old.ID {
		t.Fatalf("archive followed cache eviction: %d %+v %v", total, rows, err)
	}
}

func TestManifestHistorySnapshotProvenanceAndIndependentHead(t *testing.T) {
	newUpdateRig(t)
	withManifestHistory(t)
	sk := nostr.Generate()
	mutable := validNapplet(t, sk, "app", "current", 10)
	address := eventAddress(mutable)
	official := historySnapshot(t, sk, address, "release", 50)
	derivative := historySnapshot(t, nostr.Generate(), address, "fork", 60)
	standalone := signedWith(t, sk, KindSnapshotNapplet, nostr.Tags{{"x", testArtifactOf("solo")}}, "solo", 70)
	for _, evt := range []nostr.Event{mutable, official, derivative, standalone} {
		archiveManifest(evt, "")
	}
	installRecord(t, installedFrom(t, mutable))
	page, err := ServiceHistory(context.Background(), address, true, false, 0, 100)
	if err != nil || page.Total != 2 || len(page.Items) != 2 || page.Items[0].Relation != "derivative" || page.Items[1].Relation != "author_snapshot" {
		t.Fatalf("snapshots %+v %v", page, err)
	}
	if page.Items[1].Lineage == nil || page.LatestObserved == nil || page.LatestObserved.EventID != mutable.ID.Hex() || page.SelectedVersion.EventID != mutable.ID.Hex() || !page.SelectedManifestAvailable {
		t.Fatalf("snapshots replaced mutable identity %+v", page)
	}
	versions, err := ServiceHistory(context.Background(), address, false, false, 0, 100)
	if err != nil || versions.Total != 1 || !versions.Items[0].Selected {
		t.Fatalf("mutable history %+v %v", versions, err)
	}
	for _, evt := range []nostr.Event{official, standalone} {
		page, err := ServiceHistory(context.Background(), eventAddress(evt), false, false, 0, 100)
		if err != nil || page.Total != 1 || page.Items[0].EventID != evt.ID.Hex() || page.Items[0].Relation != "snapshot" {
			t.Fatalf("snapshot identity %+v %v", page, err)
		}
	}
}

func TestManifestHistoryRefreshPreservesSelectionAndOfflineHistory(t *testing.T) {
	newUpdateRig(t)
	withManifestHistory(t)
	sk := nostr.Generate()
	old := validNapplet(t, sk, "app", "old", 10)
	newer := validNapplet(t, sk, "app", "new", 20)
	unrelated := validNapplet(t, sk, "other", "other", 30)
	archiveManifest(old, "")
	installRecord(t, installedFrom(t, old))
	prior := fetchHistory
	t.Cleanup(func() { fetchHistory = prior })
	calls := 0
	fetchHistory = func(context.Context, nostr.EntityPointer, bool) historyRefresh {
		calls++
		return historyRefresh{events: []nostr.RelayEvent{{Event: newer}, {Event: unrelated}}, status: "partial"}
	}
	page, err := ServiceHistory(context.Background(), eventAddress(old), false, true, 0, 100)
	if err != nil || calls != 1 || page.Total != 2 || page.SelectedVersion.EventID != old.ID.Hex() || page.LatestObserved.EventID != newer.ID.Hex() || page.LookupStatus != "partial" {
		t.Fatalf("refresh %+v calls=%d %v", page, calls, err)
	}
	if installed, _ := InstalledNapp(eventAddress(old)); installed.EventID != old.ID.Hex() {
		t.Fatal("refresh moved pin")
	}
	page, err = ServiceHistory(context.Background(), eventAddress(old), false, false, 0, 100)
	if err != nil || calls != 1 || page.Total != 2 || page.LookupStatus != "not_requested" {
		t.Fatalf("local history dialed relays: %+v calls=%d %v", page, calls, err)
	}
	for _, status := range []string{"timeout", "unavailable", "answered"} {
		fetchHistory = func(context.Context, nostr.EntityPointer, bool) historyRefresh { return historyRefresh{status: status} }
		page, err = ServiceHistory(context.Background(), eventAddress(old), false, true, 0, 100)
		if err != nil || page.Total != 2 || page.LookupStatus != status || page.HistoryComplete {
			t.Fatalf("empty %s erased history: %+v %v", status, page, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ServiceHistory(ctx, eventAddress(old), false, true, 0, 100); !errors.Is(err, ErrServiceTimeout) {
		t.Fatalf("cancellation %v", err)
	}
}

func TestManifestHistoryMissingSignedInstallRecord(t *testing.T) {
	newUpdateRig(t)
	withManifestHistory(t)
	evt := validNapplet(t, nostr.Generate(), "app", "installed", 10)
	installRecord(t, installedFrom(t, evt))
	page, err := ServiceHistory(context.Background(), eventAddress(evt), false, false, 0, 100)
	if err != nil || page.SelectedVersion == nil || page.SelectedManifestAvailable || page.Total != 0 || page.LatestObserved != nil {
		t.Fatalf("invented signed manifest: %+v %v", page, err)
	}
	wire, _ := json.Marshal(page)
	if !strings.Contains(string(wire), `"items":[]`) || strings.Contains(string(wire), dataDir) || strings.Contains(string(wire), `"event":`) {
		t.Fatalf("unsafe/ambiguous history wire %s", wire)
	}
}

func TestManifestHistoryMiddlewareArchivesBeforeHeadSelection(t *testing.T) {
	newUpdateRig(t)
	withSystem(t)
	sk := nostr.Generate()
	older := validNapplet(t, sk, "app", "older", 10)
	newer := validNapplet(t, sk, "app", "newer", 20)
	relay := &nostr.Relay{URL: "wss://history.example"}
	sys.Pool.EventMiddleware(nostr.RelayEvent{Event: newer, Relay: relay})
	sys.Pool.EventMiddleware(nostr.RelayEvent{Event: older, Relay: relay})
	page, err := ServiceHistory(context.Background(), eventAddress(older), false, false, 0, 100)
	if err != nil || page.Total != 2 || page.Items[1].EventID != older.ID.Hex() {
		t.Fatalf("middleware lost non-head publication: %+v %v", page, err)
	}
	// Exact pin resolution still works after the general cache loses it.
	if err := sys.Store.DeleteEvent(older.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ptr, _ := ParseCanonicalServiceAddress(eventAddress(older))
	if got := fetchSyncedPin(ctx, ptr, older.ID); got == nil || got.ID != older.ID {
		t.Fatal("exact pin did not use archive")
	}
	withCatalog(t)
	detail, err := ServiceDetail(eventAddress(older))
	if err != nil || len(detail.Versions) != 2 {
		t.Fatalf("detail did not use history: %+v %v", detail, err)
	}
}

func TestManifestHistoryRefreshAgainstRelays(t *testing.T) {
	newUpdateRig(t)
	sk := nostr.Generate()
	withSyncSystem(t, sk.Public())
	// Cache an empty author outbox list: this test only talks to local relays.
	sys.RelayListCache.Set(sk.Public(), sdk.GenericList[string, sdk.Relay]{PubKey: sk.Public(), Items: []sdk.Relay{}})
	if cache, ok := sys.RelayListCache.(*cache_memory.RistrettoCache[sdk.GenericList[string, sdk.Relay]]); ok {
		cache.Cache.Wait()
	}
	old := validNapplet(t, sk, "app", "old", 10)
	newer := validNapplet(t, sk, "app", "newer", 20)
	address := eventAddress(old)
	snapshot := historySnapshot(t, sk, address, "release", 30)
	derivative := historySnapshot(t, nostr.Generate(), address, "derivative", 40)
	r1, r2 := fastRelay(t, old, snapshot), fastRelay(t, newer, derivative)
	setRelays := func(urls ...string) {
		stateMu.Lock()
		state.Relays = urls
		stateMu.Unlock()
	}
	setRelays(r1, r2)
	priorLimits := defaultRelayLimits
	defaultRelayLimits = relayLimits{time.Second, 100 * time.Millisecond}
	t.Cleanup(func() { defaultRelayLimits = priorLimits })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err := ServiceHistory(ctx, address, false, true, 0, 100)
	if err != nil || page.LookupStatus != "answered" || page.Total != 2 || page.LatestObserved.EventID != newer.ID.Hex() {
		t.Fatalf("cross-relay versions %+v %v", page, err)
	}
	page, err = ServiceHistory(ctx, address, true, true, 0, 100)
	if err != nil || page.LookupStatus != "answered" || page.Total != 2 || page.Items[0].Relation != "derivative" || page.Items[1].Relation != "author_snapshot" || page.LatestObserved.EventID != newer.ID.Hex() {
		t.Fatalf("lineage refresh %+v %v", page, err)
	}
	setRelays(r1, silentRelay(t))
	page, err = ServiceHistory(ctx, address, false, true, 0, 100)
	if err != nil || page.LookupStatus != "partial" || page.Total != 2 {
		t.Fatalf("partial refresh %+v %v", page, err)
	}
	setRelays(silentRelay(t))
	page, err = ServiceHistory(ctx, address, false, true, 0, 100)
	if err != nil || page.LookupStatus != "timeout" || page.Total != 2 {
		t.Fatalf("silent refresh %+v %v", page, err)
	}
}
