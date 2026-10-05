package backend

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"fiatjaf.com/nostr"
)

// tiedPair is two validly signed versions of one address published in the
// same second, returned as (lower id, higher id).
func tiedPair(t *testing.T, sk nostr.SecretKey) (nostr.Event, nostr.Event) {
	t.Helper()
	index := NappPath{Path: "/index.html", Sha256: testArtifact}
	a := signedWith(t, sk, KindNapplet, nip5dTags("app", index), "first", 100)
	b := signedWith(t, sk, KindNapplet, nip5dTags("app", index), "second", 100)
	if bytes.Compare(a.ID[:], b.ID[:]) > 0 {
		a, b = b, a
	}
	return a, b
}

func TestPickLatestTieBreaksOnLowestID(t *testing.T) {
	sk := nostr.Generate()
	low, high := tiedPair(t, sk)

	for name, order := range map[string][]nostr.Event{
		"low first":  {low, high},
		"high first": {high, low},
	} {
		m := latestByAddress{}
		for _, evt := range order {
			m.add(evt)
		}
		if len(m) != 1 {
			t.Fatalf("%s: %d addresses, want 1", name, len(m))
		}
		if got := m[eventAddress(low)]; got.ID != low.ID {
			t.Errorf("%s: winner %s, want the lower id %s", name, got.ID.Hex(), low.ID.Hex())
		}
	}

	// created_at still comes first: a later event beats a lower id
	later := signedWith(t, sk, KindNapplet, nip5dTags("app", NappPath{Path: "/index.html", Sha256: testArtifact}), "later", 101)
	m := latestByAddress{}
	m.add(later)
	if m.add(low) {
		t.Error("an older event with a lower id replaced a later one")
	}
	if m[eventAddress(later)].ID != later.ID {
		t.Error("the later event lost")
	}
}

func TestPickLatestRejectsForgedID(t *testing.T) {
	sk := nostr.Generate()
	honest, other := tiedPair(t, sk)

	// a relay rewrites the id field of a validly signed event to all zeros:
	// the signature still verifies (it is checked against the recomputed
	// id), and 00…0 would sort before every real id
	forged := other
	forged.ID = nostr.ID{}
	if !forged.VerifySignature() {
		t.Fatal("fixture: the forged event's signature should still verify")
	}

	for name, order := range map[string][]nostr.Event{
		"forged first": {forged, honest},
		"forged last":  {honest, forged},
	} {
		m := latestByAddress{}
		for _, evt := range order {
			accepted := m.add(evt)
			if evt.ID == forged.ID && accepted {
				t.Errorf("%s: an event with a forged id was accepted", name)
			}
		}
		if got := m[eventAddress(honest)]; got.ID != honest.ID {
			t.Errorf("%s: winner %s, want the honest event %s", name, got.ID.Hex(), honest.ID.Hex())
		}
	}
}

func TestPickLatestIgnoresBadSignature(t *testing.T) {
	sk := nostr.Generate()
	evt := signedWith(t, sk, KindNapplet, nip5dTags("app", NappPath{Path: "/index.html", Sha256: testArtifact}), "", 100)
	evt.Sig[0] ^= 0xff
	m := latestByAddress{}
	if m.add(evt) || len(m) != 0 {
		t.Fatal("an event with a corrupted signature was accepted")
	}

	// and a kind the launcher does not follow never is
	note := signedWith(t, sk, 1, nostr.Tags{}, "hi", 100)
	if m.add(note) {
		t.Fatal("a kind 1 note was accepted as a manifest")
	}
}

func TestRootNappletAddressIgnoresDTag(t *testing.T) {
	sk := nostr.Generate()
	tags := nostr.Tags{{"d", "stray"}, {"path", "/index.html", testArtifact}, {"title", "Root"}}
	evt := signedWith(t, sk, KindRootNapplet, tags, "", 100)
	want := "15129:" + sk.Public().Hex() + ":"
	if got := eventAddress(evt); got != want {
		t.Fatalf("address = %q, want %q", got, want)
	}

	// a root napplet with and one without a stray d tag are one address
	bare := signedWith(t, sk, KindRootNapplet, nostr.Tags{{"path", "/index.html", testArtifact}, {"title", "Root"}}, "", 101)
	m := latestByAddress{}
	m.add(evt)
	m.add(bare)
	if len(m) != 1 || m[want].ID != bare.ID {
		t.Fatalf("root napplets split by d: %d addresses", len(m))
	}
	if n := nappFromLatest(evt); n.ID != want {
		t.Errorf("listed id = %q, want %q", n.ID, want)
	}
}

func TestInvalidLatestIsUnavailable(t *testing.T) {
	sk := nostr.Generate()
	index := NappPath{Path: "/index.html", Sha256: testArtifact}
	valid := signedWith(t, sk, KindNapplet, nip5dTags("app", index), "", 100)
	// the newer version has no path tags (and no x, server or content), so
	// it is no valid manifest in either schema
	invalid := signedWith(t, sk, KindNapplet, nostr.Tags{{"d", "app"}, {"title", "App"}}, "", 200)

	for name, order := range map[string][]nostr.Event{
		"valid first":   {valid, invalid},
		"invalid first": {invalid, valid},
	} {
		events := make(chan nostr.RelayEvent, len(order))
		for _, evt := range order {
			events <- nostr.RelayEvent{Event: evt}
		}
		close(events)

		var last []Napp
		collectDiscovery(events, make(chan struct{}), time.Hour, func(list []Napp, done bool) {
			last = list
		})
		if len(last) != 1 {
			t.Fatalf("%s: %d entries, want one for the address", name, len(last))
		}
		n := last[0]
		if n.Unavailable != reasonRequiredTags {
			t.Fatalf("%s: want the newer event listed unavailable (%q), got %+v", name, reasonRequiredTags, n)
		}
		if n.EventID != invalid.ID.Hex() || n.CreatedAt != invalid.CreatedAt {
			t.Errorf("%s: entry is not the newer event: id %s at %d", name, n.EventID, n.CreatedAt)
		}
		if n.ID != eventAddress(invalid) || n.D != "app" || !n.IsNapplet() || n.ManifestKind() != KindNapplet {
			t.Errorf("%s: identity: %+v", name, n)
		}
		if len(n.Paths) != 0 || len(n.Servers) != 0 || len(n.Actions) != 0 || n.ArtifactHash != "" {
			t.Errorf("%s: an unavailable entry carries something to install or run: %+v", name, n)
		}
	}

	// a valid winner is listed with its event id
	if n := nappFromLatest(valid); n.Unavailable != "" || n.EventID != valid.ID.Hex() {
		t.Errorf("valid winner: %+v", n)
	}
}

func TestUnavailableReasonCatalogue(t *testing.T) {
	index := NappPath{Path: "/index.html", Sha256: testArtifact}
	asset := NappPath{Path: "/a.js", Sha256: strings.Repeat("ab", 32)}
	web := func(edit func(nostr.Tags) nostr.Tags) nostr.Tags { return edit(validNappletTags()) }
	without := func(name string) func(nostr.Tags) nostr.Tags {
		return func(tags nostr.Tags) nostr.Tags {
			out := nostr.Tags{}
			for _, tag := range tags {
				if tag[0] != name {
					out = append(out, tag)
				}
			}
			return out
		}
	}
	plus := func(extra ...nostr.Tag) func(nostr.Tags) nostr.Tags {
		return func(tags nostr.Tags) nostr.Tags { return append(tags, extra...) }
	}
	swap := func(name string, tag nostr.Tag) func(nostr.Tags) nostr.Tags {
		return func(tags nostr.Tags) nostr.Tags { return append(without(name)(tags), tag) }
	}

	cases := []struct {
		name    string
		kind    nostr.Kind
		tags    nostr.Tags
		content string
		want    string
	}{
		// NIP-5D file lists
		{"nip5d traversal", KindNapplet, nip5dTags("app", index, NappPath{Path: "/../../evil", Sha256: testArtifact}), "", reasonFileList},
		{"nip5d duplicate path", KindNapplet, nip5dTags("app", index, index), "", reasonFileList},
		{"nip5d no index", KindNapplet, nip5dTags("app", asset), "", reasonFileList},
		{"nip5d bad sha", KindNapplet, nip5dTags("app", NappPath{Path: "/index.html", Sha256: "nothex"}), "", reasonFileList},
		{"root without paths", KindRootNapplet, validNappletTags(), "x", reasonFileList},
		{"nip5d wrong aggregate", KindNapplet, append(nip5dTags("app", index), nostr.Tag{"x", strings.Repeat("0", 64), "aggregate"}), "", reasonHashes},
		{"nip5d named without d", KindNapplet, nip5dTags("", index), "", reasonRequiredTags},

		// WEB-NAPPLET required tags
		{"two d", KindNapplet, web(plus(nostr.Tag{"d", "other"})), "x", reasonRequiredTags},
		{"no x", KindNapplet, web(without("x")), "x", reasonRequiredTags},
		{"two titles", KindNapplet, web(plus(nostr.Tag{"title", "Other"})), "x", reasonRequiredTags},
		{"no server", KindNapplet, web(without("server")), "x", reasonRequiredTags},
		{"empty content", KindNapplet, validNappletTags(), " ", reasonRequiredTags},
		{"legacy requires", KindNapplet, web(plus(nostr.Tag{"requires", "relay"})), "x", reasonRequiredTags},

		// WEB-NAPPLET conventions
		{"malformed i", KindNapplet, web(plus(nostr.Tag{"i", "bogus"})), "x", reasonConventions},
		{"convention twice", KindNapplet, web(plus(nostr.Tag{"i", "napplet:feed/open"})), "x", reasonConventions},
		{"z not a token", KindNapplet, web(swap("z", nostr.Tag{"z", "Not A Token"})), "x", reasonConventions},
		{"convention without z", KindNapplet, web(without("z")), "x", reasonConventions},
		{"malformed R", KindNapplet, web(plus(nostr.Tag{"R", "Bad"})), "x", reasonConventions},
	}
	for _, c := range cases {
		evt := signedEvent(t, c.kind, c.tags, c.content)
		n := nappFromLatest(evt)
		if n.Unavailable != c.want {
			t.Errorf("%s: Unavailable = %q, want %q", c.name, n.Unavailable, c.want)
		}
	}

	// anything without a category is the default, also through wrapping
	if got := unavailableReason(errors.New("something else")); got != reasonManifest {
		t.Errorf("uncategorized: %q", got)
	}
	wrapped := fmt.Errorf("context: %w", invalidManifest(reasonHashes, errors.New("mismatch")))
	if got := unavailableReason(wrapped); got != reasonHashes {
		t.Errorf("wrapped: %q", got)
	}
}

func TestUnavailableReasonNeverCarriesAuthorText(t *testing.T) {
	index := NappPath{Path: "/index.html", Sha256: testArtifact}
	hostile := "/../‮evil"
	evt := signedEvent(t, KindNapplet, nip5dTags("app", index, NappPath{Path: hostile, Sha256: testArtifact}), "")

	// the validator's own text names the path (it goes to the log) ...
	_, err := nappletFromEvent(evt)
	if err == nil || !strings.Contains(err.Error(), "evil") {
		t.Fatalf("fixture: want an error naming the path, got %v", err)
	}
	// ... the listed reason is exactly the catalogue phrase
	n := nappFromLatest(evt)
	if n.Unavailable != reasonFileList {
		t.Fatalf("Unavailable = %q, want %q", n.Unavailable, reasonFileList)
	}

	conv := signedEvent(t, KindNapplet, append(validNappletTags(), nostr.Tag{"i", "napplet:‮evil"}), "x")
	if got := nappFromLatest(conv).Unavailable; got != reasonConventions {
		t.Fatalf("convention: Unavailable = %q, want %q", got, reasonConventions)
	}
	for _, got := range []string{n.Unavailable, nappFromLatest(conv).Unavailable} {
		if strings.Contains(got, "evil") || strings.ContainsRune(got, '‮') {
			t.Errorf("author text in the reason: %q", got)
		}
	}
}

func TestUnavailableNameSanitized(t *testing.T) {
	title := "Bad\n‮" + strings.Repeat("x", 100)
	evt := signedEvent(t, KindNapplet, nostr.Tags{{"d", "app"}, {"title", title}, {"title", "Second"}}, "")
	n := nappFromLatest(evt)
	if n.Unavailable == "" {
		t.Fatalf("fixture should be invalid: %+v", n)
	}
	if !strings.HasPrefix(n.Name, "Bad x") {
		t.Errorf("name = %q, want the first title sanitized", n.Name)
	}
	if utf8.RuneCountInString(n.Name) > 64 {
		t.Errorf("name has %d runes, want at most 64", utf8.RuneCountInString(n.Name))
	}
	for _, r := range n.Name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			t.Errorf("name keeps rune %U", r)
		}
	}
	if n.D != "app" {
		t.Errorf("d = %q", n.D)
	}

	untitled := signedEvent(t, KindNapplet, nostr.Tags{{"d", "app"}}, "")
	if n := nappFromLatest(untitled); n.Unavailable == "" || n.Name != "" {
		t.Errorf("untitled: name %q, unavailable %q", n.Name, n.Unavailable)
	}
}
