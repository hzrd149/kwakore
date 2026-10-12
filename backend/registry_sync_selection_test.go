package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

func TestSyncExplicitChoicesOrderByRevision(t *testing.T) {
	address := "35129:" + testNappletKey.Public().Hex() + ":app"
	newRelease := syncPin{EventID: hexID('b'), CreatedAt: 200}
	oldChoice := syncPin{EventID: hexID('a'), CreatedAt: 100, Pinned: true, Revision: 1, SelectionID: strings.Repeat("a", 32)}
	newChoice := syncPin{EventID: newRelease.EventID, CreatedAt: 200, Pinned: true, Revision: 2, SelectionID: strings.Repeat("b", 32)}
	rollback := syncPin{EventID: oldChoice.EventID, CreatedAt: 100, Pinned: true, Revision: 3, SelectionID: strings.Repeat("c", 32)}
	for _, pair := range [][2]syncPin{{newRelease, oldChoice}, {oldChoice, newChoice}, {newChoice, rollback}} {
		merged := mergeSyncLists(nil, map[string]syncPin{address: pair[0]}, map[string]syncPin{address: pair[1]})
		if !samePinSelection(merged[address], pair[1]) {
			t.Fatalf("user choice lost to publication age: %+v", merged)
		}
	}
	concurrent := rollback
	concurrent.EventID, concurrent.SelectionID = newChoice.EventID, strings.Repeat("d", 32)
	for _, pair := range [][2]syncPin{{rollback, concurrent}, {concurrent, rollback}} {
		merged := mergeSyncLists(nil, map[string]syncPin{address: pair[0]}, map[string]syncPin{address: pair[1]})
		if !samePinSelection(merged[address], concurrent) {
			t.Fatal("concurrent choices did not converge")
		}
	}
	removed := mergeSyncLists(map[string]syncPin{address: oldChoice}, map[string]syncPin{address: rollback}, nil)
	if len(removed) != 0 {
		t.Fatal("version 2 deletion did not beat a concurrent selection")
	}
}

func TestSyncSelectionEncodingAndValidation(t *testing.T) {
	address := "35129:" + testNappletKey.Public().Hex() + ":app"
	pin := syncPin{EventID: hexID('a'), Pinned: true, Revision: 5, SelectionID: strings.Repeat("b", 32)}
	data, err := encodeSyncList(map[string]syncPin{address: pin})
	if err != nil || !strings.Contains(string(data), `"v":2`) {
		t.Fatalf("not versioned %s %v", data, err)
	}
	decoded, version, err := decodeSyncListVersion(data)
	if err != nil || version != 2 || !samePinSelection(decoded[address], pin) {
		t.Fatalf("round trip %+v v%d %v", decoded, version, err)
	}
	for _, corrupt := range []string{
		strings.Replace(string(data), `"v":2`, `"v":1`, 1),
		strings.Replace(string(data), `"pinned":true`, `"pinned":false`, 1),
		strings.Replace(string(data), `"selection_revision":5`, `"selection_revision":0`, 1),
		strings.Replace(string(data), `"selection_revision":5`, `"selection_revision":9007199254740992`, 1),
		strings.Replace(string(data), strings.Repeat("b", 32), "bad", 1),
	} {
		if _, err := decodeSyncList([]byte(corrupt)); !errors.Is(err, errSyncListInvalid) {
			t.Fatalf("malformed choice accepted %s: %v", corrupt, err)
		}
	}
}

func TestSyncRollbackAndLegacyRegressionAgainstRelay(t *testing.T) {
	_, old, newer := selectionFixture(t)
	withSyncSystem(t, old.PubKey)
	archiveManifest(old, "")
	archiveManifest(newer, "")
	if err := cacheArtifact(installedFrom(t, old).Paths[0].Sha256, []byte("old code")); err != nil {
		t.Fatal(err)
	}
	if err := cacheArtifact(installedFrom(t, newer).Paths[0].Sha256, []byte("new code")); err != nil {
		t.Fatal(err)
	}
	signerKey := nostr.Generate()
	signer := keyer.NewPlainKeySigner(signerKey)
	withUserKeyer(t, signer, signerKey.Public())
	relay := fastRelay(t)
	stubInstallSync(t, "choices", relay)
	fetchSyncedPin = func(_ context.Context, _ nostr.EntityPointer, id nostr.ID) *nostr.Event { return archivedEvent(id) }
	n := selectInstalled(t, newer)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runInstallSync(ctx)
	stateMu.Lock()
	initial := *state.Sync
	stateMu.Unlock()
	if initial.Version != 2 {
		t.Fatal("first explicit selection did not upgrade sync format")
	}
	publish := func(pins map[string]syncPin, version int, at nostr.Timestamp) {
		t.Helper()
		content, err := sealSyncListVersion(ctx, signer, signerKey.Public(), pins, version)
		if err != nil {
			t.Fatal(err)
		}
		evt := signedWith(t, signerKey, installSyncKind, nostr.Tags{{"d", "choices"}}, content, at)
		r, err := sys.Pool.EnsureRelay(relay)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Publish(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}
	rollback := syncPin{EventID: old.ID.Hex(), Pinned: true, Revision: n.SelectionRevision + 1, SelectionID: strings.Repeat("c", 32)}
	publish(map[string]syncPin{n.Address(): rollback}, 2, initial.CreatedAt+10)
	runInstallSync(ctx)
	current, _ := InstalledNapp(n.ID)
	if !samePinSelection(selectionPin(current), rollback) {
		t.Fatalf("remote rollback lost %+v", current)
	}
	// An older client publishes a newer list event but knows no user-choice
	// revision. It must not replace the exact selected code or remove the pin.
	publish(map[string]syncPin{}, 1, initial.CreatedAt+20)
	runInstallSync(ctx)
	current, exists := InstalledNapp(n.ID)
	if !exists || current.EventID != old.ID.Hex() || current.SelectionRevision != rollback.Revision {
		t.Fatalf("legacy list undid explicit pin %+v", current)
	}
	latest := fetchInstallSyncEvent(ctx, signerKey.Public(), "choices")
	if latest == nil {
		t.Fatal("no repaired list")
	}
	list, version, err := openSyncListVersion(ctx, signer, signerKey.Public(), "choices", *latest)
	if err != nil || version != 2 || !samePinSelection(list[n.Address()], rollback) {
		t.Fatalf("legacy regression was not repaired %+v v%d %v", list, version, err)
	}
	// A v2 removal remains authoritative, and the empty list stays v2.
	publish(map[string]syncPin{}, 2, latest.CreatedAt+10)
	runInstallSync(ctx)
	if _, exists := InstalledNapp(n.ID); exists {
		t.Fatal("explicit v2 removal was ignored")
	}
	stateMu.Lock()
	sticky := state.Sync.Version
	stateMu.Unlock()
	if sticky != 2 {
		t.Fatal("empty list regressed its format")
	}
}

func TestSyncMissingExplicitPinNeverFallsBack(t *testing.T) {
	_, old, newer := selectionFixture(t)
	n := selectInstalled(t, newer)
	priorFetch, priorInstall := fetchSyncedPin, syncServiceInstall
	t.Cleanup(func() { fetchSyncedPin, syncServiceInstall = priorFetch, priorInstall })
	fetchSyncedPin = func(context.Context, nostr.EntityPointer, nostr.ID) *nostr.Event { return nil }
	fallbacks := 0
	syncServiceInstall = func(context.Context, string, []string) (ServiceInstallResult, error) {
		fallbacks++
		return ServiceInstallResult{}, nil
	}
	pin := syncPin{EventID: old.ID.Hex(), Pinned: true, Revision: 2, SelectionID: randomID()}
	if err := installSyncSelection(context.Background(), n.Address(), pin); !errors.Is(err, errSyncPinNotFound) || fallbacks != 0 {
		t.Fatalf("explicit pin fell back: %v, %d calls", err, fallbacks)
	}
	current, _ := InstalledNapp(n.ID)
	if current.EventID != newer.ID.Hex() {
		t.Fatal("missing exact pin changed code")
	}
}

func TestSelectionSameEventNewerRemoteChoiceUpdatesMetadataOnly(t *testing.T) {
	_, old, _ := selectionFixture(t)
	n := selectInstalled(t, old)
	files := seedNapplet(t, n, randomID())
	remote := selectionPin(n)
	remote.Revision++
	remote.SelectionID = randomID()
	r, err := selectResolvedContext(context.Background(), installedFrom(t, old), true, &remote, false)
	if err != nil || r.DataReset || r.Outcome != "unchanged" {
		t.Fatalf("metadata choice %+v %v", r, err)
	}
	assertFiles(t, "metadata choice", files.all(), true)
	current, _ := InstalledNapp(n.ID)
	if current.SelectionRevision != remote.Revision || current.SelectionID != remote.SelectionID {
		t.Fatal("same-event revision was lost")
	}
	wire, _ := json.Marshal(serviceDescriptor(current))
	if !strings.Contains(string(wire), `"pinned":true`) {
		t.Fatalf("pin not exposed %s", wire)
	}
}
