package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

func selectionFixture(t *testing.T) (*blobRig, nostr.Event, nostr.Event) {
	t.Helper()
	newReclaimRig(t)
	withManifestHistory(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	old := blobs.servedNapplet(t, sk, "app", "old code", 10)
	newer := blobs.servedNapplet(t, sk, "app", "new code", 20)
	archiveManifest(old, "")
	archiveManifest(newer, "")
	return blobs, old, newer
}

func selectInstalled(t *testing.T, evt nostr.Event) Napp {
	t.Helper()
	if _, err := ServiceInstallVersion(context.Background(), eventAddress(evt), evt.ID.Hex(), nil); err != nil {
		t.Fatal(err)
	}
	n, ok := InstalledNapp(eventAddress(evt))
	if !ok || !n.Pinned || n.SelectionRevision == 0 || !hex32.MatchString(n.SelectionID) {
		t.Fatalf("selected record %+v", n)
	}
	return n
}

func TestSelectionForwardRollbackOfflineAndReset(t *testing.T) {
	blobs, old, newer := selectionFixture(t)
	n := selectInstalled(t, old)
	ci, _ := openWindowOf(t, n)
	files := seedNapplet(t, n, ci.storageInstance)
	WindowClosed(ci.ID())
	held := storageFor(files.shared)
	r, err := ServiceSelect(context.Background(), n.Address(), newer.ID.Hex())
	if err != nil || r.Direction != "forward" || !r.DataReset || !r.Pinned || r.PreviousVersion.EventID != old.ID.Hex() {
		t.Fatalf("forward selection %+v %v", r, err)
	}
	assertFiles(t, "forward reset", files.all(), false)
	if err := held.set(files.shared, "late", "write", nappletStorageQuota, errNappletQuota, nil); !errors.Is(err, errStoreReclaimed) {
		t.Fatalf("stale storage writer %v", err)
	}
	current, _ := InstalledNapp(n.ID)
	if current.SelectionRevision <= n.SelectionRevision {
		t.Fatal("selection revision did not advance")
	}
	// Saved data in the target old scope must not be resurrected on rollback.
	targetFiles := seedNapplet(t, n, ci.storageInstance)
	currentFiles := seedNapplet(t, current, ci.storageInstance)
	blobs.mu.Lock()
	clear(blobs.blobs)
	blobs.mu.Unlock()
	hits := blobs.count()
	stateMu.Lock()
	state.BlossomServers = []string{}
	stateMu.Unlock()
	r, err = ServiceSelect(context.Background(), n.Address(), old.ID.Hex())
	if err != nil || r.Direction != "backward" || !r.DataReset || r.InstalledVersion.EventID != old.ID.Hex() || blobs.count() != hits {
		t.Fatalf("offline rollback %+v %v", r, err)
	}
	assertFiles(t, "target reset", targetFiles.all(), false)
	assertFiles(t, "previous reset", currentFiles.all(), false)
	restored, _ := InstalledNapp(n.ID)
	if data, err := nappletDocument(restored); err != nil || string(data) != "old code" {
		t.Fatalf("restored code %q %v", data, err)
	}
	if restored.SelectionRevision <= current.SelectionRevision {
		t.Fatal("rollback was ordered by publication age")
	}
	page, err := ServiceHistory(context.Background(), n.Address(), false, false, 0, 100)
	if err != nil || !page.Items[0].Cached || !page.Items[1].Cached || page.SelectedVersion.EventID != old.ID.Hex() || page.LatestObserved.EventID != newer.ID.Hex() {
		t.Fatalf("history after rollback %+v %v", page, err)
	}
}

func TestSelectionSameArtifactResetAndSamePinNoop(t *testing.T) {
	blobs, old, _ := selectionFixture(t)
	sk := nostr.Generate()
	first := blobs.servedNapplet(t, sk, "metadata", "same bytes", 10)
	second := first
	second.CreatedAt = 20
	second.Content = "different description"
	if err := second.Sign(sk); err != nil {
		t.Fatal(err)
	}
	archiveManifest(first, "")
	archiveManifest(second, "")
	n := selectInstalled(t, first)
	files := seedNapplet(t, n, randomID())
	held := storageFor(files.shared)
	if r, err := ServiceSelect(context.Background(), n.Address(), second.ID.Hex()); err != nil || !r.DataReset {
		t.Fatalf("same artifact switch %+v %v", r, err)
	}
	assertFiles(t, "same artifact shared/config", []string{files.shared, files.config}, false)
	if err := held.set(files.shared, "stale", "value", nappletStorageQuota, errNappletQuota, nil); !errors.Is(err, errStoreReclaimed) {
		t.Fatalf("old writer survived %v", err)
	}
	current, _ := InstalledNapp(n.ID)
	seed := seedNapplet(t, current, randomID())
	hits := blobs.count()
	r, err := ServiceSelect(context.Background(), n.Address(), second.ID.Hex())
	if err != nil || r.Outcome != "unchanged" || r.DataReset || blobs.count() != hits {
		t.Fatalf("same pin %+v %v", r, err)
	}
	assertFiles(t, "same pin data", seed.all(), true)
	after, _ := InstalledNapp(n.ID)
	if after.SelectionRevision != current.SelectionRevision || after.SelectionID != current.SelectionID {
		t.Fatal("no-op changed selection stamp")
	}
	if _, err := ServiceSelect(context.Background(), n.Address(), old.ID.Hex()); !errors.Is(err, ErrServiceConflict) {
		t.Fatalf("different address accepted: %v", err)
	}
}

func TestSelectionRefusesOpenWindowsAndBadTargets(t *testing.T) {
	blobs, old, newer := selectionFixture(t)
	n := selectInstalled(t, old)
	ci, _ := openWindowOf(t, n)
	files := seedNapplet(t, n, ci.storageInstance)
	hits := blobs.count()
	if _, err := ServiceSelect(context.Background(), n.Address(), newer.ID.Hex()); !errors.Is(err, ErrServiceBusy) || hits != blobs.count() {
		t.Fatalf("open window selection %v, requests %d", err, blobs.count()-hits)
	}
	assertFiles(t, "busy selection", files.all(), true)
	WindowClosed(ci.ID())
	broken := invalidNapplet(t, nostr.Generate(), "app", 30)
	archiveManifest(broken, "")
	if _, err := ServiceInstallVersion(context.Background(), eventAddress(broken), broken.ID.Hex(), nil); !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("invalid exact manifest accepted %v", err)
	}
	if _, err := ServiceSelect(context.Background(), n.Address(), strings.Repeat("e", 64)); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("missing event %v", err)
	}
	if _, err := ServiceSelect(context.Background(), n.Address(), strings.Repeat("E", 64)); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("uppercase id %v", err)
	}
}

func TestSelectionFailedDownloadAndStateCommitPreserveData(t *testing.T) {
	blobs, old, newer := selectionFixture(t)
	n := selectInstalled(t, old)
	files := seedNapplet(t, n, randomID())
	next := installedFrom(t, newer)
	blobs.mu.Lock()
	data := blobs.blobs[next.Paths[0].Sha256]
	delete(blobs.blobs, next.Paths[0].Sha256)
	blobs.mu.Unlock()
	if _, err := ServiceSelect(context.Background(), n.Address(), newer.ID.Hex()); err == nil {
		t.Fatal("missing blob selection succeeded")
	}
	assertFiles(t, "failed download", files.all(), true)
	if current, _ := InstalledNapp(n.ID); current.EventID != old.ID.Hex() {
		t.Fatal("failed download changed selection")
	}
	blobs.add(data)
	stateSaveBlocked.Store(true)
	t.Cleanup(func() { stateSaveBlocked.Store(false) })
	if _, err := ServiceSelect(context.Background(), n.Address(), newer.ID.Hex()); err == nil {
		t.Fatal("blocked state commit succeeded")
	}
	stateSaveBlocked.Store(false)
	assertFiles(t, "failed commit", files.all(), true)
	current, _ := InstalledNapp(n.ID)
	if current.EventID != old.ID.Hex() || current.SelectionRevision != n.SelectionRevision {
		t.Fatalf("failed commit changed record %+v", current)
	}
	if data, err := nappletDocument(current); err != nil || string(data) != "old code" {
		t.Fatalf("failed commit changed code %q %v", data, err)
	}
}

func TestSelectionRecoveryCompletesResetBeforeLaunch(t *testing.T) {
	_, old, newer := selectionFixture(t)
	n := selectInstalled(t, old)
	files := seedNapplet(t, n, randomID())
	if err := os.Remove(files.shared); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(files.shared, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(files.shared, "obstruction"), []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceSelect(context.Background(), n.Address(), newer.ID.Hex()); err == nil || !mutationExists(n.ID) {
		t.Fatalf("reset failure lost journal %v", err)
	}
	current, _ := InstalledNapp(n.ID)
	if current.EventID != newer.ID.Hex() {
		t.Fatal("reset was attempted before the state committed")
	}
	if _, err := ServiceLaunch(context.Background(), n.Address()); !errors.Is(err, ErrServiceBusy) {
		t.Fatalf("pending reset was launchable %v", err)
	}
	if err := os.RemoveAll(files.shared); err != nil {
		t.Fatal(err)
	}
	if err := recoverRegistryMutations(); err != nil {
		t.Fatal(err)
	}
	if mutationExists(n.ID) {
		t.Fatal("completed reset journal remains")
	}
	assertFiles(t, "recovered reset", []string{files.shared, files.config}, false)
}

func TestSelectionCorruptCacheCannotExecuteWrongBytes(t *testing.T) {
	_, old, newer := selectionFixture(t)
	n := selectInstalled(t, old)
	next := installedFrom(t, newer)
	if err := os.WriteFile(cachedArtifactPath(next.Paths[0].Sha256), []byte("wrong code"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceSelect(context.Background(), n.Address(), newer.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	current, _ := InstalledNapp(n.ID)
	if data, err := nappletDocument(current); err != nil || string(data) != "new code" {
		t.Fatalf("wrong cached bytes executed %q %v", data, err)
	}
}

func TestSelectionStampAndPinPersist(t *testing.T) {
	_, old, _ := selectionFixture(t)
	n := selectInstalled(t, old)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var saved AppState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	got := saved.InstalledNapps[n.ID]
	if !got.Pinned || got.SelectionRevision != n.SelectionRevision || got.SelectionID != n.SelectionID {
		t.Fatalf("pin not persisted %+v", got)
	}
}
