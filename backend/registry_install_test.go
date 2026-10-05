package backend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

// ─── monotonic installs and one writer per id (WR-03) ────────────

func TestInstallRefusesOlderVersion(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	v2 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v2", 20))
	if err := InstallNapp(v2); err != nil {
		t.Fatal(err)
	}
	files := seedNapplet(t, v2, randomID())
	requests := blobs.count()

	if err := InstallNapp(v1); !errors.Is(err, errOlderVersion) {
		t.Fatalf("installing an older version: %v, want errOlderVersion", err)
	}
	if blobs.count() != requests {
		t.Error("the refused install downloaded files")
	}
	if rec, _ := InstalledNapp(v2.ID); rec.EventID != v2.EventID {
		t.Fatalf("installed %s after a refused downgrade", rec.EventID)
	}
	assertFiles(t, "newer version", files.all(), true)

	// an update to an older event is refused at commit time too
	SetFetchErr("")
	applyUpdate(v2, v1)
	if got := fetchErr(); got != "update failed: "+errOlderVersion.Error() {
		t.Fatalf("launcher error %q", got)
	}
	rec, _ := InstalledNapp(v2.ID)
	if rec.EventID != v2.EventID {
		t.Fatalf("installed %s after a refused update", rec.EventID)
	}
	if doc, err := nappletDocument(rec); err != nil || string(doc) != "v2" {
		t.Errorf("installed document %q, %v", doc, err)
	}
	assertFiles(t, "newer version", files.all(), true)

	// the same version again is a reinstall, not a downgrade
	if err := InstallNapp(v2); err != nil {
		t.Fatalf("reinstalling the installed version: %v", err)
	}
}

func TestInstallUpdateUninstallRefuseWhileBusy(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	if err := InstallNapp(v1); err != nil {
		t.Fatal(err)
	}
	// a Try (or any other writer) holds the id
	if !trySetBusy(v1.ID) {
		t.Fatal("could not claim the id")
	}
	t.Cleanup(func() { setBusy(v1.ID, false) })
	requests := blobs.count()

	if err := InstallNapp(v1); !errors.Is(err, errBusy) {
		t.Fatalf("install while busy: %v, want errBusy", err)
	}
	if !IsBusy(v1.ID) {
		t.Fatal("a refused install cleared the claim it did not own")
	}
	SetFetchErr("")
	Update(v1.ID)
	if got := fetchErr(); got != "update failed: "+errBusy.Error() {
		t.Fatalf("update while busy: %q", got)
	}
	SetFetchErr("")
	Uninstall(v1.ID)
	if got := fetchErr(); got != "uninstall failed: "+errBusy.Error() {
		t.Fatalf("uninstall while busy: %q", got)
	}
	if _, ok := InstalledNapp(v1.ID); !ok {
		t.Fatal("an uninstall ran while the id was busy")
	}
	if !IsBusy(v1.ID) || blobs.count() != requests {
		t.Fatalf("busy %v, %d new requests", IsBusy(v1.ID), blobs.count()-requests)
	}
}

// TestTrialInstallDoesNotDowngrade: the user installs a newer version from
// the store while "Did you like X?" is up. Accepting it keeps that version
// instead of installing the trial's older event.
func TestTrialInstallDoesNotDowngrade(t *testing.T) {
	r := newTrialRig(t)
	resetTrialPrompts(t)
	old := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
	newer := r.event(t, "paint", 20, trialFile{"/index.html", "<!doctype html>v2"})
	// offline: the lookup finds nothing, so the trial's event is the target
	trial := installedFrom(t, old)
	s := newTrialSession(t, trial)

	done := make(chan struct{})
	go func() {
		finishNappletTrial(s.ci)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	var p *Prompt
	for p == nil {
		if p = CurrentPrompt(); p == nil {
			if time.Now().After(deadline) {
				t.Fatal("no install prompt")
			}
			time.Sleep(time.Millisecond)
		}
	}
	want := installedFrom(t, newer)
	installRecord(t, want)
	AnswerPrompt(p.ID, Answer{OK: true, Scope: ScopeOnce})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the trial did not finish")
	}
	backgroundSyncs.Wait()

	rec, _ := InstalledNapp(trial.ID)
	if rec.EventID != want.EventID {
		t.Fatalf("installed %s, want the newer %s", rec.EventID, want.EventID)
	}
	if got := fetchErr(); got != "" {
		t.Errorf("launcher error %q", got)
	}
	if n := trialDataNotice(trial); len(n) != 1 || n[0].Detail != trialDataDifferentVersion {
		t.Errorf("notice = %+v", n)
	}
	if onDisk(s.shared) || onDisk(s.instance) {
		t.Error("the older trial's data was written")
	}
}

// ─── staged installs (WR-01) ─────────────────────────────────────

// halfServedNapplet is a napplet manifest for d whose index.html (document)
// is served by b and whose second file is not: its download fails after
// index.html arrived.
func (b *blobRig) halfServedNapplet(t *testing.T, sk nostr.SecretKey, d, document string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	index := NappPath{Path: "/index.html", Sha256: b.add([]byte(document))}
	missing := NappPath{Path: "/app.js", Sha256: hashOf("never served " + document)}
	tags := nip5dTags(d, index, missing)
	for _, tag := range tags {
		if tag[0] == "server" {
			tag[1] = b.url
		}
	}
	return signedWith(t, sk, KindNapplet, tags, document, at)
}

// assertInstalledIntact checks that want is still the installed record, that
// its document still boots, and that no staging or set-aside directory is
// left next to its install dir.
func assertInstalledIntact(t *testing.T, want Napp, document string) {
	t.Helper()
	rec, ok := InstalledNapp(want.ID)
	if !ok || rec.EventID != want.EventID {
		t.Fatalf("installed %v %s, want %s", ok, rec.EventID, want.EventID)
	}
	if doc, err := nappletDocument(rec); err != nil || string(doc) != document {
		t.Fatalf("installed document %q, %v, want %q", doc, err, document)
	}
	base, err := nappBaseDir(want.ID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(base))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(base) && strings.HasPrefix(e.Name(), filepath.Base(base)) {
			t.Errorf("left behind next to the install dir: %s", e.Name())
		}
	}
}

func TestFailedInstallOverKeepsInstalledCopy(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	if err := InstallNapp(v1); err != nil {
		t.Fatal(err)
	}
	files := seedNapplet(t, v1, randomID())

	v2 := installedFrom(t, blobs.halfServedNapplet(t, sk, "app", "v2", 20))
	if err := InstallNapp(v2); err == nil {
		t.Fatal("an install with a missing file succeeded")
	}
	assertInstalledIntact(t, v1, "v1")
	assertFiles(t, "installed version", files.all(), true)
}

func TestFailedUpdateKeepsInstalledCopy(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	if err := InstallNapp(v1); err != nil {
		t.Fatal(err)
	}
	files := seedNapplet(t, v1, randomID())

	// index.html of the new version downloads fine; the other file fails
	v2 := installedFrom(t, blobs.halfServedNapplet(t, sk, "app", "v2", 20))
	SetFetchErr("")
	applyUpdate(v1, v2)
	if got := fetchErr(); !strings.HasPrefix(got, "update failed: ") {
		t.Fatalf("launcher error %q", got)
	}
	assertInstalledIntact(t, v1, "v1")
	assertFiles(t, "installed version", files.all(), true)
}

func TestFailedSwapKeepsInstalledCopy(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	if err := InstallNapp(v1); err != nil {
		t.Fatal(err)
	}
	v2 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v2", 20))

	// the installed copy moves aside, then the new one cannot move in
	prev := renameInstallDir
	t.Cleanup(func() { renameInstallDir = prev })
	renameInstallDir = func(from, to string) error {
		if strings.Contains(filepath.Base(from), stagingInfix) {
			return errors.New("injected rename failure")
		}
		return os.Rename(from, to)
	}
	if err := InstallNapp(v2); err == nil {
		t.Fatal("an install whose swap failed succeeded")
	}
	SetFetchErr("")
	applyUpdate(v1, v2)
	if got := fetchErr(); !strings.HasPrefix(got, "update failed: ") {
		t.Fatalf("launcher error %q", got)
	}
	assertInstalledIntact(t, v1, "v1")

	renameInstallDir = prev
	if err := InstallNapp(v2); err != nil {
		t.Fatal(err)
	}
	assertInstalledIntact(t, v2, "v2")
}

func TestInstallClearsStaleStaging(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	base, err := nappBaseDir(v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	// what a crash in the middle of an install leaves
	for _, dir := range []string{base + stagingInfix + "1", base + oldInfix + "2"} {
		writeFixture(t, filepath.Join(dir, "index.html"))
	}
	other := filepath.Join(filepath.Dir(base), "another"+stagingInfix+"1")
	writeFixture(t, filepath.Join(other, "index.html"))

	if err := InstallNapp(v1); err != nil {
		t.Fatal(err)
	}
	assertInstalledIntact(t, v1, "v1")
	if !onDisk(other) {
		t.Error("another napp's staging directory was removed")
	}
}
