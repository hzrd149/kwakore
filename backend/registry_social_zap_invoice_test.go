package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// ─── invoice rig ─────────────────────────────────────────────────

// invoiceFor is a bolt11-shaped invoice for msats.
func invoiceFor(msats int64) string {
	if msats%100 == 0 {
		return fmt.Sprintf("lnbc%dn1pqqqqqq", msats/100)
	}
	return fmt.Sprintf("lnbc%dp1pqqqqqq", msats*10)
}

// invoiceRig is a signer, an author with one archived "app" version, the
// author's lud16 and a fake provider reached through resourceClient: its
// LNURL-pay answer and its callback, which by default returns an invoice
// for the amount asked.
type invoiceRig struct {
	publishFixture
	http     *httpRig
	lud16    *atomic.Value
	provider nostr.SecretKey
	mu       sync.Mutex
	answer   string
	callback func(r *http.Request) (int, string)
}

func newInvoiceRig(t *testing.T) *invoiceRig {
	t.Helper()
	r := &invoiceRig{publishFixture: newPublishFixture(t, "", false), lud16: &atomic.Value{}, provider: nostr.Generate()}
	r.lud16.Store(zapLUD16)
	withZapLUD16(t, r.lud16)
	r.answer = lnurlpAnswer(r.provider.Public(), true)
	r.callback = func(req *http.Request) (int, string) {
		msats, err := strconv.ParseInt(req.URL.Query().Get("amount"), 10, 64)
		if err != nil {
			return 400, `{"status":"ERROR","reason":"bad amount"}`
		}
		return 200, `{"pr":"` + invoiceFor(msats) + `","routes":[]}`
	}
	prevRelays := socialZapRelays
	socialZapRelays = func(context.Context, socialTarget) []string { return []string{"wss://r1.example", "wss://r2.example"} }
	t.Cleanup(func() { socialZapRelays = prevRelays })
	r.http = newHTTPRig(t, func(req *http.Request) (int, string) {
		r.mu.Lock()
		answer, callback := r.answer, r.callback
		r.mu.Unlock()
		switch {
		case req.URL.Host == "pay.example.com" && req.URL.Path == "/.well-known/lnurlp/alice":
			return 200, answer
		case req.URL.Host == "pay.example.com" && req.URL.Path == "/cb":
			return callback(req)
		}
		return 404, ""
	})
	return r
}

func (r *invoiceRig) setAnswer(answer string) {
	r.mu.Lock()
	r.answer = answer
	r.mu.Unlock()
	lnurlpCache.Clear()
}

func (r *invoiceRig) setCallback(callback func(req *http.Request) (int, string)) {
	r.mu.Lock()
	r.callback = callback
	r.mu.Unlock()
}

// callbacks are the callback requests made so far.
func (r *invoiceRig) callbacks() []*url.URL {
	var out []*url.URL
	for _, u := range r.http.seen() {
		if u.Path == "/cb" {
			out = append(out, u)
		}
	}
	return out
}

// lastRequest is the zap request the last callback carried.
func (r *invoiceRig) lastRequest(t *testing.T) (nostr.Event, url.Values) {
	t.Helper()
	calls := r.callbacks()
	if len(calls) == 0 {
		t.Fatal("no callback request")
	}
	q := calls[len(calls)-1].Query()
	var req nostr.Event
	if err := json.Unmarshal([]byte(q.Get("nostr")), &req); err != nil {
		t.Fatal(err)
	}
	return req, q
}

func noZapRequestStored(t *testing.T) {
	t.Helper()
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindZapRequest}}, 10) {
		t.Fatalf("a zap request was stored: %s", evt.ID.Hex())
	}
}

// ─── the happy path ──────────────────────────────────────────────

func TestSocialZapInvoice(t *testing.T) {
	r := newInvoiceRig(t)
	pk := r.author.Public()
	result, err := ServiceSocialZap(context.Background(), r.address, 21000, "thanks", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != r.address || result.Invoice != zapInvoice21 || result.AmountMsats != 21000 || result.Recipient != pk.Hex() {
		t.Fatalf("result %+v", result)
	}
	calls := r.callbacks()
	if len(calls) != 1 || !strings.HasPrefix(calls[0].String(), "https://pay.example.com/cb?k=1&") {
		t.Fatalf("callbacks %v", calls)
	}
	req, q := r.lastRequest(t)
	if q.Get("k") != "1" || q.Get("amount") != "21000" || q.Get("lnurl") != zapLNURL() || q.Get("comment") != "thanks" {
		t.Fatalf("callback query %v", q)
	}
	if req.Kind != nostr.KindZapRequest || !req.CheckID() || !req.VerifySignature() || req.PubKey != r.me.Public() ||
		req.Content != "thanks" || req.ID.Hex() != result.ZapRequestID {
		t.Fatalf("zap request %+v", req)
	}
	wantTags(t, "35129 zap request", req.Tags, nostr.Tags{
		{"relays", "wss://r1.example", "wss://r2.example"}, {"amount", "21000"}, {"lnurl", zapLNURL()},
		{"p", pk.Hex()}, {"e", r.version.ID.Hex()}, {"a", r.address}, {"k", "35129"},
	})
	noZapRequestStored(t)
	if n := len(r.got()); n != 0 {
		t.Fatalf("published %d events", n)
	}
}

func TestSocialZapInvoiceRootAndSnapshot(t *testing.T) {
	r := newInvoiceRig(t)
	pk := r.author.Public()
	rootID := strings.Repeat("cd", 32)
	installRecord(t, Napp{ID: "root-app", Format: FormatNapplet, Kind: KindRootNapplet, Author: pk, EventID: rootID})
	root := "15129:" + pk.Hex() + ":"
	if _, err := ServiceSocialZap(context.Background(), root, 21000, "", ""); err != nil {
		t.Fatal(err)
	}
	req, q := r.lastRequest(t)
	wantTags(t, "15129 zap request", req.Tags, nostr.Tags{
		{"relays", "wss://r1.example", "wss://r2.example"}, {"amount", "21000"}, {"lnurl", zapLNURL()},
		{"p", pk.Hex()}, {"e", rootID}, {"a", root}, {"k", "15129"},
	})
	if q.Has("comment") {
		t.Fatalf("an empty comment was sent: %v", q)
	}

	snapID := strings.Repeat("ab", 32)
	snapshot := "5129:" + pk.Hex() + ":" + snapID
	if _, err := ServiceSocialZap(context.Background(), snapshot, 21000, "", ""); err != nil {
		t.Fatal(err)
	}
	req, _ = r.lastRequest(t)
	wantTags(t, "5129 zap request", req.Tags, nostr.Tags{
		{"relays", "wss://r1.example", "wss://r2.example"}, {"amount", "21000"}, {"lnurl", zapLNURL()},
		{"p", pk.Hex()}, {"e", snapID}, {"k", "5129"},
	})
	noZapRequestStored(t)
}

func TestSocialZapInvoiceVersionChoice(t *testing.T) {
	r := newInvoiceRig(t)
	newer := validNapplet(t, r.author, "app", "v2", 20)
	archiveManifest(newer, "")
	ctx := context.Background()
	// the newest archived version by default, the named one when given
	if _, err := ServiceSocialZap(ctx, r.address, 21000, "", ""); err != nil {
		t.Fatal(err)
	}
	if req, _ := r.lastRequest(t); req.Tags.Find("e")[1] != newer.ID.Hex() {
		t.Fatalf("default version %v", req.Tags)
	}
	if _, err := ServiceSocialZap(ctx, r.address, 21000, "", r.version.ID.Hex()); err != nil {
		t.Fatal(err)
	}
	if req, _ := r.lastRequest(t); req.Tags.Find("e")[1] != r.version.ID.Hex() {
		t.Fatalf("named version %v", req.Tags)
	}
	before := len(r.callbacks())
	if _, err := ServiceSocialZap(ctx, r.address, 21000, "", strings.Repeat("ef", 32)); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("unknown version: %v", err)
	}
	if len(r.callbacks()) != before {
		t.Fatal("an unknown version reached the callback")
	}

	// a napplet with no known version is zapped by address alone
	bare := nappletAddress(r.author.Public(), "unseen")
	if _, err := ServiceSocialZap(ctx, bare, 21000, "", ""); err != nil {
		t.Fatal(err)
	}
	if req, _ := r.lastRequest(t); req.Tags.Find("e") != nil || req.Tags.Find("a")[1] != bare {
		t.Fatalf("bare tags %v", req.Tags)
	}
}

func TestSocialZapInvoiceNoComments(t *testing.T) {
	r := newInvoiceRig(t)
	r.setAnswer(strings.Replace(lnurlpAnswer(r.provider.Public(), true), `"commentAllowed":20`, `"commentAllowed":0`, 1))
	if _, err := ServiceSocialZap(context.Background(), r.address, 21000, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, q := r.lastRequest(t); q.Has("comment") {
		t.Fatalf("comment parameter without comment support: %v", q)
	}
	if _, err := ServiceSocialZap(context.Background(), r.address, 21000, "hi", ""); !errors.Is(err, ErrServiceInvalidSocial) {
		t.Fatalf("a comment the provider takes none of: %v", err)
	}
}

// ─── refusals ────────────────────────────────────────────────────

// zapErrorText is every refusal's text, for the leak scan.
var zapErrorText []string

func expectZapError(t *testing.T, what string, want error, address string, amount int64, comment, eventID string) {
	t.Helper()
	result, err := ServiceSocialZap(context.Background(), address, amount, comment, eventID)
	if !errors.Is(err, want) {
		t.Fatalf("%s: %+v %v, want %v", what, result, err, want)
	}
	zapErrorText = append(zapErrorText, err.Error())
}

func TestSocialZapInvoiceInvalidParams(t *testing.T) {
	r := newInvoiceRig(t)
	for _, amount := range []int64{999, 100000001, 0, -1} {
		expectZapError(t, fmt.Sprintf("amount %d", amount), ErrServiceInvalidSocial, r.address, amount, "", "")
	}
	expectZapError(t, "long comment", ErrServiceInvalidSocial, r.address, 21000, strings.Repeat("é", 21), "")
	if _, err := ServiceSocialZap(context.Background(), r.address, 21000, strings.Repeat("é", 20), ""); err != nil {
		t.Fatalf("a comment at the limit: %v", err)
	}
	expectZapError(t, "bad event id", ErrServiceInvalidSocial, r.address, 21000, "", strings.Repeat("A", 64))
	expectZapError(t, "bad address", ErrServiceInvalidAddress, "bad", 21000, "", "")
	noZapRequestStored(t)
}

func TestSocialZapInvoiceProviderFailures(t *testing.T) {
	r := newInvoiceRig(t)
	good := lnurlpAnswer(r.provider.Public(), true)
	answers := map[string]string{
		"not a pay request": strings.Replace(good, `"tag":"payRequest"`, `"tag":"withdrawRequest"`, 1),
		"min above max":     strings.Replace(good, `"minSendable":1000`, `"minSendable":200000000`, 1),
		"http callback":     strings.Replace(good, `"https://pay.example.com/cb?k=1"`, `"http://pay.example.com/cb?k=1"`, 1),
		"userinfo callback": strings.Replace(good, `"https://pay.example.com/cb?k=1"`, `"https://u:p@pay.example.com/cb?k=1"`, 1),
		"fragment callback": strings.Replace(good, `"https://pay.example.com/cb?k=1"`, `"https://pay.example.com/cb?k=1#x"`, 1),
		"long callback":     strings.Replace(good, `"https://pay.example.com/cb?k=1"`, `"https://pay.example.com/cb?k=`+strings.Repeat("1", 2048)+`"`, 1),
		"no callback":       strings.Replace(good, `"callback":"https://pay.example.com/cb?k=1",`, ``, 1),
	}
	for what, answer := range answers {
		r.setAnswer(answer)
		expectZapError(t, what, ErrServiceUnavailable, r.address, 21000, "", "")
	}
	if n := len(r.callbacks()); n != 0 {
		t.Fatalf("a refused provider's callback was requested %d times", n)
	}

	r.setAnswer(good)
	callbacks := map[string]func(*http.Request) (int, string){
		"invoice mismatch": func(*http.Request) (int, string) { return 200, `{"pr":"` + zapInvoice100 + `"}` },
		"provider error":   func(*http.Request) (int, string) { return 200, `{"status":"ERROR","reason":"https://leak.example"}` },
		"not an invoice":   func(*http.Request) (int, string) { return 200, `{"pr":"https://leak.example/pay"}` },
		"no amount":        func(*http.Request) (int, string) { return 200, `{"pr":"lnbc1pqqqqqq"}` },
		"huge invoice":     func(*http.Request) (int, string) { return 200, `{"pr":"lnbc210n1` + strings.Repeat("q", 4096) + `"}` },
		"not json":         func(*http.Request) (int, string) { return 200, `leak` },
		"refused":          func(*http.Request) (int, string) { return 500, `{"reason":"leak"}` },
		"unreachable":      func(*http.Request) (int, string) { return 0, "" },
	}
	for what, callback := range callbacks {
		r.setCallback(callback)
		expectZapError(t, what, ErrServiceUnavailable, r.address, 21000, "", "")
	}

	// a 404 on the LNURL endpoint itself
	r.lud16.Store("bob@pay.example.com")
	lnurlpCache.Clear()
	expectZapError(t, "lnurlp 404", ErrServiceUnavailable, r.address, 21000, "", "")
	noZapRequestStored(t)
	if n := len(r.got()); n != 0 {
		t.Fatalf("published %d events", n)
	}
}

func TestSocialZapInvoiceNotZappable(t *testing.T) {
	r := newInvoiceRig(t)
	r.lud16.Store("")
	expectZapError(t, "no lud16", ErrServiceNotZappable, r.address, 21000, "", "")
	r.lud16.Store("not an address")
	expectZapError(t, "bad lud16", ErrServiceNotZappable, r.address, 21000, "", "")
	r.lud16.Store(zapLUD16)
	r.setAnswer(lnurlpAnswer(r.provider.Public(), false))
	expectZapError(t, "no nostr", ErrServiceNotZappable, r.address, 21000, "", "")
	r.setAnswer(`{"tag":"payRequest","callback":"https://pay.example.com/cb","minSendable":1,"maxSendable":100000,"allowsNostr":true,"nostrPubkey":"zz"}`)
	expectZapError(t, "bad provider key", ErrServiceNotZappable, r.address, 21000, "", "")
	if n := len(r.callbacks()); n != 0 {
		t.Fatalf("%d callbacks for an author who cannot be zapped", n)
	}
}

// ─── the signer and deadlines ────────────────────────────────────

func TestSocialZapInvoiceSigner(t *testing.T) {
	r := newInvoiceRig(t)
	withUserKeyer(t, nil, nostr.ZeroPK)
	expectZapError(t, "no signer", ErrServiceSignerUnavailable, r.address, 21000, "", "")
	if n := r.http.n.Load(); n != 0 {
		t.Fatalf("%d http requests without a signer", n)
	}

	withUserKeyer(t, stallKeyer{keyer.NewPlainKeySigner(r.me)}, r.me.Public())
	socialSignTimeout = 50 * time.Millisecond
	expectZapError(t, "stalled signer", ErrServiceTimeout, r.address, 21000, "", "")

	withUserKeyer(t, keyer.NewPlainKeySigner(nostr.Generate()), r.me.Public())
	expectZapError(t, "wrong key", ErrServiceUnavailable, r.address, 21000, "", "")
	if n := len(r.callbacks()); n != 0 {
		t.Fatalf("%d callbacks without a good signature", n)
	}
}

func TestSocialZapInvoiceCallbackTimeout(t *testing.T) {
	r := newInvoiceRig(t)
	r.setCallback(func(req *http.Request) (int, string) {
		<-req.Context().Done()
		return 0, ""
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := ServiceSocialZap(ctx, r.address, 21000, "", ""); !errors.Is(err, ErrServiceTimeout) {
		t.Fatalf("stalled callback: %v", err)
	}
}

// ─── wire hygiene ────────────────────────────────────────────────

func TestSocialZapInvoiceCarriesNoURLs(t *testing.T) {
	r := newInvoiceRig(t)
	result, err := ServiceSocialZap(context.Background(), r.address, 21000, "thanks", "")
	if err != nil {
		t.Fatal(err)
	}
	result.Invoice = ""
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 5 {
		t.Fatalf("result keys %v", m)
	}
	texts := append([]string{string(raw)}, zapErrorText...)
	for _, err := range []error{ErrServiceNotZappable, ErrServiceUnavailable, ErrServiceTimeout, ErrServiceInvalidSocial, ErrServiceSignerUnavailable} {
		texts = append(texts, err.Error())
	}
	for _, text := range texts {
		for _, marker := range []string{"https://", "pay.example.com", "lnurl", "wss://", "leak", "/"} {
			if strings.Contains(text, marker) {
				t.Errorf("%q carries %q", text, marker)
			}
		}
	}
}

func TestSocialZapCallbackURL(t *testing.T) {
	values := url.Values{"amount": {"21000"}}
	for callback, want := range map[string]string{
		"https://pay.example.com/cb":           "https://pay.example.com/cb?amount=21000",
		"https://pay.example.com/cb?k=1&z=%41": "https://pay.example.com/cb?k=1&z=%41&amount=21000",
		"https://pay.example.com/cb?":          "https://pay.example.com/cb?amount=21000",
		"http://pay.example.com/cb":            "",
		"https:///cb":                          "",
		"https://u@pay.example.com/cb":         "",
		"https://pay.example.com/cb#f":         "",
		"pay.example.com/cb":                   "",
		"":                                     "",
	} {
		got, ok := socialZapCallbackURL(callback, values)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v, want %q", callback, got, ok, want)
		}
	}
}
