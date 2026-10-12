package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

// ─── test rig ────────────────────────────────────────────────────

// manifestLookups stands in for the relays the service update check asks:
// per address, the events a lookup returns and whether a relay answered.
type manifestLookups struct {
	mu       sync.Mutex
	events   map[string][]nostr.Event
	silent   map[string]bool
	calls    map[string]int
	answered bool // the default for an address without an entry
}

func stubServiceManifests(t *testing.T) *manifestLookups {
	t.Helper()
	m := &manifestLookups{events: map[string][]nostr.Event{}, silent: map[string]bool{}, calls: map[string]int{}, answered: true}
	old := fetchServiceManifestEvents
	fetchServiceManifestEvents = func(_ context.Context, n Napp) ([]nostr.Event, bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		a := n.Address()
		m.calls[a]++
		if m.silent[a] {
			return nil, false
		}
		return append([]nostr.Event(nil), m.events[a]...), m.answered
	}
	t.Cleanup(func() { fetchServiceManifestEvents = old })
	return m
}

func (m *manifestLookups) set(address string, events ...nostr.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[address] = events
}

func (m *manifestLookups) count(address string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[address]
}

func (m *manifestLookups) total() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		n += c
	}
	return n
}

func installedItem(t *testing.T, address string) ServiceInstalledItem {
	t.Helper()
	for _, item := range ServiceInstalled(0, 500).Items {
		if item.Address == address {
			return item
		}
	}
	t.Fatalf("%s is not installed", address)
	return ServiceInstalledItem{}
}

func hasUpdateEntry(id string) bool {
	_, ok := updateSet.Load().Load(id)
	return ok
}

// ─── the check ───────────────────────────────────────────────────

func TestServiceCheckUpdatesFindsNewerVersion(t *testing.T) {
	newReclaimRig(t)
	sk := nostr.Generate()
	A1, A2 := validNapplet(t, sk, "a", "v1", 10), validNapplet(t, sk, "a", "v2", 20)
	B1 := validNapplet(t, sk, "b", "", 10)
	a, b := installedFrom(t, A1), installedFrom(t, B1)
	installRecord(t, a)
	installRecord(t, b)
	lookups := stubServiceManifests(t)
	lookups.set(a.Address(), A1, A2)
	lookups.set(b.Address(), B1)

	result, err := ServiceCheckUpdates(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Items) != 1 {
		t.Fatalf("result %+v", result)
	}
	item := result.Items[0]
	if item.Address != a.Address() || item.InstalledVersion.EventID != A1.ID.Hex() || item.AvailableVersion.EventID != A2.ID.Hex() || item.AvailableVersion.CreatedAt != 20 {
		t.Fatalf("item %+v", item)
	}

	// napplet.installed shows the update on A and null on B
	if got := installedItem(t, a.Address()).UpdateAvailable; got == nil || got.EventID != A2.ID.Hex() {
		t.Fatalf("A update_available = %+v", got)
	}
	bItem := installedItem(t, b.Address())
	if bItem.UpdateAvailable != nil {
		t.Fatalf("B update_available = %+v", bItem.UpdateAvailable)
	}
	wire, err := json.Marshal(bItem)
	if err != nil || !strings.Contains(string(wire), `"update_available":null`) {
		t.Fatalf("B on the wire: %s %v", wire, err)
	}
	// nothing was installed
	if rec, ok := InstalledNapp(a.ID); !ok || rec.EventID != A1.ID.Hex() {
		t.Fatalf("the check changed the installed record: %+v", rec)
	}
}

func TestServiceCheckUpdatesIgnoresOlderSameAndInvalid(t *testing.T) {
	newReclaimRig(t)
	sk := nostr.Generate()
	A0, A1 := validNapplet(t, sk, "a", "v0", 5), validNapplet(t, sk, "a", "v1", 10)
	a := installedFrom(t, A1)
	installRecord(t, a)
	lookups := stubServiceManifests(t)
	before := installedItem(t, a.Address())
	for name, events := range map[string][]nostr.Event{
		"older":   {A0},
		"same":    {A1},
		"invalid": {A1, invalidNapplet(t, sk, "a", 30)},
	} {
		lookups.set(a.Address(), events...)
		result, err := ServiceCheckUpdates(t.Context(), "")
		if err != nil || !result.Complete || len(result.Items) != 0 || result.Items == nil {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
		after := installedItem(t, a.Address())
		if after.UpdateAvailable != nil || after.Available != before.Available {
			t.Fatalf("%s: item %+v, was %+v", name, after, before)
		}
	}
}

func TestServiceCheckUpdatesPartialAndUnanswered(t *testing.T) {
	newReclaimRig(t)
	sk := nostr.Generate()
	A1, A2 := validNapplet(t, sk, "a", "v1", 10), validNapplet(t, sk, "a", "v2", 20)
	B1, B2 := validNapplet(t, sk, "b", "v1", 10), validNapplet(t, sk, "b", "v2", 20)
	a, b := installedFrom(t, A1), installedFrom(t, B1)
	installRecord(t, a)
	installRecord(t, b)
	// B has an entry from an earlier check
	b2 := installedFrom(t, B2)
	mergeUpdateState(b.ID, updateState(b, b2))
	lookups := stubServiceManifests(t)
	lookups.set(a.Address(), A2)
	lookups.silent[b.Address()] = true

	result, err := ServiceCheckUpdates(t.Context(), "")
	if err != nil || result.Complete {
		t.Fatalf("partial: %+v %v", result, err)
	}
	found := map[string]bool{}
	for _, item := range result.Items {
		found[item.Address] = true
	}
	if !found[a.Address()] || !hasUpdateEntry(b.ID) {
		t.Fatalf("partial result %+v, B entry kept %v", result, hasUpdateEntry(b.ID))
	}

	// nobody answers: Unavailable, and the cache stays as it was
	lookups.silent[a.Address()] = true
	if _, err := ServiceCheckUpdates(t.Context(), ""); !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("no answer: %v", err)
	}
	if !hasUpdateEntry(a.ID) || !hasUpdateEntry(b.ID) {
		t.Fatal("an unanswered check changed the cache")
	}
}

func TestServiceCheckUpdatesEmptyAddressAndCancel(t *testing.T) {
	newReclaimRig(t)
	lookups := stubServiceManifests(t)
	result, err := ServiceCheckUpdates(t.Context(), "")
	if err != nil || !result.Complete || result.Items == nil {
		t.Fatalf("empty install: %+v %v", result, err)
	}
	if wire, _ := json.Marshal(result); string(wire) != `{"items":[],"complete":true}` {
		t.Fatalf("empty install on the wire: %s", wire)
	}

	sk := nostr.Generate()
	A1, B1 := validNapplet(t, sk, "a", "", 10), validNapplet(t, sk, "b", "", 10)
	a, b := installedFrom(t, A1), installedFrom(t, B1)
	installRecord(t, a)
	installRecord(t, b)
	lookups.set(a.Address(), A1)
	lookups.set(b.Address(), B1)

	// one address: only that napplet is asked about
	if _, err := ServiceCheckUpdates(t.Context(), a.Address()); err != nil {
		t.Fatal(err)
	}
	if lookups.count(a.Address()) != 1 || lookups.count(b.Address()) != 0 {
		t.Fatalf("lookups a=%d b=%d", lookups.count(a.Address()), lookups.count(b.Address()))
	}
	if _, err := ServiceCheckUpdates(t.Context(), "35129:"+nostr.Generate().Public().Hex()+":missing"); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("unknown address: %v", err)
	}
	if _, err := ServiceCheckUpdates(t.Context(), "not-an-address"); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("noncanonical address: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := lookups.total()
	if _, err := ServiceCheckUpdates(ctx, ""); !errors.Is(err, ErrServiceTimeout) {
		t.Fatalf("cancelled: %v", err)
	}
	if lookups.total() != calls {
		t.Fatal("a cancelled check asked the relays")
	}
}

func TestServiceCheckUpdatesBypassesTheCache(t *testing.T) {
	newReclaimRig(t)
	sk := nostr.Generate()
	A1, A2 := validNapplet(t, sk, "a", "v1", 10), validNapplet(t, sk, "a", "v2", 20)
	a := installedFrom(t, A1)
	installRecord(t, a)
	// a cached valid entry, which the relays no longer confirm
	mergeUpdateState(a.ID, updateState(a, installedFrom(t, A2)))
	lookups := stubServiceManifests(t)
	lookups.set(a.Address(), A1)
	result, err := ServiceCheckUpdates(t.Context(), "")
	if err != nil || len(result.Items) != 0 {
		t.Fatalf("result %+v %v", result, err)
	}
	if lookups.count(a.Address()) != 1 || hasUpdateEntry(a.ID) {
		t.Fatal("the cached entry was trusted")
	}
}

// ─── the background check ────────────────────────────────────────

func withServiceUpdateSchedule(t *testing.T, online bool, check func(context.Context) error) {
	t.Helper()
	oldDelay, oldEvery, oldCheck, oldOnline := serviceUpdateCheckDelay, serviceUpdateCheckEvery, serviceUpdateCheck, updateCheckOnline
	serviceUpdateCheckDelay, serviceUpdateCheckEvery, serviceUpdateCheck = 5*time.Millisecond, 5*time.Millisecond, check
	updateCheckOnline = func() bool { return online }
	t.Cleanup(func() {
		serviceUpdateCheckDelay, serviceUpdateCheckEvery, serviceUpdateCheck, updateCheckOnline = oldDelay, oldEvery, oldCheck, oldOnline
	})
}

func TestStartServiceUpdateChecksRunsAndStops(t *testing.T) {
	var runs atomic.Int32
	withServiceUpdateSchedule(t, true, func(context.Context) error {
		runs.Add(1)
		return ErrServiceUnavailable
	})
	ctx, cancel := context.WithCancel(context.Background())
	wait := StartServiceUpdateChecks(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() < 2 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("ran %d times", runs.Load())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wait did not return after cancel")
	}
}

func TestStartServiceUpdateChecksOffline(t *testing.T) {
	var runs atomic.Int32
	withServiceUpdateSchedule(t, false, func(context.Context) error {
		runs.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	wait := StartServiceUpdateChecks(ctx)
	time.Sleep(60 * time.Millisecond)
	cancel()
	wait()
	if n := runs.Load(); n != 0 {
		t.Fatalf("an offline service checked %d times", n)
	}
}
