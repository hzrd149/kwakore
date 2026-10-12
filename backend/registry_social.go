package backend

import (
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

// The social categories a client can ask a refresh for.
const (
	SocialReactions = "reactions"
	SocialComments  = "comments"
)

const (
	// socialCommentRunes caps comment text on the wire.
	socialCommentRunes = 2000
	// socialAuthorNameRunes caps an author's cached display name.
	socialAuthorNameRunes = 64
	// socialReactionRunes caps reaction content on the wire; ingest already
	// limits it to 64 bytes.
	socialReactionRunes = 64
	// socialVersionLimit is how many archived manifests name versions.
	socialVersionLimit = 20
	// socialEmojiURLBytes caps a NIP-30 emoji image url.
	socialEmojiURLBytes = 512
)

// ErrServiceInvalidSocial is a social request with categories outside
// SocialCategories or repeated ones.
var ErrServiceInvalidSocial = errors.New("invalid social parameters")

// SocialCategories is every category a refresh can name, sorted. The daemon
// validates params against it, so the two lists cannot drift.
func SocialCategories() []string {
	return []string{SocialComments, SocialReactions}
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
		if n.Address() == address && hex64.MatchString(n.EventID) && !slices.Contains(t.VersionIDs, n.EventID) {
			t.VersionIDs = append(t.VersionIDs, n.EventID)
		}
	}
	stateMu.Unlock()
	return t, nil
}

// socialTagNames are the address and event tag names that link evt's kind
// to its target: lowercase for a reaction, the uppercase root scope for a
// comment.
func socialTagNames(kind nostr.Kind) (string, string, bool) {
	switch kind {
	case nostr.KindReaction:
		return "a", "e", true
	case nostr.KindComment:
		return "A", "E", true
	}
	return "", "", false
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
		if tag[0] == eName && slices.Contains(t.VersionIDs, tag[1]) {
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
