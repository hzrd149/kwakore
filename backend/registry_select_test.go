package backend

import (
	"bytes"
	"testing"
	"time"

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
		if n.Unavailable == "" {
			t.Fatalf("%s: the older valid version was listed instead: %+v", name, n)
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
