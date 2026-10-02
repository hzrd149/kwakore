package backend

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/sdk"
)

// asUser logs sk in for the test, with a state file of its own, and undoes
// it (and any followed relay list) afterwards.
func asUser(t *testing.T, sk nostr.SecretKey) {
	t.Helper()
	statePath = filepath.Join(t.TempDir(), "state.json")
	stateMu.Lock()
	oldRelays := state.Relays
	state.UserRelays, state.DiscoverOnUserRelays = nil, nil
	stateMu.Unlock()
	userKeyer, userPubkey = keyer.NewPlainKeySigner(sk), sk.Public()
	t.Cleanup(func() {
		stopUserRelays()
		userRelayTasks.Wait()
		userKeyer, userPubkey = nil, nostr.ZeroPK
		stateMu.Lock()
		state.Relays, state.DiscoverOnUserRelays = oldRelays, nil
		stateMu.Unlock()
	})
}

func relayList(t *testing.T, sk nostr.SecretKey, at nostr.Timestamp, tags ...nostr.Tag) nostr.Event {
	t.Helper()
	evt := nostr.Event{Kind: nostr.KindRelayListMetadata, CreatedAt: at, Tags: tags}
	if err := evt.Sign(sk); err != nil {
		t.Fatal(err)
	}
	return evt
}

// noSystem runs a test without an sdk system, so anything that would reach
// for the network panics instead.
func noSystem(t *testing.T) {
	old := sys
	sys = nil
	t.Cleanup(func() { sys = old })
}

func TestRelayListFromEvent(t *testing.T) {
	evt := relayList(t, nostr.Generate(), nostr.Now(),
		nostr.Tag{"r", "wss://both.example.com"},
		nostr.Tag{"r", "wss://in.example.com/", "read"},
		nostr.Tag{"r", "out.example.com", "write"},
		nostr.Tag{"r", "wss://both.example.com/"},
		nostr.Tag{"p", "wss://not-a-relay.example.com"},
		nostr.Tag{"r"},
	)
	write, read := relayListFromEvent(evt)
	if want := []string{"wss://both.example.com", "wss://out.example.com"}; !slices.Equal(write, want) {
		t.Errorf("write = %v, want %v", write, want)
	}
	if want := []string{"wss://both.example.com", "wss://in.example.com"}; !slices.Equal(read, want) {
		t.Errorf("read = %v, want %v", read, want)
	}
}

func TestDiscoveryRelaysUnion(t *testing.T) {
	noSystem(t)
	sk := nostr.Generate()
	asUser(t, sk)
	stateMu.Lock()
	state.Relays = []string{"wss://apps.example.com", "wss://shared.example.com"}
	stateMu.Unlock()

	startUserRelays(sk.Public())
	applyUserRelayEvent(relayList(t, sk, nostr.Now(),
		nostr.Tag{"r", "wss://shared.example.com", "write"},
		nostr.Tag{"r", "wss://mine.example.com", "write"},
		nostr.Tag{"r", "wss://inbox.example.com", "read"},
	))

	want := []string{"wss://apps.example.com", "wss://shared.example.com", "wss://mine.example.com"}
	if got := discoveryRelays(); !slices.Equal(got, want) {
		t.Fatalf("discovery relays = %v, want %v", got, want)
	}

	rediscovered := 0
	old := rediscover
	rediscover = func() { rediscovered++ }
	t.Cleanup(func() { rediscover = old })

	SetDiscoverOnUserRelays(false)
	if rediscovered != 1 {
		t.Fatalf("turning it off reran discovery %d times", rediscovered)
	}
	want = []string{"wss://apps.example.com", "wss://shared.example.com"}
	if got := discoveryRelays(); !slices.Equal(got, want) {
		t.Fatalf("with it off, discovery relays = %v, want %v", got, want)
	}
	SetDiscoverOnUserRelays(false)
	if rediscovered != 1 {
		t.Fatal("saving the same value reran discovery")
	}
}

func TestPersistedUserRelaysFollowTheirOwner(t *testing.T) {
	noSystem(t)
	sk, other := nostr.Generate(), nostr.Generate()
	asUser(t, sk)

	stateMu.Lock()
	state.UserRelays = &userRelayList{Pubkey: other.Public(), Write: []string{"wss://theirs.example.com"}, LoadedAt: time.Now()}
	stateMu.Unlock()
	startUserRelays(sk.Public())
	if l, ok := userRelays(); ok {
		t.Fatalf("someone else's persisted list was taken: %+v", l)
	}

	stateMu.Lock()
	state.UserRelays = &userRelayList{Pubkey: sk.Public(), Write: []string{"wss://mine.example.com"}, CreatedAt: 10, LoadedAt: time.Now()}
	stateMu.Unlock()
	startUserRelays(sk.Public())
	if got := UserWriteRelays(); !slices.Equal(got, []string{"wss://mine.example.com"}) {
		t.Fatalf("persisted list not adopted: %v", got)
	}

	// an older version, or one by someone else, doesn't replace it
	if applyUserRelayEvent(relayList(t, sk, 5, nostr.Tag{"r", "wss://old.example.com"})) {
		t.Error("an older list was applied")
	}
	if applyUserRelayEvent(relayList(t, other, 20, nostr.Tag{"r", "wss://theirs.example.com"})) {
		t.Error("another author's list was applied")
	}

	stopUserRelays()
	stateMu.Lock()
	persisted := state.UserRelays
	stateMu.Unlock()
	if persisted != nil {
		t.Fatal("logging out kept the persisted list")
	}
}

func TestNip65ListsUsesLoadedUserList(t *testing.T) {
	noSystem(t) // a lookup through the sdk would panic
	sk := nostr.Generate()
	asUser(t, sk)
	startUserRelays(sk.Public())
	applyUserRelayEvent(relayList(t, sk, nostr.Now(),
		nostr.Tag{"r", "wss://out.example.com", "write"},
		nostr.Tag{"r", "wss://in.example.com", "read"},
	))

	write, read, found := nip65Lists(context.Background(), sk.Public())
	if !found || !slices.Equal(write, []string{"wss://out.example.com"}) || !slices.Equal(read, []string{"wss://in.example.com"}) {
		t.Fatalf("nip65Lists = %v, %v, %v", write, read, found)
	}
}

func TestOutboxDirectedFallsBackToUserRelays(t *testing.T) {
	noSystem(t)
	sk := nostr.Generate()
	asUser(t, sk)
	stateMu.Lock()
	state.Relays = []string{"wss://apps.example.com"}
	stateMu.Unlock()
	startUserRelays(sk.Public())
	// write relays only: no inbox to read from, so the filter has nowhere
	// to go but the fallback
	applyUserRelayEvent(relayList(t, sk, nostr.Now(), nostr.Tag{"r", "wss://out.example.com", "write"}))

	var got []string
	for _, df := range outboxDirected(context.Background(), nostr.Filter{Kinds: []nostr.Kind{1}}, nil, nil) {
		got = append(got, df.Relay)
	}
	if !slices.Equal(got, []string{"wss://out.example.com"}) {
		t.Fatalf("directed to %v, want the user's relays", got)
	}

	stopUserRelays()
	userKeyer = nil // signed out: no list to look up either
	got = nil
	for _, df := range outboxDirected(context.Background(), nostr.Filter{Kinds: []nostr.Kind{1}}, nil, nil) {
		got = append(got, df.Relay)
	}
	if !slices.Equal(got, []string{"wss://apps.example.com"}) {
		t.Fatalf("signed out, directed to %v, want the Settings relays", got)
	}
}

func TestUserRelaysLiveUpdate(t *testing.T) {
	withSystem(t)
	store := &slicestore.SliceStore{}
	store.Init()
	r := khatru.NewRelay()
	r.UseEventstore(store, 100)
	srv := httptest.NewServer(r)
	defer srv.Close()
	relay := "ws" + strings.TrimPrefix(srv.URL, "http")
	sys.RelayListRelays = sdk.NewRelayStream(relay)

	sk := nostr.Generate()
	asUser(t, sk)

	var rediscovered atomic.Int32
	old := rediscover
	rediscover = func() { rediscovered.Add(1) }
	t.Cleanup(func() { rediscover = old })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// the lists below name relays on a closed port, so the watcher's dials
	// to them fail at once; publishing goes through a pool of our own
	pool := nostr.NewPool()
	defer pool.Close("test over")
	pub := func(evt nostr.Event) {
		t.Helper()
		rr, err := pool.EnsureRelay(relay)
		if err != nil {
			t.Fatal(err)
		}
		if err := rr.Publish(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for !ok() {
			if ctx.Err() != nil {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	now := nostr.Now()
	pub(relayList(t, sk, now-100, nostr.Tag{"r", "ws://127.0.0.1:1/first"}))
	startUserRelays(sk.Public())
	waitUserRelays(5 * time.Second)
	waitFor("the first list", func() bool { return slices.Equal(UserWriteRelays(), []string{"ws://127.0.0.1:1/first"}) })

	discoveryRelays()                  // a discovery run saw the first list
	time.Sleep(200 * time.Millisecond) // let the live subscription open

	pub(relayList(t, sk, now-200, nostr.Tag{"r", "ws://127.0.0.1:1/older"}))
	pub(relayList(t, nostr.Generate(), now, nostr.Tag{"r", "ws://127.0.0.1:1/stranger"}))
	pub(relayList(t, sk, now, nostr.Tag{"r", "ws://127.0.0.1:1/second", "write"}))
	waitFor("the update", func() bool { return slices.Equal(UserWriteRelays(), []string{"ws://127.0.0.1:1/second"}) })
	waitFor("a rediscovery", func() bool { return rediscovered.Load() == 1 })

	stateMu.Lock()
	persisted := state.UserRelays
	stateMu.Unlock()
	if persisted == nil || !slices.Equal(persisted.Write, []string{"ws://127.0.0.1:1/second"}) {
		t.Fatalf("persisted = %+v", persisted)
	}
}
