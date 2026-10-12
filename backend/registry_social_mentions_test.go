package backend

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// mentionsStored is the id of every kind 1 in the store, sorted.
func mentionsStored(t *testing.T) []string {
	t.Helper()
	ids := []string{}
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindTextNote}}, 10000) {
		ids = append(ids, evt.ID.Hex())
	}
	slices.Sort(ids)
	return ids
}

func mentionIDs(page ServiceMentionPage) []string {
	ids := []string{}
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

// ─── matching ────────────────────────────────────────────────────

func TestSocialMentionMatch(t *testing.T) {
	sk := nostr.Generate()
	pk := sk.Public()
	x := nostr.Generate()
	version := strings.Repeat("ab", 32)
	address := nappletAddress(pk, "app")
	other := nappletAddress(pk, "other")
	target := socialTarget{Address: address, Author: pk, Kind: KindNapplet, VersionIDs: []string{version}}
	near := version[:16] + strings.Repeat("0", 48)
	for _, tc := range []struct {
		name         string
		kind         nostr.Kind
		tags         nostr.Tags
		match, quote bool
	}{
		{"a mention", nostr.KindTextNote, nostr.Tags{{"a", address}}, true, false},
		{"q address", nostr.KindTextNote, nostr.Tags{{"q", address}}, true, true},
		{"q version", nostr.KindTextNote, nostr.Tags{{"q", version}}, true, true},
		{"a and q", nostr.KindTextNote, nostr.Tags{{"a", address}, {"q", address}}, true, true},
		{"e only", nostr.KindTextNote, nostr.Tags{{"e", version}}, false, false},
		{"other a", nostr.KindTextNote, nostr.Tags{{"a", other}}, false, false},
		{"other q", nostr.KindTextNote, nostr.Tags{{"q", other}}, false, false},
		{"uppercase A", nostr.KindTextNote, nostr.Tags{{"A", address}}, false, false},
		{"uppercase Q", nostr.KindTextNote, nostr.Tags{{"Q", address}}, false, false},
		{"q prefix decoy", nostr.KindTextNote, nostr.Tags{{"q", near}}, false, false},
		{"reaction with q", nostr.KindReaction, nostr.Tags{{"q", address}}, false, false},
		{"comment with q", nostr.KindComment, nostr.Tags{{"q", version}}, false, false},
	} {
		evt := signedWith(t, x, tc.kind, tc.tags, "hi", 10)
		if got := socialMatches(evt, target); got != tc.match {
			t.Errorf("%s: match %v", tc.name, got)
		}
		if tc.kind == nostr.KindTextNote {
			if got := socialMentionQuote(evt, target); got != tc.quote {
				t.Errorf("%s: quote %v", tc.name, got)
			}
		}
	}

	root := "15129:" + pk.Hex() + ":"
	rootTarget := socialTarget{Address: root, Author: pk, Kind: KindRootNapplet, VersionIDs: []string{}}
	if !socialMatches(signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", root}}, "", 10), rootTarget) {
		t.Error("15129 a mention did not match")
	}
	if !socialMatches(signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"q", root}}, "", 10), rootTarget) {
		t.Error("15129 quote did not match")
	}
	for _, tag := range []string{"a", "q"} {
		if socialMatches(signedWith(t, x, nostr.KindTextNote, nostr.Tags{{tag, strings.TrimSuffix(root, ":")}}, "", 10), rootTarget) {
			t.Errorf("15129 %s without the trailing colon matched", tag)
		}
	}

	snapID := strings.Repeat("cd", 32)
	snapAddress := "5129:" + pk.Hex() + ":" + snapID
	snapshot := socialTarget{Address: snapAddress, Author: pk, Kind: KindSnapshotNapplet, Snapshot: true, VersionIDs: []string{snapID}}
	quoted := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"q", snapID}}, "", 10)
	if !socialMatches(quoted, snapshot) || !socialMentionQuote(quoted, snapshot) {
		t.Error("a snapshot quote by id did not match")
	}
	for _, tag := range []string{"a", "q"} {
		if socialMatches(signedWith(t, x, nostr.KindTextNote, nostr.Tags{{tag, snapAddress}}, "", 10), snapshot) {
			t.Errorf("a snapshot named by %s address matched", tag)
		}
	}
}

func TestSocialMentionFilters(t *testing.T) {
	pk := nostr.Generate().Public()
	address := nappletAddress(pk, "app")
	v1, v2 := strings.Repeat("11", 32), strings.Repeat("22", 32)
	named := socialTarget{Address: address, Author: pk, Kind: KindNapplet, VersionIDs: []string{v1, v2}}
	filters := socialFilters(named, SocialMentions)
	if len(filters) != 2 {
		t.Fatalf("%d filters", len(filters))
	}
	if !slices.Equal(filters[0].Kinds, []nostr.Kind{nostr.KindTextNote}) || len(filters[0].Tags) != 1 || !slices.Equal(filters[0].Tags["a"], []string{address}) {
		t.Fatalf("address filter %+v", filters[0])
	}
	if !slices.Equal(filters[1].Kinds, []nostr.Kind{nostr.KindTextNote}) || len(filters[1].Tags) != 1 || !slices.Equal(filters[1].Tags["q"], []string{address, v1, v2}) {
		t.Fatalf("quote filter %+v", filters[1])
	}
	for _, f := range filters {
		if f.Limit != socialLimits.PerFilter {
			t.Fatalf("limit %d", f.Limit)
		}
	}
	// the version ids of the target are not changed by the address prefix
	if !slices.Equal(named.VersionIDs, []string{v1, v2}) {
		t.Fatalf("version ids changed: %v", named.VersionIDs)
	}

	bare := socialTarget{Address: address, Author: pk, Kind: KindNapplet, VersionIDs: []string{}}
	filters = socialFilters(bare, SocialMentions)
	if len(filters) != 2 || !slices.Equal(filters[0].Tags["a"], []string{address}) || !slices.Equal(filters[1].Tags["q"], []string{address}) {
		t.Fatalf("filters without versions %+v", filters)
	}

	// reactions and comments still never ask for the address in #e/#E
	for _, category := range []string{SocialReactions, SocialComments} {
		for _, f := range socialFilters(bare, category) {
			if f.Tags["e"] != nil || f.Tags["E"] != nil {
				t.Fatalf("%s asked an event tag without versions: %+v", category, f)
			}
		}
	}
}

// ─── refresh ─────────────────────────────────────────────────────

func TestSocialMentionsRefresh(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	author := nostr.Generate()
	version := archivedVersion(t, author, "app")
	address := nappletAddress(author.Public(), "app")
	other := nappletAddress(author.Public(), "other")
	now := nostr.Now()
	x := nostr.Generate()
	byA := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}}, "look at this", now-30)
	byQ := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"q", address}}, "quoting the address", now-20)
	byVersion := signedWith(t, nostr.Generate(), nostr.KindTextNote, nostr.Tags{{"q", version.ID.Hex()}}, "quoting a version", now-10)
	manyTags := nostr.Tags{{"a", address}}
	for range 200 {
		manyTags = append(manyTags, nostr.Tag{"t", "x"})
	}
	forged := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}}, "real", now)
	forged.Content = "forged"
	bad := []nostr.Event{
		signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", other}}, "wrong address", now),
		signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"e", version.ID.Hex()}}, "e only", now),
		signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"A", address}}, "uppercase", now),
		signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}}, strings.Repeat("x", 8193), now),
		signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}}, "future", now+3600),
		signedWith(t, x, nostr.KindTextNote, manyTags, "many tags", now),
		forged,
	}
	relay.set(append([]nostr.Event{byA, byQ, byVersion}, bad...)...)

	summary, err := ServiceSocial(t.Context(), address, true, []string{SocialMentions})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mentionsStored(t), idsOf(byA, byQ, byVersion); !slices.Equal(got, want) {
		t.Fatalf("stored %v, want %v", got, want)
	}
	if summary.Mentions.Count != 3 {
		t.Fatalf("mentions %+v", summary.Mentions)
	}
	if s := summary.Sync[SocialMentions]; s.Status != "answered" || s.SyncedAt == nil {
		t.Fatalf("mentions sync %+v", s)
	}
	for _, category := range []string{SocialReactions, SocialComments} {
		if s := summary.Sync[category]; s.Status != "not_requested" || s.SyncedAt != nil {
			t.Fatalf("%s sync %+v", category, s)
		}
	}

	page, err := ServiceMentions(t.Context(), address, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mentionIDs(page), []string{byVersion.ID.Hex(), byQ.ID.Hex(), byA.ID.Hex()}; !slices.Equal(got, want) {
		t.Fatalf("items %v, want %v", got, want)
	}
	if page.Total != 3 || page.NextOffset != nil {
		t.Fatalf("total %d next %v", page.Total, page.NextOffset)
	}
	for i, quote := range []bool{true, true, false} {
		item := page.Items[i]
		if item.Quote != quote || item.Truncated || item.Author == "" || item.CreatedAt == 0 {
			t.Errorf("item %d %+v", i, item)
		}
	}
	if page.Items[2].Content != "look at this" || page.Items[2].Author != x.Public().Hex() {
		t.Errorf("a mention %+v", page.Items[2])
	}
	first, err := ServiceMentions(t.Context(), address, 0, 2)
	if err != nil || len(first.Items) != 2 || first.Total != 3 || first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("first page %+v %v", first, err)
	}
}

// ─── the local post-filter ───────────────────────────────────────

func TestSocialMentionPrefixDecoyNeverListed(t *testing.T) {
	socialRig(t)
	author := nostr.Generate()
	version := archivedVersion(t, author, "app")
	address := nappletAddress(author.Public(), "app")
	x := nostr.Generate()
	near := version.ID.Hex()[:16] + strings.Repeat("0", 48)
	decoy := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"q", near}}, "decoy", 20)
	real := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"q", version.ID.Hex()}}, "real", 10)
	for _, evt := range []nostr.Event{decoy, real} {
		if err := sys.Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	page, err := ServiceMentions(t.Context(), address, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := mentionIDs(page); !slices.Equal(got, []string{real.ID.Hex()}) || page.Total != 1 {
		t.Fatalf("items %v", got)
	}
	summary, err := ServiceSocial(t.Context(), address, false, nil)
	if err != nil || summary.Mentions.Count != 1 {
		t.Fatalf("summary %+v %v", summary.Mentions, err)
	}
	// the pitfall is real: the store's #q index matches the decoy by prefix
	prefixHit := false
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindTextNote}, Tags: nostr.TagMap{"q": {version.ID.Hex()}}}, 100) {
		prefixHit = prefixHit || evt.ID == decoy.ID
	}
	if !prefixHit {
		t.Fatal("the store no longer matches #q by prefix; the decoy proves nothing")
	}
}

func TestSocialMentionLongIdentifierFallback(t *testing.T) {
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
	byA := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}, {"p", pk.Hex()}}, "mention", 10)
	byQ := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"q", address}, {"p", pk.Hex()}}, "quote", 20)
	byVersion := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"q", version.ID.Hex()}}, "version quote", 30)
	pOnly := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"p", pk.Hex()}}, "just the author", 40)
	relay.set(byA, byQ, byVersion, pOnly)
	if _, err := ServiceSocial(t.Context(), address, true, []string{SocialMentions}); err != nil {
		t.Fatal(err)
	}
	// pOnly is never stored: it does not name the napplet
	if got, want := mentionsStored(t), idsOf(byA, byQ, byVersion); !slices.Equal(got, want) {
		t.Fatalf("stored %v, want %v", got, want)
	}
	if err := sys.Store.SaveEvent(pOnly); err != nil {
		t.Fatal(err)
	}
	page, err := ServiceMentions(t.Context(), address, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mentionIDs(page), []string{byVersion.ID.Hex(), byQ.ID.Hex(), byA.ID.Hex()}; !slices.Equal(got, want) {
		t.Fatalf("items %v, want %v", got, want)
	}
}

// ─── sanitizing ──────────────────────────────────────────────────

func TestSocialMentionSanitized(t *testing.T) {
	socialRig(t)
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x := nostr.Generate()
	hostile := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}}, "one\ntwo\tthree‮evil\u0007bell", 20)
	long := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}}, strings.Repeat("é", 2001), 10)
	for _, evt := range []nostr.Event{hostile, long} {
		if err := sys.Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	page, err := ServiceMentions(t.Context(), address, 0, 100)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("page %+v %v", page, err)
	}
	if got := page.Items[0]; got.Content != "one\ntwo\tthreeevilbell" || got.Truncated {
		t.Fatalf("hostile %+v", got)
	}
	if got := page.Items[1]; got.Content != strings.Repeat("é", 2000) || !got.Truncated {
		t.Fatalf("long: %d runes, truncated %v", len([]rune(got.Content)), got.Truncated)
	}
}

// ─── caps ────────────────────────────────────────────────────────

func TestSocialMentionCaps(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	socialLimits.AuthorMentions = 2
	socialLimits.Mentions = 7
	me := nostr.Generate()
	withUserKeyer(t, keyer.NewPlainKeySigner(me), me.Public())
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	tags := nostr.Tags{{"a", address}}
	x, y, z := nostr.Generate(), nostr.Generate(), nostr.Generate()
	var mine, xs, ys, zs []nostr.Event
	for i := range 4 {
		mine = append(mine, signedWith(t, me, nostr.KindTextNote, tags, "mine", nostr.Timestamp(1+i)))
		xs = append(xs, signedWith(t, x, nostr.KindTextNote, tags, "x", nostr.Timestamp(200+i)))
	}
	for i := range 2 {
		ys = append(ys, signedWith(t, y, nostr.KindTextNote, tags, "y", nostr.Timestamp(100+i)))
	}
	// empty notes are capped like any other, never taken for votes
	for i := range 3 {
		zs = append(zs, signedWith(t, z, nostr.KindTextNote, tags, "", nostr.Timestamp(300+i)))
	}
	relay.set(slices.Concat(mine, xs, ys, zs)...)
	if _, err := ServiceSocial(t.Context(), address, true, []string{SocialMentions}); err != nil {
		t.Fatal(err)
	}
	// per author: X keeps its newest two and Z (empty notes) its newest two.
	// That leaves the user's four, X's two, Y's two and Z's two: ten against
	// the cap of seven, so the three oldest that are not the user's go (Y's
	// two, then X's older one), and the user's own are never counted out. Had
	// Z's empty notes escaped the per-author cap, Z's oldest would be newer
	// than X's kept note and survive in its place.
	stored := mentionsStored(t)
	want := slices.Concat(idsOf(mine...), idsOf(xs[3], zs[1], zs[2]))
	slices.Sort(want)
	if !slices.Equal(stored, want) {
		t.Errorf("stored %v, want %v", stored, want)
	}
	for _, evt := range mine {
		if !slices.Contains(stored, evt.ID.Hex()) {
			t.Errorf("own mention at %d was dropped", evt.CreatedAt)
		}
	}
	for _, evt := range []nostr.Event{xs[0], xs[1], zs[0]} {
		if slices.Contains(stored, evt.ID.Hex()) {
			t.Errorf("%q at %d survived the per-author cap", evt.Content, evt.CreatedAt)
		}
	}
	for _, evt := range append(slices.Clone(ys), xs[2]) {
		if slices.Contains(stored, evt.ID.Hex()) {
			t.Errorf("%q at %d survived the address cap", evt.Content, evt.CreatedAt)
		}
	}
}

func TestSocialMentionAddressCapDropsOldest(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	socialLimits.AuthorMentions = 2
	socialLimits.Mentions = 3
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	tags := nostr.Tags{{"a", address}}
	x := nostr.Generate()
	var xs, others []nostr.Event
	for i := range 4 {
		xs = append(xs, signedWith(t, x, nostr.KindTextNote, tags, "x", nostr.Timestamp(100+i)))
	}
	for i := range 3 {
		others = append(others, signedWith(t, nostr.Generate(), nostr.KindTextNote, tags, "o", nostr.Timestamp(50+i)))
	}
	relay.set(slices.Concat(xs, others)...)
	if _, err := ServiceSocial(t.Context(), address, true, []string{SocialMentions}); err != nil {
		t.Fatal(err)
	}
	// X keeps its newest two; then the cap of three keeps the newest other
	if got, want := mentionsStored(t), idsOf(xs[2], xs[3], others[2]); !slices.Equal(got, want) {
		t.Fatalf("stored %v, want %v", got, want)
	}
}

// ─── deletions ───────────────────────────────────────────────────

func TestSocialMentionDeletion(t *testing.T) {
	relay := newSocialRelay(t)
	socialRig(t, relay.url)
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	x, y, stranger := nostr.Generate(), nostr.Generate(), nostr.Generate()
	m := signedWith(t, x, nostr.KindTextNote, nostr.Tags{{"a", address}}, "mine to delete", 10)
	n := signedWith(t, y, nostr.KindTextNote, nostr.Tags{{"q", address}}, "stays", 10)
	relay.set(m, n)
	summary, err := ServiceSocial(t.Context(), address, true, []string{SocialMentions})
	if err != nil || summary.Mentions.Count != 2 {
		t.Fatalf("first refresh %+v %v", summary.Mentions, err)
	}

	deletion := signedWith(t, x, nostr.KindDeletion, nostr.Tags{{"e", m.ID.Hex()}, {"k", "1"}}, "", 20)
	foreign := signedWith(t, stranger, nostr.KindDeletion, nostr.Tags{{"e", n.ID.Hex()}, {"k", "1"}}, "", 20)
	relay.set(m, n, deletion, foreign)
	summary, err = ServiceSocial(t.Context(), address, true, []string{SocialMentions})
	if err != nil {
		t.Fatal(err)
	}
	if got := mentionsStored(t); !slices.Equal(got, idsOf(n)) {
		t.Fatalf("after the deletion %v", got)
	}
	if summary.Mentions.Count != 1 {
		t.Fatalf("mentions %+v", summary.Mentions)
	}
	if !socialTombstoned(sys.Store, m) {
		t.Fatal("the author's deletion was not kept as a tombstone")
	}
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}}, 10) {
		if evt.ID == foreign.ID {
			t.Fatal("a foreign deletion was stored")
		}
	}

	// served again without the deletion, the mention stays out
	relay.set(m, n)
	summary, err = ServiceSocial(t.Context(), address, true, []string{SocialMentions})
	if err != nil {
		t.Fatal(err)
	}
	if got := mentionsStored(t); !slices.Equal(got, idsOf(n)) || summary.Mentions.Count != 1 {
		t.Fatalf("a tombstoned mention came back: %v %+v", got, summary.Mentions)
	}
	page, err := ServiceMentions(t.Context(), address, 0, 100)
	if err != nil || !slices.Equal(mentionIDs(page), []string{n.ID.Hex()}) {
		t.Fatalf("page %+v %v", page, err)
	}
}

// ─── prune ───────────────────────────────────────────────────────

func TestSocialMentionPrune(t *testing.T) {
	socialRig(t)
	me := nostr.Generate()
	withUserKeyer(t, keyer.NewPlainKeySigner(me), me.Public())
	author := nostr.Generate()
	address := nappletAddress(author.Public(), "app")
	theirs := signedWith(t, nostr.Generate(), nostr.KindTextNote, nostr.Tags{{"a", address}}, "theirs", 10)
	mine := signedWith(t, me, nostr.KindTextNote, nostr.Tags{{"q", address}}, "mine", 10)
	unrelated := signedWith(t, nostr.Generate(), nostr.KindTextNote, nostr.Tags{{"t", "x"}}, "unrelated", 10)
	for _, evt := range []nostr.Event{theirs, mine, unrelated} {
		if err := sys.Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	pruneNappletSocial(address)
	if got, want := mentionsStored(t), idsOf(mine, unrelated); !slices.Equal(got, want) {
		t.Fatalf("after prune %v, want %v", got, want)
	}
}

// ─── errors ──────────────────────────────────────────────────────

func TestSocialMentionsErrors(t *testing.T) {
	socialRig(t)
	if _, err := ServiceMentions(t.Context(), "bad", 0, 10); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("bad address: %v", err)
	}
	address := nappletAddress(nostr.Generate().Public(), "app")
	page, err := ServiceMentions(t.Context(), address, 0, 10)
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.Total != 0 || page.NextOffset != nil {
		t.Fatalf("unknown address %+v %v", page, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ServiceMentions(ctx, address, 0, 10); !errors.Is(err, ErrServiceTimeout) {
		t.Fatalf("cancelled: %v", err)
	}
}
