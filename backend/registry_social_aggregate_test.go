package backend

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

// ─── aggregation rig ─────────────────────────────────────────────

// aggTarget is a named napplet target with two known versions.
func aggTarget(t *testing.T) socialTarget {
	t.Helper()
	pk := nostr.Generate().Public()
	return socialTarget{
		Address:    nappletAddress(pk, "app"),
		Author:     pk,
		Kind:       KindNapplet,
		VersionIDs: []string{strings.Repeat("1", 64), strings.Repeat("2", 64)},
	}
}

// reactTo is a kind 7 by sk on the target's address, optionally on one
// version, at a fixed time.
func reactTo(t *testing.T, sk nostr.SecretKey, target socialTarget, version, content string, at nostr.Timestamp, extra ...nostr.Tag) nostr.Event {
	t.Helper()
	tags := nostr.Tags{{"a", target.Address}}
	if version != "" {
		tags = append(tags, nostr.Tag{"e", version})
	}
	tags = append(tags, extra...)
	return signedWith(t, sk, nostr.KindReaction, tags, content, at)
}

// commentOn is a kind 1111 by sk on the target, top-level when parent is
// empty and otherwise a reply to it.
func commentOn(t *testing.T, sk nostr.SecretKey, target socialTarget, parent, content string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	tags := nostr.Tags{{"A", target.Address}, {"K", "35129"}, {"P", target.Author.Hex()}}
	if parent == "" {
		tags = append(tags, nostr.Tag{"a", target.Address}, nostr.Tag{"k", "35129"})
	} else {
		tags = append(tags, nostr.Tag{"e", parent}, nostr.Tag{"k", "1111"})
	}
	return signedWith(t, sk, nostr.KindComment, tags, content, at)
}

func deletionOf(t *testing.T, sk nostr.SecretKey, ids ...string) nostr.Event {
	t.Helper()
	tags := nostr.Tags{}
	for _, id := range ids {
		tags = append(tags, nostr.Tag{"e", id})
	}
	return signedWith(t, sk, nostr.KindDeletion, tags, "", 100)
}

// ─── votes ───────────────────────────────────────────────────────

func TestSocialVotes(t *testing.T) {
	target := aggTarget(t)
	a, b, c, d := nostr.Generate(), nostr.Generate(), nostr.Generate(), nostr.Generate()
	tieUp := reactTo(t, c, target, "", "+", 30)
	tieDown := reactTo(t, c, target, "", "-", 30)
	// the lowest id wins a tie
	cWinsUp := bytes.Compare(tieUp.ID[:], tieDown.ID[:]) < 0
	reactions := []nostr.Event{
		reactTo(t, a, target, "", "+", 10),
		reactTo(t, a, target, "", "-", 20), // A's latest: down
		reactTo(t, b, target, "", "", 10),  // "" counts as up
		tieUp, tieDown,
		reactTo(t, d, target, "", "🔥", 40), // emoji is never a vote
	}
	got := socialSummary(target, reactions, nil, nil, nil)
	up, down := 1, 1
	if cWinsUp {
		up++
	} else {
		down++
	}
	want := ServiceSocialVotes{Up: up, Down: down, Score: up - down}
	if got.Votes != want {
		t.Fatalf("votes %+v, want %+v", got.Votes, want)
	}
}

// ─── emoji ───────────────────────────────────────────────────────

func TestSocialEmoji(t *testing.T) {
	target := aggTarget(t)
	a, b, c := nostr.Generate(), nostr.Generate(), nostr.Generate()
	reactions := []nostr.Event{
		reactTo(t, a, target, "", "🔥", 10),
		reactTo(t, a, target, "", "🔥", 11), // the same author counts once
		reactTo(t, b, target, "", "🔥", 12),
		reactTo(t, c, target, "", "🚀", 13),
		reactTo(t, a, target, "", ":pepe:", 14, nostr.Tag{"emoji", "pepe", "https://cdn.example/old.png"}),
		reactTo(t, b, target, "", ":pepe:", 15, nostr.Tag{"emoji", "pepe", "https://cdn.example/pepe.png"}),
		reactTo(t, c, target, "", ":pepe:", 16, nostr.Tag{"emoji", "pepe", "http://insecure.example/x.png"}),
		reactTo(t, a, target, "", ":bad:", 17, nostr.Tag{"emoji", "bad", "http://insecure.example/x.png"}),
		reactTo(t, a, target, "", "+", 18),
		reactTo(t, b, target, "", "-", 18),
		reactTo(t, c, target, "", "", 18),
	}
	got := socialSummary(target, reactions, nil, nil, nil).Emoji
	want := []ServiceSocialEmoji{
		{Content: ":pepe:", Shortcode: "pepe", URL: "https://cdn.example/pepe.png", Count: 3},
		{Content: "🔥", Count: 2},
		{Content: ":bad:", Shortcode: "bad", Count: 1},
		{Content: "🚀", Count: 1},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("emoji\n got %+v\nwant %+v", got, want)
	}
}

func TestSocialEmojiCapped(t *testing.T) {
	target := aggTarget(t)
	sk := nostr.Generate()
	var reactions []nostr.Event
	for i := range 25 {
		reactions = append(reactions, reactTo(t, sk, target, "", fmt.Sprintf("e%02d", i), nostr.Timestamp(10+i)))
	}
	got := socialSummary(target, reactions, nil, nil, nil).Emoji
	if len(got) != 20 || got[0].Content != "e00" || got[19].Content != "e19" {
		t.Fatalf("capped emoji: %d %+v", len(got), got)
	}
}

// ─── by_event ────────────────────────────────────────────────────

func TestSocialByEvent(t *testing.T) {
	target := aggTarget(t)
	v1, v2 := target.VersionIDs[0], target.VersionIDs[1]
	unknown := strings.Repeat("9", 64)
	a, b, c, d, e := nostr.Generate(), nostr.Generate(), nostr.Generate(), nostr.Generate(), nostr.Generate()
	reactions := []nostr.Event{
		reactTo(t, a, target, v1, "+", 10),
		reactTo(t, a, target, v2, "-", 20), // A's latest vote, in v2
		reactTo(t, b, target, v1, "+", 10),
		reactTo(t, c, target, "", "+", 10),      // by a only: ""
		reactTo(t, d, target, unknown, "-", 10), // unknown version: ""
		reactTo(t, e, target, v1, "🔥", 10),
		reactTo(t, e, target, v1, "🔥", 11), // one author, one emoji
		reactTo(t, e, target, v1, "🚀", 12),
	}
	summary := socialSummary(target, reactions, nil, nil, nil)
	want := []ServiceSocialByEvent{
		{EventID: v1, Up: 1, Emoji: 2},
		{EventID: "", Up: 1, Down: 1},
		{EventID: v2, Down: 1},
	}
	if !slices.Equal(summary.ByEvent, want) {
		t.Fatalf("by_event\n got %+v\nwant %+v", summary.ByEvent, want)
	}
	up, down := 0, 0
	for _, bucket := range summary.ByEvent {
		up += bucket.Up
		down += bucket.Down
	}
	if up != summary.Votes.Up || down != summary.Votes.Down {
		t.Fatalf("by_event sums %d/%d, votes %+v", up, down, summary.Votes)
	}
}

// ─── deletions ───────────────────────────────────────────────────

func TestSocialDeletionsOnlyByAuthor(t *testing.T) {
	target := aggTarget(t)
	a, b, stranger := nostr.Generate(), nostr.Generate(), nostr.Generate()
	deleted := reactTo(t, a, target, "", "+", 10)
	kept := reactTo(t, b, target, "", "+", 10)
	comment := commentOn(t, b, target, "", "hi", 10)
	deletions := []nostr.Event{
		deletionOf(t, a, deleted.ID.Hex()),
		deletionOf(t, stranger, kept.ID.Hex(), comment.ID.Hex()),
	}
	summary := socialSummary(target, []nostr.Event{deleted, kept}, []nostr.Event{comment}, deletions, nil)
	if summary.Votes != (ServiceSocialVotes{Up: 1, Score: 1}) {
		t.Fatalf("votes %+v", summary.Votes)
	}
	if summary.Comments.Count != 1 {
		t.Fatalf("a stranger deleted a comment: %+v", summary.Comments)
	}
	live := socialLive([]nostr.Event{deleted, kept}, deletions)
	if len(live) != 1 || live[0].ID != kept.ID {
		t.Fatalf("live %v", live)
	}
}

// ─── own ─────────────────────────────────────────────────────────

func TestSocialOwn(t *testing.T) {
	target := aggTarget(t)
	v1 := target.VersionIDs[0]
	me, other := nostr.Generate(), nostr.Generate()
	myVote := reactTo(t, me, target, v1, "+", 20)
	fire := reactTo(t, me, target, v1, "🔥", 30)
	rocket := reactTo(t, me, target, "", "🚀", 40)
	reactions := []nostr.Event{
		reactTo(t, me, target, v1, "-", 10),
		myVote, fire, rocket,
		reactTo(t, other, target, v1, "-", 50),
		reactTo(t, other, target, v1, "👀", 50),
	}
	if own := socialSummary(target, reactions, nil, nil, nil).Own; own != nil {
		t.Fatalf("own without a viewer: %+v", own)
	}
	pk := me.Public()
	own := socialSummary(target, reactions, nil, nil, &pk).Own
	if own == nil || own.Vote != "up" || own.VoteEventID != myVote.ID.Hex() {
		t.Fatalf("own %+v", own)
	}
	want := []ServiceSocialOwnReaction{
		{ID: rocket.ID.Hex(), Content: "🚀", EventID: ""},
		{ID: fire.ID.Hex(), Content: "🔥", EventID: v1},
	}
	if !slices.Equal(own.Reactions, want) {
		t.Fatalf("own reactions %+v", own.Reactions)
	}
	nobody := nostr.Generate().Public()
	own = socialSummary(target, reactions, nil, nil, &nobody).Own
	if own == nil || own.Vote != "none" || own.VoteEventID != "" || own.Reactions == nil || len(own.Reactions) != 0 {
		t.Fatalf("own of a quiet viewer %+v", own)
	}
}

// ─── comments ────────────────────────────────────────────────────

func TestSocialCommentTree(t *testing.T) {
	target := aggTarget(t)
	a, b := nostr.Generate(), nostr.Generate()
	root := commentOn(t, a, target, "", "root", 10)
	versionTop := signedWith(t, b, nostr.KindComment, nostr.Tags{
		{"A", target.Address}, {"E", target.VersionIDs[0]}, {"e", target.VersionIDs[0]},
	}, "on a version", 20)
	reply1 := commentOn(t, b, target, root.ID.Hex(), "reply one", 30)
	reply2 := commentOn(t, a, target, root.ID.Hex(), "reply two", 40)
	nested := commentOn(t, a, target, reply1.ID.Hex(), "nested", 50)
	missing := strings.Repeat("7", 64)
	orphan := commentOn(t, b, target, missing, "orphan ‮ text", 60)
	comments := []nostr.Event{root, versionTop, reply1, reply2, nested, orphan}

	pk := a.Public()
	top := commentItems(target, comments, "", &pk)
	ids := func(items []ServiceComment) []string {
		out := []string{}
		for _, c := range items {
			out = append(out, c.ID)
		}
		return out
	}
	if got, want := ids(top), []string{orphan.ID.Hex(), versionTop.ID.Hex(), root.ID.Hex()}; !slices.Equal(got, want) {
		t.Fatalf("top level %v, want %v", got, want)
	}
	if top[0].ParentID != missing || top[0].Content != "orphan  text" || top[0].Own {
		t.Fatalf("orphan %+v", top[0])
	}
	if top[1].ParentID != "" || top[2].ParentID != "" || top[2].ReplyCount != 2 || !top[2].Own {
		t.Fatalf("top-level items %+v", top)
	}
	replies := commentItems(target, comments, root.ID.Hex(), &pk)
	if got, want := ids(replies), []string{reply2.ID.Hex(), reply1.ID.Hex()}; !slices.Equal(got, want) {
		t.Fatalf("replies %v, want %v", got, want)
	}
	if replies[1].ReplyCount != 1 || replies[1].ParentID != root.ID.Hex() || replies[1].Own {
		t.Fatalf("reply %+v", replies[1])
	}
	if got := commentItems(target, comments, strings.Repeat("8", 64), nil); got == nil || len(got) != 0 {
		t.Fatalf("unknown parent %v", got)
	}

	summary := socialSummary(target, nil, comments, nil, nil)
	if summary.Comments.Count != 6 || summary.Comments.LatestAt == nil || *summary.Comments.LatestAt != 60 {
		t.Fatalf("comment summary %+v", summary.Comments)
	}
}

func TestSocialCommentTruncated(t *testing.T) {
	target := aggTarget(t)
	long := commentOn(t, nostr.Generate(), target, "", strings.Repeat("x", socialCommentRunes+5), 10)
	items := commentItems(target, []nostr.Event{long}, "", nil)
	if len(items) != 1 || !items[0].Truncated || len([]rune(items[0].Content)) != socialCommentRunes {
		t.Fatalf("truncated comment %+v", items)
	}
}

func TestSocialReactionItems(t *testing.T) {
	target := aggTarget(t)
	v1 := target.VersionIDs[0]
	a, b := nostr.Generate(), nostr.Generate()
	pepe := reactTo(t, a, target, v1, ":pepe:", 20, nostr.Tag{"emoji", "pepe", "https://cdn.example/pepe.png"})
	bad := reactTo(t, b, target, "", ":bad:", 30, nostr.Tag{"emoji", "bad", "http://insecure.example/x.png"})
	up := reactTo(t, b, target, v1, "+", 10)
	all := reactionItems(target, []nostr.Event{pepe, bad, up}, "")
	if len(all) != 3 || all[0].ID != bad.ID.Hex() || all[1].ID != pepe.ID.Hex() || all[2].ID != up.ID.Hex() {
		t.Fatalf("items %+v", all)
	}
	if all[0].URL != "" || all[0].Shortcode != "bad" || all[0].EventID != "" {
		t.Fatalf("insecure emoji item %+v", all[0])
	}
	if all[1].URL != "https://cdn.example/pepe.png" || all[1].Shortcode != "pepe" || all[1].EventID != v1 || all[1].Author != a.Public().Hex() {
		t.Fatalf("pepe item %+v", all[1])
	}
	only := reactionItems(target, []nostr.Event{pepe, bad, up}, v1)
	if len(only) != 2 || only[0].ID != pepe.ID.Hex() || only[1].ID != up.ID.Hex() {
		t.Fatalf("v1 items %+v", only)
	}
	if got := reactionItems(target, []nostr.Event{pepe}, strings.Repeat("9", 64)); got == nil || len(got) != 0 {
		t.Fatalf("unknown event %v", got)
	}
}
