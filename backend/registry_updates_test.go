package backend

import (
	"context"
	"slices"
	"sync"
	"testing"

	"fiatjaf.com/nostr"
	"github.com/puzpuzpuz/xsync/v3"
)

// ─── test rig ────────────────────────────────────────────────────

// fakeManifests stands in for fetchManifestEvents: it hands every lookup
// the same events, whatever napps it asked about (selection sorts out which
// address each belongs to), and counts the lookups.
type fakeManifests struct {
	mu     sync.Mutex
	events []nostr.Event
	calls  int
}

func (f *fakeManifests) set(events ...nostr.Event) {
	f.mu.Lock()
	f.events = events
	f.mu.Unlock()
}

func (f *fakeManifests) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeManifests) fetch(ctx context.Context, napps []Napp) []nostr.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return slices.Clone(f.events)
}

// newUpdateRig isolates the launcher state, empties the update set and the
// launcher error, and swaps the relay lookup for a fake one.
func newUpdateRig(t *testing.T) *fakeManifests {
	t.Helper()
	setupNapTest(t)
	isolateState(t)
	f := &fakeManifests{}
	prev := fetchManifestEvents
	fetchManifestEvents = f.fetch
	resetUpdateSet()
	SetFetchErr("")
	t.Cleanup(func() {
		// a launch-time check may still be running: it reads the seam
		backgroundSyncs.Wait()
		fetchManifestEvents = prev
		resetUpdateSet()
		SetFetchErr("")
		ls.mu.Lock()
		ls.installed = nil
		ls.mu.Unlock()
	})
	return f
}

func resetUpdateSet() {
	updateSetMu.Lock()
	updateSet.Store(xsync.NewMapOf[string, Napp]())
	updateSetMu.Unlock()
}

// installRecord records n as installed (no files) and republishes the list.
// It carries an author name, so Snapshot never starts a profile lookup.
func installRecord(t *testing.T, n Napp) {
	t.Helper()
	n.AuthorName = "tester"
	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	state.InstalledNapps[n.ID] = n
	stateMu.Unlock()
	refreshInstalled()
}

// installedSnapshot is n's entry in Snapshot().Installed.
func installedSnapshot(t *testing.T, id string) Napp {
	t.Helper()
	for _, n := range Snapshot().Installed {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("%s is not in the installed snapshot", id)
	return Napp{}
}

// validNapplet is a signed, valid NIP-5D napplet manifest for d.
func validNapplet(t *testing.T, sk nostr.SecretKey, d, content string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	index := NappPath{Path: "/index.html", Sha256: testArtifactOf(d + content)}
	return signedWith(t, sk, KindNapplet, nip5dTags(d, index), content, at)
}

// invalidNapplet is a signed napplet event for d with no path tags (and no
// x, server or content): no valid manifest in either schema.
func invalidNapplet(t *testing.T, sk nostr.SecretKey, d string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	return signedWith(t, sk, KindNapplet, nostr.Tags{{"d", d}, {"title", "Broken"}}, "", at)
}

// installedFrom is the record an install of evt saves.
func installedFrom(t *testing.T, evt nostr.Event) Napp {
	t.Helper()
	n := nappFromLatest(evt)
	if n.Unavailable != "" {
		t.Fatalf("fixture is unavailable: %s", n.Unavailable)
	}
	return n
}

// ─── selection ───────────────────────────────────────────────────

func TestUpdateCheckMarksInvalidLatestUnavailable(t *testing.T) {
	f := newUpdateRig(t)
	sk := nostr.Generate()
	n := installedFrom(t, validNapplet(t, sk, "app", "", 10))
	installRecord(t, n)

	broken := invalidNapplet(t, sk, "app", 20)
	states := updateStates([]Napp{n}, []nostr.Event{broken})
	entry := states[n.ID]
	if entry == nil || entry.Unavailable != reasonRequiredTags {
		t.Fatalf("entry for a newer invalid event: %+v", entry)
	}
	if entry.ID != n.ID || entry.EventID != broken.ID.Hex() {
		t.Errorf("entry identity: id %q event %q", entry.ID, entry.EventID)
	}

	// the full check shows it on the installed record, with no update
	var checking []bool
	f.set(broken)
	prev := fetchManifestEvents
	fetchManifestEvents = func(ctx context.Context, napps []Napp) []nostr.Event {
		checking = append(checking, updateChecking.Load())
		return prev(ctx, napps)
	}
	CheckForUpdates()
	fetchManifestEvents = prev
	if !slices.Equal(checking, []bool{true}) {
		t.Errorf("UpdateCheckRunning during the lookup: %v, want [true]", checking)
	}
	if updateChecking.Load() {
		t.Error("UpdateCheckRunning still set after the check")
	}
	got := installedSnapshot(t, n.ID)
	if got.Unavailable != reasonRequiredTags || got.UpdateAvailable != nil {
		t.Fatalf("snapshot: unavailable %q, update %v", got.Unavailable, got.UpdateAvailable)
	}
	// the installed record itself is untouched
	if rec, _ := InstalledNapp(n.ID); rec.Unavailable != "" || rec.EventID != n.EventID {
		t.Errorf("installed record changed: %+v", rec)
	}

	// a valid newer version replaces the unavailable state with an update
	newer := validNapplet(t, sk, "app", "v2", 20)
	f.set(newer)
	CheckForUpdates()
	got = installedSnapshot(t, n.ID)
	if got.Unavailable != "" || got.UpdateAvailable == nil || got.UpdateAvailable.EventID != newer.ID.Hex() {
		t.Fatalf("snapshot after a valid newer version: unavailable %q, update %+v", got.Unavailable, got.UpdateAvailable)
	}
	if got.UpdateAvailable.ID != n.ID {
		t.Errorf("update carries id %q, want %q", got.UpdateAvailable.ID, n.ID)
	}
}

func TestUpdateCheckOffersOnlyNIP01Newer(t *testing.T) {
	newUpdateRig(t)
	sk := nostr.Generate()
	low, high := tiedPair(t, sk)

	cases := []struct {
		name      string
		installed nostr.Event
		events    []nostr.Event
		offer     *nostr.Event
	}{
		{"same second, lower id", high, []nostr.Event{low}, &low},
		{"same second, higher id", low, []nostr.Event{high}, nil},
		{"older", high, []nostr.Event{validNapplet(t, sk, "app", "old", 50)}, nil},
		{"the installed one", high, []nostr.Event{high}, nil},
	}
	for _, c := range cases {
		n := installedFrom(t, c.installed)
		entry, found := updateStates([]Napp{n}, c.events)[n.ID]
		if !found {
			t.Errorf("%s: the address's events were not considered", c.name)
			continue
		}
		switch {
		case c.offer == nil && entry != nil:
			t.Errorf("%s: offered %s", c.name, entry.EventID)
		case c.offer != nil && (entry == nil || entry.EventID != c.offer.ID.Hex() || entry.Unavailable != ""):
			t.Errorf("%s: offered %+v, want %s", c.name, entry, c.offer.ID.Hex())
		}
	}

	// another author's napplet under the same d and the same author's napp
	// under the same d are other addresses: nothing is found for n
	n := installedFrom(t, low)
	other := validNapplet(t, nostr.Generate(), "app", "", 500)
	napp := testNappEvent(sk, "app", "Napp", 500).Event
	if states := updateStates([]Napp{n}, []nostr.Event{other, napp}); len(states) != 0 {
		t.Errorf("events of other addresses produced %+v", states)
	}

	// an older event clears a stale entry; nothing found keeps it
	installRecord(t, n)
	mergeUpdateState(n.ID, &Napp{ID: n.ID, CreatedAt: 999, EventID: "ff"})
	f := &fakeManifests{}
	prev := fetchManifestEvents
	fetchManifestEvents = f.fetch
	defer func() { fetchManifestEvents = prev }()
	CheckForUpdates()
	if got := installedSnapshot(t, n.ID); got.UpdateAvailable == nil {
		t.Error("a check that found nothing dropped the previous update")
	}
	f.set(low)
	CheckForUpdates()
	if got := installedSnapshot(t, n.ID); got.UpdateAvailable != nil || got.Unavailable != "" {
		t.Errorf("the installed version is the latest, but the snapshot says %+v / %q", got.UpdateAvailable, got.Unavailable)
	}
}
