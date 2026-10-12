package backend

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

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
)

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
