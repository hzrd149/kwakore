package backend

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"fiatjaf.com/nostr"
)

// Napplet social data is the NIP-25 reactions (votes and emoji) and NIP-22
// comments other people publish about a napplet. It is read per canonical
// address (Napp.Address), installed or not, and only ever from the local
// event store: a refresh (registry_social_fetch.go) is the one path that
// asks relays, and it keeps what it verified in that same store.
//
// An event is about a napplet when it names the napplet exactly: a kind 7
// whose "a" tag is the canonical address or whose "e" tag is one of its
// known version ids, and a kind 1111 whose root "A" or "E" tag does the
// same. A snapshot (5129) has no NIP-01 address, so only its event id
// names it. Every local read post-filters these exact tags, because the
// LMDB and bolt indexes do not hold full values: a tag value over 100 bytes
// is not indexed at all, a 64-hex value is indexed by its first 8 bytes and
// an a-shaped value keeps only 30 bytes of its d. An index hit is therefore
// a candidate, never a match.
//
// A napplet is also mentioned by kind 1 notes, which name it with an "a"
// tag (a mention) or a NIP-18 "q" tag (a quote). A "q" may name the address
// as well as a version id; a snapshot is again named only by its id.
//
// A napplet is zapped through NIP-57 kind 9735 receipts, which copy the "a"
// and "e" tags of their zap request. A receipt counts only when the provider
// behind the author's lightning address signed it and its embedded kind 9734
// request is valid, names the author as its one "p", names the napplet by
// "a" (never a snapshot) or by a known version's "e", and asks for exactly
// the invoice's amount (registry_social_zap.go).

// The social categories a client can ask a refresh for.
const (
	SocialReactions = "reactions"
	SocialComments  = "comments"
	SocialMentions  = "mentions"
	SocialZaps      = "zaps"
)

const (
	// socialCommentRunes caps comment text on the wire.
	socialCommentRunes = 2000
	// socialMentionRunes caps mention text on the wire.
	socialMentionRunes = 2000
	// socialAuthorNameRunes caps an author's cached display name.
	socialAuthorNameRunes = 64
	// socialReactionRunes caps reaction content on the wire; ingest already
	// limits it to 64 bytes.
	socialReactionRunes = 64
	// socialVersionLimit is how many archived manifests name versions.
	socialVersionLimit = 20
	// socialEmojiURLBytes caps a NIP-30 emoji image url.
	socialEmojiURLBytes = 512
	// socialZapCommentRunes caps a zap comment on the wire.
	socialZapCommentRunes = 280
	// socialZapTopLimit caps a summary's top zappers.
	socialZapTopLimit = 5
)

// ErrServiceInvalidSocial is a social request with categories outside
// SocialCategories or repeated ones.
var ErrServiceInvalidSocial = errors.New("invalid social parameters")

// SocialCategories is every category a refresh can name, sorted. The daemon
// validates params against it, so the two lists cannot drift.
func SocialCategories() []string {
	return []string{SocialComments, SocialMentions, SocialReactions, SocialZaps}
}

// socialTarget is one canonical napplet address and the event ids that name
// its versions.
type socialTarget struct {
	Address  string
	Author   nostr.PubKey
	Kind     nostr.Kind
	Snapshot bool
	// VersionIDs are the lowercase hex ids of the newest archived manifests
	// of the address, then the installed one; a snapshot's is its own id.
	VersionIDs []string
	// Installed is the installed record's event id for the address, or "".
	Installed string
}

// newSocialTarget parses a canonical address and collects its version ids
// from the manifest archive (read only) and the installed record.
func newSocialTarget(address string) (socialTarget, error) {
	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil {
		return socialTarget{}, err
	}
	t := socialTarget{Address: address, Author: ptr.PublicKey, Kind: ptr.Kind, VersionIDs: []string{}}
	if ptr.Kind == KindSnapshotNapplet {
		t.Snapshot = true
		t.VersionIDs = []string{ptr.Identifier}
		return t, nil
	}
	if a := manifestHistory.Load(); a != nil {
		records, _, _, err := a.page(address, false, 0, socialVersionLimit)
		if err != nil {
			log.Warn().Err(err).Msg("social version ids could not be read")
		}
		for _, record := range records {
			if id := record.Event.ID.Hex(); !slices.Contains(t.VersionIDs, id) {
				t.VersionIDs = append(t.VersionIDs, id)
			}
		}
	}
	stateMu.Lock()
	for _, n := range state.InstalledNapps {
		if n.Address() != address || !hex64.MatchString(n.EventID) {
			continue
		}
		t.Installed = n.EventID
		if !slices.Contains(t.VersionIDs, n.EventID) {
			t.VersionIDs = append(t.VersionIDs, n.EventID)
		}
	}
	stateMu.Unlock()
	return t, nil
}

// socialTagNames are the address and event tag names that link evt's kind
// to its target: lowercase for a reaction and for a zap receipt (which
// copies its request's), the uppercase root scope for a comment, and "a"
// and the NIP-18 quote "q" for a note.
func socialTagNames(kind nostr.Kind) (string, string, bool) {
	switch kind {
	case nostr.KindReaction, nostr.KindZap:
		return "a", "e", true
	case nostr.KindComment:
		return "A", "E", true
	case nostr.KindTextNote:
		return "a", "q", true
	}
	return "", "", false
}

// socialRefValue says whether value, in kind's event tag, names t: a known
// version id, or for a note's "q" also the address (never a snapshot's).
func socialRefValue(t socialTarget, kind nostr.Kind, value string) bool {
	if slices.Contains(t.VersionIDs, value) {
		return true
	}
	return kind == nostr.KindTextNote && !t.Snapshot && t.Address != "" && value == t.Address
}

// socialRefValues are the values kind's event tag may carry to name t: the
// tag list of its relay filter and local query.
func socialRefValues(t socialTarget, kind nostr.Kind) []string {
	values := slices.Clone(t.VersionIDs)
	if kind == nostr.KindTextNote && !t.Snapshot && t.Address != "" {
		values = append([]string{t.Address}, values...)
	}
	return values
}

// socialMentionQuote says whether a note quotes t: one of its "q" tags
// names it.
func socialMentionQuote(evt nostr.Event, t socialTarget) bool {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "q" && socialRefValue(t, nostr.KindTextNote, tag[1]) {
			return true
		}
	}
	return false
}

// socialMatches says whether evt names t exactly.
func socialMatches(evt nostr.Event, t socialTarget) bool {
	aName, eName, ok := socialTagNames(evt.Kind)
	if !ok {
		return false
	}
	for _, tag := range evt.Tags {
		if len(tag) < 2 {
			continue
		}
		if tag[0] == aName && !t.Snapshot && tag[1] == t.Address {
			return true
		}
		if tag[0] == eName && socialRefValue(t, evt.Kind, tag[1]) {
			return true
		}
	}
	return false
}

// socialVersionOf is the first version of t that evt names by event id, or
// "" when it names none (an address-only or unknown-version event).
func socialVersionOf(evt nostr.Event, t socialTarget) string {
	_, eName, ok := socialTagNames(evt.Kind)
	if !ok {
		return ""
	}
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == eName && slices.Contains(t.VersionIDs, tag[1]) {
			return tag[1]
		}
	}
	return ""
}

// socialText is attacker-authored text made safe to show: newline and tab
// stay, every other control rune and every format (Cf, bidi) rune goes, and
// at most limit runes are kept. truncated says runes were cut.
func socialText(raw string, limit int) (string, bool) {
	var b strings.Builder
	count := 0
	for _, r := range raw {
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			continue
		}
		if count >= limit {
			return b.String(), true
		}
		b.WriteRune(r)
		count++
	}
	return b.String(), false
}

// socialEmojiURL is a NIP-30 emoji image url when it is safe to hand a
// client (https, a host, no userinfo, at most 512 bytes), else "".
func socialEmojiURL(raw string) string {
	if len(raw) > socialEmojiURLBytes || !strings.HasPrefix(raw, "https://") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return raw
}

// socialCategoriesParam is the categories a refresh names: all of them when
// none are given, else a subset of SocialCategories with no repeats.
func socialCategoriesParam(categories []string) ([]string, error) {
	if len(categories) == 0 {
		return SocialCategories(), nil
	}
	for i, c := range categories {
		if !slices.Contains(SocialCategories(), c) || slices.Contains(categories[:i], c) {
			return nil, ErrServiceInvalidSocial
		}
	}
	return slices.Clone(categories), nil
}

// ServiceSocial is the reactions, comments, mentions and zaps summary of a napplet address,
// installed or not, read from the local store. refresh first asks relays for
// the named categories (all when none are named), within the refresh floor.
func ServiceSocial(ctx context.Context, address string, refresh bool, categories []string) (ServiceSocialSummary, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceSocialSummary{}, err
	}
	categories, err = socialCategoriesParam(categories)
	if err != nil {
		return ServiceSocialSummary{}, err
	}
	if sys == nil {
		return ServiceSocialSummary{}, ErrServiceUnavailable
	}
	if ctx.Err() != nil {
		return ServiceSocialSummary{}, ErrServiceTimeout
	}
	if refresh {
		if err := socialRefresh(ctx, t, categories); err != nil || ctx.Err() != nil {
			return ServiceSocialSummary{}, ErrServiceTimeout
		}
	}
	summary, live := socialAggregate(t)
	summary.Sync = socialSyncStatus(address)
	if viewer := socialViewer(); viewer != nil {
		summary.Own = socialOwnState(t, live, *viewer)
	}
	return summary, nil
}

// socialRead is the target and its live held events of kind, after the
// shared checks of the paged reads.
func socialRead(ctx context.Context, address, id string, kind nostr.Kind) (socialTarget, []nostr.Event, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return socialTarget{}, nil, err
	}
	if id != "" && !hex64.MatchString(id) {
		return socialTarget{}, nil, ErrServiceInvalidSocial
	}
	if sys == nil {
		return socialTarget{}, nil, ErrServiceUnavailable
	}
	if ctx.Err() != nil {
		return socialTarget{}, nil, ErrServiceTimeout
	}
	held := socialHeld(t, kind)
	return t, socialLive(held, socialDeletions(held)), nil
}

// ServiceReactions pages a napplet's reactions newest first, all of them or
// those on one version. An unknown version is an empty page.
func ServiceReactions(ctx context.Context, address, eventID string, offset, limit int) (ServiceReactionPage, error) {
	t, live, err := socialRead(ctx, address, eventID, nostr.KindReaction)
	if err != nil {
		return ServiceReactionPage{}, err
	}
	items, total, next := pageOf(reactionItems(t, live, eventID), offset, limit)
	items = slices.Clone(items)
	socialAuthorNames(items, func(r *ServiceReaction) (string, *string) { return r.Author, &r.AuthorName })
	return ServiceReactionPage{Items: items, Total: total, NextOffset: next}, nil
}

// ServiceComments pages the top-level comments of a napplet, or the replies
// to one comment, newest first. An unknown parent is an empty page.
func ServiceComments(ctx context.Context, address, parentID string, offset, limit int) (ServiceCommentPage, error) {
	t, live, err := socialRead(ctx, address, parentID, nostr.KindComment)
	if err != nil {
		return ServiceCommentPage{}, err
	}
	items, total, next := pageOf(commentItems(t, live, parentID, socialViewer()), offset, limit)
	items = slices.Clone(items)
	socialAuthorNames(items, func(c *ServiceComment) (string, *string) { return c.Author, &c.AuthorName })
	return ServiceCommentPage{Items: items, Total: total, NextOffset: next}, nil
}

// ServiceMentions pages the kind 1 notes that mention or quote a napplet,
// newest first.
func ServiceMentions(ctx context.Context, address string, offset, limit int) (ServiceMentionPage, error) {
	t, live, err := socialRead(ctx, address, "", nostr.KindTextNote)
	if err != nil {
		return ServiceMentionPage{}, err
	}
	items, total, next := pageOf(mentionItems(t, live), offset, limit)
	items = slices.Clone(items)
	socialAuthorNames(items, func(m *ServiceMention) (string, *string) { return m.Author, &m.AuthorName })
	return ServiceMentionPage{Items: items, Total: total, NextOffset: next}, nil
}

// ─── wire types ─────────────────────────────────────────────────────────

// ServiceSocialSync is one category's last refresh: a lookup_status value
// and when it finished, or null when it never ran.
type ServiceSocialSync struct {
	Status   string `json:"status"`
	SyncedAt *int64 `json:"synced_at"`
}

type ServiceSocialVotes struct {
	Up    int `json:"up"`
	Down  int `json:"down"`
	Score int `json:"score"`
}

type ServiceSocialEmoji struct {
	Content   string `json:"content"`
	Shortcode string `json:"shortcode,omitempty"`
	URL       string `json:"url,omitempty"`
	Count     int    `json:"count"`
}

type ServiceSocialByEvent struct {
	EventID string `json:"event_id"`
	Up      int    `json:"up"`
	Down    int    `json:"down"`
	Emoji   int    `json:"emoji"`
}

type ServiceSocialCommentSummary struct {
	Count    int    `json:"count"`
	LatestAt *int64 `json:"latest_at"`
}

type ServiceSocialMentionSummary struct {
	Count int `json:"count"`
}

// ServiceSocialZapper is one sender's verified zaps of a napplet.
type ServiceSocialZapper struct {
	Pubkey string `json:"pubkey"`
	Msats  int64  `json:"msats"`
	Count  int    `json:"count"`
}

// ServiceSocialZapSummary totals a napplet's verified zaps. Top is the
// biggest senders, at most five, never null.
type ServiceSocialZapSummary struct {
	Count      int                   `json:"count"`
	TotalMsats int64                 `json:"total_msats"`
	Top        []ServiceSocialZapper `json:"top"`
}

type ServiceSocialOwnReaction struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	EventID string `json:"event_id"`
}

type ServiceSocialOwn struct {
	Vote        string                     `json:"vote"`
	VoteEventID string                     `json:"vote_event_id"`
	Reactions   []ServiceSocialOwnReaction `json:"reactions"`
}

// ServiceSocialSummary is napplet.social's result.
type ServiceSocialSummary struct {
	Address  string                       `json:"address"`
	Sync     map[string]ServiceSocialSync `json:"sync"`
	Votes    ServiceSocialVotes           `json:"votes"`
	Emoji    []ServiceSocialEmoji         `json:"emoji"`
	ByEvent  []ServiceSocialByEvent       `json:"by_event"`
	Comments ServiceSocialCommentSummary  `json:"comments"`
	Mentions ServiceSocialMentionSummary  `json:"mentions"`
	Zaps     ServiceSocialZapSummary      `json:"zaps"`
	// Own is null when no signer is set up.
	Own *ServiceSocialOwn `json:"own"`
}

type ServiceReaction struct {
	ID         string `json:"id"`
	Author     string `json:"author"`
	AuthorName string `json:"author_name,omitempty"`
	Content    string `json:"content"`
	Shortcode  string `json:"shortcode,omitempty"`
	URL        string `json:"url,omitempty"`
	EventID    string `json:"event_id"`
	CreatedAt  int64  `json:"created_at"`
}

type ServiceComment struct {
	ID         string `json:"id"`
	Author     string `json:"author"`
	AuthorName string `json:"author_name,omitempty"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
	CreatedAt  int64  `json:"created_at"`
	ParentID   string `json:"parent_id"`
	ReplyCount int    `json:"reply_count"`
	Own        bool   `json:"own"`
}

type ServiceReactionPage struct {
	Items      []ServiceReaction `json:"items"`
	Total      int               `json:"total"`
	NextOffset *int              `json:"next_offset"`
}

type ServiceCommentPage struct {
	Items      []ServiceComment `json:"items"`
	Total      int              `json:"total"`
	NextOffset *int             `json:"next_offset"`
}

// ServiceMention is a kind 1 note about a napplet. Quote is true when the
// note quotes it with a "q" tag, false for an "a" tag mention only.
type ServiceMention struct {
	ID         string `json:"id"`
	Author     string `json:"author"`
	AuthorName string `json:"author_name,omitempty"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated"`
	CreatedAt  int64  `json:"created_at"`
	Quote      bool   `json:"quote"`
}

type ServiceMentionPage struct {
	Items      []ServiceMention `json:"items"`
	Total      int              `json:"total"`
	NextOffset *int             `json:"next_offset"`
}

// ServiceZap is one verified zap receipt of a napplet. Sender is the zap
// request's pubkey; EventID is the version the request names, or "".
type ServiceZap struct {
	ReceiptID   string `json:"receipt_id"`
	Sender      string `json:"sender"`
	SenderName  string `json:"sender_name,omitempty"`
	AmountMsats int64  `json:"amount_msats"`
	Comment     string `json:"comment"`
	EventID     string `json:"event_id"`
	CreatedAt   int64  `json:"created_at"`
}

// ServiceZapPage is napplet.zaps' result. TotalMsats sums every verified
// receipt, not only the page.
type ServiceZapPage struct {
	Items      []ServiceZap `json:"items"`
	Total      int          `json:"total"`
	TotalMsats int64        `json:"total_msats"`
	NextOffset *int         `json:"next_offset"`
}
