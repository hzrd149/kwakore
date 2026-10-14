package backend

import (
	"context"
	"encoding/hex"
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// fakeLists is a relayListFunc over fixed NIP-65 lists.
type fakeList struct{ write, read []string }

func fakeLists(m map[nostr.PubKey]fakeList) relayListFunc {
	return func(_ context.Context, pk nostr.PubKey) ([]string, []string, bool) {
		l, ok := m[pk]
		return l.write, l.read, ok
	}
}

func TestDirectFilterGroupsAuthors(t *testing.T) {
	a, b, c := nostr.Generate().Public(), nostr.Generate().Public(), nostr.Generate().Public()
	f := nostr.Filter{Authors: []nostr.PubKey{a, b, c}, Kinds: []nostr.Kind{1}, Limit: 20}
	outboxes := map[nostr.PubKey][]string{
		a: {"wss://shared.example.com", "wss://a.example.com"},
		b: {"wss://shared.example.com/"},
		// c has no relays known
	}
	dfs := directFilter(f, outboxes, []string{"wss://hint.example.com"}, []string{"wss://fallback.example.com"})

	got := map[string][]nostr.PubKey{}
	for _, df := range dfs {
		if _, dup := got[df.Relay]; dup {
			t.Errorf("%s asked twice", df.Relay)
		}
		got[df.Relay] = df.Authors
		if df.Limit != 20 || len(df.Kinds) != 1 {
			t.Errorf("%s lost the rest of the filter: %v", df.Relay, df.Filter)
		}
	}
	want := map[string][]nostr.PubKey{
		"wss://shared.example.com":   {a, b},
		"wss://a.example.com":        {a},
		"wss://hint.example.com":     {a, b, c}, // hints get the whole filter
		"wss://fallback.example.com": {c},       // only who nobody else covers
	}
	if len(got) != len(want) {
		t.Fatalf("relays: got %v", got)
	}
	for u, authors := range want {
		if !slices.Equal(got[u], authors) {
			t.Errorf("%s: got %v, want %v", u, got[u], authors)
		}
	}
	if len(f.Authors) != 3 {
		t.Error("the napplet's filter was changed")
	}

	// nothing named and nowhere to go: the fallback relays, whole filter
	dfs = directFilter(nostr.Filter{Kinds: []nostr.Kind{1}}, nil, nil, []string{"wss://fallback.example.com"})
	if len(dfs) != 1 || dfs[0].Relay != "wss://fallback.example.com" {
		t.Errorf("fallback: %v", dfs)
	}
	// a full-filter relay is enough, no fallback needed
	dfs = directFilter(nostr.Filter{Kinds: []nostr.Kind{1}}, nil, []string{"wss://hint.example.com"}, []string{"wss://fallback.example.com"})
	if len(dfs) != 1 || dfs[0].Relay != "wss://hint.example.com" {
		t.Errorf("hint only: %v", dfs)
	}
}

func TestDirectFilterCapsRelays(t *testing.T) {
	var authors []nostr.PubKey
	outboxes := map[nostr.PubKey][]string{}
	for i := range outboxMaxDirected + 10 {
		pk := nostr.Generate().Public()
		authors = append(authors, pk)
		outboxes[pk] = []string{"wss://r" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".example.com"}
	}
	dfs := directFilter(nostr.Filter{Authors: authors}, outboxes, nil, []string{"wss://fallback.example.com"})
	if len(dfs) > outboxMaxDirected+1 {
		t.Fatalf("%d directed filters", len(dfs))
	}
	covered := map[nostr.PubKey]bool{}
	for _, df := range dfs {
		for _, pk := range df.Authors {
			covered[pk] = true
		}
	}
	if len(covered) != len(authors) {
		t.Errorf("%d of %d authors asked anywhere", len(covered), len(authors))
	}
}

// publicOnly is a relayCheck for tests: it turns away what napExplicitRelay
// would (localhost, loopback and private IP literals, and .lan hosts stand
// in for a hostname that resolves privately) without ever resolving DNS.
func publicOnly(_ context.Context, raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return "", errors.New("invalid relay url")
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".lan") {
		return "", errors.New("relay not allowed")
	}
	if ip, err := netip.ParseAddr(host); err == nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return "", errors.New("relay not allowed")
	}
	return nostr.NormalizeURL(raw), nil
}

// relaysOf is where directed filters go, in order, and the authors each one
// asks for.
func relaysOf(dfs []nostr.DirectedFilter) ([]string, map[string]nostr.Filter) {
	urls := []string{}
	byRelay := map[string]nostr.Filter{}
	for _, df := range dfs {
		urls = append(urls, df.Relay)
		byRelay[df.Relay] = df.Filter
	}
	return urls, byRelay
}

func TestOutboxDirectedRoutesByRelayLists(t *testing.T) {
	alice, bob, carol, dave, self := nostr.Generate().Public(), nostr.Generate().Public(),
		nostr.Generate().Public(), nostr.Generate().Public(), nostr.Generate().Public()
	rt := relayRouting{
		lists: fakeLists(map[nostr.PubKey]fakeList{
			alice: {write: []string{"wss://a1.example.com", "wss://a2.example.com", "wss://a3.example.com", "wss://a4.example.com"}},
			carol: {read: []string{"wss://carol-in.example.com"}},
			dave:  {write: []string{"wss://dave.example.com"}},
			// bob has no relay list
		}),
		check:    publicOnly,
		fallback: []string{"wss://fb.example.com"},
	}
	ctx := context.Background()
	defaults := []string{"wss://relay.damus.io", "wss://nos.lol"}
	noDefaults := func(urls []string) {
		t.Helper()
		for _, u := range urls {
			if slices.Contains(defaults, u) {
				t.Errorf("asked the sdk default %s", u)
			}
		}
	}

	// alice on her first three write relays, bob (no list) on the fallback
	urls, by := relaysOf(outboxDirected(ctx, rt, nostr.Filter{Authors: []nostr.PubKey{alice, bob}, Kinds: []nostr.Kind{1}}, nil, nil))
	noDefaults(urls)
	sorted := slices.Sorted(slices.Values(urls))
	if want := []string{"wss://a1.example.com", "wss://a2.example.com", "wss://a3.example.com", "wss://fb.example.com"}; !slices.Equal(sorted, want) {
		t.Fatalf("authors: directed to %v, want %v", sorted, want)
	}
	for _, u := range []string{"wss://a1.example.com", "wss://a2.example.com", "wss://a3.example.com"} {
		if !slices.Equal(by[u].Authors, []nostr.PubKey{alice}) {
			t.Errorf("%s asks for %v, want alice", u, by[u].Authors)
		}
	}
	if !slices.Equal(by["wss://fb.example.com"].Authors, []nostr.PubKey{bob}) {
		t.Errorf("the fallback asks for %v, want bob", by["wss://fb.example.com"].Authors)
	}

	// a tagged person is reached on their read relays, with the whole filter
	pf := nostr.Filter{Kinds: []nostr.Kind{4}, Tags: nostr.TagMap{"p": {carol.Hex()}}}
	urls, by = relaysOf(outboxDirected(ctx, rt, pf, nil, nil))
	if !slices.Equal(urls, []string{"wss://carol-in.example.com"}) || len(by[urls[0]].Tags["p"]) != 1 {
		t.Errorf("#p carol: directed to %v", urls)
	}
	// a tagged person with no list is looked for on the fallback
	urls, _ = relaysOf(outboxDirected(ctx, rt, nostr.Filter{Tags: nostr.TagMap{"p": {bob.Hex()}}}, nil, nil))
	if !slices.Equal(urls, []string{"wss://fb.example.com"}) {
		t.Errorf("#p bob: directed to %v, want the fallback", urls)
	}

	// a filter naming nobody goes to the fallback, all of it, even when the
	// user's own read list holds only one of those relays
	both := rt
	both.self = self
	both.lists = fakeLists(map[nostr.PubKey]fakeList{self: {read: []string{"wss://fb.example.com"}}})
	both.fallback = []string{"wss://fb.example.com", "wss://fb2.example.com"}
	urls, _ = relaysOf(outboxDirected(ctx, both, nostr.Filter{Kinds: []nostr.Kind{1}}, nil, nil))
	if !slices.Equal(urls, both.fallback) {
		t.Errorf("nobody named: directed to %v, want %v", urls, both.fallback)
	}

	// hint authors count only when the filter names no authors, and their
	// relays get the whole filter
	urls, _ = relaysOf(outboxDirected(ctx, rt, nostr.Filter{Authors: []nostr.PubKey{alice}}, []nostr.PubKey{dave}, nil))
	if slices.Contains(urls, "wss://dave.example.com") {
		t.Errorf("a hint author was asked beside the filter's authors: %v", urls)
	}
	urls, by = relaysOf(outboxDirected(ctx, rt, nostr.Filter{Kinds: []nostr.Kind{1}}, []nostr.PubKey{dave}, nil))
	if !slices.Equal(urls, []string{"wss://dave.example.com"}) || len(by[urls[0]].Authors) != 0 {
		t.Errorf("hint author: directed to %v", urls)
	}
	// a hint author with no list adds the fallback
	urls, _ = relaysOf(outboxDirected(ctx, rt, nostr.Filter{Kinds: []nostr.Kind{1}}, []nostr.PubKey{dave, bob}, nil))
	if !slices.Equal(urls, []string{"wss://dave.example.com", "wss://fb.example.com"}) {
		t.Errorf("hint authors with one missing: directed to %v", urls)
	}
}

func TestOutboxReadsDropPrivateRelays(t *testing.T) {
	mallory, self := nostr.Generate().Public(), nostr.Generate().Public()
	rt := relayRouting{
		lists: fakeLists(map[nostr.PubKey]fakeList{
			mallory: {
				write: []string{"wss://pub.example.com", "ws://127.0.0.1:7777", "wss://relay.lan"},
				read:  []string{"ws://127.0.0.1:4848"},
			},
			self: {write: []string{"wss://home.lan"}},
		}),
		check:    publicOnly,
		self:     self,
		fallback: []string{"wss://fb.example.com"},
	}
	ctx := context.Background()
	private := func(urls []string) bool {
		return slices.ContainsFunc(urls, func(u string) bool {
			return strings.Contains(u, "127.0.0.1") || strings.Contains(u, ".lan")
		})
	}

	urls, _ := relaysOf(outboxDirected(ctx, rt, nostr.Filter{Authors: []nostr.PubKey{mallory}}, nil, nil))
	if !slices.Equal(urls, []string{"wss://pub.example.com"}) {
		t.Errorf("mallory's outbox: directed to %v", urls)
	}
	urls, _ = relaysOf(outboxDirected(ctx, rt, nostr.Filter{Tags: nostr.TagMap{"p": {mallory.Hex()}}}, nil, nil))
	if !slices.Equal(urls, []string{"wss://fb.example.com"}) {
		t.Errorf("mallory's private inbox: directed to %v, want the fallback", urls)
	}
	// the user's own list is their choice
	urls, _ = relaysOf(outboxDirected(ctx, rt, nostr.Filter{Authors: []nostr.PubKey{self}}, nil, nil))
	if !slices.Equal(urls, []string{"wss://home.lan"}) {
		t.Errorf("own outbox: directed to %v", urls)
	}

	relays := outboxEventRelays(ctx, rt, []nostr.PubKey{mallory}, nil)
	if !slices.Contains(relays, "wss://pub.example.com") || !slices.Contains(relays, "wss://fb.example.com") || private(relays) {
		t.Errorf("getEvent relays: %v", relays)
	}

	plan := outboxPlan(ctx, rt, []nostr.PubKey{mallory}, "read")
	if plan.Source != "fallback" || !slices.Equal(plan.MissingAuthors, []string{mallory.Hex()}) || private(plan.Relays) {
		t.Errorf("read plan: %+v", plan)
	}

	// the daemon's own check, on IP literals so no DNS is needed
	real := relayRouting{
		lists: fakeLists(map[nostr.PubKey]fakeList{
			mallory: {write: []string{"ws://127.0.0.1:7777", "wss://1.1.1.1", "ws://192.168.1.2"}},
		}),
		check:    napExplicitRelay,
		fallback: []string{"wss://fb.example.com"},
	}
	urls, _ = relaysOf(outboxDirected(ctx, real, nostr.Filter{Authors: []nostr.PubKey{mallory}}, nil, nil))
	if !slices.Equal(urls, []string{"wss://1.1.1.1"}) {
		t.Errorf("napExplicitRelay: directed to %v", urls)
	}
}

func TestOutboxPlan(t *testing.T) {
	a, b := nostr.Generate().Public(), nostr.Generate().Public()
	rt := relayRouting{
		lists: fakeLists(map[nostr.PubKey]fakeList{
			a: {
				write: []string{"wss://w1.example.com", "wss://w2.example.com", "wss://w3.example.com", "wss://w4.example.com", "wss://w5.example.com"},
				read:  []string{"wss://r.example.com"},
			},
		}),
		check:    publicOnly,
		fallback: []string{"wss://fallback.example.com/"},
	}
	ctx := context.Background()
	three := []string{"wss://w1.example.com", "wss://w2.example.com", "wss://w3.example.com"}

	plan := outboxPlan(ctx, rt, []nostr.PubKey{a}, "write")
	if plan.Source != "nip65" || !slices.Equal(plan.Relays, three) || plan.MissingAuthors != nil {
		t.Errorf("write plan: %+v", plan)
	}
	plan = outboxPlan(ctx, rt, []nostr.PubKey{a, b}, "write")
	if plan.Source != "nip65" || !slices.Equal(plan.Relays, append(slices.Clone(three), "wss://fallback.example.com")) ||
		!slices.Equal(plan.MissingAuthors, []string{b.Hex()}) {
		t.Errorf("partial plan: %+v", plan)
	}
	plan = outboxPlan(ctx, rt, []nostr.PubKey{a}, "read")
	if !slices.Equal(plan.Relays, []string{"wss://r.example.com"}) {
		t.Errorf("read plan: %+v", plan)
	}
	plan = outboxPlan(ctx, rt, []nostr.PubKey{b}, "write")
	if plan.Source != "fallback" || !slices.Equal(plan.Relays, []string{"wss://fallback.example.com"}) {
		t.Errorf("fallback plan: %+v", plan)
	}

	// past the author cap nobody is looked up, even one with a list
	var many []nostr.PubKey
	for range outboxMaxAuthors {
		many = append(many, nostr.Generate().Public())
	}
	plan = outboxPlan(ctx, rt, append(many, a), "write")
	if !slices.Contains(plan.MissingAuthors, a.Hex()) || slices.Contains(plan.Relays, "wss://w1.example.com") {
		t.Errorf("an author past the cap was looked up: %+v", plan.Relays)
	}
	if plan.Relays == nil {
		t.Error("nil relays")
	}
}

// Every recipient's relay list is looked up at once: a lookup that only
// answers once all three are asked fails a sequential fan-out.
func TestOutboxFanoutLooksUpInboxesConcurrently(t *testing.T) {
	user := nostr.Generate().Public()
	recipients := []nostr.PubKey{nostr.Generate().Public(), nostr.Generate().Public(), nostr.Generate().Public()}
	inbox := map[nostr.PubKey]string{
		recipients[0]: "wss://in1.example.com",
		recipients[1]: "wss://in2.example.com",
		recipients[2]: "wss://in3.example.com",
	}
	var asked atomic.Int32
	all := make(chan struct{})
	var once sync.Once
	lists := func(ctx context.Context, pk nostr.PubKey) ([]string, []string, bool) {
		if asked.Add(1) == int32(len(recipients)) {
			once.Do(func() { close(all) })
		}
		select {
		case <-all:
			return nil, []string{inbox[pk]}, true
		case <-ctx.Done():
			return nil, nil, false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	targets, inboxes, err := outboxFanout(ctx, lists, publicOnly, user, false, recipients, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"wss://in1.example.com", "wss://in2.example.com", "wss://in3.example.com"}; !slices.Equal(targets, want) {
		t.Errorf("targets %v, want %v", targets, want)
	}
	for _, pk := range recipients {
		if !slices.Equal(inboxes[pk], []string{inbox[pk]}) {
			t.Errorf("inboxes[%s] = %v", pk.Hex(), inboxes[pk])
		}
	}
}

func TestOutboxFanout(t *testing.T) {
	user, bob, nolist := nostr.Generate().Public(), nostr.Generate().Public(), nostr.Generate().Public()
	lists := fakeLists(map[nostr.PubKey]fakeList{
		user: {write: []string{"wss://1.1.1.1", "wss://8.8.8.8"}, read: []string{"wss://9.9.9.9"}},
		// bob's inbox list names a relay on the user's own network: skipped
		bob: {write: []string{"wss://4.4.4.4"}, read: []string{"wss://8.8.8.8", "ws://192.168.1.2"}},
	})
	ctx := context.Background()

	targets, inboxes, err := outboxFanout(ctx, lists, napExplicitRelay, user, true, []nostr.PubKey{bob}, []string{"wss://1.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(targets, []string{"wss://1.1.1.1", "wss://8.8.8.8", "wss://1.0.0.1"}) {
		t.Errorf("targets: %v", targets)
	}
	if !slices.Equal(inboxes[bob], []string{"wss://8.8.8.8"}) {
		t.Errorf("bob's inboxes: %v", inboxes[bob])
	}

	// inbox only: the user's own outbox stays out of it
	targets, _, err = outboxFanout(ctx, lists, napExplicitRelay, user, false, []nostr.PubKey{bob}, nil, nil)
	if err != nil || !slices.Equal(targets, []string{"wss://8.8.8.8"}) {
		t.Errorf("inbox only: %v %v", targets, err)
	}

	if _, _, err := outboxFanout(ctx, lists, napExplicitRelay, user, true, []nostr.PubKey{nolist}, nil, nil); !errors.Is(err, errNoInbox) {
		t.Errorf("a recipient with no relay list: %v", err)
	}
	for _, bad := range []string{"ws://127.0.0.1:7777", "wss://localhost", "https://1.1.1.1"} {
		if _, _, err := outboxFanout(ctx, lists, napExplicitRelay, user, true, nil, []string{bad}, nil); !errors.Is(err, errPolicyDenied) {
			t.Errorf("explicit relay %s: %v", bad, err)
		}
	}
	// no relay list of one's own: the configured relays stand in for it
	targets, _, err = outboxFanout(ctx, lists, napExplicitRelay, nolist, true, nil, nil, []string{"wss://1.1.1.1/"})
	if err != nil || !slices.Equal(targets, []string{"wss://1.1.1.1"}) {
		t.Errorf("fallback outbox: %v %v", targets, err)
	}
	if _, _, err := outboxFanout(ctx, lists, napExplicitRelay, nolist, true, nil, nil, nil); err == nil {
		t.Error("published to nowhere")
	}
}

// NAP-RELAY reads route through the same relay lists and public-host rule,
// two relays per person.
func TestNapRelaysForUsesRelayLists(t *testing.T) {
	a, b, p, none, self := nostr.Generate().Public(), nostr.Generate().Public(), nostr.Generate().Public(),
		nostr.Generate().Public(), nostr.Generate().Public()
	rt := relayRouting{
		lists: fakeLists(map[nostr.PubKey]fakeList{
			a: {write: []string{"wss://a1.example.com", "ws://127.0.0.1:7777", "wss://a2.example.com", "wss://a3.example.com"}},
			b: {write: []string{"wss://b1.example.com"}},
			p: {read: []string{"wss://p1.example.com", "wss://p2.example.com", "wss://p3.example.com"}},
			// the user's own read relays are trusted and not capped
			self: {read: []string{"wss://s1.example.com", "wss://home.lan", "wss://s3.example.com"}},
		}),
		check:    publicOnly,
		self:     self,
		fallback: []string{"wss://fb.example.com"},
	}
	ctx := context.Background()
	defaults := []string{"wss://relay.damus.io", "wss://nos.lol", "wss://relay.primal.net"}
	check := func(what string, got, want []string) {
		t.Helper()
		if slices.ContainsFunc(got, func(u string) bool { return slices.Contains(defaults, u) }) {
			t.Errorf("%s: asked an sdk default: %v", what, got)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", what, got, want)
		}
	}

	f := nostr.Filter{Authors: []nostr.PubKey{a, none}, Tags: nostr.TagMap{"p": {p.Hex()}}}
	check("authors, tagged and self", napRelaysFor(ctx, rt, f), []string{
		"wss://a1.example.com", "wss://a2.example.com",
		"wss://p1.example.com", "wss://p2.example.com",
		"wss://s1.example.com", "wss://home.lan", "wss://s3.example.com",
		"wss://fb.example.com", // none has no list
	})

	signedOut := rt
	signedOut.self = nostr.ZeroPK
	check("authors in filter order", napRelaysFor(ctx, signedOut, nostr.Filter{Authors: []nostr.PubKey{b, a}}),
		[]string{"wss://b1.example.com", "wss://a1.example.com", "wss://a2.example.com"})
	check("nobody named, signed out", napRelaysFor(ctx, signedOut, nostr.Filter{Kinds: []nostr.Kind{1}}),
		[]string{"wss://fb.example.com"})
}

// withSystem gives a test an sdk system on a fresh store, with no relays
// configured, so nothing dials out.
func withSystem(t *testing.T) {
	t.Helper()
	closeStore, err := initSystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sys.Pool.Close("test over")
		closeStore()
		sys = nil
	})
}

func signedNote(t *testing.T, content string) nostr.Event {
	t.Helper()
	sk := nostr.Generate()
	evt := nostr.Event{Kind: 1, Content: content, CreatedAt: nostr.Now(), Tags: nostr.Tags{}}
	if err := evt.Sign(sk); err != nil {
		t.Fatal(err)
	}
	return evt
}

func TestNapOutboxGetEvent(t *testing.T) {
	setupNapTest(t)
	withSystem(t)
	evt := signedNote(t, "hello")
	if err := sys.Store.SaveEvent(evt); err != nil {
		t.Fatal(err)
	}
	ci, rec := openNapplet(t, "reader")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "outbox.getEvent", "id": "1", "eventId": evt.ID.Hex()})
	got := rec.wait(t, "outbox.getEvent.result", 1)
	res, _ := got["result"].(map[string]any)
	ev, _ := res["event"].(map[string]any)
	if got["id"] != "1" || ev["content"] != "hello" || got["error"] != nil {
		t.Errorf("stored event: %v", got)
	}

	other := signedNote(t, "elsewhere")
	post(t, ci, map[string]any{"type": "outbox.getEvent", "id": "2", "eventId": other.ID.Hex(),
		"options": map[string]any{"timeoutMs": 1000}})
	if got := rec.wait(t, "outbox.getEvent.result", 2); got["error"] != "not found" || got["result"] != nil {
		t.Errorf("unknown event: %v", got)
	}

	post(t, ci, map[string]any{"type": "outbox.getEvent", "id": "3", "eventId": "nope"})
	if got := rec.wait(t, "outbox.getEvent.result", 3); got["error"] != "invalid filter" {
		t.Errorf("bad id: %v", got)
	}
}

func TestNapOutboxSubscribe(t *testing.T) {
	setupNapTest(t)
	withSystem(t)
	evt := signedNote(t, "stored")
	if err := sys.Store.SaveEvent(evt); err != nil {
		t.Fatal(err)
	}
	ci, rec := openNapplet(t, "feed")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "outbox.subscribe", "id": "s", "subId": "feed",
		"filters": []any{map[string]any{"kinds": []int{1}}}})
	got := rec.wait(t, "outbox.event", 1)
	res, _ := got["result"].(map[string]any)
	if ev, _ := res["event"].(map[string]any); got["subId"] != "feed" || ev["content"] != "stored" {
		t.Errorf("event: %v", got)
	}
	// no relays configured: the shell ends the stream, and says so
	if got := rec.wait(t, "outbox.closed", 1); got["subId"] != "feed" {
		t.Errorf("closed: %v", got)
	}

	post(t, ci, map[string]any{"type": "outbox.subscribe", "id": "s2", "subId": "bad", "filters": "nope"})
	if got := rec.wait(t, "outbox.closed", 2); got["subId"] != "bad" || got["reason"] != "invalid filter" {
		t.Errorf("invalid filters: %v", got)
	}
	// a request that doesn't decode is still answered on its subId
	post(t, ci, map[string]any{"type": "outbox.subscribe", "id": "s3", "subId": "typo",
		"filters": []any{map[string]any{"kinds": []int{1}}}, "options": map[string]any{"limit": "ten"}})
	if got := rec.wait(t, "outbox.closed", 3); got["subId"] != "typo" || got["reason"] != "invalid filter" {
		t.Errorf("undecodable request: %v", got)
	}
	// and so is a close
	post(t, ci, map[string]any{"type": "outbox.close", "id": "c", "subId": "feed"})
	if got := rec.wait(t, "outbox.closed", 4); got["subId"] != "feed" {
		t.Errorf("close: %v", got)
	}
	for _, typ := range rec.types() {
		if typ == "outbox.eose" || typ == "relay.eose" {
			t.Errorf("an outbox subscription sent %s", typ)
		}
	}
}

// A closed outbox subscription whose pump ends late must not untrack a
// re-subscription with the same subId (CR-03).
func TestNapOutboxResubscribeKeepsLiveEntry(t *testing.T) {
	testResubscribeKeepsLiveEntry(t, resubscribeCase{
		subscribe: "outbox.subscribe", close: "outbox.close", closed: "outbox.closed",
		key:     outboxSubKey,
		refused: "too many subscriptions",
	})
}

func TestNapOutboxResolveRelaysNeedsAuthors(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "planner")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "outbox.resolveRelays", "id": "1", "target": map[string]any{}})
	if got := rec.wait(t, "outbox.resolveRelays.result", 1); got["error"] != "no authors" {
		t.Errorf("no target, signed out: %v", got)
	}
	post(t, ci, map[string]any{"type": "outbox.resolveRelays", "id": "2",
		"target": map[string]any{"pubkey": nostr.Generate().Public().Hex(), "direction": "sideways"}})
	if got := rec.wait(t, "outbox.resolveRelays.result", 2); got["error"] != "invalid target" {
		t.Errorf("bad direction: %v", got)
	}
}

// The domain set reaches the napplet only through the activation's
// install({domains}): window.napplet is what it can detect (NIP-5D presence
// detection), never a handshake.
func TestNapDomainsAdvertiseOutbox(t *testing.T) {
	if !slices.Contains(napDomains, "outbox") {
		t.Fatalf("napDomains lacks outbox: %v", napDomains)
	}
	doc, err := buildSrcdoc([]byte("<p>x</p>"), napDomains)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `"outbox"`) {
		t.Error("the srcdoc's install call does not grant outbox")
	}
}

func TestNapOutboxQueryLimitAndOrder(t *testing.T) {
	setupNapTest(t)
	withSystem(t)
	for i, content := range []string{"old", "mid", "new"} {
		evt := signedNote(t, content)
		evt.CreatedAt = nostr.Timestamp(1000 + i)
		sk := nostr.Generate()
		if err := evt.Sign(sk); err != nil {
			t.Fatal(err)
		}
		if err := sys.Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	ci, rec := openNapplet(t, "lister")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "outbox.query", "id": "q",
		"filters": []any{map[string]any{"kinds": []int{1}, "limit": 2}}})
	got := rec.wait(t, "outbox.query.result", 1)
	evs, _ := got["events"].([]any)
	var contents []string
	for _, e := range evs {
		contents = append(contents, e.(map[string]any)["event"].(map[string]any)["content"].(string))
	}
	if !slices.Equal(contents, []string{"new", "mid"}) {
		t.Errorf("query: %v (%v)", contents, got)
	}
}

// A napplet gets events exactly as they were signed: the user's DMs reach it
// as ciphertext through both relay and outbox, never decrypted, since the
// decrypted copy would fail its own id and signature check.
func TestNapDeliversDMsAsSigned(t *testing.T) {
	setupNapTest(t)
	withSystem(t)
	userSK, peerSK := nostr.Generate(), nostr.Generate()
	prevKeyer, prevPK := userKeyer, userPubkey
	userKeyer, userPubkey = keyer.NewPlainKeySigner(userSK), userSK.Public()
	t.Cleanup(func() { userKeyer, userPubkey = prevKeyer, prevPK })

	ct, err := keyer.NewPlainKeySigner(peerSK).Nip04Encrypt(context.Background(), "hello there", userSK.Public())
	if err != nil {
		t.Fatal(err)
	}
	dm := nostr.Event{Kind: 4, Content: ct, CreatedAt: nostr.Now(), Tags: nostr.Tags{{"p", userSK.Public().Hex()}}}
	if err := dm.Sign(peerSK); err != nil {
		t.Fatal(err)
	}
	if err := sys.Store.SaveEvent(dm); err != nil {
		t.Fatal(err)
	}
	ci, rec := openNapplet(t, "dms")
	ready(t, ci, rec, 1)

	filters := []any{map[string]any{"kinds": []int{4}}}
	post(t, ci, map[string]any{"type": "relay.query", "id": "r", "filters": filters})
	post(t, ci, map[string]any{"type": "outbox.query", "id": "o", "filters": filters})
	post(t, ci, map[string]any{"type": "outbox.getEvent", "id": "g", "eventId": dm.ID.Hex()})
	got := []map[string]any{
		rec.wait(t, "relay.query.result", 1)["events"].([]any)[0].(map[string]any),
		rec.wait(t, "outbox.query.result", 1)["events"].([]any)[0].(map[string]any),
		rec.wait(t, "outbox.getEvent.result", 1)["result"].(map[string]any),
	}
	for i, res := range got {
		if ev := res["event"].(map[string]any); ev["content"] != ct || ev["sig"] != hex.EncodeToString(dm.Sig[:]) {
			t.Errorf("delivery %d changed the event: %v", i, ev)
		}
	}
}
