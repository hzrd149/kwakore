package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// ─── zap rigs ────────────────────────────────────────────────────

const (
	zapLUD16    = "alice@pay.example.com"
	zapEndpoint = "https://pay.example.com/.well-known/lnurlp/alice"
	// zapInvoice21 asks for 21000 msats, zapInvoice100 for 100000.
	zapInvoice21  = "lnbc210n1pqqqqqq"
	zapInvoice100 = "lnbc1u1pqqqqqq"
)

// zapLNURL is the bech32 lnurl of the fake provider's endpoint.
func zapLNURL() string { return bech32Encode("lnurl", []byte(zapEndpoint)) }

// zapRequestEvent is a kind 9734 sender signs with tags and content.
func zapRequestEvent(t *testing.T, sender nostr.SecretKey, tags nostr.Tags, content string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	return signedWith(t, sender, nostr.KindZapRequest, tags, content, at)
}

// zapReceiptEvent wraps req in a kind 9735 provider signs: tags, then the
// bolt11 and the description.
func zapReceiptEvent(t *testing.T, provider nostr.SecretKey, req nostr.Event, tags nostr.Tags, bolt11 string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	all := append(slices.Clone(tags), nostr.Tag{"bolt11", bolt11}, nostr.Tag{"description", req.String()})
	return signedWith(t, provider, nostr.KindZap, all, "", at)
}

// zapFor is a receipt of a zap by sender on target, named by the tags, for
// msats with a matching invoice: the request carries p, the target tags,
// amount and the provider's lnurl, and the receipt copies p and the tags.
func zapFor(t *testing.T, provider, sender nostr.SecretKey, author nostr.PubKey, target nostr.Tags, invoice string, msats string, comment string, at nostr.Timestamp) (nostr.Event, nostr.Event) {
	t.Helper()
	reqTags := nostr.Tags{{"relays", "wss://r.example"}, {"amount", msats}, {"lnurl", zapLNURL()}, {"p", author.Hex()}}
	reqTags = append(reqTags, target...)
	req := zapRequestEvent(t, sender, reqTags, comment, at-1)
	receiptTags := append(nostr.Tags{{"p", author.Hex()}}, target...)
	return zapReceiptEvent(t, provider, req, receiptTags, invoice, at), req
}

// httpRig is resourceClient answered by serve, counting and recording
// every request; the lnurlp cache is cleared before and after.
type httpRig struct {
	n    atomic.Int32
	mu   sync.Mutex
	urls []*url.URL
}

func (r *httpRig) seen() []*url.URL {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.urls)
}

// newHTTPRig swaps resourceClient for serve. A status of 0 is a transport
// error.
func newHTTPRig(t *testing.T, serve func(r *http.Request) (int, string)) *httpRig {
	t.Helper()
	rig := &httpRig{}
	prev := resourceClient
	lnurlpCache.Clear()
	resourceClient = &http.Client{Transport: napRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		rig.n.Add(1)
		rig.mu.Lock()
		rig.urls = append(rig.urls, r.URL)
		rig.mu.Unlock()
		code, body := serve(r)
		if code == 0 {
			return nil, errors.New("connection refused")
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	t.Cleanup(func() {
		resourceClient = prev
		lnurlpCache.Clear()
	})
	return rig
}

// lnurlpAnswer is the fake provider's LNURL-pay answer.
func lnurlpAnswer(provider nostr.PubKey, allowsNostr bool) string {
	allows := "false"
	if allowsNostr {
		allows = "true"
	}
	return `{"tag":"payRequest","callback":"https://pay.example.com/cb?k=1","minSendable":1000,"maxSendable":100000000,` +
		`"commentAllowed":20,"metadata":"[[\"text/plain\",\"alice\"]]","allowsNostr":` + allows + `,"nostrPubkey":"` + provider.Hex() + `"}`
}

// withZapLUD16 points the lud16 seam at a value tests can change.
func withZapLUD16(t *testing.T, lud16 *atomic.Value) {
	t.Helper()
	prev := socialZapLUD16
	socialZapLUD16 = func(context.Context, nostr.PubKey) string { return lud16.Load().(string) }
	t.Cleanup(func() { socialZapLUD16 = prev })
}

// filterRelay is a khatru relay that records every filter and answers each
// REQ with every event.
type filterRelay struct {
	url     string
	mu      sync.Mutex
	filters []nostr.Filter
	events  []nostr.Event
}

func newFilterRelay(t *testing.T, events ...nostr.Event) *filterRelay {
	t.Helper()
	r := &filterRelay{events: events}
	r.url = khatruRelay(t, func(ctx context.Context, f nostr.Filter) iter.Seq[nostr.Event] {
		r.mu.Lock()
		r.filters = append(r.filters, f)
		events := slices.Clone(r.events)
		r.mu.Unlock()
		return func(yield func(nostr.Event) bool) {
			for _, evt := range events {
				if !yield(evt) {
					return
				}
			}
		}
	})
	return r
}

func (r *filterRelay) set(events ...nostr.Event) {
	r.mu.Lock()
	r.events = events
	r.mu.Unlock()
}

// zapFilters is every recorded filter that asks for kind 9735.
func (r *filterRelay) zapFilters() []nostr.Filter {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []nostr.Filter
	for _, f := range r.filters {
		if slices.Contains(f.Kinds, nostr.KindZap) {
			out = append(out, f)
		}
	}
	return out
}

func zapsStored(t *testing.T) []string {
	t.Helper()
	ids := []string{}
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindZap}}, 10000) {
		ids = append(ids, evt.ID.Hex())
	}
	slices.Sort(ids)
	return ids
}

// zapRig is socialRig with a filter relay, the fake provider and the lud16
// seam, for one napplet with one archived version.
type zapRig struct {
	relay    *filterRelay
	http     *httpRig
	lud16    *atomic.Value
	provider nostr.SecretKey
	author   nostr.SecretKey
	version  nostr.Event
	address  string
	allows   *atomic.Bool
	down     *atomic.Bool
}

func newZapRig(t *testing.T) *zapRig {
	t.Helper()
	relay := newFilterRelay(t)
	socialRig(t, relay.url)
	r := &zapRig{relay: relay, lud16: &atomic.Value{}, provider: nostr.Generate(), author: nostr.Generate(),
		allows: &atomic.Bool{}, down: &atomic.Bool{}}
	r.lud16.Store(zapLUD16)
	r.allows.Store(true)
	withZapLUD16(t, r.lud16)
	r.http = newHTTPRig(t, func(req *http.Request) (int, string) {
		if r.down.Load() {
			return 0, ""
		}
		if req.URL.String() != zapEndpoint {
			return 404, ""
		}
		return 200, lnurlpAnswer(r.provider.Public(), r.allows.Load())
	})
	r.version = archivedVersion(t, r.author, "app")
	r.address = nappletAddress(r.author.Public(), "app")
	return r
}

// ─── the identity refactor ───────────────────────────────────────

func TestZapProviderRefactorParity(t *testing.T) {
	provider := nostr.Generate().Public()
	answers := map[string]func() (int, string){
		"/.well-known/lnurlp/good":    func() (int, string) { return 200, lnurlpAnswer(provider, true) },
		"/.well-known/lnurlp/nonostr": func() (int, string) { return 200, lnurlpAnswer(provider, false) },
		"/.well-known/lnurlp/badkey":  func() (int, string) { return 200, `{"allowsNostr":true,"nostrPubkey":"zz"}` },
		"/.well-known/lnurlp/down":    func() (int, string) { return 500, "" },
		"/.well-known/lnurlp/sloppy": func() (int, string) {
			return 200, `{"allowsNostr":true,"nostrPubkey":"` + provider.Hex() + `","minSendable":"1000","commentAllowed":1.5,"maxSendable":1e3,"callback":7}`
		},
	}
	rig := newHTTPRig(t, func(r *http.Request) (int, string) {
		if answer, ok := answers[r.URL.Path]; ok {
			return answer()
		}
		return 404, ""
	})
	ctx := context.Background()

	pk, ok, err := zapProvider(ctx, "good@pay.example.com")
	if err != nil || !ok || pk != provider {
		t.Fatalf("good: %v %v %v", pk, ok, err)
	}
	for _, lud16 := range []string{"nonostr@pay.example.com", "badkey@pay.example.com"} {
		if pk, ok, err := zapProvider(ctx, lud16); err != nil || ok || pk != nostr.ZeroPK {
			t.Fatalf("%s: %v %v %v", lud16, pk, ok, err)
		}
	}
	if pk, ok, err := zapProvider(ctx, "down@pay.example.com"); err == nil || err.Error() != "zap provider unreachable" || ok || pk != nostr.ZeroPK {
		t.Fatalf("down: %v %v %v", pk, ok, err)
	}
	before := rig.n.Load()
	if pk, ok, err := zapProvider(ctx, "not-an-address"); err != nil || ok || pk != nostr.ZeroPK || rig.n.Load() != before {
		t.Fatalf("invalid lud16: %v %v %v, %d requests", pk, ok, err, rig.n.Load()-before)
	}
	// a provider whose other fields are the wrong JSON type is still a provider
	if pk, ok, err := zapProvider(ctx, "sloppy@pay.example.com"); err != nil || !ok || pk != provider {
		t.Fatalf("sloppy: %v %v %v", pk, ok, err)
	}
	info, valid, err := lnurlpParams(ctx, "sloppy@pay.example.com")
	if err != nil || !valid || info.MinSendable != 0 || info.MaxSendable != 0 || info.CommentAllowed != 0 || info.Callback != "" {
		t.Fatalf("sloppy params %+v %v %v", info, valid, err)
	}
	// a second lookup within the hour is cached
	before = rig.n.Load()
	if _, ok, _ := zapProvider(ctx, "good@pay.example.com"); !ok || rig.n.Load() != before {
		t.Fatalf("cached lookup made %d requests", rig.n.Load()-before)
	}
}

func TestLnurlpParamsFullAnswer(t *testing.T) {
	provider := nostr.Generate().Public()
	newHTTPRig(t, func(r *http.Request) (int, string) { return 200, lnurlpAnswer(provider, true) })
	info, valid, err := lnurlpParams(context.Background(), zapLUD16)
	if err != nil || !valid {
		t.Fatalf("%v %v", valid, err)
	}
	if info.Callback != "https://pay.example.com/cb?k=1" || info.MinSendable != 1000 || info.MaxSendable != 100000000 ||
		info.CommentAllowed != 20 || info.Tag != "payRequest" || !info.AllowsNostr || info.NostrPubkey != provider {
		t.Fatalf("info %+v", info)
	}
	if pk, ok := info.provider(); !ok || pk != provider {
		t.Fatalf("provider %v %v", pk, ok)
	}
	newHTTPRig(t, func(r *http.Request) (int, string) {
		return 200, `{"allowsNostr":true,"nostrPubkey":"x","commentAllowed":-5}`
	})
	info, _, _ = lnurlpParams(context.Background(), zapLUD16)
	if info.CommentAllowed != 0 {
		t.Fatalf("negative commentAllowed read as %d", info.CommentAllowed)
	}
	if _, ok := info.provider(); ok {
		t.Fatal("an unparsable nostrPubkey is a provider")
	}
}

// ─── receipt verification ────────────────────────────────────────

func TestSocialZapReceiptVerification(t *testing.T) {
	provider, sender, author := nostr.Generate(), nostr.Generate(), nostr.Generate()
	pk := author.Public()
	version := strings.Repeat("ab", 32)
	address := nappletAddress(pk, "app")
	target := socialTarget{Address: address, Author: pk, Kind: KindNapplet, VersionIDs: []string{version}}
	now := nostr.Now()

	byA, _ := zapFor(t, provider, sender, pk, nostr.Tags{{"a", address}}, zapInvoice21, "21000", "hi", now)
	if z, ok := socialZapOf(byA, target, provider.Public()); !ok || z.Msats != 21000 || z.Sender != sender.Public() || z.EventID != "" || z.Comment != "hi" {
		t.Fatalf("a-tag zap %+v %v", z, ok)
	}
	byE, req := zapFor(t, provider, sender, pk, nostr.Tags{{"e", version}}, zapInvoice100, "100000", "", now)
	if z, ok := socialZapOf(byE, target, provider.Public()); !ok || z.Msats != 100000 || z.EventID != version || z.RequestID != req.ID {
		t.Fatalf("e-tag zap %+v %v", z, ok)
	}
	root := "15129:" + pk.Hex() + ":"
	rootTarget := socialTarget{Address: root, Author: pk, Kind: KindRootNapplet, VersionIDs: []string{}}
	byRoot, _ := zapFor(t, provider, sender, pk, nostr.Tags{{"a", root}}, zapInvoice21, "21000", "", now)
	if _, ok := socialZapOf(byRoot, rootTarget, provider.Public()); !ok {
		t.Fatal("a root napplet zap with its trailing colon was rejected")
	}
	// no amount tag at all is fine: the invoice names the amount
	noAmount := zapReceiptEvent(t, provider, zapRequestEvent(t, sender, nostr.Tags{{"p", pk.Hex()}, {"a", address}}, "", now), nostr.Tags{{"a", address}}, zapInvoice21, now)
	if _, ok := socialZapOf(noAmount, target, provider.Public()); !ok {
		t.Fatal("a request without an amount tag was rejected")
	}

	reqWith := func(tags nostr.Tags) nostr.Event { return zapRequestEvent(t, sender, tags, "", now) }
	tampered, _ := zapFor(t, provider, sender, pk, nostr.Tags{{"a", address}}, zapInvoice21, "21000", "", now)
	desc := tampered.Tags.Find("description")
	var treq nostr.Event
	_ = treq.UnmarshalJSON([]byte(desc[1]))
	treq.Content = "forged"
	tamperedReceipt := zapReceiptEvent(t, provider, treq, nostr.Tags{{"a", address}}, zapInvoice21, now)
	other := nostr.Generate().Public().Hex()
	wrongProvider, _ := zapFor(t, nostr.Generate(), sender, pk, nostr.Tags{{"a", address}}, zapInvoice21, "21000", "", now)
	huge := zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"a", address}}), nostr.Tags{{"a", address}}, "lnbc220000001pqqqqqq", now)
	if m, ok := bolt11Msats("lnbc220000001pqqqqqq"); !ok || m <= socialZapMaxMsats {
		t.Fatalf("the huge invoice reads %d %v", m, ok)
	}
	rejected := map[string]nostr.Event{
		"wrong provider": wrongProvider,
		"bad signature":  tamperedReceipt,
		"p for another":  zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", other}, {"a", address}}), nostr.Tags{{"a", address}}, zapInvoice21, now),
		"two p":          zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"p", other}, {"a", address}}), nostr.Tags{{"a", address}}, zapInvoice21, now),
		"two e":          zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"e", version}, {"e", version}}), nostr.Tags{{"e", version}}, zapInvoice21, now),
		"two a":          zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"a", address}, {"a", address}}), nostr.Tags{{"a", address}}, zapInvoice21, now),
		"amount differs": zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"a", address}, {"amount", "5000"}}), nostr.Tags{{"a", address}}, zapInvoice21, now),
		"amount garbage": zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"a", address}, {"amount", "21k"}}), nostr.Tags{{"a", address}}, zapInvoice21, now),
		"no amount":      zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"a", address}, {"amount", "21000"}}), nostr.Tags{{"a", address}}, "lnbc1pqqqqqq", now),
		"over 21M btc":   huge,
		"other address":  zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"a", nappletAddress(pk, "other")}}), nostr.Tags{{"a", address}}, zapInvoice21, now),
		"unknown e":      zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"e", strings.Repeat("cd", 32)}}), nostr.Tags{{"e", version}}, zapInvoice21, now),
		"not a request":  zapReceiptEvent(t, provider, signedWith(t, sender, nostr.KindReaction, nostr.Tags{{"p", pk.Hex()}, {"a", address}}, "+", now), nostr.Tags{{"a", address}}, zapInvoice21, now),
	}
	for name, evt := range rejected {
		if z, ok := socialZapOf(evt, target, provider.Public()); ok {
			t.Errorf("%s accepted: %+v", name, z)
		}
	}
	if _, ok := socialZapOf(byA, target, nostr.ZeroPK); ok {
		t.Error("a receipt counted without a provider")
	}

	// a snapshot is named by its id only
	snapID := strings.Repeat("ef", 32)
	snapshot := socialTarget{Address: "5129:" + pk.Hex() + ":" + snapID, Author: pk, Kind: KindSnapshotNapplet, Snapshot: true, VersionIDs: []string{snapID}}
	snapA := zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"a", snapshot.Address}}), nostr.Tags{{"a", snapshot.Address}}, zapInvoice21, now)
	if _, ok := socialZapOf(snapA, snapshot, provider.Public()); ok {
		t.Error("a snapshot zap by a alone was accepted")
	}
	snapE := zapReceiptEvent(t, provider, reqWith(nostr.Tags{{"p", pk.Hex()}, {"e", snapID}}), nostr.Tags{{"e", snapID}}, zapInvoice21, now)
	if z, ok := socialZapOf(snapE, snapshot, provider.Public()); !ok || z.EventID != snapID {
		t.Errorf("a snapshot zap by e was rejected: %+v", z)
	}
}

func TestSocialZapFilters(t *testing.T) {
	pk := nostr.Generate().Public()
	provider := nostr.Generate().Public()
	v1, v2 := strings.Repeat("11", 32), strings.Repeat("22", 32)
	named := socialTarget{Address: nappletAddress(pk, "app"), Author: pk, Kind: KindNapplet, VersionIDs: []string{v1, v2}}
	if socialFilters(named, SocialZaps) != nil {
		t.Fatal("zaps have relay-only filters")
	}
	if socialZapFilters(named, nostr.ZeroPK) != nil {
		t.Fatal("filters without a provider")
	}
	filters := socialZapFilters(named, provider)
	if len(filters) != 2 ||
		!slices.Equal(filters[0].Tags["a"], []string{named.Address}) || len(filters[0].Tags) != 1 ||
		!slices.Equal(filters[1].Tags["e"], []string{v1, v2}) || len(filters[1].Tags) != 1 {
		t.Fatalf("filters %+v", filters)
	}
	for _, f := range filters {
		if !slices.Equal(f.Kinds, []nostr.Kind{nostr.KindZap}) || !slices.Equal(f.Authors, []nostr.PubKey{provider}) || f.Limit != socialLimits.PerFilter {
			t.Fatalf("filter %+v", f)
		}
	}
	bare := socialTarget{Address: named.Address, Author: pk, Kind: KindNapplet, VersionIDs: []string{}}
	if filters := socialZapFilters(bare, provider); len(filters) != 1 || filters[0].Tags["e"] != nil {
		t.Fatalf("filters without versions %+v", filters)
	}
	snapID := strings.Repeat("ab", 32)
	snapshot := socialTarget{Address: "5129:" + pk.Hex() + ":" + snapID, Author: pk, Kind: KindSnapshotNapplet, Snapshot: true, VersionIDs: []string{snapID}}
	filters = socialZapFilters(snapshot, provider)
	if len(filters) != 1 || !slices.Equal(filters[0].Tags["e"], []string{snapID}) || filters[0].Tags["a"] != nil {
		t.Fatalf("snapshot filters %+v", filters)
	}
	for _, target := range []socialTarget{named, bare, snapshot} {
		for _, f := range socialZapFilters(target, provider) {
			for name, values := range f.Tags {
				if len(values) == 0 {
					t.Fatalf("empty #%s", name)
				}
			}
		}
	}
}

// ─── refresh ─────────────────────────────────────────────────────

func TestSocialZapsRefresh(t *testing.T) {
	r := newZapRig(t)
	pk := r.author.Public()
	now := nostr.Now()
	s1, s2, x := nostr.Generate(), nostr.Generate(), nostr.Generate()
	long := "‮" + strings.Repeat("é", 300)
	r1, _ := zapFor(t, r.provider, s1, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", long, now-30)
	r2, _ := zapFor(t, r.provider, s1, pk, nostr.Tags{{"e", r.version.ID.Hex()}}, zapInvoice100, "100000", "", now-20)
	r3, req3 := zapFor(t, r.provider, s2, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", "s2", now-10)
	// a second receipt for the same request counts once, as the newest
	r3b := zapReceiptEvent(t, r.provider, req3, nostr.Tags{{"p", pk.Hex()}, {"a", r.address}}, zapInvoice21, now-5)

	wrongProvider, _ := zapFor(t, nostr.Generate(), x, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", "", now)
	tampered, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", "", now)
	desc := tampered.Tags.Find("description")
	var treq nostr.Event
	_ = treq.UnmarshalJSON([]byte(desc[1]))
	treq.Content = "forged"
	badSig := zapReceiptEvent(t, r.provider, treq, nostr.Tags{{"p", pk.Hex()}, {"a", r.address}}, zapInvoice21, now)
	pMismatch := zapReceiptEvent(t, r.provider,
		zapRequestEvent(t, x, nostr.Tags{{"p", nostr.Generate().Public().Hex()}, {"a", r.address}}, "", now),
		nostr.Tags{{"p", pk.Hex()}, {"a", r.address}}, zapInvoice21, now)
	amountMismatch, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "5000", "", now)
	lnurlMismatch := zapReceiptEvent(t, r.provider,
		zapRequestEvent(t, x, nostr.Tags{{"p", pk.Hex()}, {"a", r.address}, {"lnurl", "lnurl1other"}}, "", now),
		nostr.Tags{{"p", pk.Hex()}, {"a", r.address}}, zapInvoice21, now)
	elsewhere, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", nappletAddress(pk, "other")}}, zapInvoice21, "21000", "", now)
	r.relay.set(r1, r2, r3, r3b, wrongProvider, badSig, pMismatch, amountMismatch, lnurlMismatch, elsewhere)

	summary, err := ServiceSocial(t.Context(), r.address, true, []string{SocialZaps})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := zapsStored(t), idsOf(r1, r2, r3, r3b); !slices.Equal(got, want) {
		t.Fatalf("stored %v, want %v", got, want)
	}
	if s := summary.Sync[SocialZaps]; s.Status != "answered" || s.SyncedAt == nil {
		t.Fatalf("zaps sync %+v", s)
	}
	for _, category := range []string{SocialReactions, SocialComments, SocialMentions} {
		if s := summary.Sync[category]; s.Status != "not_requested" {
			t.Fatalf("%s sync %+v", category, s)
		}
	}
	want := ServiceSocialZapSummary{Count: 3, TotalMsats: 142000, Top: []ServiceSocialZapper{
		{Pubkey: s1.Public().Hex(), Msats: 121000, Count: 2},
		{Pubkey: s2.Public().Hex(), Msats: 21000, Count: 1},
	}}
	if got := summary.Zaps; got.Count != want.Count || got.TotalMsats != want.TotalMsats || !slices.Equal(got.Top, want.Top) {
		t.Fatalf("zaps %+v, want %+v", got, want)
	}
	filters := r.relay.zapFilters()
	if len(filters) != 2 {
		t.Fatalf("%d zap filters: %+v", len(filters), filters)
	}
	var sawA, sawE bool
	for _, f := range filters {
		if !slices.Equal(f.Authors, []nostr.PubKey{r.provider.Public()}) {
			t.Fatalf("filter authors %+v", f)
		}
		sawA = sawA || slices.Equal(f.Tags["a"], []string{r.address})
		sawE = sawE || slices.Equal(f.Tags["e"], []string{r.version.ID.Hex()})
	}
	if !sawA || !sawE {
		t.Fatalf("filters %+v", filters)
	}

	saved(t, signedWith(t, s1, 0, nostr.Tags{}, `{"name":"Sam"}`, 10))
	page, err := ServiceZaps(t.Context(), r.address, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.TotalMsats != 142000 || page.NextOffset != nil || len(page.Items) != 3 {
		t.Fatalf("page %+v", page)
	}
	if got := []string{page.Items[0].ReceiptID, page.Items[1].ReceiptID, page.Items[2].ReceiptID}; !slices.Equal(got, []string{r3b.ID.Hex(), r2.ID.Hex(), r1.ID.Hex()}) {
		t.Fatalf("order %v", got)
	}
	first, second, third := page.Items[0], page.Items[1], page.Items[2]
	if first.Sender != s2.Public().Hex() || first.AmountMsats != 21000 || first.Comment != "s2" || first.EventID != "" || first.SenderName != "" || first.CreatedAt != int64(now-5) {
		t.Errorf("first %+v", first)
	}
	if second.EventID != r.version.ID.Hex() || second.AmountMsats != 100000 || second.SenderName != "Sam" {
		t.Errorf("second %+v", second)
	}
	if third.Comment != strings.Repeat("é", 280) || third.EventID != "" {
		t.Errorf("third comment %q", third.Comment)
	}
	paged, err := ServiceZaps(t.Context(), r.address, 1, 1)
	if err != nil || len(paged.Items) != 1 || paged.Items[0].ReceiptID != r2.ID.Hex() || paged.TotalMsats != 142000 || paged.NextOffset == nil || *paged.NextOffset != 2 {
		t.Fatalf("paged %+v %v", paged, err)
	}

	// nothing on the wire names the provider, its urls, the raw events or paths
	for _, v := range []any{summary, page} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"https://", "lnurl", "pay.example.com", "wss://", "bolt11", "description", "sig", "tags", dataDir, "‮"} {
			if marker != "" && strings.Contains(string(raw), marker) {
				t.Errorf("%s carries %q", raw, marker)
			}
		}
	}
}

func TestSocialZapsLongAddressDecoy(t *testing.T) {
	r := newZapRig(t)
	pk := r.author.Public()
	d := strings.Repeat("d", 40)
	address := nappletAddress(pk, d+"x")
	decoy := nappletAddress(pk, d+"y")
	if len(address) <= 100 {
		t.Fatalf("address too short for the p fallback: %d", len(address))
	}
	x := nostr.Generate()
	good, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", address}}, zapInvoice21, "21000", "", nostr.Now()-5)
	bad, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", decoy}}, zapInvoice21, "21000", "", nostr.Now())
	r.relay.set(good, bad)
	summary, err := ServiceSocial(t.Context(), address, true, []string{SocialZaps})
	if err != nil {
		t.Fatal(err)
	}
	if got := zapsStored(t); !slices.Equal(got, idsOf(good)) {
		t.Fatalf("stored %v", got)
	}
	// even held, the decoy is never listed for the address
	saved(t, bad)
	page, err := ServiceZaps(t.Context(), address, 0, 10)
	if err != nil || summary.Zaps.Count != 1 || page.Total != 1 || page.Items[0].ReceiptID != good.ID.Hex() {
		t.Fatalf("summary %+v page %+v %v", summary.Zaps, page, err)
	}
}

func TestSocialZapsSnapshotByEventOnly(t *testing.T) {
	r := newZapRig(t)
	pk := r.author.Public()
	snapID := strings.Repeat("ab", 32)
	address := "5129:" + pk.Hex() + ":" + snapID
	x := nostr.Generate()
	byE, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"e", snapID}}, zapInvoice21, "21000", "", nostr.Now())
	byA, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", address}}, zapInvoice21, "21000", "", nostr.Now())
	r.relay.set(byE, byA)
	summary, err := ServiceSocial(t.Context(), address, true, []string{SocialZaps})
	if err != nil {
		t.Fatal(err)
	}
	if got := zapsStored(t); !slices.Equal(got, idsOf(byE)) || summary.Zaps.Count != 1 {
		t.Fatalf("stored %v, zaps %+v", got, summary.Zaps)
	}
	for _, f := range r.relay.zapFilters() {
		if f.Tags["a"] != nil || !slices.Equal(f.Tags["e"], []string{snapID}) {
			t.Fatalf("snapshot filter %+v", f)
		}
	}
}

func TestSocialZapsNotZappable(t *testing.T) {
	r := newZapRig(t)
	pk := r.author.Public()
	x := nostr.Generate()
	receipt, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", "", nostr.Now())
	r.relay.set(receipt)

	unavailable := func(what string) ServiceSocialSummary {
		t.Helper()
		lnurlpCache.Clear()
		summary, err := ServiceSocial(t.Context(), r.address, true, []string{SocialZaps})
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if s := summary.Sync[SocialZaps]; s.Status != "unavailable" || s.SyncedAt == nil {
			t.Fatalf("%s: sync %+v", what, s)
		}
		if n := len(r.relay.zapFilters()); n != 0 {
			t.Fatalf("%s: %d zap REQs", what, n)
		}
		return summary
	}
	r.lud16.Store("")
	unavailable("no lud16")
	r.lud16.Store(zapLUD16)
	r.allows.Store(false)
	unavailable("no nostr")
	r.allows.Store(true)
	r.down.Store(true)
	unavailable("unreachable")
	if n := len(zapsStored(t)); n != 0 {
		t.Fatalf("%d receipts stored without a provider", n)
	}

	// a provider is recorded, survives a transient failure and goes once
	// the author has no lightning address
	r.down.Store(false)
	lnurlpCache.Clear()
	summary, err := ServiceSocial(t.Context(), r.address, true, []string{SocialZaps})
	if err != nil || summary.Zaps.Count != 1 {
		t.Fatalf("zappable: %+v %v", summary.Zaps, err)
	}
	r.down.Store(true)
	lnurlpCache.Clear()
	if summary, err := ServiceSocial(t.Context(), r.address, true, []string{SocialZaps}); err != nil || summary.Zaps.Count != 1 || summary.Sync[SocialZaps].Status != "unavailable" {
		t.Fatalf("after a transient failure: %+v %v", summary, err)
	}
	r.lud16.Store("")
	summary, err = ServiceSocial(t.Context(), r.address, true, []string{SocialZaps})
	if err != nil || summary.Zaps.Count != 0 || summary.Zaps.Top == nil {
		t.Fatalf("after the lud16 went: %+v %v", summary.Zaps, err)
	}
	page, err := ServiceZaps(t.Context(), r.address, 0, 10)
	if err != nil || page.Total != 0 || page.Items == nil || page.TotalMsats != 0 {
		t.Fatalf("page %+v %v", page, err)
	}
}

// ─── retention ───────────────────────────────────────────────────

func TestSocialZapsCapKeepsOwnAndPrunes(t *testing.T) {
	r := newZapRig(t)
	socialLimits.Zaps = 2
	pk := r.author.Public()
	me := nostr.Generate()
	withUserKeyer(t, keyer.NewPlainKeySigner(me), me.Public())
	now := nostr.Now()
	mine, _ := zapFor(t, r.provider, me, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", "", now-100)
	var others []nostr.Event
	for i := range 3 {
		evt, _ := zapFor(t, r.provider, nostr.Generate(), pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", "", now-nostr.Timestamp(30-i))
		others = append(others, evt)
	}
	r.relay.set(append([]nostr.Event{mine}, others...)...)
	if _, err := ServiceSocial(t.Context(), r.address, true, []string{SocialZaps}); err != nil {
		t.Fatal(err)
	}
	if got, want := zapsStored(t), idsOf(mine, others[2]); !slices.Equal(got, want) {
		t.Fatalf("after the cap %v, want %v", got, want)
	}
	target, err := newSocialTarget(r.address)
	if err != nil {
		t.Fatal(err)
	}
	socialPruneEvents(target)
	if got, want := zapsStored(t), idsOf(mine); !slices.Equal(got, want) {
		t.Fatalf("after the prune %v, want %v", got, want)
	}
	saved(t, others[0])
	pruneNappletSocial(r.address)
	if got, want := zapsStored(t), idsOf(mine); !slices.Equal(got, want) {
		t.Fatalf("after the uninstall prune %v, want %v", got, want)
	}
}

func TestSocialZapsIgnoreDeletions(t *testing.T) {
	r := newZapRig(t)
	pk := r.author.Public()
	x := nostr.Generate()
	receipt, _ := zapFor(t, r.provider, x, pk, nostr.Tags{{"a", r.address}}, zapInvoice21, "21000", "", nostr.Now()-10)
	r.relay.set(receipt)
	if _, err := ServiceSocial(t.Context(), r.address, true, nil); err != nil {
		t.Fatal(err)
	}
	byProvider := signedWith(t, r.provider, nostr.KindDeletion, nostr.Tags{{"e", receipt.ID.Hex()}, {"k", "9735"}}, "", nostr.Now())
	r.relay.set(receipt, byProvider)
	summary, err := ServiceSocial(t.Context(), r.address, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := zapsStored(t); !slices.Equal(got, idsOf(receipt)) || summary.Zaps.Count != 1 {
		t.Fatalf("stored %v, zaps %+v", got, summary.Zaps)
	}
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}}, 10) {
		t.Fatalf("a deletion of a receipt was stored: %s", evt.ID.Hex())
	}
	// even a held kind 5 by the provider leaves the receipt counted
	saved(t, byProvider)
	socialResetMemo()
	summary, err = ServiceSocial(t.Context(), r.address, false, nil)
	if err != nil || summary.Zaps.Count != 1 {
		t.Fatalf("with a held deletion %+v %v", summary.Zaps, err)
	}
}

func TestSocialZapSummaryPure(t *testing.T) {
	a, b, c := nostr.Generate().Public(), nostr.Generate().Public(), nostr.Generate().Public()
	zaps := []socialZap{
		{Sender: a, Msats: 5000}, {Sender: b, Msats: 3000}, {Sender: b, Msats: 2000}, {Sender: c, Msats: 5000},
	}
	summary := zapSummary(zaps)
	if summary.Count != 4 || summary.TotalMsats != 15000 || len(summary.Top) != 3 {
		t.Fatalf("summary %+v", summary)
	}
	// b ties a and c on msats but zapped twice; a and c tie on both
	first, second, third := summary.Top[0], summary.Top[1], summary.Top[2]
	lo, hi := a.Hex(), c.Hex()
	if lo > hi {
		lo, hi = hi, lo
	}
	if first.Pubkey != b.Hex() || first.Count != 2 || second.Pubkey != lo || third.Pubkey != hi {
		t.Fatalf("top %+v", summary.Top)
	}
	var many []socialZap
	for range 7 {
		many = append(many, socialZap{Sender: nostr.Generate().Public(), Msats: 9_000_000_000_000_000_000 / 4})
	}
	big := zapSummary(many)
	if len(big.Top) != socialZapTopLimit || big.TotalMsats != 9_223_372_036_854_775_807 {
		t.Fatalf("big %+v", big)
	}
	if empty := zapSummary(nil); empty.Top == nil || empty.Count != 0 {
		t.Fatalf("empty %+v", empty)
	}
	if items := zapItems(nil); items == nil {
		t.Fatal("nil items")
	}
}
