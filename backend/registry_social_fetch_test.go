package backend

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// ─── social relay rig ────────────────────────────────────────────

// socialRelay is a khatru relay that answers every REQ with every event it
// was given, whatever the filter, and counts the REQs.
type socialRelay struct {
	url    string
	reqs   atomic.Int32
	mu     sync.Mutex
	events []nostr.Event
	// gate, when set, holds every answer until it closes.
	gate chan struct{}
}

func newSocialRelay(t *testing.T, events ...nostr.Event) *socialRelay {
	t.Helper()
	r := &socialRelay{events: events}
	r.url = khatruRelay(t, func(ctx context.Context, _ nostr.Filter) iter.Seq[nostr.Event] {
		r.reqs.Add(1)
		r.mu.Lock()
		events, gate := slices.Clone(r.events), r.gate
		r.mu.Unlock()
		return func(yield func(nostr.Event) bool) {
			if gate != nil {
				select {
				case <-gate:
				case <-ctx.Done():
					return
				}
			}
			for _, evt := range events {
				if !yield(evt) {
					return
				}
			}
		}
	})
	return r
}

func (r *socialRelay) set(events ...nostr.Event) {
	r.mu.Lock()
	r.events = events
	r.mu.Unlock()
}

// socialRig gives a test a fresh store with no relays configured, the
// social seams pointed at urls and the limits restored afterwards.
func socialRig(t *testing.T, urls ...string) {
	t.Helper()
	newUpdateRig(t)
	withSystem(t)
	prevRelays, prevLimits, prevFloor := socialRelays, socialLimits, socialRefreshFloor
	socialRelays = func(context.Context, socialTarget) []string { return slices.Clone(urls) }
	socialRefreshFloor = 0
	t.Cleanup(func() {
		socialRelays, socialLimits, socialRefreshFloor = prevRelays, prevLimits, prevFloor
		socialResetMemo()
	})
	socialResetMemo()
}

// socialStored is every kind 7 and 1111 in the store, by id.
func socialStored(t *testing.T) map[nostr.ID]nostr.Event {
	t.Helper()
	out := map[nostr.ID]nostr.Event{}
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindReaction, nostr.KindComment}}, 10000) {
		out[evt.ID] = evt
	}
	return out
}

func storedIDs(t *testing.T) []string {
	t.Helper()
	ids := []string{}
	for id := range socialStored(t) {
		ids = append(ids, id.Hex())
	}
	slices.Sort(ids)
	return ids
}

func idsOf(events ...nostr.Event) []string {
	ids := []string{}
	for _, evt := range events {
		ids = append(ids, evt.ID.Hex())
	}
	slices.Sort(ids)
	return ids
}

// archivedVersion archives a valid manifest for d and returns it.
func archivedVersion(t *testing.T, sk nostr.SecretKey, d string) nostr.Event {
	t.Helper()
	evt := validNapplet(t, sk, d, "v1", 10)
	archiveManifest(evt, "")
	return evt
}

// ─── ingest ──────────────────────────────────────────────────────

func TestSocialRefreshIngestsOnlyExactMatches(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	author := nostr.Generate()
	version := archivedVersion(t, author, "app")
	address := nappletAddress(author.Public(), "app")
	other := nappletAddress(author.Public(), "other")
	now := nostr.Now()
	x := nostr.Generate()
	vote := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}, {"e", version.ID.Hex()}}, "+", now)
	pepe := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}, {"emoji", "pepe", "https://cdn.example/pepe.png"}}, ":pepe:", now)
	comment := signedWith(t, x, nostr.KindComment, nostr.Tags{{"A", address}}, "hello", now)
	manyTags := nostr.Tags{{"a", address}}
	for range 200 {
		manyTags = append(manyTags, nostr.Tag{"t", "x"})
	}
	forgedID := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "🔥", now)
	forgedID.Content = "🚀"
	bad := []nostr.Event{
		signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", other}}, "+", now),
		signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", now+3600),
		signedWith(t, x, nostr.KindReaction, manyTags, "+", now),
		signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, strings.Repeat("x", 65), now),
		signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "a b", now),
		signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "x‮", now),
		signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, ":pepe:", now),
		signedWith(t, x, nostr.KindComment, nostr.Tags{{"A", address}}, strings.Repeat("x", 8193), now),
		signedWith(t, x, nostr.KindComment, nostr.Tags{{"a", address}}, "lowercase only", now),
		forgedID,
	}
	relay.set(append([]nostr.Event{vote, pepe, comment}, bad...)...)

	summary, err := ServiceSocial(t.Context(), address, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := storedIDs(t), idsOf(vote, pepe, comment); !slices.Equal(got, want) {
		t.Fatalf("stored %v, want %v", got, want)
	}
	for _, category := range SocialCategories() {
		s := summary.Sync[category]
		if s.Status != "answered" || s.SyncedAt == nil {
			t.Errorf("%s sync %+v", category, s)
		}
	}
	if summary.Votes.Up != 1 || len(summary.Emoji) != 1 || summary.Emoji[0].URL != "https://cdn.example/pepe.png" || summary.Comments.Count != 1 {
		t.Fatalf("summary %+v", summary)
	}
	if len(summary.ByEvent) == 0 || summary.Own != nil {
		t.Fatalf("summary %+v", summary)
	}
}

// ─── the long-identifier local fallback ──────────────────────────

func TestSocialLongIdentifierLocalFallback(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	author := nostr.Generate()
	pk := author.Public()
	d := strings.Repeat("d", 40)
	version := archivedVersion(t, author, d)
	address := nappletAddress(pk, d)
	if len(address) <= 100 {
		t.Fatalf("address too short for the fallback: %d", len(address))
	}
	x := nostr.Generate()
	byA := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}, {"p", pk.Hex()}}, "+", 10)
	byE := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"e", version.ID.Hex()}}, "🔥", 20)
	comment := signedWith(t, x, nostr.KindComment, nostr.Tags{{"A", address}, {"P", pk.Hex()}}, "long", 30)
	relay.set(byA, byE, comment)
	// a decoy that shares the 8-byte index prefix of the version id
	near := version.ID.Hex()[:16] + strings.Repeat("0", 48)
	if near == version.ID.Hex() {
		t.Fatal("decoy equals the version id")
	}
	decoy := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"e", near}, {"p", pk.Hex()}}, "-", 40)
	if err := sys.Store.SaveEvent(decoy); err != nil {
		t.Fatal(err)
	}

	if _, err := ServiceSocial(t.Context(), address, true, nil); err != nil {
		t.Fatal(err)
	}
	reactions, err := ServiceReactions(t.Context(), address, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, item := range reactions.Items {
		got = append(got, item.ID)
	}
	slices.Sort(got)
	if want := idsOf(byA, byE); !slices.Equal(got, want) || reactions.Total != 2 {
		t.Fatalf("reactions %v, want %v", got, want)
	}
	comments, err := ServiceComments(t.Context(), address, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments.Items) != 1 || comments.Items[0].ID != comment.ID.Hex() {
		t.Fatalf("comments %+v", comments)
	}
	byVersion, err := ServiceReactions(t.Context(), address, version.ID.Hex(), 0, 100)
	if err != nil || len(byVersion.Items) != 1 || byVersion.Items[0].ID != byE.ID.Hex() {
		t.Fatalf("by version %+v %v", byVersion, err)
	}

	// the pitfall is real: the store's #e index matches the decoy by prefix
	prefixHit := false
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindReaction}, Tags: nostr.TagMap{"e": {version.ID.Hex()}}}, 100) {
		prefixHit = prefixHit || evt.ID == decoy.ID
	}
	if !prefixHit {
		t.Fatal("the store no longer matches #e by prefix; the decoy proves nothing")
	}
	// and the local read drops it
	target, err := newSocialTarget(address)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := idsOf(socialHeld(target, nostr.KindReaction)...), idsOf(byA, byE); !slices.Equal(got, want) {
		t.Fatalf("held %v, want %v", got, want)
	}
}

// ─── caps ────────────────────────────────────────────────────────

func TestSocialCapsKeepOwnEvents(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	socialLimits.AuthorEmoji = 2
	socialLimits.AuthorComments = 2
	socialLimits.Reactions = 5
	socialLimits.Comments = 3
	me := nostr.Generate()
	withUserKeyer(t, keyer.NewPlainKeySigner(me), me.Public())
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x := nostr.Generate()
	var served, mine, xEmoji, xComments, votes []nostr.Event
	for i := range 4 {
		at := nostr.Timestamp(100 + i)
		xEmoji = append(xEmoji, signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "e"+string(rune('a'+i)), at))
		xComments = append(xComments, signedWith(t, x, nostr.KindComment, nostr.Tags{{"A", address}}, "x", at))
		mine = append(mine,
			signedWith(t, me, nostr.KindReaction, nostr.Tags{{"a", address}}, "m"+string(rune('a'+i)), at-50),
			signedWith(t, me, nostr.KindComment, nostr.Tags{{"A", address}}, "mine", at-50))
		votes = append(votes, signedWith(t, nostr.Generate(), nostr.KindReaction, nostr.Tags{{"a", address}}, "+", nostr.Timestamp(1+i)))
	}
	served = append(append(append(append(served, mine...), xEmoji...), xComments...), votes...)
	relay.set(served...)

	if _, err := ServiceSocial(t.Context(), address, true, nil); err != nil {
		t.Fatal(err)
	}
	stored := socialStored(t)
	for _, evt := range mine {
		if _, ok := stored[evt.ID]; !ok {
			t.Errorf("own event %s was dropped", evt.Content)
		}
	}
	// per author: only X's newest two emoji survive, and then the address
	// cap of 5 reactions drops the oldest non-own ones: every vote and X's
	// older emoji
	if _, ok := stored[xEmoji[3].ID]; !ok {
		t.Error("X's newest emoji was dropped")
	}
	for _, evt := range append(append([]nostr.Event{xEmoji[0], xEmoji[1], xEmoji[2]}, votes...), xComments...) {
		if _, ok := stored[evt.ID]; ok {
			t.Errorf("%d %q at %d survived the caps", evt.Kind, evt.Content, evt.CreatedAt)
		}
	}
	if len(stored) != len(mine)+1 {
		t.Fatalf("stored %d events, want %d", len(stored), len(mine)+1)
	}
}

// ─── deletions ───────────────────────────────────────────────────

func TestSocialTombstoneBlocksReingest(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x := nostr.Generate()
	r := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	keep := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "🔥", 10)
	relay.set(r, keep)
	if _, err := ServiceSocial(t.Context(), address, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := storedIDs(t); !slices.Equal(got, idsOf(r, keep)) {
		t.Fatalf("first refresh stored %v", got)
	}
	deletion := signedWith(t, x, nostr.KindDeletion, nostr.Tags{{"e", r.ID.Hex()}, {"k", "7"}}, "", 20)
	relay.set(r, keep, deletion)
	summary, err := ServiceSocial(t.Context(), address, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := storedIDs(t); !slices.Equal(got, idsOf(keep)) {
		t.Fatalf("after the deletion %v", got)
	}
	if summary.Votes.Up != 0 {
		t.Fatalf("a deleted vote counted: %+v", summary.Votes)
	}
	tombstones := 0
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}}, 10) {
		if evt.ID == deletion.ID {
			tombstones++
		}
	}
	if tombstones != 1 {
		t.Fatal("the deletion was not kept as a tombstone")
	}
	// the relay still serves r, without the deletion: it stays out
	relay.set(r, keep)
	if _, err := ServiceSocial(t.Context(), address, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := storedIDs(t); !slices.Equal(got, idsOf(keep)) {
		t.Fatalf("a tombstoned reaction came back: %v", got)
	}
}

func TestSocialForeignDeletionIgnored(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x, stranger := nostr.Generate(), nostr.Generate()
	s := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	foreign := signedWith(t, stranger, nostr.KindDeletion, nostr.Tags{{"e", s.ID.Hex()}}, "", 20)
	relay.set(s, foreign)
	summary, err := ServiceSocial(t.Context(), address, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := storedIDs(t); !slices.Equal(got, idsOf(s)) || summary.Votes.Up != 1 {
		t.Fatalf("stored %v, votes %+v", got, summary.Votes)
	}
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}}, 10) {
		t.Fatalf("a foreign deletion was stored: %s", evt.ID.Hex())
	}
}

// ─── concurrency and the floor ───────────────────────────────────

func TestSocialRefreshSingleflightAndFloor(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	socialRefreshFloor = time.Minute
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x := nostr.Generate()
	relay.set(signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10))
	gate := make(chan struct{})
	relay.mu.Lock()
	relay.gate = gate
	relay.mu.Unlock()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := ServiceSocial(t.Context(), address, true, nil)
			errs <- err
		})
	}
	// let both callers reach the flight before the relay answers
	deadline := time.Now().Add(5 * time.Second)
	for relay.reqs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// one #a filter per category (no version ids, so no #e/#E), then one
	// deletion chunk for the held reaction
	first := relay.reqs.Load()
	if first != 3 {
		t.Fatalf("%d relay queries for two concurrent refreshes, want 3", first)
	}
	summary, err := ServiceSocial(t.Context(), address, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if relay.reqs.Load() != first {
		t.Fatal("a refresh inside the floor asked the relay")
	}
	if s := summary.Sync[SocialReactions]; s.Status != "answered" || s.SyncedAt == nil {
		t.Fatalf("cached sync %+v", s)
	}
}

// ─── the status index ────────────────────────────────────────────

func TestSocialIndexEvictsLeastRecentNonInstalled(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	socialLimits.IndexEntries = 3
	x := nostr.Generate()

	installedKey := nostr.Generate()
	installedEvt := validNapplet(t, installedKey, "app", "v1", 10)
	installRecord(t, installedFrom(t, installedEvt))
	installed := nappletAddress(installedKey.Public(), "app")

	addresses := []string{installed}
	for range 4 {
		addresses = append(addresses, nappletAddress(nostr.Generate().Public(), "app"))
	}
	reactions := map[string]nostr.Event{}
	var all []nostr.Event
	for _, address := range addresses {
		evt := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
		reactions[address] = evt
		all = append(all, evt)
	}
	relay.set(all...)
	for _, address := range addresses {
		if _, err := ServiceSocial(t.Context(), address, true, []string{SocialReactions}); err != nil {
			t.Fatal(err)
		}
	}
	index := readSocialIndex(t)
	if len(index) != 3 {
		t.Fatalf("index has %d entries, want 3", len(index))
	}
	for _, address := range []string{installed, addresses[3], addresses[4]} {
		if _, ok := index[address]; !ok {
			t.Errorf("%s was evicted", address)
		}
	}
	stored := socialStored(t)
	for i, address := range addresses {
		_, held := stored[reactions[address].ID]
		if evicted := i == 1 || i == 2; held == evicted {
			t.Errorf("address %d: held %v", i, held)
		}
	}
	if s := socialSyncStatus(addresses[1])[SocialReactions]; s.Status != "not_requested" || s.SyncedAt != nil {
		t.Fatalf("evicted sync %+v", s)
	}
}

func readSocialIndex(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := sys.KVStore.Get([]byte(socialIndexKey))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]json.RawMessage{}
	if raw != nil {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// ─── no relays ───────────────────────────────────────────────────

func TestSocialNoRelaysUnavailable(t *testing.T) {
	socialRig(t)
	address := nappletAddress(nostr.Generate().Public(), "app")
	summary, err := ServiceSocial(t.Context(), address, true, []string{SocialComments})
	if err != nil {
		t.Fatal(err)
	}
	if s := summary.Sync[SocialComments]; s.Status != "unavailable" || s.SyncedAt == nil {
		t.Fatalf("comments sync %+v", s)
	}
	if s := summary.Sync[SocialReactions]; s.Status != "not_requested" || s.SyncedAt != nil {
		t.Fatalf("reactions sync %+v", s)
	}
}

func TestSocialFiltersNeverEmptyTagLists(t *testing.T) {
	pk := nostr.Generate().Public()
	named := socialTarget{Address: nappletAddress(pk, "app"), Author: pk, Kind: KindNapplet, VersionIDs: []string{}}
	for _, category := range SocialCategories() {
		filters := socialFilters(named, category)
		if len(filters) != 1 {
			t.Fatalf("%s: %d filters without version ids", category, len(filters))
		}
		for _, f := range filters {
			for name, values := range f.Tags {
				if len(values) == 0 {
					t.Fatalf("%s: empty #%s", category, name)
				}
			}
			if f.Limit != socialLimits.PerFilter {
				t.Fatalf("limit %d", f.Limit)
			}
		}
	}
	id := strings.Repeat("ab", 32)
	snapshot := socialTarget{Address: "5129:" + pk.Hex() + ":" + id, Author: pk, Kind: KindSnapshotNapplet, Snapshot: true, VersionIDs: []string{id}}
	reactions := socialFilters(snapshot, SocialReactions)
	if len(reactions) != 1 || len(reactions[0].Tags["e"]) != 1 || reactions[0].Tags["a"] != nil {
		t.Fatalf("snapshot reaction filters %+v", reactions)
	}
	comments := socialFilters(snapshot, SocialComments)
	if len(comments) != 1 || len(comments[0].Tags["E"]) != 1 || comments[0].Tags["A"] != nil {
		t.Fatalf("snapshot comment filters %+v", comments)
	}
}

// ─── uninstall ───────────────────────────────────────────────────

func TestSocialPrunedOnUninstall(t *testing.T) {
	newReclaimRig(t)
	withSystem(t)
	socialResetMemo()
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	if err := InstallNapp(v1); err != nil {
		t.Fatal(err)
	}
	address := v1.Address()
	me, x := nostr.Generate(), nostr.Generate()
	withUserKeyer(t, keyer.NewPlainKeySigner(me), me.Public())
	theirs := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	theirComment := signedWith(t, x, nostr.KindComment, nostr.Tags{{"A", address}}, "hi", 10)
	mine := signedWith(t, me, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	elsewhere := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", nappletAddress(sk.Public(), "other")}}, "+", 10)
	target, err := newSocialTarget(address)
	if err != nil {
		t.Fatal(err)
	}
	for _, evt := range []nostr.Event{theirs, theirComment, mine, elsewhere} {
		if err := sys.Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	socialRecordSync(target, map[string]string{SocialReactions: "answered"})
	if _, ok := readSocialIndex(t)[address]; !ok {
		t.Fatal("index entry missing before uninstall")
	}

	result, err := ServiceUninstall(t.Context(), address)
	if err != nil || result.Outcome != "removed" || !result.CleanupComplete {
		t.Fatalf("uninstall %+v %v", result, err)
	}
	if got, want := storedIDs(t), idsOf(mine, elsewhere); !slices.Equal(got, want) {
		t.Fatalf("after uninstall %v, want %v", got, want)
	}
	if _, ok := readSocialIndex(t)[address]; ok {
		t.Fatal("index entry survived uninstall")
	}
}

func TestSocialPruneWithoutSystem(t *testing.T) {
	// never panics or errors without a store or with a bad address
	pruneNappletSocial("bad")
	if sys == nil {
		pruneNappletSocial(nappletAddress(nostr.Generate().Public(), "app"))
	}
}

// ─── reads ───────────────────────────────────────────────────────

func TestSocialUnknownAddressSummary(t *testing.T) {
	socialRig(t)
	address := nappletAddress(nostr.Generate().Public(), "app")
	summary, err := ServiceSocial(t.Context(), address, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Address != address || summary.Votes != (ServiceSocialVotes{}) || len(summary.Emoji) != 0 || len(summary.ByEvent) != 0 ||
		summary.Comments.Count != 0 || summary.Comments.LatestAt != nil || summary.Own != nil {
		t.Fatalf("summary %+v", summary)
	}
	for _, category := range SocialCategories() {
		if s := summary.Sync[category]; s.Status != "not_requested" || s.SyncedAt != nil {
			t.Fatalf("%s sync %+v", category, s)
		}
	}
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"emoji":[]`, `"by_event":[]`, `"own":null`, `"latest_at":null`, `"synced_at":null`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %s", data, want)
		}
	}
	reactions, err := ServiceReactions(t.Context(), address, "", 0, 100)
	if err != nil || reactions.Items == nil || reactions.Total != 0 || reactions.NextOffset != nil {
		t.Fatalf("reactions %+v %v", reactions, err)
	}
	comments, err := ServiceComments(t.Context(), address, strings.Repeat("a", 64), 0, 100)
	if err != nil || comments.Items == nil || comments.Total != 0 {
		t.Fatalf("comments %+v %v", comments, err)
	}
}

func TestSocialServiceErrors(t *testing.T) {
	socialRig(t)
	address := nappletAddress(nostr.Generate().Public(), "app")
	if _, err := ServiceSocial(t.Context(), "bad", false, nil); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("bad address: %v", err)
	}
	for _, categories := range [][]string{{"mentions"}, {SocialReactions, SocialReactions}, {""}} {
		if _, err := ServiceSocial(t.Context(), address, false, categories); !errors.Is(err, ErrServiceInvalidSocial) {
			t.Fatalf("%v: %v", categories, err)
		}
	}
	if _, err := ServiceReactions(t.Context(), address, "XYZ", 0, 10); !errors.Is(err, ErrServiceInvalidSocial) {
		t.Fatalf("bad event id: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ServiceSocial(ctx, address, true, nil); !errors.Is(err, ErrServiceTimeout) {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestSocialAuthorNamePaged(t *testing.T) {
	socialRig(t)
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x := nostr.Generate()
	profile := signedWith(t, x, 0, nostr.Tags{}, `{"display_name":"Ann‮\nie","picture":"https://cdn.example/p.png"}`, 10)
	r := signedWith(t, x, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	for _, evt := range []nostr.Event{profile, r} {
		if err := sys.Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	page, err := ServiceReactions(t.Context(), address, "", 0, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].AuthorName != "Ann ie" {
		t.Fatalf("page %+v %v", page, err)
	}
}
