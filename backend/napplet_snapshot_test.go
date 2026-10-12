package backend

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"fiatjaf.com/nostr"

	"kwakore/backend/desktopentry"
)

// ─── kind 5129 snapshots ─────────────────────────────────────────

// snapshotTags is a minimal current-shape snapshot manifest.
func snapshotTags(extra ...nostr.Tag) nostr.Tags {
	return append(nostr.Tags{{"x", testArtifact}, {"title", "Snap"}}, extra...)
}

// servedSnapshot is a valid kind 5129 manifest whose index.html is document,
// served by b.
func (b *blobRig) servedSnapshot(t *testing.T, sk nostr.SecretKey, document string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	tags := nostr.Tags{{"x", b.add([]byte(document))}, {"server", b.url}, {"title", "Snap"}}
	return signedWith(t, sk, KindSnapshotNapplet, tags, document, at)
}

func TestSnapshotIdentity(t *testing.T) {
	evt := signedEvent(t, KindSnapshotNapplet, snapshotTags(), "a snapshot")
	n, err := nappletFromEvent(evt)
	if err != nil {
		t.Fatalf("snapshot rejected: %v", err)
	}
	want := "5129:" + evt.PubKey.Hex() + ":" + evt.ID.Hex()
	if n.D != "" || n.EventID != evt.ID.Hex() || !n.IsSnapshot() {
		t.Errorf("identity fields: d %q event %q snapshot %v", n.D, n.EventID, n.IsSnapshot())
	}
	if n.ID != want || n.Address() != want || eventAddress(evt) != want {
		t.Errorf("id %q address %q event address %q, want %q", n.ID, n.Address(), eventAddress(evt), want)
	}
	latest := nappFromLatest(evt)
	if latest.Unavailable != "" || latest.ID != want || latest.Address() != want {
		t.Errorf("nappFromLatest: %+v", latest)
	}

	ptr, err := ParseCanonicalServiceAddress(want)
	if err != nil {
		t.Fatalf("canonical snapshot address refused: %v", err)
	}
	if !n.matchesAddress(ptr) {
		t.Error("does not match its own address")
	}
	other := ptr
	other.Identifier = strings.Repeat("0", 64)
	if n.matchesAddress(other) {
		t.Error("matches another event id")
	}
	if code := n.Naddr(); !strings.HasPrefix(code, "nevent1") {
		t.Errorf("share code %q", code)
	}

	// a legacy-shaped snapshot is named the same way
	legacy := signedEvent(t, KindSnapshotNapplet, nostr.Tags{{"path", "/index.html", testArtifact}}, "")
	ln, err := nappletFromEvent(legacy)
	if err != nil || ln.Address() != "5129:"+legacy.PubKey.Hex()+":"+legacy.ID.Hex() || ln.ID != ln.Address() {
		t.Errorf("legacy snapshot: %q %v", ln.Address(), err)
	}
}

func TestSnapshotLineage(t *testing.T) {
	pk := nostr.Generate().Public().Hex()
	pk2 := nostr.Generate().Public().Hex()
	parent := "35129:" + pk2 + ":parent"
	root := "15129:" + pk + ":"

	n, err := nappletFromEvent(signedEvent(t, KindSnapshotNapplet,
		snapshotTags(nostr.Tag{"a", parent}, nostr.Tag{"A", root}), "desc"))
	if err != nil || n.Parent != parent || n.Root != root {
		t.Fatalf("lineage: parent %q root %q err %v", n.Parent, n.Root, err)
	}

	for name, tags := range map[string]nostr.Tags{
		"malformed":    snapshotTags(nostr.Tag{"a", "not an address"}, nostr.Tag{"A", root}),
		"other kind":   snapshotTags(nostr.Tag{"a", "30023:" + pk + ":post"}, nostr.Tag{"A", root}),
		"control rune": snapshotTags(nostr.Tag{"a", "35129:" + pk + ":bad\nname"}, nostr.Tag{"A", root}),
		"two parents":  snapshotTags(nostr.Tag{"a", parent}, nostr.Tag{"a", "35129:" + pk + ":other"}, nostr.Tag{"A", root}),
	} {
		n, err := nappletFromEvent(signedEvent(t, KindSnapshotNapplet, tags, "desc"))
		if err != nil {
			t.Errorf("%s: lineage rejected the event: %v", name, err)
			continue
		}
		if n.Parent != "" || n.Root != root {
			t.Errorf("%s: parent %q root %q", name, n.Parent, n.Root)
		}
	}

	// lineage means nothing outside a snapshot
	named, err := nappletFromEvent(signedEvent(t, KindNapplet,
		nostr.Tags{{"d", "app"}, {"x", testArtifact}, {"a", parent}, {"A", root}}, "desc"))
	if err != nil || named.Parent != "" || named.Root != "" {
		t.Errorf("named napplet lineage: %q %q %v", named.Parent, named.Root, err)
	}
	rootNapplet, err := nappletFromEvent(signedEvent(t, KindRootNapplet,
		nostr.Tags{{"x", testArtifact}, {"a", parent}, {"A", root}}, "desc"))
	if err != nil || rootNapplet.Parent != "" || rootNapplet.Root != "" {
		t.Errorf("root napplet lineage: %q %q %v", rootNapplet.Parent, rootNapplet.Root, err)
	}
}

func TestParseCanonicalSnapshotAddress(t *testing.T) {
	pk := nostr.Generate().Public().Hex()
	id := strings.Repeat("ab", 32)
	valid := "5129:" + pk + ":" + id
	if _, err := ParseCanonicalServiceAddress(valid); err != nil {
		t.Errorf("service refused %q: %v", valid, err)
	}
	if err := desktopentry.CanonicalAddress(valid); err != nil {
		t.Errorf("desktopentry refused %q: %v", valid, err)
	}
	for _, bad := range []string{
		"5129:" + pk + ":",
		"5129:" + pk + ":" + id[:63],
		"5129:" + pk + ":" + strings.ToUpper(id),
		"5129:" + pk + ":" + id[:63] + "g",
		"05129:" + pk + ":" + id,
	} {
		if _, err := ParseCanonicalServiceAddress(bad); err == nil {
			t.Errorf("service accepted %q", bad)
		}
		if err := desktopentry.CanonicalAddress(bad); err == nil {
			t.Errorf("desktopentry accepted %q", bad)
		}
	}
}

func TestSnapshotResolveAndInstall(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	snap := blobs.servedSnapshot(t, sk, "first", 10)
	other := blobs.servedSnapshot(t, sk, "second", 20)
	address := eventAddress(snap)

	oldEvents := addressEvents
	addressEvents = func(context.Context, nostr.EntityPointer) ([]nostr.Event, error) {
		return []nostr.Event{other, snap}, nil
	}
	t.Cleanup(func() { addressEvents = oldEvents })
	serviceCalls := 0
	oldService := fetchServiceManifestEvents
	fetchServiceManifestEvents = func(context.Context, Napp) ([]nostr.Event, bool) {
		serviceCalls++
		return []nostr.Event{other}, true
	}
	t.Cleanup(func() { fetchServiceManifestEvents = oldService })
	manifestCalls := 0
	oldManifests := fetchManifestEvents
	fetchManifestEvents = func(context.Context, []Napp) []nostr.Event {
		manifestCalls++
		return []nostr.Event{other}
	}
	t.Cleanup(func() { fetchManifestEvents = oldManifests })

	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil {
		t.Fatal(err)
	}
	f := addressFilter(ptr)
	if len(f.IDs) != 1 || f.IDs[0] != snap.ID || !slices.Equal(f.Kinds, []nostr.Kind{KindSnapshotNapplet}) ||
		!slices.Equal(f.Authors, []nostr.PubKey{sk.Public()}) || f.Tags != nil {
		t.Errorf("address filter: %+v", f)
	}

	result, err := ServiceInstall(t.Context(), address, nil)
	if err != nil || result.Address != address || result.Outcome != "installed" ||
		result.InstalledVersion.EventID != snap.ID.Hex() {
		t.Fatalf("install: %+v %v", result, err)
	}
	backgroundSyncs.Wait()
	installed := ServiceInstalled(0, 100)
	if len(installed.Items) != 1 || installed.Items[0].Address != address {
		t.Fatalf("installed: %+v", installed.Items)
	}
	detail, err := ServiceDetail(address)
	if err != nil || !detail.Installed || detail.Address != address ||
		detail.InstalledVersion == nil || detail.InstalledVersion.EventID != snap.ID.Hex() {
		t.Fatalf("detail: %+v %v", detail, err)
	}

	// immutable: never looked up, never offered an update
	if _, err := ServiceUpdate(t.Context(), address); !errors.Is(err, ErrServiceNoUpdate) {
		t.Errorf("update: %v", err)
	}
	check, err := ServiceCheckUpdates(t.Context(), address)
	if err != nil || len(check.Items) != 0 || check.Items == nil || !check.Complete {
		t.Errorf("update check: %+v %v", check, err)
	}
	if serviceCalls != 0 {
		t.Errorf("snapshot looked up %d times", serviceCalls)
	}
	record := installedFrom(t, snap)
	if states := updateStates([]Napp{record}, []nostr.Event{other, snap}); len(states) != 0 {
		t.Errorf("update states: %+v", states)
	}
	CheckForUpdates()
	if manifestCalls != 0 {
		t.Errorf("manifest lookups: %d", manifestCalls)
	}

	// carried by the installed-list sync like any other address
	if !syncableAddress(address) {
		t.Error("snapshot address is not syncable")
	}
	if _, err := resolveSyncedPin(t.Context(), address, other.ID.Hex()); !errors.Is(err, errSyncPinMismatch) {
		t.Errorf("pin with another event id: %v", err)
	}
}
