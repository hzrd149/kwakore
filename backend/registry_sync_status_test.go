package backend

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// resetInstallSyncRecord clears the in-memory pass record for one test.
func resetInstallSyncRecord(t *testing.T) {
	t.Helper()
	reset := func() {
		installSyncRecordMu.Lock()
		installSyncRecord.id, installSyncRecord.result, installSyncRecord.napplets = "", "", 0
		installSyncRecord.lastAttempt, installSyncRecord.lastSuccess = time.Time{}, time.Time{}
		installSyncRecordMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func expectSyncResult(t *testing.T, step, result string, success bool, napplets int) InstallSyncReport {
	t.Helper()
	r := InstallSyncStatus()
	if !r.Enabled || r.LastResult == nil || *r.LastResult != result || r.LastAttempt == nil {
		t.Fatalf("%s: report %+v, want result %q", step, r, result)
	}
	if success != (r.LastSuccess != nil) {
		t.Fatalf("%s: last_success = %v", step, r.LastSuccess)
	}
	if napplets >= 0 && r.Napplets != napplets {
		t.Fatalf("%s: napplets = %d, want %d", step, r.Napplets, napplets)
	}
	return r
}

func TestInstallSyncStatusOffAndSkipped(t *testing.T) {
	newReclaimRig(t)
	resetInstallSyncRecord(t)
	stubInstallSync(t, "")
	withUserKeyer(t, nil, nostr.ZeroPK)

	// sync off: disabled, and the JSON has every field as null or zero
	runInstallSync(context.Background())
	b, err := json.Marshal(InstallSyncStatus())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"enabled":false,"last_attempt":null,"last_success":null,"last_result":null,"napplets":0}` {
		t.Fatalf("off report %s", b)
	}

	// sync on with nobody signed in: skipped
	installSyncID = func() string { return "laptops" }
	runInstallSync(context.Background())
	expectSyncResult(t, "no signer", "skipped", false, 0)
}

func TestInstallSyncStatusAgainstRelay(t *testing.T) {
	newReclaimRig(t)
	resetInstallSyncRecord(t)
	nk := nostr.Generate()
	withSyncSystem(t, nk.Public())
	sk := nostr.Generate()
	k, pk := keyer.NewPlainKeySigner(sk), sk.Public()
	withUserKeyer(t, k, pk)
	relay := fastRelay(t)
	stubInstallSync(t, "status", relay)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	X := validNapplet(t, nk, "x", "", 10)
	Z := validNapplet(t, nk, "z", "", 10)
	rec := &syncRecorder{manifests: map[string]nostr.Event{X.ID.Hex(): X, Z.ID.Hex(): Z}, fail: map[string]bool{}}
	installSyncPin = rec.install(t)
	installSyncRemove = rec.remove

	// a pass cancelled before it starts records nothing
	cancelled, stop := context.WithCancel(ctx)
	stop()
	runInstallSync(cancelled)
	if r := InstallSyncStatus(); r.LastResult != nil || r.LastAttempt != nil {
		t.Fatalf("cancelled pass recorded %+v", r)
	}

	// a successful pass publishes the local list
	installRecord(t, installedFrom(t, X))
	runInstallSync(ctx)
	ok := expectSyncResult(t, "first pass", "ok", true, 1)
	if ok.LastAttempt.Location() != time.UTC {
		t.Fatalf("last_attempt is not UTC: %v", ok.LastAttempt)
	}

	start := nostr.Now()
	publish := func(content string, at nostr.Timestamp) {
		t.Helper()
		evt := signedWith(t, sk, installSyncKind, nostr.Tags{{"d", "status"}}, content, at)
		r, err := sys.Pool.EnsureRelay(relay)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Publish(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	// an install from the list that fails
	rec.fail[installedFrom(t, Z).Address()] = true
	content, err := sealSyncList(ctx, k, pk, map[string]syncPin{
		installedFrom(t, X).Address(): {EventID: X.ID.Hex()},
		installedFrom(t, Z).Address(): {EventID: Z.ID.Hex()},
	})
	if err != nil {
		t.Fatal(err)
	}
	publish(content, start+100)
	runInstallSync(ctx)
	failed := expectSyncResult(t, "failed install", "failed", true, 2)
	// last_success is the earlier ok pass, unchanged
	if !failed.LastSuccess.Equal(*ok.LastSuccess) {
		t.Fatalf("last_success moved on a failed pass: %v, was %v", failed.LastSuccess, ok.LastSuccess)
	}

	// a list sealed for another key cannot be read
	foreign, err := keyer.NewPlainKeySigner(nostr.Generate()).Encrypt(ctx, `{"v":1,"napplets":[]}`, pk)
	if err != nil {
		t.Fatal(err)
	}
	publish(foreign, start+200)
	runInstallSync(ctx)
	expectSyncResult(t, "unreadable", "unreadable", true, 2)

	// a new sync id starts with an empty report until its own pass records
	installSyncID = func() string { return "other" }
	if r := InstallSyncStatus(); !r.Enabled || r.LastResult != nil || r.LastAttempt != nil || r.LastSuccess != nil || r.Napplets != 0 {
		t.Fatalf("report for a new sync id %+v", r)
	}
	recordInstallSyncResult("other", installSyncSkipped, -1)
	expectSyncResult(t, "new id", "skipped", false, 0)
}
