package backend

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/sdk"
	cache_memory "fiatjaf.com/nostr/sdk/cache/memory"
)

// ─── loop rig ────────────────────────────────────────────────────

// loopCall is one socialRefreshOne call the loop made.
type loopCall struct {
	address string
	at      time.Time
}

type loopRecorder struct {
	mu    sync.Mutex
	calls []loopCall
	// interval is socialRefreshEvery's answer, online updateCheckOnline's
	interval atomic.Int64
	online   atomic.Bool
}

func (r *loopRecorder) snapshot() []loopCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// waitCalls waits up to two seconds for at least n calls.
func (r *loopRecorder) waitCalls(t *testing.T, n int) []loopCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		calls := r.snapshot()
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d refresh calls, want at least %d", len(calls), n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func drainSocialWake() {
	select {
	case <-socialRefreshWake:
	default:
	}
}

// socialLoopRig shrinks the loop's seams to milliseconds, records every
// refresh instead of fetching, and restores everything afterwards.
func socialLoopRig(t *testing.T) *loopRecorder {
	t.Helper()
	r := &loopRecorder{}
	r.online.Store(true)
	prevDelay, prevStagger, prevRetry, prevTimeout := socialRefreshDelay, socialRefreshStagger, socialRefreshOfflineRetry, socialRefreshTimeout
	prevEvery, prevOne, prevOnline, prevPrefetch := socialRefreshEvery, socialRefreshOne, updateCheckOnline, prefetchNappletSocial
	socialRefreshDelay = 5 * time.Millisecond
	socialRefreshStagger = 20 * time.Millisecond
	socialRefreshOfflineRetry = 20 * time.Millisecond
	socialRefreshTimeout = time.Second
	socialRefreshEvery = func() time.Duration { return time.Duration(r.interval.Load()) }
	socialRefreshOne = func(_ context.Context, address string) {
		r.mu.Lock()
		r.calls = append(r.calls, loopCall{address, time.Now()})
		r.mu.Unlock()
	}
	updateCheckOnline = func() bool { return r.online.Load() }
	drainSocialWake()
	t.Cleanup(func() {
		socialRefreshDelay, socialRefreshStagger, socialRefreshOfflineRetry, socialRefreshTimeout = prevDelay, prevStagger, prevRetry, prevTimeout
		socialRefreshEvery, socialRefreshOne, updateCheckOnline, prefetchNappletSocial = prevEvery, prevOne, prevOnline, prevPrefetch
		drainSocialWake()
	})
	return r
}

// startSocialLoop starts the loop and stops it in a cleanup that runs
// before the rig's restores.
func startSocialLoop(t *testing.T) (context.CancelFunc, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wait := StartServiceSocialRefresh(ctx)
	t.Cleanup(func() {
		cancel()
		wait()
	})
	return cancel, wait
}

// installLoopNapplets records n installed napplets and returns their
// addresses, sorted.
func installLoopNapplets(t *testing.T, n int) []string {
	t.Helper()
	var addresses []string
	for range n {
		evt := validNapplet(t, nostr.Generate(), "app", "v1", 10)
		napp := installedFrom(t, evt)
		installRecord(t, napp)
		addresses = append(addresses, napp.Address())
	}
	slices.Sort(addresses)
	return addresses
}

func callAddresses(calls []loopCall) []string {
	out := []string{}
	for _, c := range calls {
		out = append(out, c.address)
	}
	return out
}

// ─── loop ────────────────────────────────────────────────────────

func TestSocialRefreshLoopInstalledOnlyStaggered(t *testing.T) {
	newUpdateRig(t)
	withSystem(t)
	r := socialLoopRig(t)
	r.interval.Store(int64(time.Hour))
	addresses := installLoopNapplets(t, 3)
	// an address with only a status-index entry is never refreshed
	stranger, err := newSocialTarget(nappletAddress(nostr.Generate().Public(), "other"))
	if err != nil {
		t.Fatal(err)
	}
	socialRecordSync(stranger, map[string]string{SocialReactions: "answered"}, nil)

	startSocialLoop(t)
	calls := r.waitCalls(t, 3)
	if got := callAddresses(calls); !slices.Equal(got, addresses) {
		t.Fatalf("refreshed %v, want %v", got, addresses)
	}
	for i := 1; i < len(calls); i++ {
		if gap := calls[i].at.Sub(calls[i-1].at); gap < socialRefreshStagger {
			t.Fatalf("calls %d and %d only %v apart", i-1, i, gap)
		}
	}
	// the interval is an hour: no second pass
	time.Sleep(100 * time.Millisecond)
	if n := len(r.snapshot()); n != 3 {
		t.Fatalf("%d calls after one pass with an hour interval", n)
	}
}

func TestSocialRefreshLoopOffline(t *testing.T) {
	newUpdateRig(t)
	withSystem(t)
	r := socialLoopRig(t)
	r.interval.Store(int64(time.Hour))
	r.online.Store(false)
	addresses := installLoopNapplets(t, 1)
	startSocialLoop(t)
	time.Sleep(100 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("%d calls while offline", n)
	}
	// online again: the next try is within the offline retry, not an hour
	r.online.Store(true)
	if got := callAddresses(r.waitCalls(t, 1)); !slices.Equal(got, addresses) {
		t.Fatalf("refreshed %v", got)
	}
}

func TestSocialRefreshLoopDisabledAtZero(t *testing.T) {
	newUpdateRig(t)
	withSystem(t)
	r := socialLoopRig(t)
	installLoopNapplets(t, 1)
	startSocialLoop(t)
	time.Sleep(100 * time.Millisecond)
	if n := len(r.snapshot()); n != 0 {
		t.Fatalf("%d calls with the refresh off", n)
	}
}

func TestSocialRefreshLoopIntervalChange(t *testing.T) {
	newUpdateRig(t)
	withSystem(t)
	r := socialLoopRig(t)
	r.interval.Store(int64(time.Hour))
	installLoopNapplets(t, 1)
	startSocialLoop(t)
	r.waitCalls(t, 1)
	time.Sleep(60 * time.Millisecond)
	if n := len(r.snapshot()); n != 1 {
		t.Fatalf("%d calls before the interval changed", n)
	}
	// a shorter interval brings the next pass forward at once
	r.interval.Store(int64(10 * time.Millisecond))
	SocialRefreshChanged()
	r.waitCalls(t, 4)
	// 0 stops further passes
	r.interval.Store(0)
	SocialRefreshChanged()
	time.Sleep(50 * time.Millisecond)
	settled := len(r.snapshot())
	time.Sleep(100 * time.Millisecond)
	if n := len(r.snapshot()); n != settled {
		t.Fatalf("calls went from %d to %d with the refresh off", settled, n)
	}
	// and a non-zero value resumes them
	r.interval.Store(int64(10 * time.Millisecond))
	SocialRefreshChanged()
	r.waitCalls(t, settled+2)
}

func TestSocialRefreshLoopStopsOnCancel(t *testing.T) {
	newUpdateRig(t)
	withSystem(t)
	r := socialLoopRig(t)
	r.interval.Store(int64(time.Hour))
	socialRefreshStagger = 10 * time.Second
	installLoopNapplets(t, 2)
	cancel, wait := startSocialLoop(t)
	r.waitCalls(t, 1)
	if _, done, ok := socialHookBegin(); !ok {
		t.Fatal("no running loop for a hook")
	} else {
		done()
	}
	// cancelled mid-stagger, the loop ends promptly
	cancel()
	stopped := make(chan struct{})
	go func() {
		wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not return after cancel")
	}
	if n := len(r.snapshot()); n != 1 {
		t.Fatalf("%d calls after a cancel mid-stagger", n)
	}
	if _, _, ok := socialHookBegin(); ok {
		t.Fatal("a hook joined a stopped loop")
	}
}

// ─── hooks ───────────────────────────────────────────────────────

// installHookRig is an install-ready state with a stub social relay whose
// only event, once the napplet exists, is a foreign + reaction on it.
type installHookRig struct {
	relay  *socialRelay
	blobs  *blobRig
	author nostr.SecretKey
	v1     nostr.Event
	v2     nostr.Event
	napp   Napp
}

func newInstallHookRig(t *testing.T) installHookRig {
	t.Helper()
	newReclaimRig(t)
	withSystem(t)
	socialResetMemo()
	blobs := newBlobRig(t)
	relay := newSocialRelay(t)
	socialSeams(t, relay.url)
	// hooks use the production refresh, over the stub relay
	production := socialRefreshOne
	socialLoopRig(t)
	socialRefreshOne = production
	// only hooks run: the first pass would be an hour away
	socialRefreshDelay = time.Hour
	sk := nostr.Generate()
	// an empty cached Blossom list: the install never asks the network
	// for the author's servers
	lists := cache_memory.New[sdk.GenericList[string, sdk.BlossomURL]](1000)
	lists.Set(sk.Public(), sdk.GenericList[string, sdk.BlossomURL]{PubKey: sk.Public(), Items: []sdk.BlossomURL{}})
	lists.Cache.Wait()
	sys.BlossomServerListCache = lists
	v1 := blobs.servedNapplet(t, sk, "app", "v1", 10)
	v2 := blobs.servedNapplet(t, sk, "app", "v2", 20)
	napp := installedFrom(t, v1)
	relay.set(signedWith(t, nostr.Generate(), nostr.KindReaction, nostr.Tags{{"a", napp.Address()}}, "+", nostr.Now()-60))
	return installHookRig{relay: relay, blobs: blobs, author: sk, v1: v1, v2: v2, napp: napp}
}

func TestScheduleNappletSocialNeedsTheDaemon(t *testing.T) {
	rig := newInstallHookRig(t)
	// the default gate: no service config outside the daemon
	prefetchNappletSocial = func() bool { return serviceConfig != nil }
	startSocialLoop(t)
	if _, err := InstallNappContext(t.Context(), rig.napp); err != nil {
		t.Fatal(err)
	}
	backgroundSyncs.Wait()
	if n := rig.relay.reqs.Load(); n != 0 {
		t.Fatalf("%d REQs without a service config", n)
	}
	if s := socialSyncStatus(rig.napp.Address())[SocialReactions]; s.Status != "not_requested" {
		t.Fatalf("reactions sync %+v", s)
	}
}

func TestScheduleNappletSocialNeedsTheLoop(t *testing.T) {
	rig := newInstallHookRig(t)
	prefetchNappletSocial = func() bool { return true }
	if _, err := InstallNappContext(t.Context(), rig.napp); err != nil {
		t.Fatal(err)
	}
	backgroundSyncs.Wait()
	if n := rig.relay.reqs.Load(); n != 0 {
		t.Fatalf("%d REQs without a running loop", n)
	}
}

func TestScheduleNappletSocialOnInstallAndUpdate(t *testing.T) {
	rig := newInstallHookRig(t)
	prefetchNappletSocial = func() bool { return true }
	rec := captureServiceEvents(t)
	startSocialLoop(t)
	address := rig.napp.Address()
	var all []ServiceEvent

	if _, err := InstallNappContext(t.Context(), rig.napp); err != nil {
		t.Fatal(err)
	}
	backgroundSyncs.Wait()
	afterInstall := rig.relay.reqs.Load()
	if afterInstall == 0 {
		t.Fatal("the install fetched no social data")
	}
	events := rec.take()
	all = append(all, events...)
	var updated []ServiceEvent
	for _, e := range events {
		if e.Type == EventSocialUpdated {
			updated = append(updated, e)
		}
	}
	if len(updated) != 1 || updated[0].Address != address || !slices.Equal(updated[0].Fields, []string{SocialReactions}) {
		t.Fatalf("social.updated after install: %+v", updated)
	}
	summary, err := ServiceSocial(t.Context(), address, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Sync[SocialReactions].Status != "answered" || summary.Votes.Up != 1 {
		t.Fatalf("summary after install %+v", summary)
	}
	if entry := string(readSocialIndex(t)[address]); !strings.Contains(entry, `"installed":true`) {
		t.Fatalf("index entry %s", entry)
	}

	old := fetchServiceManifestEvents
	fetchServiceManifestEvents = func(context.Context, Napp) ([]nostr.Event, bool) { return []nostr.Event{rig.v2}, true }
	t.Cleanup(func() { fetchServiceManifestEvents = old })
	if _, err := ServiceUpdate(t.Context(), address); err != nil {
		t.Fatal(err)
	}
	backgroundSyncs.Wait()
	if n := rig.relay.reqs.Load(); n <= afterInstall {
		t.Fatalf("the update fetched nothing: %d REQs, %d before", n, afterInstall)
	}
	events = rec.take()
	all = append(all, events...)
	for _, e := range events {
		if e.Type == EventSocialUpdated {
			t.Fatalf("social.updated after an update that changed nothing: %+v", e)
		}
	}
	assertPublicEvents(t, all)
}

func TestSocialMarkInstalled(t *testing.T) {
	newUpdateRig(t)
	withSystem(t)
	addresses := installLoopNapplets(t, 1)
	stranger := nappletAddress(nostr.Generate().Public(), "other")
	socialMarkInstalled(stranger)
	if _, ok := readSocialIndex(t)[stranger]; ok {
		t.Fatal("an uninstalled address was marked")
	}
	socialMarkInstalled(addresses[0])
	entry := string(readSocialIndex(t)[addresses[0]])
	if !strings.Contains(entry, `"installed":true`) || !strings.Contains(entry, `"categories":{}`) {
		t.Fatalf("created entry %s", entry)
	}
}

// ─── social.updated ──────────────────────────────────────────────

func socialUpdatedEvents(t *testing.T, rec *eventRecorder) [][]string {
	t.Helper()
	events := rec.ofType(EventSocialUpdated)
	assertPublicEvents(t, events)
	out := [][]string{}
	for _, e := range events {
		out = append(out, e.Fields)
	}
	return out
}

func TestSocialUpdatedOnlyOnChange(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	rec := captureServiceEvents(t)
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x := nostr.Generate()
	reaction := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	relay.set(reaction)
	refresh := func() [][]string {
		t.Helper()
		if _, err := ServiceSocial(t.Context(), address, true, nil); err != nil {
			t.Fatal(err)
		}
		events := rec.ofType(EventSocialUpdated)
		assertPublicEvents(t, events)
		out := [][]string{}
		for _, e := range events {
			if e.Address != address {
				t.Fatalf("event for %q", e.Address)
			}
			out = append(out, e.Fields)
		}
		return out
	}
	if got := refresh(); !slices.EqualFunc(got, [][]string{{SocialReactions}}, slices.Equal) {
		t.Fatalf("first refresh emitted %v", got)
	}
	if got := refresh(); len(got) != 0 {
		t.Fatalf("an identical refresh emitted %v", got)
	}
	comment := signedWith(t, x, nostr.KindComment, nostr.Tags{{"A", address}}, "hello", 10)
	relay.set(reaction, comment)
	if got := refresh(); !slices.EqualFunc(got, [][]string{{SocialComments}}, slices.Equal) {
		t.Fatalf("a new comment emitted %v", got)
	}
	deletion := signedWith(t, x, nostr.KindDeletion, nostr.Tags{{"e", comment.ID.Hex()}, {"k", "1111"}}, "", 20)
	relay.set(reaction, comment, deletion)
	if got := refresh(); !slices.EqualFunc(got, [][]string{{SocialComments}}, slices.Equal) {
		t.Fatalf("a deleted comment emitted %v", got)
	}
}

func TestSocialUpdatedNotWithoutRelays(t *testing.T) {
	socialRig(t)
	rec := captureServiceEvents(t)
	address := nappletAddress(nostr.Generate().Public(), "app")
	summary, err := ServiceSocial(t.Context(), address, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Sync[SocialReactions].Status != "unavailable" {
		t.Fatalf("sync %+v", summary.Sync)
	}
	if got := socialUpdatedEvents(t, rec); len(got) != 0 {
		t.Fatalf("an unavailable refresh emitted %v", got)
	}
}

func TestSocialUpdatedAfterOwnActions(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	rec := captureServiceEvents(t)
	ctx := context.Background()
	check := func(what string, want ...string) {
		t.Helper()
		events := rec.ofType(EventSocialUpdated)
		assertPublicEvents(t, events)
		if len(want) == 0 {
			if len(events) != 0 {
				t.Fatalf("%s emitted %+v", what, events)
			}
			return
		}
		if len(events) != 1 || events[0].Address != fx.address || !slices.Equal(events[0].Fields, want) {
			t.Fatalf("%s emitted %+v, want one with %v", what, events, want)
		}
	}
	if _, err := ServiceSocialVote(ctx, fx.address, "up", ""); err != nil {
		t.Fatal(err)
	}
	check("vote up", SocialReactions)
	if res, err := ServiceSocialVote(ctx, fx.address, "up", ""); err != nil || res.Outcome != "unchanged" {
		t.Fatalf("repeat vote %+v %v", res, err)
	}
	check("an unchanged vote")
	if _, err := ServiceSocialVote(ctx, fx.address, "none", ""); err != nil {
		t.Fatal(err)
	}
	check("vote none", SocialReactions)
	if _, err := ServiceSocialVote(ctx, fx.address, "sideways", ""); err == nil {
		t.Fatal("a bad vote was accepted")
	}
	check("a refused vote")
	if _, err := ServiceSocialReact(ctx, fx.address, "🔥", "", ""); err != nil {
		t.Fatal(err)
	}
	check("react", SocialReactions)
	comment, err := ServiceSocialComment(ctx, fx.address, "nice", "")
	if err != nil {
		t.Fatal(err)
	}
	check("comment", SocialComments)
	if _, err := ServiceSocialShare(ctx, fx.address, "look"); err != nil {
		t.Fatal(err)
	}
	check("share", SocialMentions)
	if _, err := ServiceSocialDelete(ctx, fx.address, comment.EventID); err != nil {
		t.Fatal(err)
	}
	check("delete of the own comment", SocialComments)
}

func TestSocialUpdatedHelpers(t *testing.T) {
	for _, category := range SocialCategories() {
		if got := socialCategoryOf(socialKind(category)); got != category {
			t.Errorf("socialCategoryOf(socialKind(%q)) = %q", category, got)
		}
	}
	if got := socialCategoryOf(nostr.KindDeletion); got != "" {
		t.Errorf("a deletion has category %q", got)
	}
	rec := captureServiceEvents(t)
	socialUpdated("addr")
	socialUpdated("addr", "")
	socialUpdated("addr", SocialZaps, SocialComments, SocialZaps)
	events := rec.take()
	if len(events) != 1 || events[0].Type != EventSocialUpdated || events[0].Address != "addr" || !slices.Equal(events[0].Fields, []string{SocialComments, SocialZaps}) {
		t.Fatalf("events %+v", events)
	}
}
