package backend

import (
	"context"
	"errors"
	"iter"
	"net"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"fiatjaf.com/nostr/khatru"
)

// ─── fake relays ───────────────────────────────────────────────────────────

// testPool is a pool of the test's own, closed when the test ends, so the
// helper core runs without the package's sdk system.
func testPool(t *testing.T) *nostr.Pool {
	t.Helper()
	pool := nostr.NewPool()
	t.Cleanup(func() { pool.Close("test over") })
	return pool
}

// khatruRelay starts a khatru relay on a slice store seeded with events. When
// query is set it replaces the stored-events lookup, which khatru must finish
// before it sends EOSE.
func khatruRelay(t *testing.T, query func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event], events ...nostr.Event) string {
	t.Helper()
	store := &slicestore.SliceStore{}
	store.Init()
	for _, evt := range events {
		if err := store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	r := khatru.NewRelay()
	r.UseEventstore(store, 100)
	if query != nil {
		r.QueryStored = query
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// fastRelay answers every REQ with its stored events and an EOSE at once.
func fastRelay(t *testing.T, events ...nostr.Event) string {
	t.Helper()
	return khatruRelay(t, nil, events...)
}

// silentRelay accepts subscriptions and never sends EOSE.
func silentRelay(t *testing.T) string {
	t.Helper()
	stop := make(chan struct{})
	url := khatruRelay(t, func(ctx context.Context, _ nostr.Filter) iter.Seq[nostr.Event] {
		return func(func(nostr.Event) bool) {
			select {
			case <-ctx.Done():
			case <-stop:
			}
		}
	})
	t.Cleanup(func() { close(stop) }) // runs before the server closes
	return url
}

// lateRelay waits delay, then sends evt and EOSE.
func lateRelay(t *testing.T, delay time.Duration, evt nostr.Event) string {
	t.Helper()
	stop := make(chan struct{})
	url := khatruRelay(t, func(ctx context.Context, _ nostr.Filter) iter.Seq[nostr.Event] {
		return func(yield func(nostr.Event) bool) {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-time.After(delay):
			}
			yield(evt)
		}
	})
	t.Cleanup(func() { close(stop) })
	return url
}

// hangingListener accepts TCP connections and never answers, so a websocket
// dial to it hangs until the dialer gives up.
func hangingListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	return "ws://" + ln.Addr().String()
}

func signed(t *testing.T, sk nostr.SecretKey, kind nostr.Kind, content string) nostr.Event {
	t.Helper()
	evt := nostr.Event{Kind: kind, Content: content, CreatedAt: nostr.Now(), Tags: nostr.Tags{}}
	if err := evt.Sign(sk); err != nil {
		t.Fatal(err)
	}
	return evt
}

// drain collects everything on q.Events until it closes and how long that
// took, failing the test if it stays open past a 5s guard.
func drain(t *testing.T, q *relayQuery, start time.Time) ([]nostr.RelayEvent, time.Duration) {
	t.Helper()
	var got []nostr.RelayEvent
	guard := time.After(5 * time.Second)
	for {
		select {
		case ie, ok := <-q.Events():
			if !ok {
				return got, time.Since(start)
			}
			got = append(got, ie)
		case <-guard:
			t.Fatalf("events still open after 5s (got %d)", len(got))
		}
	}
}

func eventIDs(got []nostr.RelayEvent) []nostr.ID {
	ids := make([]nostr.ID, 0, len(got))
	for _, ie := range got {
		ids = append(ids, ie.ID)
	}
	return ids
}

func targetsFor(filter nostr.Filter, urls ...string) []nostr.DirectedFilter {
	dfs := make([]nostr.DirectedFilter, 0, len(urls))
	for _, u := range urls {
		dfs = append(dfs, nostr.DirectedFilter{Filter: filter, Relay: u})
	}
	return dfs
}

// ─── query rule ────────────────────────────────────────────────────────────

func TestRelayQueryAllEOSEReturnsAtOnce(t *testing.T) {
	sk := nostr.Generate()
	a, b := signed(t, sk, 1, "a"), signed(t, sk, 1, "b")
	r1, r2 := fastRelay(t, a), fastRelay(t, a, b)

	start := time.Now()
	q := queryRelays(context.Background(), testPool(t), relayLimits{10 * time.Second, 5 * time.Second},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}}, r1, r2), "test", false)
	got, took := drain(t, q, start)

	// every relay sent EOSE, so the request ends at once, with A once
	if took >= 2*time.Second {
		t.Fatalf("took %s with every relay answered", took)
	}
	ids := eventIDs(got)
	if len(ids) != 2 || !slices.Contains(ids, a.ID) || !slices.Contains(ids, b.ID) {
		t.Fatalf("got %v, want exactly A and B", ids)
	}
	if !q.Answered() || !q.Complete() || !q.AllAnswered() {
		t.Fatalf("answered %v complete %v, want both", q.Answered(), q.Complete())
	}
}

func TestRelayQueryGraceAfterFirstEOSE(t *testing.T) {
	sk := nostr.Generate()
	a := signed(t, sk, 1, "a")
	fast, silent := fastRelay(t, a), silentRelay(t)

	grace := 300 * time.Millisecond
	start := time.Now()
	q := queryRelays(context.Background(), testPool(t), relayLimits{10 * time.Second, grace},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}}, fast, silent), "test", false)
	got, took := drain(t, q, start)

	// the silent relay holds the request only for the grace after the first EOSE
	if took < grace || took >= 2*time.Second {
		t.Fatalf("took %s, want between %s and 2s", took, grace)
	}
	if ids := eventIDs(got); !slices.Equal(ids, []nostr.ID{a.ID}) {
		t.Fatalf("got %v, want A", ids)
	}
	if !q.Answered() || q.Complete() || q.AllAnswered() {
		t.Fatalf("answered %v complete %v, want answered and not complete", q.Answered(), q.Complete())
	}
}

func TestRelayQueryLateEOSEInsideGrace(t *testing.T) {
	sk := nostr.Generate()
	a, l := signed(t, sk, 1, "a"), signed(t, sk, 1, "late")
	fast, late := fastRelay(t, a), lateRelay(t, 150*time.Millisecond, l)

	start := time.Now()
	q := queryRelays(context.Background(), testPool(t), relayLimits{10 * time.Second, 2 * time.Second},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}}, fast, late), "test", false)
	got, took := drain(t, q, start)

	// the late relay answers inside the grace, so its event arrives and the
	// request ends when it does, not when the grace runs out
	ids := eventIDs(got)
	if len(ids) != 2 || !slices.Contains(ids, a.ID) || !slices.Contains(ids, l.ID) {
		t.Fatalf("got %v, want A and L", ids)
	}
	if took >= 1500*time.Millisecond {
		t.Fatalf("took %s, want under 1.5s", took)
	}
	if !q.Complete() {
		t.Fatal("not complete although every relay sent EOSE")
	}
}

func TestRelayQueryHardCap(t *testing.T) {
	silent, hanging := silentRelay(t), hangingListener(t)

	limit := 400 * time.Millisecond
	start := time.Now()
	q := queryRelays(context.Background(), testPool(t), relayLimits{limit, 5 * time.Second},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}}, silent, hanging), "test", false)
	got, took := drain(t, q, start)

	// no relay answers and one dial hangs: the cap ends the request, and the
	// hanging connect is abandoned rather than holding it for the 7s dial
	if took < limit || took >= 2*time.Second {
		t.Fatalf("took %s, want between %s and 2s", took, limit)
	}
	if len(got) != 0 || q.Answered() || q.Complete() {
		t.Fatalf("got %d events, answered %v, complete %v; want nothing", len(got), q.Answered(), q.Complete())
	}
}

func TestRelayQueryRefusedAddsNoDelay(t *testing.T) {
	fast := fastRelay(t, signed(t, nostr.Generate(), 1, "a"))

	start := time.Now()
	q := queryRelays(context.Background(), testPool(t), relayLimits{20 * time.Second, 10 * time.Second},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}}, fast, "ws://127.0.0.1:1"), "test", false)
	_, took := drain(t, q, start)

	// a refused connection counts as finished at once
	if took >= 2*time.Second {
		t.Fatalf("took %s with one relay refused", took)
	}
	if !q.Complete() {
		t.Fatal("not complete although every relay answered or failed")
	}
}

func TestRelayQueryCallerCancel(t *testing.T) {
	silent := silentRelay(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	q := queryRelays(ctx, testPool(t), relayLimits{10 * time.Second, 10 * time.Second},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}}, silent), "test", false)
	time.AfterFunc(100*time.Millisecond, cancel)
	_, took := drain(t, q, start)

	// the caller's ctx always ends the request sooner than the rule
	if took >= time.Second {
		t.Fatalf("took %s after the caller cancelled", took)
	}
}

func TestRelayQueryDirected(t *testing.T) {
	sk := nostr.Generate()
	note, reaction := signed(t, sk, 1, "note"), signed(t, sk, 7, "+")
	fast, silent := fastRelay(t, note, reaction), silentRelay(t)

	grace := 300 * time.Millisecond
	start := time.Now()
	q := queryRelays(context.Background(), testPool(t), relayLimits{10 * time.Second, grace}, []nostr.DirectedFilter{
		{Filter: nostr.Filter{Kinds: []nostr.Kind{1}}, Relay: fast},
		{Filter: nostr.Filter{Kinds: []nostr.Kind{7}}, Relay: fast},
		{Filter: nostr.Filter{Kinds: []nostr.Kind{1}}, Relay: silent},
	}, "test", false)
	got, took := drain(t, q, start)

	// each (relay, filter) target is its own subscription; the silent one
	// holds the request only for the grace
	ids := eventIDs(got)
	if len(ids) != 2 || !slices.Contains(ids, note.ID) || !slices.Contains(ids, reaction.ID) {
		t.Fatalf("got %v, want the note and the reaction", ids)
	}
	if took < grace || took >= 2*time.Second {
		t.Fatalf("took %s, want between %s and 2s", took, grace)
	}
}

func TestRelayQueryMiddleware(t *testing.T) {
	sk := nostr.Generate()
	a := signed(t, sk, 1, "a")
	r1, r2 := fastRelay(t, a), fastRelay(t, a)

	pool := testPool(t)
	var seen, dups atomic.Int32
	var mu sync.Mutex
	var queried []string
	pool.EventMiddleware = func(nostr.RelayEvent) { seen.Add(1) }
	pool.DuplicateMiddleware = func(string, nostr.ID) { dups.Add(1) }
	pool.QueryMiddleware = func(relay string, pk nostr.PubKey, kind nostr.Kind) {
		mu.Lock()
		defer mu.Unlock()
		if pk == sk.Public() && kind == 1 {
			queried = append(queried, relay)
		}
	}

	q := queryRelays(context.Background(), pool, relayLimits{10 * time.Second, 5 * time.Second},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}, Authors: []nostr.PubKey{sk.Public()}}, r1, r2), "test", false)
	got, _ := drain(t, q, time.Now())

	// the pool's middleware still sees the query, the event once and the
	// duplicate once, and the duplicate is not delivered
	if len(got) != 1 {
		t.Fatalf("got %d events, want A once", len(got))
	}
	if seen.Load() != 1 || dups.Load() != 1 {
		t.Fatalf("event middleware %d, duplicate middleware %d; want 1 and 1", seen.Load(), dups.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queried) != 2 || !slices.Contains(queried, nostr.NormalizeURL(r1)) || !slices.Contains(queried, nostr.NormalizeURL(r2)) {
		t.Fatalf("query middleware saw %v, want both relays", queried)
	}
}

func TestRelayQueryLiveKeepsStreaming(t *testing.T) {
	sk := nostr.Generate()
	a := signed(t, sk, 1, "a")
	fast, silent := fastRelay(t, a), silentRelay(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	grace := 200 * time.Millisecond
	start := time.Now()
	q := queryRelays(ctx, testPool(t), relayLimits{10 * time.Second, grace},
		targetsFor(nostr.Filter{Kinds: []nostr.Kind{1}}, fast, silent), "test", true)

	recv := func(what string) nostr.RelayEvent {
		t.Helper()
		select {
		case ie, ok := <-q.Events():
			if !ok {
				t.Fatalf("events closed waiting for %s", what)
			}
			return ie
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s within 3s", what)
		}
		return nostr.RelayEvent{}
	}
	if ie := recv("stored event"); ie.ID != a.ID {
		t.Fatalf("got %s, want A", ie.ID)
	}

	// done follows the rule while the subscriptions stay open
	select {
	case <-q.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("done not closed within 2s")
	}
	if took := time.Since(start); took < grace {
		t.Fatalf("done after %s, before the %s grace", took, grace)
	}

	pub := testPool(t)
	rr, err := pub.EnsureRelay(fast)
	if err != nil {
		t.Fatal(err)
	}
	b := signed(t, sk, 1, "b")
	if err := rr.Publish(ctx, b); err != nil {
		t.Fatal(err)
	}
	if ie := recv("live event"); ie.ID != b.ID {
		t.Fatalf("got %s, want B", ie.ID)
	}

	cancel()
	guard := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-q.Events():
			if !ok {
				return
			}
		case <-guard:
			t.Fatal("events still open 2s after the caller cancelled")
		}
	}
}

// ─── publish cap ───────────────────────────────────────────────────────────

func TestPublishWithinCapsSilentRelay(t *testing.T) {
	good, hanging, refused := fastRelay(t), hangingListener(t), "ws://127.0.0.1:1"
	evt := signed(t, nostr.Generate(), 1, "hello")

	start := time.Now()
	results := map[string]error{}
	n := 0
	for res := range publishWithin(context.Background(), testPool(t), 400*time.Millisecond,
		[]string{good, hanging, refused, good}, evt) {
		results[res.RelayURL] = res.Error
		n++
	}

	// one result per distinct relay, ending by the cap; the relay that never
	// answered is reported with the fixed timeout error
	if took := time.Since(start); took >= 2*time.Second {
		t.Fatalf("took %s, want the 400ms cap", took)
	}
	if n != 3 {
		t.Fatalf("got %d results (%v), want 3", n, results)
	}
	if err, ok := results[good]; !ok || err != nil {
		t.Fatalf("good relay: %v (present %v), want success", err, ok)
	}
	if err := results[refused]; err == nil || errors.Is(err, errRelayTimeout) {
		t.Fatalf("refused relay: %v, want its own connect error", err)
	}
	if err := results[hanging]; !errors.Is(err, errRelayTimeout) {
		t.Fatalf("hanging relay: %v, want errRelayTimeout", err)
	}
}

// ─── penalty box ───────────────────────────────────────────────────────────

func TestSystemPenaltyBoxEnabled(t *testing.T) {
	withSystem(t)
	url := "ws://127.0.0.1:1/penalty"
	sys.Pool.AddToPenaltyBox(url, time.Minute)

	// the sdk system runs the penalty box, so a dead relay is not redialled
	// by every request
	_, err := sys.Pool.EnsureRelay(url)
	if err == nil || !strings.Contains(err.Error(), "penalty box") {
		t.Fatalf("EnsureRelay = %v, want a penalty box error", err)
	}
}
