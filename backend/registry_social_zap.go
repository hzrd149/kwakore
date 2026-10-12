package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
)

// Napplet zaps: the NIP-57 receipts that pay a napplet's author for it, and
// (in ServiceSocialZap) the zap request a client pays itself. The provider
// behind the author's lightning address is the only key whose receipts
// count; a refresh records it in the status index, so reads never touch
// the network. No LNURL or callback URL, provider host or raw error ever
// leaves this file: failures are the service sentinels only.

// ErrServiceNotZappable is a zap target whose author has no lightning
// address, or one whose provider does not take nostr zaps.
var ErrServiceNotZappable = errors.New("the napplet author cannot receive zaps")

const (
	// socialZapLookupTimeout bounds the author's profile and LNURL lookup.
	socialZapLookupTimeout = 15 * time.Second
	// socialZapDescriptionMax caps a receipt's embedded zap request.
	socialZapDescriptionMax = 16 << 10
	// socialZapMaxMsats is 21 million bitcoin; no real zap is bigger.
	socialZapMaxMsats int64 = 2_100_000_000_000_000_000
	// socialZapRelaysPerSource caps the user's and the author's relays in a
	// zap request's "relays" tag, and socialZapRelayMax the whole tag.
	socialZapRelaysPerSource = 3
	socialZapRelayMax        = 6
	// socialZapCallbackMax caps the provider's callback url.
	socialZapCallbackMax = 2048
	// socialZapInvoiceMax caps the invoice a provider returns.
	socialZapInvoiceMax = 4096
	// socialZapAnswerMax caps the callback's answer.
	socialZapAnswerMax = 64 << 10
)

// socialZapInvoiceRe is a lowercased bolt11 invoice's shape.
var socialZapInvoiceRe = regexp.MustCompile(`^ln[0-9a-z]+$`)

// socialZapLUD16 is the lightning address in author's kind 0, or "". Tests
// replace it.
var socialZapLUD16 = func(ctx context.Context, author nostr.PubKey) string {
	if sys == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return sys.FetchProfileMetadata(ctx, author).LUD16
}

// socialZapEndpoint is an author's LNURL-pay answer, the key that signs
// their receipts, the endpoint and its bech32 lnurl.
type socialZapEndpoint struct {
	info     lnurlpInfo
	provider nostr.PubKey
	endpoint string
	lnurl    string
}

// socialZapLookup resolves author's lightning address to its LNURL-pay
// endpoint and receipt signer. No address, an invalid one or a provider
// without nostr support is not zappable; a lookup past ctx is a timeout and
// any other failure is unavailable.
func socialZapLookup(ctx context.Context, author nostr.PubKey) (socialZapEndpoint, error) {
	lud16 := socialZapLUD16(ctx, author)
	if strings.TrimSpace(lud16) == "" {
		return socialZapEndpoint{}, ErrServiceNotZappable
	}
	endpoint, ok := lnurlpURL(lud16)
	if !ok {
		return socialZapEndpoint{}, ErrServiceNotZappable
	}
	info, valid, err := lnurlpParams(ctx, lud16)
	if err != nil {
		if errors.Is(err, errIdentityTimeout) || ctx.Err() != nil {
			return socialZapEndpoint{}, ErrServiceTimeout
		}
		log.Warn().Msg("zap provider lookup failed")
		return socialZapEndpoint{}, ErrServiceUnavailable
	}
	if !valid {
		return socialZapEndpoint{}, ErrServiceNotZappable
	}
	provider, ok := info.provider()
	if !ok {
		return socialZapEndpoint{}, ErrServiceNotZappable
	}
	lnurl := bech32Encode("lnurl", []byte(endpoint))
	if lnurl == "" {
		return socialZapEndpoint{}, ErrServiceUnavailable
	}
	return socialZapEndpoint{info: info, provider: provider, endpoint: endpoint, lnurl: lnurl}, nil
}

// socialZapRequestMatches says whether a zap request is for t: exactly one
// "p", the author (NIP-57 allows no more), at most one "e" and one "a", and
// either the address in "a" (never for a snapshot, which has none) or a
// known version in "e".
func socialZapRequestMatches(req nostr.Event, t socialTarget) bool {
	var p, e, a []nostr.Tag
	for _, tag := range req.Tags {
		if len(tag) == 0 {
			continue
		}
		switch tag[0] {
		case "p":
			p = append(p, tag)
		case "e":
			e = append(e, tag)
		case "a":
			a = append(a, tag)
		}
	}
	if len(p) != 1 || len(p[0]) < 2 || p[0][1] != t.Author.Hex() || len(e) > 1 || len(a) > 1 {
		return false
	}
	if len(a) == 1 && len(a[0]) >= 2 && !t.Snapshot && a[0][1] == t.Address {
		return true
	}
	return len(e) == 1 && len(e[0]) >= 2 && slices.Contains(t.VersionIDs, e[0][1])
}

// socialZap is one verified receipt.
type socialZap struct {
	ReceiptID nostr.ID
	RequestID nostr.ID
	Sender    nostr.PubKey
	Msats     int64
	// Comment is the request's raw content; zapItems makes it safe.
	Comment string
	// EventID is the version the request names, or "".
	EventID string
	Receipt nostr.Event
	Request nostr.Event
}

// socialZapOf verifies a receipt for t from provider: a valid request for
// t, a positive invoice amount no bigger than 21 million bitcoin, and every
// "amount" tag of the request equal to it.
func socialZapOf(evt nostr.Event, t socialTarget, provider nostr.PubKey) (socialZap, bool) {
	if provider == nostr.ZeroPK {
		return socialZap{}, false
	}
	info, ok := verifyZapReceipt(evt, provider, func(req nostr.Event) bool { return socialZapRequestMatches(req, t) })
	if !ok || info.Msats <= 0 || info.Msats > socialZapMaxMsats {
		return socialZap{}, false
	}
	req := info.Request
	eventID := ""
	for _, tag := range req.Tags {
		if len(tag) == 0 {
			continue
		}
		switch tag[0] {
		case "amount":
			if len(tag) < 2 {
				return socialZap{}, false
			}
			n, err := strconv.ParseInt(tag[1], 10, 64)
			if err != nil || n != info.Msats {
				return socialZap{}, false
			}
		case "e":
			if len(tag) >= 2 && slices.Contains(t.VersionIDs, tag[1]) {
				eventID = tag[1]
			}
		}
	}
	return socialZap{
		ReceiptID: evt.ID, RequestID: req.ID, Sender: req.PubKey, Msats: info.Msats,
		Comment: req.Content, EventID: eventID, Receipt: evt, Request: req,
	}, true
}

// socialZapSender is the pubkey of a receipt's embedded request, parsed
// without any check. It only decides which receipts are the user's own.
func socialZapSender(evt nostr.Event) (nostr.PubKey, bool) {
	desc := evt.Tags.Find("description")
	if desc == nil || len(desc) < 2 {
		return nostr.ZeroPK, false
	}
	var req nostr.Event
	if err := req.UnmarshalJSON([]byte(desc[1])); err != nil || req.PubKey == nostr.ZeroPK {
		return nostr.ZeroPK, false
	}
	return req.PubKey, true
}

// socialZapFilters are the relay filters for t's receipts from provider:
// by address (never for a snapshot) and by version ids. No filter carries
// an empty tag list, and there are none without a provider.
func socialZapFilters(t socialTarget, provider nostr.PubKey) []nostr.Filter {
	if provider == nostr.ZeroPK {
		return nil
	}
	base := func(tags nostr.TagMap) nostr.Filter {
		return nostr.Filter{Kinds: []nostr.Kind{nostr.KindZap}, Authors: []nostr.PubKey{provider}, Tags: tags, Limit: socialLimits.PerFilter}
	}
	var filters []nostr.Filter
	if !t.Snapshot && t.Address != "" {
		filters = append(filters, base(nostr.TagMap{"a": {t.Address}}))
	}
	if len(t.VersionIDs) > 0 {
		filters = append(filters, base(nostr.TagMap{"e": slices.Clone(t.VersionIDs)}))
	}
	return filters
}

// socialZapIngestable says whether a relay's receipt may be kept for t: a
// real id and signature, an exact tag match, sane size and time, a verified
// request (socialZapOf) and, per NIP-57 appendix F, a request "lnurl" equal
// to the author's.
func socialZapIngestable(evt nostr.Event, t socialTarget, z socialZapEndpoint, now time.Time) bool {
	if evt.Kind != nostr.KindZap || len(evt.Tags) > socialMaxTags || len(evt.Content) > socialMaxContent ||
		evt.CreatedAt.Time().After(now.Add(socialFutureSkew)) {
		return false
	}
	desc := evt.Tags.Find("description")
	if desc == nil || len(desc) < 2 || len(desc[1]) > socialZapDescriptionMax {
		return false
	}
	if !evt.CheckID() || !evt.VerifySignature() || !socialMatches(evt, t) {
		return false
	}
	zap, ok := socialZapOf(evt, t, z.provider)
	if !ok {
		return false
	}
	for _, tag := range zap.Request.Tags {
		if len(tag) > 0 && tag[0] == "lnurl" && (len(tag) < 2 || !strings.EqualFold(tag[1], z.lnurl)) {
			return false
		}
	}
	return true
}

// socialZapIngest keeps every ingestable receipt of receipts. Receipts are
// never deletable (no NIP-09), so there is no tombstone check.
func socialZapIngest(t socialTarget, receipts []nostr.Event, z socialZapEndpoint) {
	now := time.Now()
	changed := false
	for _, evt := range receipts {
		if !socialZapIngestable(evt, t, z, now) {
			continue
		}
		switch err := sys.Store.SaveEvent(evt); {
		case err == nil:
			changed = true
		case errors.Is(err, eventstore.ErrDupEvent):
		default:
			log.Warn().Err(err).Msg("zap receipt could not be saved")
		}
	}
	if changed {
		socialGeneration.Add(1)
	}
}

// ServiceZaps pages a napplet's verified zaps newest first, counting only
// receipts signed by the provider the last zaps refresh recorded. NIP-09
// is never applied to receipts.
func ServiceZaps(ctx context.Context, address string, offset, limit int) (ServiceZapPage, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceZapPage{}, err
	}
	if sys == nil {
		return ServiceZapPage{}, ErrServiceUnavailable
	}
	if ctx.Err() != nil {
		return ServiceZapPage{}, ErrServiceTimeout
	}
	zaps := socialZapsOf(t, socialHeld(t, nostr.KindZap), socialZapProviderOf(address))
	items, total, next := pageOf(zapItems(zaps), offset, limit)
	items = slices.Clone(items)
	socialAuthorNames(items, func(z *ServiceZap) (string, *string) { return z.Sender, &z.SenderName })
	return ServiceZapPage{Items: items, Total: total, TotalMsats: zapSummary(zaps).TotalMsats, NextOffset: next}, nil
}

// ─── the invoice request ────────────────────────────────────────────────

// socialZapRelays is the zap request's "relays" tag, where the provider
// publishes the receipt: up to three of the user's read relays (the
// configured relays without any) and up to three public inbox relays of the
// author, whose list is not to be trusted; the discovery relays when both
// are empty, and at most six. Tests replace it.
var socialZapRelays = func(ctx context.Context, t socialTarget) []string {
	if sys == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	own := UserReadRelays()
	if len(own) == 0 {
		own = Relays()
	}
	own = normalizedUnique(own)
	if len(own) > socialZapRelaysPerSource {
		own = own[:socialZapRelaysPerSource]
	}
	_, read, _ := nip65Lists(ctx, t.Author)
	relays := normalizedUnique(append(own, socialPublicRelays(ctx, read, socialZapRelaysPerSource)...))
	if len(relays) == 0 {
		relays = normalizedUnique(discoveryRelays())
	}
	if len(relays) > socialZapRelayMax {
		relays = relays[:socialZapRelayMax]
	}
	return relays
}

// socialZapRequestEvent is the NIP-57 kind 9734 for a zap of amount msats
// on t: relays, amount, lnurl, the author in "p", the version in "e" when
// there is one, the address in "a" except for a snapshot, and the napplet
// kind in "k". The comment is its content.
func socialZapRequestEvent(t socialTarget, version string, relays []string, amount int64, lnurl, comment string, at nostr.Timestamp) nostr.Event {
	tags := nostr.Tags{
		append(nostr.Tag{"relays"}, relays...),
		{"amount", strconv.FormatInt(amount, 10)},
		{"lnurl", lnurl},
		{"p", t.Author.Hex()},
	}
	if version != "" {
		tags = append(tags, nostr.Tag{"e", version})
	}
	if !t.Snapshot {
		tags = append(tags, nostr.Tag{"a", t.Address})
	}
	tags = append(tags, nostr.Tag{"k", socialKindTag(t.Kind)})
	return nostr.Event{Kind: nostr.KindZapRequest, CreatedAt: at, Tags: tags, Content: comment}
}

// socialZapCallbackURL is the provider's callback with values appended
// (LUD-06), its own query left byte for byte. ok is false unless the
// callback is an https url with a host, no userinfo, no fragment and at
// most 2048 bytes.
func socialZapCallbackURL(callback string, values url.Values) (string, bool) {
	if callback == "" || len(callback) > socialZapCallbackMax || strings.Contains(callback, "#") {
		return "", false
	}
	u, err := url.Parse(callback)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", false
	}
	if len(values) == 0 {
		return callback, true
	}
	sep := "?"
	switch {
	case u.RawQuery != "":
		sep = "&"
	case strings.HasSuffix(callback, "?"):
		sep = ""
	}
	return callback + sep + values.Encode(), true
}

// socialZapInvoice asks the callback for an invoice and checks it is a
// bolt11 for exactly amount msats. Nothing the provider says, nor the url,
// is logged or returned.
func socialZapInvoice(ctx context.Context, callbackURL string, amount int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL, nil)
	if err != nil {
		return "", ErrServiceUnavailable
	}
	resp, err := resourceClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrServiceTimeout
		}
		log.Warn().Msg("zap invoice request failed")
		return "", ErrServiceUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Msg("zap invoice request refused")
		return "", ErrServiceUnavailable
	}
	var answer struct {
		PR     string `json:"pr"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, socialZapAnswerMax)).Decode(&answer); err != nil {
		if ctx.Err() != nil {
			return "", ErrServiceTimeout
		}
		log.Warn().Msg("zap invoice answer unreadable")
		return "", ErrServiceUnavailable
	}
	if strings.EqualFold(answer.Status, "ERROR") {
		log.Warn().Msg("zap provider refused the invoice")
		return "", ErrServiceUnavailable
	}
	pr := strings.ToLower(strings.TrimSpace(answer.PR))
	if len(pr) > socialZapInvoiceMax || !socialZapInvoiceRe.MatchString(pr) {
		log.Warn().Msg("zap provider returned no invoice")
		return "", ErrServiceUnavailable
	}
	if msats, ok := bolt11Msats(pr); !ok || msats != amount {
		log.Warn().Msg("zap invoice amount differs from the request")
		return "", ErrServiceUnavailable
	}
	return pr, nil
}

// ServiceSocialZap asks the napplet author's lightning provider for an
// invoice of amountMsats that zaps the napplet, on the version eventID
// names (else the installed or newest known one). It signs a NIP-57 zap
// request with the service signer and hands it to the provider's callback,
// but never publishes or stores it, and the daemon never pays: the client
// pays the returned bolt11 itself, and the provider then publishes the
// receipt to the request's relays. Only the invoice's amount is checked,
// not its description hash, since no bolt11 decoder is vendored. A profile
// with only a lud06 is not zappable.
func ServiceSocialZap(ctx context.Context, address string, amountMsats int64, comment, eventID string) (ServiceSocialZapResult, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceSocialZapResult{}, err
	}
	if amountMsats < 1 || amountMsats > socialZapMaxMsats || len(comment) > socialMaxContent ||
		(eventID != "" && !hex64.MatchString(eventID)) {
		return ServiceSocialZapResult{}, ErrServiceInvalidSocial
	}
	if sys == nil {
		return ServiceSocialZapResult{}, ErrServiceUnavailable
	}
	if ctx.Err() != nil {
		return ServiceSocialZapResult{}, ErrServiceTimeout
	}
	k, pk, err := socialSigner()
	if err != nil {
		return ServiceSocialZapResult{}, err
	}
	version := ""
	if eventID != "" || t.Snapshot {
		if version, err = socialVersion(t, eventID); err != nil {
			return ServiceSocialZapResult{}, err
		}
	} else if v, err := socialVersion(t, ""); err == nil {
		version = v
	}

	lookupCtx, cancel := context.WithTimeout(ctx, socialZapLookupTimeout)
	z, err := socialZapLookup(lookupCtx, t.Author)
	cancel()
	if err != nil {
		return ServiceSocialZapResult{}, err
	}
	info := z.info
	if info.Tag != "payRequest" || info.MinSendable < 1 || info.MaxSendable < info.MinSendable {
		log.Warn().Msg("zap provider answer is not a pay request")
		return ServiceSocialZapResult{}, ErrServiceUnavailable
	}
	if _, ok := socialZapCallbackURL(info.Callback, nil); !ok {
		log.Warn().Msg("zap provider callback refused")
		return ServiceSocialZapResult{}, ErrServiceUnavailable
	}
	if amountMsats < info.MinSendable || amountMsats > info.MaxSendable || utf8.RuneCountInString(comment) > info.CommentAllowed {
		return ServiceSocialZapResult{}, ErrServiceInvalidSocial
	}
	relays := socialZapRelays(ctx, t)
	if len(relays) == 0 {
		return ServiceSocialZapResult{}, ErrServiceUnavailable
	}

	tmpl := socialZapRequestEvent(t, version, relays, amountMsats, z.lnurl, comment, nostr.Now())
	release, err := socialAcquire(ctx)
	if err != nil {
		return ServiceSocialZapResult{}, err
	}
	signed, err := socialSign(ctx, k, pk, tmpl)
	release()
	if err != nil {
		return ServiceSocialZapResult{}, err
	}

	values := url.Values{}
	values.Set("amount", strconv.FormatInt(amountMsats, 10))
	values.Set("nostr", signed.String())
	values.Set("lnurl", z.lnurl)
	if info.CommentAllowed > 0 && comment != "" {
		values.Set("comment", comment)
	}
	callback, ok := socialZapCallbackURL(info.Callback, values)
	if !ok {
		return ServiceSocialZapResult{}, ErrServiceUnavailable
	}
	invoice, err := socialZapInvoice(ctx, callback, amountMsats)
	if err != nil {
		return ServiceSocialZapResult{}, err
	}
	log.Info().Int64("msats", amountMsats).Msg("zap invoice ready")
	return ServiceSocialZapResult{
		Address: address, Invoice: invoice, AmountMsats: amountMsats,
		Recipient: t.Author.Hex(), ZapRequestID: signed.ID.Hex(),
	}, nil
}

// ServiceSocialZapResult is napplet.zap's result: the bolt11 to pay and the
// id of the zap request it carries. It never names the provider.
type ServiceSocialZapResult struct {
	Address      string `json:"address"`
	Invoice      string `json:"invoice"`
	AmountMsats  int64  `json:"amount_msats"`
	Recipient    string `json:"recipient"`
	ZapRequestID string `json:"zap_request_id"`
}
