package backend

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// resetInstallSyncKicks drops any pending kick and debounce timer, before
// and after the test.
func resetInstallSyncKicks(t *testing.T) {
	t.Helper()
	reset := func() {
		stopInstallSyncTimer()
		for {
			select {
			case <-installSyncKick:
			default:
				return
			}
		}
	}
	reset()
	t.Cleanup(reset)
}

func withInstallSyncDebounce(t *testing.T, d time.Duration) {
	t.Helper()
	prev := installSyncDebounce
	installSyncDebounce = d
	t.Cleanup(func() { installSyncDebounce = prev })
}

// stubInstallSyncPass replaces the pass with one that reports on the
// returned channel.
func stubInstallSyncPass(t *testing.T, pass func(context.Context)) {
	t.Helper()
	prev := installSyncPass
	installSyncPass = pass
	t.Cleanup(func() { installSyncPass = prev })
}

// startInstallSyncWorker runs a worker until the test ends. Registered after
// the seams are replaced, it stops the worker before they are put back.
func startInstallSyncWorker(t *testing.T) (cancel context.CancelFunc, wait func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wait = StartInstallSync(ctx)
	t.Cleanup(func() {
		cancel()
		wait()
	})
	return cancel, wait
}

func expectKick(t *testing.T, within time.Duration, what string) {
	t.Helper()
	select {
	case <-installSyncKick:
	case <-time.After(within):
		t.Fatalf("no kick: %s", what)
	}
}

func expectNoKick(t *testing.T, within time.Duration, what string) {
	t.Helper()
	select {
	case <-installSyncKick:
		t.Fatalf("unexpected kick: %s", what)
	case <-time.After(within):
	}
}

func expectPass(t *testing.T, passes <-chan struct{}, within time.Duration, what string) {
	t.Helper()
	select {
	case <-passes:
	case <-time.After(within):
		t.Fatalf("no pass: %s", what)
	}
}

func expectNoPass(t *testing.T, passes <-chan struct{}, within time.Duration, what string) {
	t.Helper()
	select {
	case <-passes:
		t.Fatalf("unexpected pass: %s", what)
	case <-time.After(within):
	}
}

func TestInstallSyncWorkerKicksAndStops(t *testing.T) {
	resetInstallSyncKicks(t)
	stubInstallSync(t, "")
	passes := make(chan struct{}, 16)
	release := make(chan struct{})
	var blocking atomic.Bool
	stubInstallSyncPass(t, func(ctx context.Context) {
		passes <- struct{}{}
		if blocking.Load() {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	cancel, wait := startInstallSyncWorker(t)

	expectPass(t, passes, time.Second, "at start")
	InstallSyncChanged()
	expectPass(t, passes, time.Second, "after a sync_id change")

	// kicks while a pass runs fold into one more pass
	blocking.Store(true)
	kickInstallSync()
	expectPass(t, passes, time.Second, "blocking pass")
	kickInstallSync()
	kickInstallSync()
	kickInstallSync()
	blocking.Store(false)
	close(release)
	expectPass(t, passes, time.Second, "after the folded kicks")
	expectNoPass(t, passes, 200*time.Millisecond, "a kick was not folded")

	cancel()
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the worker did not stop")
	}
}

func TestInstallSyncLocalChangeDebounce(t *testing.T) {
	resetInstallSyncKicks(t)
	stubInstallSync(t, "test")
	withInstallSyncDebounce(t, 50*time.Millisecond)
	a, b := "35129:"+hexID('a')+":a", "35129:"+hexID('a')+":b"

	start := time.Now()
	installSyncLocalChange(a)
	time.Sleep(10 * time.Millisecond)
	installSyncLocalChange(a)
	time.Sleep(10 * time.Millisecond)
	installSyncLocalChange(a)
	expectKick(t, time.Second, "after a burst")
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("kicked after %v, before the debounce", elapsed)
	}
	expectNoKick(t, 150*time.Millisecond, "a burst kicked twice")

	// the sync's own change schedules nothing; another address still does
	markInstallSyncApplying(a)
	installSyncLocalChange(a)
	expectNoKick(t, 200*time.Millisecond, "the sync's own change")
	installSyncLocalChange(b)
	expectKick(t, time.Second, "a concurrent user change")
	doneInstallSyncApplying(a)
	if installSyncIsApplying(a) {
		t.Fatal("the applying mark stayed")
	}

	// sync off schedules nothing
	installSyncID = func() string { return "" }
	installSyncLocalChange(a)
	expectNoKick(t, 200*time.Millisecond, "sync off")
}

func TestInstallSyncMutationsKick(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	resetInstallSyncKicks(t)
	stubInstallSync(t, "test")
	withInstallSyncDebounce(t, 10*time.Millisecond)
	evt := blobs.servedNapplet(t, nostr.Generate(), "app", "v1", 10)
	address := eventAddress(evt)
	old := addressEvents
	addressEvents = func(context.Context, nostr.EntityPointer) ([]nostr.Event, error) { return []nostr.Event{evt}, nil }
	t.Cleanup(func() { addressEvents = old })

	if _, err := ServiceInstall(t.Context(), address, nil); err != nil {
		t.Fatal(err)
	}
	expectKick(t, time.Second, "after an install")
	if _, err := ServiceUninstall(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	expectKick(t, time.Second, "after an uninstall")
}

func TestInstallSyncIdentityChangeKicks(t *testing.T) {
	resetInstallSyncKicks(t)
	pushIdentityChanged()
	expectKick(t, 10*time.Millisecond, "after an identity change")
}

func TestInstallSyncLiveEventKicks(t *testing.T) {
	resetInstallSyncKicks(t)
	withSystem(t)
	sk := nostr.Generate()
	pk := sk.Public()
	withUserKeyer(t, keyer.NewPlainKeySigner(sk), pk)
	relay := fastRelay(t)
	stubInstallSync(t, "test", relay)
	stateMu.Lock()
	saved := state.Sync
	state.Sync = nil
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		state.Sync = saved
		stateMu.Unlock()
	})
	passes := make(chan struct{}, 16)
	stubInstallSyncPass(t, func(context.Context) { passes <- struct{}{} })
	startInstallSyncWorker(t)
	expectPass(t, passes, time.Second, "at start")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := sys.Pool.EnsureRelay(relay)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(d string, at nostr.Timestamp) nostr.Event {
		t.Helper()
		evt := signedWith(t, sk, installSyncKind, nostr.Tags{{"d", d}}, "x", at)
		if err := r.Publish(ctx, evt); err != nil {
			t.Fatal(err)
		}
		return evt
	}
	now := nostr.Now()

	// give the subscription a moment to reach the relay; an event it
	// misses live still arrives as the stored newest one
	time.Sleep(100 * time.Millisecond)
	publish("test", now+10)
	expectPass(t, passes, 3*time.Second, "a newer list from another machine")

	// this machine's own publish comes back and is ignored
	own := signedWith(t, sk, installSyncKind, nostr.Tags{{"d", "test"}}, "own", now+20)
	setInstallSyncLastPublished(own.ID.Hex())
	if err := r.Publish(ctx, own); err != nil {
		t.Fatal(err)
	}
	expectNoPass(t, passes, 300*time.Millisecond, "this machine's own publish")

	// another sync_id is not this list
	publish("other", now+30)
	expectNoPass(t, passes, 300*time.Millisecond, "another d")
}
