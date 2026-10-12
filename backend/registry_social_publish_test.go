package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/khatru"
)

// ─── publish rig ─────────────────────────────────────────────────

// captureRelay is a khatru relay that records every event published to it,
// in arrival order, and rejects them all when reject is set.
func captureRelay(t *testing.T, reject bool) (string, func() []nostr.Event) {
	t.Helper()
	store := &slicestore.SliceStore{}
	store.Init()
	r := khatru.NewRelay()
	r.UseEventstore(store, 100)
	var mu sync.Mutex
	var got []nostr.Event
	r.OnEvent = func(ctx context.Context, evt nostr.Event) (bool, string) {
		mu.Lock()
		got = append(got, evt)
		mu.Unlock()
		if reject {
			return true, "blocked: test"
		}
		return false, ""
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), func() []nostr.Event {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

// socialPublishRig is socialRig with the publish seam pointed at urls and
// hint. It returns the parent author of every publish lookup.
func socialPublishRig(t *testing.T, hint string, urls ...string) *[]nostr.PubKey {
	t.Helper()
	socialRig(t)
	prevRelays, prevSign, prevActions := socialPublishRelays, socialSignTimeout, socialActions
	var mu sync.Mutex
	parents := &[]nostr.PubKey{}
	socialPublishRelays = func(_ context.Context, _ socialTarget, parent nostr.PubKey) ([]string, string) {
		mu.Lock()
		*parents = append(*parents, parent)
		mu.Unlock()
		return slices.Clone(urls), hint
	}
	socialActions = make(chan struct{}, 1)
	t.Cleanup(func() {
		socialPublishRelays, socialSignTimeout, socialActions = prevRelays, prevSign, prevActions
	})
	return parents
}

// publishFixture is a signer, a napplet author and one archived version of
// the author's "app".
type publishFixture struct {
	me      nostr.SecretKey
	author  nostr.SecretKey
	address string
	version nostr.Event
	got     func() []nostr.Event
	parents *[]nostr.PubKey
}

func newPublishFixture(t *testing.T, hint string, reject bool) publishFixture {
	t.Helper()
	url, got := captureRelay(t, reject)
	parents := socialPublishRig(t, hint, url)
	me := nostr.Generate()
	withUserKeyer(t, keyer.NewPlainKeySigner(me), me.Public())
	author := nostr.Generate()
	version := archivedVersion(t, author, "app")
	return publishFixture{
		me: me, author: author, address: nappletAddress(author.Public(), "app"),
		version: version, got: got, parents: parents,
	}
}

func saved(t *testing.T, evt nostr.Event) nostr.Event {
	t.Helper()
	if err := sys.Store.SaveEvent(evt); err != nil {
		t.Fatal(err)
	}
	return evt
}

func stored(id nostr.ID) bool {
	for range sys.Store.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		return true
	}
	return false
}

func wantTags(t *testing.T, what string, got, want nostr.Tags) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s tags\n got %v\nwant %v", what, got, want)
	}
}

func ownVote(t *testing.T, address string) string {
	t.Helper()
	summary, err := ServiceSocial(context.Background(), address, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Own == nil {
		t.Fatal("no own state with a signer")
	}
	return summary.Own.Vote
}

// stallKeyer signs nothing until its context ends.
type stallKeyer struct{ nostr.Keyer }

func (stallKeyer) SignEvent(ctx context.Context, _ *nostr.Event) error {
	<-ctx.Done()
	return ctx.Err()
}

// ─── votes ───────────────────────────────────────────────────────

func TestSocialVotePublishesExactTags(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	author := fx.author.Public().Hex()
	res, err := ServiceSocialVote(context.Background(), fx.address, "up", "")
	if err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 1 || got[0].Kind != nostr.KindReaction || got[0].Content != "+" {
		t.Fatalf("relay got %v", got)
	}
	wantTags(t, "35129 vote", got[0].Tags, nostr.Tags{
		{"e", fx.version.ID.Hex()}, {"p", author}, {"k", "35129"}, {"a", fx.address},
	})
	want := ServiceSocialVoteResult{Address: fx.address, Outcome: "published", EventID: got[0].ID.Hex(), Deleted: []string{}, Published: 1}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("result %+v, want %+v", res, want)
	}
	if !stored(got[0].ID) {
		t.Error("the vote was not kept locally")
	}
}

func TestSocialVoteRelayHint(t *testing.T) {
	fx := newPublishFixture(t, "wss://hint.example", false)
	if _, err := ServiceSocialVote(context.Background(), fx.address, "down", ""); err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 1 || got[0].Content != "-" {
		t.Fatalf("relay got %v", got)
	}
	wantTags(t, "hinted vote", got[0].Tags, nostr.Tags{
		{"e", fx.version.ID.Hex(), "wss://hint.example"}, {"p", fx.author.Public().Hex()}, {"k", "35129"}, {"a", fx.address},
	})
}

func TestSocialVoteRootAndSnapshotTags(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	pk := fx.author.Public()
	rootID := strings.Repeat("cd", 32)
	installRecord(t, Napp{ID: "root-app", Format: FormatNapplet, Kind: KindRootNapplet, Author: pk, EventID: rootID})
	root := "15129:" + pk.Hex() + ":"
	if _, err := ServiceSocialVote(context.Background(), root, "up", ""); err != nil {
		t.Fatal(err)
	}
	snapID := strings.Repeat("ab", 32)
	snapshot := "5129:" + pk.Hex() + ":" + snapID
	if _, err := ServiceSocialVote(context.Background(), snapshot, "up", ""); err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 2 {
		t.Fatalf("relay got %d events", len(got))
	}
	wantTags(t, "15129 vote", got[0].Tags, nostr.Tags{{"e", rootID}, {"p", pk.Hex()}, {"k", "15129"}, {"a", root}})
	wantTags(t, "5129 vote", got[1].Tags, nostr.Tags{{"e", snapID}, {"p", pk.Hex()}, {"k", "5129"}})
}

func TestSocialVoteVersionChoice(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	newer := validNapplet(t, fx.author, "app", "v2", 20)
	archiveManifest(newer, "")
	installed := validNapplet(t, fx.author, "app", "installed", 15)
	installRecord(t, installedFrom(t, installed))

	// no event_id: the installed version
	if _, err := ServiceSocialVote(ctx, fx.address, "up", ""); err != nil {
		t.Fatal(err)
	}
	// a known version
	if _, err := ServiceSocialVote(ctx, fx.address, "down", fx.version.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	// an unknown version publishes nothing
	before := len(fx.got())
	if _, err := ServiceSocialVote(ctx, fx.address, "up", strings.Repeat("ef", 32)); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("unknown version: %v", err)
	}
	if len(fx.got()) != before {
		t.Fatal("an unknown version published")
	}
	// not installed: the newest archived version
	solo := validNapplet(t, fx.author, "solo", "a", 30)
	archiveManifest(solo, "")
	if _, err := ServiceSocialVote(ctx, nappletAddress(fx.author.Public(), "solo"), "up", ""); err != nil {
		t.Fatal(err)
	}
	// no known version at all
	if _, err := ServiceSocialVote(ctx, nappletAddress(fx.author.Public(), "none"), "up", ""); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("no version: %v", err)
	}

	var versions []string
	for _, evt := range fx.got() {
		if evt.Kind == nostr.KindReaction {
			versions = append(versions, evt.Tags[0][1])
		}
	}
	want := []string{installed.ID.Hex(), fx.version.ID.Hex(), solo.ID.Hex()}
	if !slices.Equal(versions, want) {
		t.Fatalf("voted versions %v, want %v", versions, want)
	}
}

func TestSocialVoteChangePublishesThenDeletes(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	prior := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"e", fx.version.ID.Hex()}, {"a", fx.address}}, "+", nostr.Now()-10))
	if v := ownVote(t, fx.address); v != "up" {
		t.Fatalf("own vote before: %q", v)
	}
	res, err := ServiceSocialVote(context.Background(), fx.address, "down", "")
	if err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 2 || got[0].Kind != nostr.KindReaction || got[0].Content != "-" || got[1].Kind != nostr.KindDeletion {
		t.Fatalf("relay got %v", got)
	}
	wantTags(t, "vote deletion", got[1].Tags, nostr.Tags{{"e", prior.ID.Hex()}, {"k", "7"}})
	if !slices.Equal(res.Deleted, []string{prior.ID.Hex()}) || res.EventID != got[0].ID.Hex() || res.Published != 1 {
		t.Fatalf("result %+v", res)
	}
	if stored(prior.ID) {
		t.Error("the previous vote is still stored")
	}
	if !socialTombstoned(sys.Store, prior) {
		t.Error("the deletion was not kept as a tombstone")
	}
	// the memo from before the vote must not hide it
	if v := ownVote(t, fx.address); v != "down" {
		t.Fatalf("own vote after: %q", v)
	}
}

func TestSocialVoteUnchanged(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	prior := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"e", fx.version.ID.Hex()}, {"a", fx.address}}, "+", nostr.Now()-10))
	res, err := ServiceSocialVote(ctx, fx.address, "up", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "unchanged" || res.EventID != prior.ID.Hex() || res.Deleted == nil || len(res.Deleted) != 0 || res.Published != 0 {
		t.Fatalf("unchanged result %+v", res)
	}
	res, err = ServiceSocialVote(ctx, fx.address, "up", fx.version.ID.Hex())
	if err != nil || res.Outcome != "unchanged" {
		t.Fatalf("unchanged on the same version: %+v %v", res, err)
	}
	other := nappletAddress(fx.author.Public(), "other")
	archiveManifest(validNapplet(t, fx.author, "other", "x", 10), "")
	res, err = ServiceSocialVote(ctx, other, "none", "")
	if err != nil || res.Outcome != "unchanged" || res.EventID != "" {
		t.Fatalf("none without a vote: %+v %v", res, err)
	}
	if n := len(fx.got()); n != 0 {
		t.Fatalf("an unchanged vote published %d events", n)
	}

	// the same vote on another version is a change
	v2 := validNapplet(t, fx.author, "app", "v2", 20)
	archiveManifest(v2, "")
	res, err = ServiceSocialVote(ctx, fx.address, "up", v2.ID.Hex())
	if err != nil || res.Outcome != "published" {
		t.Fatalf("vote on another version: %+v %v", res, err)
	}
	got := fx.got()
	if len(got) != 2 || got[0].Tags[0][1] != v2.ID.Hex() || got[1].Kind != nostr.KindDeletion {
		t.Fatalf("relay got %v", got)
	}
	wantTags(t, "version change deletion", got[1].Tags, nostr.Tags{{"e", prior.ID.Hex()}, {"k", "7"}})
}

func TestSocialVoteNoRelayReached(t *testing.T) {
	fx := newPublishFixture(t, "", true)
	prior := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"e", fx.version.ID.Hex()}, {"a", fx.address}}, "+", nostr.Now()))
	res, err := ServiceSocialVote(context.Background(), fx.address, "down", "")
	if err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 1 || got[0].Kind != nostr.KindReaction {
		t.Fatalf("relay got %v, want only the vote attempt", got)
	}
	if res.Outcome != "published" || res.Published != 0 || res.Failed != 1 || len(res.Deleted) != 0 || res.Deleted == nil {
		t.Fatalf("result %+v", res)
	}
	if !stored(prior.ID) {
		t.Error("the previous vote was removed although nothing reached a relay")
	}
	if v := ownVote(t, fx.address); v != "down" {
		t.Fatalf("own vote: %q", v)
	}
}

func TestSocialVoteNone(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	prior := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"e", fx.version.ID.Hex()}, {"a", fx.address}}, "+", nostr.Now()-10))
	ownVote(t, fx.address)
	res, err := ServiceSocialVote(context.Background(), fx.address, "none", "")
	if err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 1 || got[0].Kind != nostr.KindDeletion {
		t.Fatalf("relay got %v", got)
	}
	wantTags(t, "withdrawal", got[0].Tags, nostr.Tags{{"e", prior.ID.Hex()}, {"k", "7"}})
	want := ServiceSocialVoteResult{Address: fx.address, Outcome: "published", Deleted: []string{prior.ID.Hex()}, Published: 1}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("result %+v, want %+v", res, want)
	}
	if v := ownVote(t, fx.address); v != "none" {
		t.Fatalf("own vote: %q", v)
	}
	if _, err := ServiceSocialVote(context.Background(), fx.address, "none", fx.version.ID.Hex()); !errors.Is(err, ErrServiceInvalidSocial) {
		t.Fatalf("none with an event id: %v", err)
	}
}

func TestSocialVoteDeletesAtMostTen(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	now := nostr.Now()
	var priors []nostr.Event
	for i := range 12 {
		content := "+"
		if i%2 == 1 {
			content = "-"
		}
		priors = append(priors, saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", fx.address}}, content, now-100+nostr.Timestamp(i))))
	}
	// an emoji reaction is not a vote and is never withdrawn
	emoji := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", fx.address}}, "🔥", now-50))
	res, err := ServiceSocialVote(context.Background(), fx.address, "none", "")
	if err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 1 {
		t.Fatalf("relay got %v", got)
	}
	want := nostr.Tags{}
	var wantIDs []string
	for i := 11; i >= 2; i-- {
		want = append(want, nostr.Tag{"e", priors[i].ID.Hex()})
		wantIDs = append(wantIDs, priors[i].ID.Hex())
	}
	want = append(want, nostr.Tag{"k", "7"})
	wantTags(t, "capped withdrawal", got[0].Tags, want)
	if !slices.Equal(res.Deleted, wantIDs) {
		t.Fatalf("deleted %v", res.Deleted)
	}
	if !stored(emoji.ID) {
		t.Error("an emoji reaction was withdrawn with the votes")
	}
}

// ─── reactions ───────────────────────────────────────────────────

func TestSocialReact(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	author := fx.author.Public().Hex()
	res, err := ServiceSocialReact(ctx, fx.address, "🔥", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceSocialReact(ctx, fx.address, ":pepe:", "https://cdn.example/p.png", fx.version.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 2 || got[0].Content != "🔥" || got[1].Content != ":pepe:" {
		t.Fatalf("relay got %v", got)
	}
	base := nostr.Tags{{"e", fx.version.ID.Hex()}, {"p", author}, {"k", "35129"}, {"a", fx.address}}
	wantTags(t, "emoji", got[0].Tags, base)
	wantTags(t, "shortcode", got[1].Tags, append(slices.Clone(base), nostr.Tag{"emoji", "pepe", "https://cdn.example/p.png"}))
	want := ServiceSocialPublishResult{Address: fx.address, Outcome: "published", EventID: got[0].ID.Hex(), Published: 1}
	if res != want {
		t.Fatalf("result %+v, want %+v", res, want)
	}

	for _, c := range []struct{ emoji, url, event string }{
		{"+", "", ""}, {"-", "", ""}, {"", "", ""},
		{":pepe:", "", ""}, {"🔥", "https://cdn.example/p.png", ""},
		{":pepe:", "http://cdn.example/p.png", ""}, {":pepe:", "https://user@cdn.example/p.png", ""},
		{"‮🔥", "", ""}, {"🔥", "", "BAD"},
	} {
		if _, err := ServiceSocialReact(ctx, fx.address, c.emoji, c.url, c.event); !errors.Is(err, ErrServiceInvalidSocial) {
			t.Errorf("react %q %q %q: %v", c.emoji, c.url, c.event, err)
		}
	}
	if n := len(fx.got()); n != 2 {
		t.Fatalf("a rejected reaction published (%d events)", n)
	}
}

// ─── comments ────────────────────────────────────────────────────

func TestSocialCommentTopLevelTags(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	pk := fx.author.Public()
	rootID := strings.Repeat("cd", 32)
	installRecord(t, Napp{ID: "root-app", Format: FormatNapplet, Kind: KindRootNapplet, Author: pk, EventID: rootID})
	root := "15129:" + pk.Hex() + ":"
	snapID := strings.Repeat("ab", 32)
	snapshot := "5129:" + pk.Hex() + ":" + snapID
	for _, address := range []string{fx.address, snapshot, root} {
		res, err := ServiceSocialComment(ctx, address, "nice napplet", "")
		if err != nil || res.Outcome != "published" || res.Published != 1 {
			t.Fatalf("comment on %s: %+v %v", address, res, err)
		}
	}
	got := fx.got()
	if len(got) != 3 {
		t.Fatalf("relay got %d events", len(got))
	}
	for _, evt := range got {
		if evt.Kind != nostr.KindComment || evt.Content != "nice napplet" {
			t.Fatalf("comment %v", evt)
		}
	}
	a := pk.Hex()
	wantTags(t, "35129 comment", got[0].Tags, nostr.Tags{
		{"A", fx.address}, {"K", "35129"}, {"P", a}, {"a", fx.address}, {"e", fx.version.ID.Hex()}, {"k", "35129"}, {"p", a},
	})
	wantTags(t, "5129 comment", got[1].Tags, nostr.Tags{
		{"E", snapID}, {"K", "5129"}, {"P", a}, {"e", snapID}, {"k", "5129"}, {"p", a},
	})
	wantTags(t, "15129 comment", got[2].Tags, nostr.Tags{
		{"A", root}, {"K", "15129"}, {"P", a}, {"a", root}, {"e", rootID}, {"k", "15129"}, {"p", a},
	})
}

func TestSocialCommentReply(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	q := nostr.Generate()
	parent := saved(t, signedWith(t, q, nostr.KindComment, nostr.Tags{{"A", fx.address}, {"e", fx.version.ID.Hex()}}, "first", nostr.Now()-10))
	res, err := ServiceSocialComment(ctx, fx.address, "a reply", parent.ID.Hex())
	if err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 1 || res.EventID != got[0].ID.Hex() {
		t.Fatalf("relay got %v, result %+v", got, res)
	}
	a := fx.author.Public().Hex()
	wantTags(t, "reply", got[0].Tags, nostr.Tags{
		{"A", fx.address}, {"K", "35129"}, {"P", a}, {"e", parent.ID.Hex()}, {"k", "1111"}, {"p", q.Public().Hex()},
	})
	if ps := *fx.parents; len(ps) != 1 || ps[0] != q.Public() {
		t.Fatalf("publish lookup parents %v", ps)
	}
	// the reply shows as a reply of the parent
	page, err := ServiceComments(ctx, fx.address, parent.ID.Hex(), 0, 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != res.EventID || !page.Items[0].Own {
		t.Fatalf("replies %+v %v", page, err)
	}

	elsewhere := nappletAddress(fx.author.Public(), "other")
	foreign := saved(t, signedWith(t, q, nostr.KindComment, nostr.Tags{{"A", elsewhere}}, "elsewhere", nostr.Now()-10))
	gone := saved(t, signedWith(t, q, nostr.KindComment, nostr.Tags{{"A", fx.address}}, "gone", nostr.Now()-10))
	saved(t, signedWith(t, q, nostr.KindDeletion, nostr.Tags{{"e", gone.ID.Hex()}, {"k", "1111"}}, "", nostr.Now()-5))
	for name, id := range map[string]string{
		"other address": foreign.ID.Hex(), "unknown": strings.Repeat("ef", 32), "deleted": gone.ID.Hex(),
	} {
		if _, err := ServiceSocialComment(ctx, fx.address, "reply", id); !errors.Is(err, ErrServiceNotFound) {
			t.Errorf("%s parent: %v", name, err)
		}
	}
	for _, content := range []string{"", "  \n\t", strings.Repeat("x", socialMaxContent+1)} {
		if _, err := ServiceSocialComment(ctx, fx.address, content, ""); !errors.Is(err, ErrServiceInvalidSocial) {
			t.Errorf("content of %d bytes: %v", len(content), err)
		}
	}
	if _, err := ServiceSocialComment(ctx, fx.address, "hi", "xyz"); !errors.Is(err, ErrServiceInvalidSocial) {
		t.Errorf("bad parent id: %v", err)
	}
	if n := len(fx.got()); n != 1 {
		t.Fatalf("a refused comment published (%d events)", n)
	}
}

// ─── deletions ───────────────────────────────────────────────────

func TestSocialDelete(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	reaction := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", fx.address}}, "🔥", nostr.Now()-10))
	comment := saved(t, signedWith(t, fx.me, nostr.KindComment, nostr.Tags{{"A", fx.address}}, "mine", nostr.Now()-10))
	foreign := saved(t, signedWith(t, nostr.Generate(), nostr.KindReaction, nostr.Tags{{"a", fx.address}}, "🔥", nostr.Now()-10))
	elsewhere := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", nappletAddress(fx.author.Public(), "other")}}, "🔥", nostr.Now()-10))

	for name, id := range map[string]string{
		"foreign": foreign.ID.Hex(), "unknown": strings.Repeat("ef", 32), "other address": elsewhere.ID.Hex(),
	} {
		if _, err := ServiceSocialDelete(ctx, fx.address, id); !errors.Is(err, ErrServiceNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ServiceSocialDelete(ctx, fx.address, "BAD"); !errors.Is(err, ErrServiceInvalidSocial) {
		t.Errorf("bad id: %v", err)
	}
	if n := len(fx.got()); n != 0 {
		t.Fatalf("a refused deletion published (%d events)", n)
	}

	res, err := ServiceSocialDelete(ctx, fx.address, reaction.ID.Hex())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceSocialDelete(ctx, fx.address, comment.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	got := fx.got()
	if len(got) != 2 {
		t.Fatalf("relay got %v", got)
	}
	wantTags(t, "reaction deletion", got[0].Tags, nostr.Tags{{"e", reaction.ID.Hex()}, {"k", "7"}})
	wantTags(t, "comment deletion", got[1].Tags, nostr.Tags{{"e", comment.ID.Hex()}, {"k", "1111"}})
	want := ServiceSocialDeleteResult{Address: fx.address, Outcome: "published", DeletionID: got[0].ID.Hex(), Published: 1}
	if res != want {
		t.Fatalf("result %+v, want %+v", res, want)
	}
	for _, evt := range []nostr.Event{reaction, comment} {
		if stored(evt.ID) || !socialTombstoned(sys.Store, evt) {
			t.Errorf("%d not removed with a tombstone", evt.Kind)
		}
	}
	if !stored(foreign.ID) || !stored(elsewhere.ID) {
		t.Error("an event not deleted was removed")
	}
	// deleting it again is not found
	if _, err := ServiceSocialDelete(ctx, fx.address, reaction.ID.Hex()); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("second deletion: %v", err)
	}
}

// ─── signer and publish failures ─────────────────────────────────

func allSocialActions(ctx context.Context, address, id string) []error {
	_, voteErr := ServiceSocialVote(ctx, address, "up", "")
	_, reactErr := ServiceSocialReact(ctx, address, "🔥", "", "")
	_, commentErr := ServiceSocialComment(ctx, address, "hi", "")
	_, deleteErr := ServiceSocialDelete(ctx, address, id)
	return []error{voteErr, reactErr, commentErr, deleteErr}
}

func TestSocialActionsNeedASigner(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	own := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", fx.address}}, "🔥", nostr.Now()-10))
	withUserKeyer(t, nil, nostr.ZeroPK)
	for i, err := range allSocialActions(context.Background(), fx.address, own.ID.Hex()) {
		if !errors.Is(err, ErrServiceSignerUnavailable) {
			t.Errorf("action %d: %v", i, err)
		}
	}
	if n := len(fx.got()); n != 0 {
		t.Fatalf("published %d events without a signer", n)
	}
}

func TestSocialActionSignTimeout(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	own := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", fx.address}}, "🔥", nostr.Now()-10))
	withUserKeyer(t, stallKeyer{keyer.NewPlainKeySigner(fx.me)}, fx.me.Public())
	socialSignTimeout = 50 * time.Millisecond
	for i, err := range allSocialActions(context.Background(), fx.address, own.ID.Hex()) {
		if !errors.Is(err, ErrServiceTimeout) {
			t.Errorf("action %d: %v", i, err)
		}
	}
	if n := len(fx.got()); n != 0 {
		t.Fatalf("published %d events after a timeout", n)
	}
}

func TestSocialActionWrongKeySigner(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	own := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", fx.address}}, "🔥", nostr.Now()-10))
	withUserKeyer(t, keyer.NewPlainKeySigner(nostr.Generate()), fx.me.Public())
	before := storedIDs(t)
	for i, err := range allSocialActions(context.Background(), fx.address, own.ID.Hex()) {
		if !errors.Is(err, ErrServiceUnavailable) {
			t.Errorf("action %d: %v", i, err)
		}
	}
	if n := len(fx.got()); n != 0 {
		t.Fatalf("published %d events signed by another key", n)
	}
	if after := storedIDs(t); !slices.Equal(before, after) {
		t.Fatalf("stored %v, want %v", after, before)
	}
	for range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}}, 1) {
		t.Fatal("a deletion signed by another key was stored")
	}
}

func TestSocialActionKeepsOtherOwnEvents(t *testing.T) {
	fx := newPublishFixture(t, "", false)
	ctx := context.Background()
	// regular kinds are never replaced: the store's ReplaceEvent would drop
	// every older event of the same kind and author
	emoji := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", fx.address}}, "🔥", nostr.Now()-10))
	comment := saved(t, signedWith(t, fx.me, nostr.KindComment, nostr.Tags{{"A", fx.address}}, "mine", nostr.Now()-10))
	other := saved(t, signedWith(t, fx.me, nostr.KindReaction, nostr.Tags{{"a", nappletAddress(fx.author.Public(), "other")}}, "+", nostr.Now()-10))
	tomb := saved(t, signedWith(t, fx.me, nostr.KindDeletion, nostr.Tags{{"e", strings.Repeat("ef", 32)}}, "", nostr.Now()-10))
	if _, err := ServiceSocialVote(ctx, fx.address, "up", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceSocialComment(ctx, fx.address, "again", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ServiceSocialDelete(ctx, fx.address, emoji.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	for _, evt := range []nostr.Event{comment, other, tomb} {
		if !stored(evt.ID) {
			t.Errorf("own kind %d was dropped by an unrelated publish", evt.Kind)
		}
	}
}

func TestSocialActionResultsCarryNoRelays(t *testing.T) {
	fx := newPublishFixture(t, "wss://hint.example", false)
	ctx := context.Background()
	var results []any
	vote, err := ServiceSocialVote(ctx, fx.address, "up", "")
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := ServiceSocialVote(ctx, fx.address, "up", "")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ServiceSocialVote(ctx, fx.address, "down", "")
	if err != nil {
		t.Fatal(err)
	}
	react, err := ServiceSocialReact(ctx, fx.address, ":pepe:", "https://cdn.example/p.png", "")
	if err != nil {
		t.Fatal(err)
	}
	comment, err := ServiceSocialComment(ctx, fx.address, "hi", "")
	if err != nil {
		t.Fatal(err)
	}
	del, err := ServiceSocialDelete(ctx, fx.address, react.EventID)
	if err != nil {
		t.Fatal(err)
	}
	results = append(results, vote, unchanged, changed, react, comment, del)
	keys := [][]string{
		{"address", "deleted", "event_id", "failed", "outcome", "published"},
		{"address", "deleted", "event_id", "failed", "outcome", "published"},
		{"address", "deleted", "event_id", "failed", "outcome", "published"},
		{"address", "event_id", "failed", "outcome", "published"},
		{"address", "event_id", "failed", "outcome", "published"},
		{"address", "deletion_id", "failed", "outcome", "published"},
	}
	for i, res := range results {
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"ws://", "wss://", "127.0.0.1", "hint.example", "relays", "/"} {
			if strings.Contains(string(raw), marker) {
				t.Errorf("result %d carries %q: %s", i, marker, raw)
			}
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		var got []string
		for k := range m {
			got = append(got, k)
		}
		slices.Sort(got)
		if !slices.Equal(got, keys[i]) {
			t.Errorf("result %d keys %v, want %v", i, got, keys[i])
		}
	}
}
