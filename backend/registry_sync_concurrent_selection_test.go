package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

func TestSyncPreservesUserChoiceMadeDuringPass(t *testing.T) {
	_, old, newer := selectionFixture(t)
	withSyncSystem(t, old.PubKey)
	archiveManifest(old, "")
	archiveManifest(newer, "")
	for _, evt := range []nostr.Event{old, newer} {
		if err := cacheArtifact(installedFrom(t, evt).Paths[0].Sha256, []byte(evt.Content)); err != nil {
			t.Fatal(err)
		}
	}
	sk := nostr.Generate()
	signer := keyer.NewPlainKeySigner(sk)
	withUserKeyer(t, signer, sk.Public())
	relay := fastRelay(t)
	stubInstallSync(t, "concurrent-choice", relay)
	fetchSyncedPin = func(_ context.Context, _ nostr.EntityPointer, id nostr.ID) *nostr.Event { return archivedEvent(id) }
	n := selectInstalled(t, newer)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runInstallSync(ctx)
	stateMu.Lock()
	at := state.Sync.CreatedAt + 10
	stateMu.Unlock()
	remote := syncPin{EventID: old.ID.Hex(), Pinned: true, Revision: 2, SelectionID: strings.Repeat("b", 32)}
	content, err := sealSyncList(ctx, signer, sk.Public(), map[string]syncPin{n.Address(): remote})
	if err != nil {
		t.Fatal(err)
	}
	evt := signedWith(t, sk, installSyncKind, nostr.Tags{{"d", "concurrent-choice"}}, content, at)
	r, err := sys.Pool.EnsureRelay(relay)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Publish(ctx, evt); err != nil {
		t.Fatal(err)
	}
	apply := installSyncSelection
	installSyncSelection = func(ctx context.Context, address string, pin syncPin) error {
		if err := apply(ctx, address, pin); err != nil {
			return err
		}
		// The user now explicitly chooses the newer code while this pass
		// still holds the snapshot of the remote rollback target.
		_, err := ServiceSelect(ctx, address, newer.ID.Hex())
		return err
	}
	runInstallSync(ctx)
	current, _ := InstalledNapp(n.ID)
	if current.EventID != newer.ID.Hex() || current.SelectionRevision != 3 {
		t.Fatalf("user selection was lost %+v", current)
	}
	latest := fetchInstallSyncEvent(ctx, sk.Public(), "concurrent-choice")
	if latest == nil {
		t.Fatal("no published choice")
	}
	pins, version, err := openSyncListVersion(ctx, signer, sk.Public(), "concurrent-choice", *latest)
	if err != nil || version != 2 || !samePinSelection(pins[n.Address()], selectionPin(current)) {
		t.Fatalf("stale sync target was published %+v v%d %v", pins, version, err)
	}
}
