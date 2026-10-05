package backend

import (
	"errors"
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
