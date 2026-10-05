package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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

// ─── applying updates and refusals ───────────────────────────────

// blobRig serves blobs from memory as the first blossom server and counts
// every request it gets.
type blobRig struct {
	url      string
	mu       sync.Mutex
	blobs    map[string][]byte
	requests int
}

func newBlobRig(t *testing.T) *blobRig {
	t.Helper()
	b := &blobRig{blobs: make(map[string][]byte)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.requests++
		data, ok := b.blobs[strings.TrimPrefix(r.URL.Path, "/")]
		b.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	b.url = server.URL
	stateMu.Lock()
	state.BlossomServers = []string{server.URL}
	stateMu.Unlock()
	return b
}

func (b *blobRig) add(data []byte) string {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	b.mu.Lock()
	b.blobs[hash] = data
	b.mu.Unlock()
	return hash
}

func (b *blobRig) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requests
}

// servedNapplet is a valid napplet manifest for d whose index.html is
// document, served by b (its only server tag).
func (b *blobRig) servedNapplet(t *testing.T, sk nostr.SecretKey, d, document string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	index := NappPath{Path: "/index.html", Sha256: b.add([]byte(document))}
	tags := nip5dTags(d, index)
	for _, tag := range tags {
		if tag[0] == "server" {
			tag[1] = b.url
		}
	}
	return signedWith(t, sk, KindNapplet, tags, document, at)
}

// fetchErr is the launcher error, read without building a snapshot (a
// listed entry with no author name would start a profile lookup).
func fetchErr() string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.fetchErr
}

// waitFetchErr waits for the launcher error to become want.
func waitFetchErr(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for fetchErr() != want {
		if time.Now().After(deadline) {
			t.Fatalf("launcher error %q, want %q", fetchErr(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNewerVersionUsesNIP01Winner(t *testing.T) {
	f := newUpdateRig(t)
	sk := nostr.Generate()
	n := installedFrom(t, validNapplet(t, sk, "app", "", 10))
	installRecord(t, n)

	// nothing cached: the lookup's NIP-01 winner, not the first event
	older := validNapplet(t, sk, "app", "old", 5)
	newer := validNapplet(t, sk, "app", "new", 20)
	f.set(older, newer)
	got := newerVersion(n)
	if got == nil || got.EventID != newer.ID.Hex() || got.ID != n.ID {
		t.Fatalf("newerVersion = %+v, want %s under %s", got, newer.ID.Hex(), n.ID)
	}

	// a newer invalid event wins over an older valid one, and gives nothing
	resetUpdateSet()
	f.set(invalidNapplet(t, sk, "app", 20), validNapplet(t, sk, "app", "mid", 15))
	if got := newerVersion(n); got != nil {
		t.Fatalf("an invalid latest version offered %+v", got)
	}
	entry, ok := updateSet.Load().Load(n.ID)
	if !ok || entry.Unavailable == "" {
		t.Fatalf("the update set does not hold the unavailable entry: %v %+v", ok, entry)
	}
	if got := installedSnapshot(t, n.ID); got.Unavailable == "" || got.UpdateAvailable != nil {
		t.Errorf("snapshot: unavailable %q, update %v", got.Unavailable, got.UpdateAvailable)
	}
}

func TestNoUpdateFromInvalidLatest(t *testing.T) {
	f := newUpdateRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	installedEvt := blobs.servedNapplet(t, sk, "app", "v1", 10)
	n := installedFrom(t, installedEvt)
	installRecord(t, n)

	broken := invalidNapplet(t, sk, "app", 20)
	f.set(installedEvt, broken)
	Update(n.ID)
	if got := fetchErr(); got != "no update found for "+n.Label() {
		t.Errorf("launcher error %q", got)
	}
	if blobs.count() != 0 {
		t.Errorf("an invalid latest version made %d blob requests", blobs.count())
	}
	if rec, _ := InstalledNapp(n.ID); rec.EventID != n.EventID {
		t.Errorf("the installed record changed to %s", rec.EventID)
	}

	// applyUpdate itself refuses an unavailable entry
	SetFetchErr("")
	applyUpdate(n, nappFromLatest(broken))
	if got := fetchErr(); got != "update failed: the latest version is invalid" {
		t.Errorf("applyUpdate launcher error %q", got)
	}
	if blobs.count() != 0 {
		t.Errorf("applyUpdate made %d blob requests", blobs.count())
	}

	// a cold Update installs exactly the NIP-01 winner of a same-second pair
	resetUpdateSet()
	SetFetchErr("")
	a := blobs.servedNapplet(t, sk, "app", "v2 a", 30)
	b := blobs.servedNapplet(t, sk, "app", "v2 b", 30)
	winner := a
	if bytes.Compare(b.ID[:], a.ID[:]) < 0 {
		winner = b
	}
	f.set(installedEvt, a, b)
	Update(n.ID)
	if got := fetchErr(); got != "" {
		t.Fatalf("update failed: %s", got)
	}
	rec, _ := InstalledNapp(n.ID)
	if rec.EventID != winner.ID.Hex() || rec.UpdateAvailable != nil || rec.Unavailable != "" {
		t.Fatalf("installed %s (update %v, unavailable %q), want the winner %s", rec.EventID, rec.UpdateAvailable, rec.Unavailable, winner.ID.Hex())
	}
	if doc, err := nappletDocument(rec); err != nil || string(doc) != winner.Content {
		t.Errorf("installed document %q, %v, want %q", doc, err, winner.Content)
	}
	if _, ok := updateSet.Load().Load(n.ID); ok {
		t.Error("the applied update is still on offer")
	}
}

func TestUnavailableCannotInstallOrTry(t *testing.T) {
	newUpdateRig(t)
	resetResolved(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	un := nappFromLatest(invalidNapplet(t, sk, "app", 20))
	if un.Unavailable == "" {
		t.Fatal("fixture should be unavailable")
	}
	const installRefused = "install failed: the latest version is invalid"
	const tryRefused = "try failed: the latest version is invalid"
	nothingHappened := func(step string) {
		t.Helper()
		if _, ok := InstalledNapp(un.ID); ok {
			t.Errorf("%s: the unavailable entry was installed", step)
		}
		if open := runningForNapp(un.ID); len(open) != 0 {
			t.Errorf("%s: %d windows opened", step, len(open))
		}
		if blobs.count() != 0 {
			t.Errorf("%s: %d blob requests", step, blobs.count())
		}
		if entries, _ := os.ReadDir(filepath.Join(dataDir, "napps")); len(entries) != 0 {
			t.Errorf("%s: %d entries under napps/", step, len(entries))
		}
	}

	Install(un)
	if got := fetchErr(); got != installRefused {
		t.Errorf("Install: launcher error %q", got)
	}
	nothingHappened("Install")

	rememberResolved(un)
	SetFetchErr("")
	if !InstallFromDiscovery(un.ID) {
		t.Fatal("InstallFromDiscovery did not find the listed entry")
	}
	waitFetchErr(t, installRefused)
	nothingHappened("InstallFromDiscovery")

	SetFetchErr("")
	TryNapplet(un)
	if got := fetchErr(); got != tryRefused {
		t.Errorf("TryNapplet: launcher error %q", got)
	}
	nothingHappened("TryNapplet")
	if err := tryNapplet(context.Background(), un); !errors.Is(err, errUnavailable) {
		t.Errorf("tryNapplet: %v", err)
	}

	for _, arg := range []string{un.ID, LaunchToken(un.ID)} {
		SetFetchErr("")
		if !TryNappletFromDiscovery(arg) {
			t.Fatalf("TryNappletFromDiscovery(%q) did not find the listed entry", arg)
		}
		if got := fetchErr(); got != tryRefused {
			t.Errorf("TryNappletFromDiscovery(%q): launcher error %q", arg, got)
		}
		nothingHappened("TryNappletFromDiscovery")
	}

	// a valid discovery entry installs with its event id, and the saved
	// record carries neither stamp
	valid := nappFromLatest(blobs.servedNapplet(t, sk, "other", "doc", 30))
	valid.UpdateAvailable = &Napp{ID: valid.ID, CreatedAt: 99}
	if err := InstallNapp(valid); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var saved AppState
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	rec, ok := saved.InstalledNapps[valid.ID]
	if !ok || rec.EventID != valid.EventID || rec.UpdateAvailable != nil || rec.Unavailable != "" {
		t.Fatalf("saved record: %v %+v", ok, rec)
	}
}
