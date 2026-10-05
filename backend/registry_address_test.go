package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

func TestParseNappAddress(t *testing.T) {
	pk := nostr.Generate().Public()
	naddr := nip19.EncodeNaddr(pk, KindNapplet, "noris", []string{"wss://relay.example.com"})
	root := nip19.EncodeNaddr(pk, KindRootNapplet, "", nil)
	napp := nip19.EncodeNaddr(pk, KindNapp, "notes", nil)

	accepts := map[string]nostr.EntityPointer{
		naddr:                                   {PublicKey: pk, Kind: KindNapplet, Identifier: "noris"},
		"nostr:" + naddr:                        {PublicKey: pk, Kind: KindNapplet, Identifier: "noris"},
		"  " + naddr + "\n":                     {PublicKey: pk, Kind: KindNapplet, Identifier: "noris"},
		"https://napplet.run/" + naddr + "?x=1": {PublicKey: pk, Kind: KindNapplet, Identifier: "noris"},
		root:                                    {PublicKey: pk, Kind: KindRootNapplet},
		napp:                                    {PublicKey: pk, Kind: KindNapp, Identifier: "notes"},
		"35129:" + pk.Hex() + ":noris":          {PublicKey: pk, Kind: KindNapplet, Identifier: "noris"},
		"nostr:35130:" + pk.Hex() + ":with:colon": {PublicKey: pk, Kind: KindNapp, Identifier: "with:colon"},
	}
	for in, want := range accepts {
		got, err := ParseNappAddress(in)
		if err != nil {
			t.Errorf("%q rejected: %v", in, err)
			continue
		}
		if got.PublicKey != want.PublicKey || got.Kind != want.Kind || got.Identifier != want.Identifier {
			t.Errorf("%q: got %+v", in, got)
		}
	}

	rejects := []string{
		"",
		"noris",
		"abcdef0123456789~notes", // a napp id, i.e. a bundle token
		"abcdef0123456789~notes +eyJuYW1lIjoieCJ9",  // with an action
		nip19.EncodeNaddr(pk, 30023, "post", nil),   // not a napp kind
		nip19.EncodeNaddr(pk, KindNapplet, "", nil), // addressable with no d
		"1:" + pk.Hex() + ":x",
		"35129:nothex:noris",
		nip19.EncodeNpub(pk),
	}
	for _, in := range rejects {
		if _, err := ParseNappAddress(in); err == nil {
			t.Errorf("%q accepted", in)
		}
		if IsNappAddress(in) {
			t.Errorf("IsNappAddress(%q)", in)
		}
	}
}

func TestNappAddressRoundTrip(t *testing.T) {
	evs := realNapplets(t)
	n, err := nappletFromEvent(evs["noris"])
	if err != nil {
		t.Fatal(err)
	}
	ptr, err := ParseNappAddress(n.Naddr())
	if err != nil {
		t.Fatal(err)
	}
	if !n.matchesAddress(ptr) {
		t.Fatalf("%s doesn't name its own napplet", n.Naddr())
	}
	if !n.MatchesQuery(n.Naddr()) {
		t.Error("the filter doesn't match the napplet's own naddr")
	}

	// same author and d, other kind: a napp and a napplet may share a d tag
	other := nip19.EncodeNaddr(n.Author, KindNapp, n.D, nil)
	if n.MatchesQuery(other) {
		t.Error("a napp address matched a napplet")
	}
	// a plain-text query still filters as before
	if !n.MatchesQuery("noris") {
		t.Error("name filter broke")
	}
}

func TestRootNappletAddress(t *testing.T) {
	sk := nostr.Generate()
	n := Napp{Author: sk.Public(), Format: FormatNapplet, Kind: KindRootNapplet}
	ptr, err := ParseNappAddress(n.Naddr())
	if err != nil {
		t.Fatal(err)
	}
	if !n.matchesAddress(ptr) {
		t.Fatal("root napplet doesn't match its address")
	}
	if f := addressFilter(ptr); f.Tags != nil || f.Kinds[0] != KindRootNapplet {
		t.Errorf("root filter: %+v", f)
	}
	if f := addressFilter(nostr.EntityPointer{PublicKey: sk.Public(), Kind: KindNapplet, Identifier: "x"}); f.Tags["d"][0] != "x" {
		t.Errorf("named filter: %+v", f)
	}
}

func TestResolvedNappsSurviveDiscovery(t *testing.T) {
	l := launcherState{}
	old := Napp{ID: "a", CreatedAt: 10}
	l.resolved = map[string]Napp{"a": {ID: "a", CreatedAt: 20}, "b": {ID: "b", CreatedAt: 5}}

	got := l.withResolved([]Napp{old, {ID: "c"}})
	if len(got) != 3 {
		t.Fatalf("want 3 napps, got %+v", got)
	}
	for _, n := range got {
		if n.ID == "a" && n.CreatedAt != 20 {
			t.Error("the newer resolved copy didn't win")
		}
	}

	l.resolved["a"] = Napp{ID: "a", CreatedAt: 1}
	for _, n := range l.withResolved([]Napp{old}) {
		if n.ID == "a" && n.CreatedAt != 10 {
			t.Error("an older resolved copy replaced the discovered one")
		}
	}
}

func TestResolveAddressPicksNIP01Winner(t *testing.T) {
	sk := nostr.Generate()
	ptr := nostr.EntityPointer{PublicKey: sk.Public(), Kind: KindNapplet, Identifier: "app"}
	low, high := tiedPair(t, sk)

	// a tie goes to the lower id, in either order
	for _, events := range [][]nostr.Event{{low, high}, {high, low}} {
		n, found := pickAddress(ptr, events)
		if !found || n.EventID != low.ID.Hex() || n.Unavailable != "" {
			t.Errorf("tie: found=%v %+v, want the lower id %s", found, n, low.ID.Hex())
		}
	}

	// what a relay sent for other addresses never competes, however new
	index := NappPath{Path: "/index.html", Sha256: testArtifact}
	other := nostr.Generate()
	strays := []nostr.Event{
		signedWith(t, other, KindNapplet, nip5dTags("app", index), "", 500),                 // wrong author
		signedWith(t, sk, KindNapp, nip5dTags("app", index), "", 500),                       // wrong kind
		signedWith(t, sk, KindNapplet, nip5dTags("other", index), "", 500),                  // wrong d
		signedWith(t, sk, KindNapplet, nostr.Tags{{"d", "other"}, {"title", "x"}}, "", 600), // wrong d, invalid
	}
	n, found := pickAddress(ptr, append(strays, low))
	if !found || n.EventID != low.ID.Hex() {
		t.Errorf("strays: found=%v %+v", found, n)
	}

	// an invalid newest is the answer, as unavailable, never the older one
	invalid := signedWith(t, sk, KindNapplet, nostr.Tags{{"d", "app"}, {"title", "App"}}, "", 200)
	n, found = pickAddress(ptr, []nostr.Event{low, invalid, high})
	if !found || n.Unavailable == "" || n.EventID != invalid.ID.Hex() || len(n.Paths) != 0 {
		t.Errorf("invalid newest: found=%v %+v", found, n)
	}

	// nothing authentic is not found
	if _, found := pickAddress(ptr, strays); found {
		t.Error("found a napplet among strays only")
	}
	forged := low
	forged.ID = nostr.ID{}
	if _, found := pickAddress(ptr, []nostr.Event{forged}); found {
		t.Error("found a napplet whose id was forged")
	}

	// and through the lookup: no events is the not-found error
	old := addressEvents
	t.Cleanup(func() { addressEvents = old })
	addressEvents = func(context.Context, nostr.EntityPointer) ([]nostr.Event, error) { return strays, nil }
	if _, err := ResolveNappAddress(context.Background(), nip19.EncodeNaddr(ptr.PublicKey, ptr.Kind, ptr.Identifier, nil)); err == nil ||
		!strings.Contains(err.Error(), "no napp or napplet found") {
		t.Errorf("lookup with nothing authentic: %v", err)
	}
}

func TestOpenAndInstallAddressRefuseUnavailable(t *testing.T) {
	setupNapTest(t)
	resetResolved(t)
	sk := nostr.Generate()
	invalid := signedWith(t, sk, KindNapplet, nostr.Tags{{"d", "app"}, {"title", "App"}}, "", 200)
	n := nappFromLatest(invalid)
	if n.Unavailable == "" {
		t.Fatal("fixture should be unavailable")
	}

	before := PendingPrompts()
	done := make(chan error, 1)
	go func() { done <- openResolved(n) }()
	select {
	case err := <-done:
		if err == nil || err.Error() != "the latest version is invalid" {
			t.Errorf("open: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("opening an unavailable address asked to install it")
	}
	if _, err := installResolved(n); err == nil || err.Error() != "the latest version is invalid" {
		t.Errorf("install: %v", err)
	}
	if PendingPrompts() != before {
		t.Error("a prompt was queued for an unavailable address")
	}
	if _, ok := InstalledNapp(n.ID); ok {
		t.Error("an unavailable address was installed")
	}
	if entries, _ := os.ReadDir(filepath.Join(dataDir, "napps")); len(entries) != 0 {
		t.Errorf("install wrote %d entries under napps/", len(entries))
	}
	// it is still listed, so the store can say why
	if got, ok := DiscoveredNapp(n.ID); !ok || got.Unavailable == "" {
		t.Errorf("unavailable entry not listed: %v %+v", ok, got)
	}
}

func TestResolvedNappsTieBreak(t *testing.T) {
	listed := Napp{ID: "a", CreatedAt: 10, EventID: strings.Repeat("5", 64)}
	cases := []struct {
		name     string
		resolved Napp
		replaces bool
	}{
		{"same second, lower id", Napp{ID: "a", CreatedAt: 10, EventID: strings.Repeat("1", 64)}, true},
		{"same second, higher id", Napp{ID: "a", CreatedAt: 10, EventID: strings.Repeat("9", 64)}, false},
		{"same second, same id", listed, false},
		{"same second, unknown id", Napp{ID: "a", CreatedAt: 10}, false},
		{"older, lower id", Napp{ID: "a", CreatedAt: 9, EventID: strings.Repeat("0", 64)}, false},
		{"newer, higher id", Napp{ID: "a", CreatedAt: 11, EventID: strings.Repeat("f", 64)}, true},
	}
	for _, c := range cases {
		l := launcherState{resolved: map[string]Napp{"a": c.resolved}}
		got := l.withResolved([]Napp{listed})
		if len(got) != 1 {
			t.Fatalf("%s: %d entries", c.name, len(got))
		}
		if replaced := got[0].EventID != listed.EventID || got[0].CreatedAt != listed.CreatedAt; replaced != c.replaces {
			t.Errorf("%s: replaced=%v, want %v", c.name, replaced, c.replaces)
		}
	}

	// rememberResolved keeps the winner the same way
	resetResolved(t)
	rememberResolved(Napp{ID: "b", CreatedAt: 10, EventID: strings.Repeat("5", 64)})
	rememberResolved(Napp{ID: "b", CreatedAt: 10, EventID: strings.Repeat("9", 64)})
	rememberResolved(Napp{ID: "b", CreatedAt: 9, EventID: strings.Repeat("0", 64)})
	ls.mu.Lock()
	kept := ls.resolved["b"].EventID
	ls.mu.Unlock()
	if kept != strings.Repeat("5", 64) {
		t.Errorf("resolved cache kept %s", kept)
	}
	rememberResolved(Napp{ID: "b", CreatedAt: 10, EventID: strings.Repeat("1", 64)})
	ls.mu.Lock()
	kept = ls.resolved["b"].EventID
	ls.mu.Unlock()
	if kept != strings.Repeat("1", 64) {
		t.Errorf("a lower id of the same second didn't replace: %s", kept)
	}
}

// resetResolved empties the resolved cache and the discovery list for one
// test and puts them back after it.
func resetResolved(t *testing.T) {
	t.Helper()
	ls.mu.Lock()
	resolved, discovery := ls.resolved, ls.discovery
	ls.resolved, ls.discovery = nil, nil
	ls.mu.Unlock()
	t.Cleanup(func() {
		ls.mu.Lock()
		ls.resolved, ls.discovery = resolved, discovery
		ls.mu.Unlock()
	})
}
