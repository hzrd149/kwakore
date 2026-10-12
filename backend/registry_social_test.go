package backend

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

// ─── social target ───────────────────────────────────────────────

func TestSocialTargetNamedNapplet(t *testing.T) {
	newUpdateRig(t)
	withManifestHistory(t)
	sk := nostr.Generate()
	pk := sk.Public()
	older := validNapplet(t, sk, "app", "old", 10)
	newer := validNapplet(t, sk, "app", "new", 20)
	other := validNapplet(t, sk, "other", "x", 30)
	installed := validNapplet(t, sk, "app", "installed", 15)
	for _, evt := range []nostr.Event{older, newer, other} {
		archiveManifest(evt, "")
	}
	installRecord(t, installedFrom(t, installed))

	address := nappletAddress(pk, "app")
	target, err := newSocialTarget(address)
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != KindNapplet || target.Author != pk || target.Snapshot || target.Address != address {
		t.Fatalf("target: %+v", target)
	}
	// archive order (newest first), then the installed event; another
	// address's manifest never joins
	want := []string{newer.ID.Hex(), older.ID.Hex(), installed.ID.Hex()}
	if !slices.Equal(target.VersionIDs, want) {
		t.Fatalf("version ids %v, want %v", target.VersionIDs, want)
	}

	// an installed event that is also archived is listed once
	archiveManifest(installed, "")
	target, err = newSocialTarget(address)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{newer.ID.Hex(), installed.ID.Hex(), older.ID.Hex()}
	if !slices.Equal(target.VersionIDs, want) {
		t.Fatalf("deduplicated version ids %v, want %v", target.VersionIDs, want)
	}
}

func TestSocialTargetRootTrailingColon(t *testing.T) {
	withManifestHistory(t)
	sk := nostr.Generate()
	pk := sk.Public()
	address := "15129:" + pk.Hex() + ":"
	target, err := newSocialTarget(address)
	if err != nil {
		t.Fatal(err)
	}
	if target.Address != address || target.Kind != KindRootNapplet || target.Snapshot {
		t.Fatalf("root target: %+v", target)
	}
	reactor := nostr.Generate()
	noColon := signedWith(t, reactor, nostr.KindReaction, nostr.Tags{{"a", "15129:" + pk.Hex()}}, "+", 10)
	withColon := signedWith(t, reactor, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	if socialMatches(noColon, target) {
		t.Error("a root address without its trailing colon matched")
	}
	if !socialMatches(withColon, target) {
		t.Error("the exact root address did not match")
	}
	upper := signedWith(t, reactor, nostr.KindComment, nostr.Tags{{"A", address}}, "hi", 10)
	lower := signedWith(t, reactor, nostr.KindComment, nostr.Tags{{"a", address}}, "hi", 10)
	if !socialMatches(upper, target) || socialMatches(lower, target) {
		t.Error("a comment must match by its root A tag only")
	}
}

func TestSocialTargetSnapshotByEventOnly(t *testing.T) {
	withManifestHistory(t)
	sk := nostr.Generate()
	pk := sk.Public()
	id := strings.Repeat("ab", 32)
	address := "5129:" + pk.Hex() + ":" + id
	target, err := newSocialTarget(address)
	if err != nil {
		t.Fatal(err)
	}
	if !target.Snapshot || target.Kind != KindSnapshotNapplet || !slices.Equal(target.VersionIDs, []string{id}) {
		t.Fatalf("snapshot target: %+v", target)
	}
	reactor := nostr.Generate()
	byA := signedWith(t, reactor, nostr.KindReaction, nostr.Tags{{"a", address}}, "+", 10)
	byE := signedWith(t, reactor, nostr.KindReaction, nostr.Tags{{"e", id}}, "+", 10)
	if socialMatches(byA, target) {
		t.Error("a snapshot matched by its a value")
	}
	if !socialMatches(byE, target) {
		t.Error("a snapshot did not match by its event id")
	}
	commentA := signedWith(t, reactor, nostr.KindComment, nostr.Tags{{"A", address}}, "hi", 10)
	commentLowerE := signedWith(t, reactor, nostr.KindComment, nostr.Tags{{"e", id}}, "hi", 10)
	commentE := signedWith(t, reactor, nostr.KindComment, nostr.Tags{{"E", id}}, "hi", 10)
	if socialMatches(commentA, target) || socialMatches(commentLowerE, target) {
		t.Error("a snapshot comment matched without its E tag")
	}
	if !socialMatches(commentE, target) {
		t.Error("a snapshot comment did not match by E")
	}
	note := signedWith(t, reactor, 1, nostr.Tags{{"e", id}}, "hi", 10)
	if socialMatches(note, target) {
		t.Error("a kind 1 matched")
	}
}

func TestSocialTargetRejectsInvalidAddresses(t *testing.T) {
	pk := nostr.Generate().Public()
	for _, address := range []string{
		"bad",
		"",
		"35129:" + strings.ToUpper(pk.Hex()) + ":app",
		"15129:" + pk.Hex() + ":d",
		"5129:" + pk.Hex() + ":xyz",
		"1:" + pk.Hex() + ":app",
	} {
		if _, err := newSocialTarget(address); !errors.Is(err, ErrServiceInvalidAddress) {
			t.Errorf("%q: %v", address, err)
		}
	}
}

// ─── text and url sanitizing ─────────────────────────────────────

func TestSocialText(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		limit     int
		want      string
		truncated bool
	}{
		{"plain", "hello", 10, "hello", false},
		{"newline and tab kept", "a\nb\tc", 10, "a\nb\tc", false},
		{"controls stripped", "a\x00b\rc\x1b\u0085d", 10, "abcd", false},
		{"bidi and format stripped", "a‮b⁦c​d", 10, "abcd", false},
		{"capped", "abcdef", 3, "abc", true},
		{"exact cap", "abc", 3, "abc", false},
		{"stripped tail is not truncation", "abc‮\x00", 3, "abc", false},
		{"runes not bytes", "ééé", 2, "éé", true},
	} {
		got, truncated := socialText(tc.raw, tc.limit)
		if got != tc.want || truncated != tc.truncated {
			t.Errorf("%s: %q %v, want %q %v", tc.name, got, truncated, tc.want, tc.truncated)
		}
	}
}

func TestSocialEmojiURL(t *testing.T) {
	long := "https://cdn.example/" + strings.Repeat("a", 512)
	for _, tc := range []struct{ raw, want string }{
		{"https://cdn.example/a.png", "https://cdn.example/a.png"},
		{"http://cdn.example/a.png", ""},
		{"javascript:alert(1)", ""},
		{"https://user:pass@cdn.example/a.png", ""},
		{"https://user@cdn.example/a.png", ""},
		{"https:///a.png", ""},
		{"https://cdn.example/%zz", ""},
		{long, ""},
		{"", ""},
		{"HTTPS://cdn.example/a.png", ""},
	} {
		if got := socialEmojiURL(tc.raw); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSocialCategoriesSorted(t *testing.T) {
	got := SocialCategories()
	if !slices.Equal(got, []string{SocialComments, SocialMentions, SocialReactions, SocialZaps}) || !slices.IsSorted(got) {
		t.Fatalf("categories %v", got)
	}
}
