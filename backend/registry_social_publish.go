package backend

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

// Social actions: voting on a napplet, reacting to it with an emoji,
// commenting on it (or replying to a comment), sharing it in a kind 1 note
// and deleting one's own reaction, comment or share. Each signs one event
// with the service signer, publishes it to the user's write relays plus the
// napplet author's inbox, and applies the change to the local social data
// at once, so napplet.social shows it without a refresh. Reaching no relay
// is not an error: the event stays in the local store and the result says
// published 0.
//
// No relay URL or raw error ever leaves this file: publishSigned's
// per-relay map is reduced to the published and failed counts, and the
// relay hint only ever goes into the signed event.

// ErrServiceSignerUnavailable is a social action without a signer set up.
var ErrServiceSignerUnavailable = errors.New("no signer is set up")

var (
	// socialSignTimeout bounds one signing, local or remote.
	socialSignTimeout = 20 * time.Second
	// socialPublishTimeout bounds one publish to every target relay.
	socialPublishTimeout = relayQueryTimeout
	// socialActions runs one social action at a time, so a slow signer
	// never has two prompts out and votes never race each other.
	socialActions = make(chan struct{}, 1)
	// socialDeletableKinds are the kinds a client may delete through
	// napplet.delete: reactions, comments and shared notes.
	socialDeletableKinds = []nostr.Kind{nostr.KindReaction, nostr.KindComment, nostr.KindTextNote}
)

const (
	// socialVoteDeleteLimit caps the previous own votes one kind 5 names.
	socialVoteDeleteLimit = 10
	// socialInboxRelays caps the inbox relays taken from one author's list.
	socialInboxRelays = 4
	// socialShareContentMax caps a share's text in bytes. It leaves room for
	// the blank line and the longest nostr: URI (two 255-byte TLV values plus
	// the key and the kind), so a share stays within the 8 KiB every reader
	// accepts.
	socialShareContentMax = socialMaxContent - 1024
	// socialNIP19Value is the most bytes one NIP-19 TLV value can hold: its
	// length is a single byte.
	socialNIP19Value = 255
)

// socialPublishRelays is where a social action publishes: the user's write
// relays (the configured relays without any), up to four public inbox
// relays of the napplet author and, for a reply, up to four of the parent
// comment's author. hint is the author's first public outbox relay, the
// NIP-25 relay hint of the "e" tag, or "". Author lists are attacker
// controlled, so every entry passes napExplicitRelay. Tests replace it.
var socialPublishRelays = func(ctx context.Context, t socialTarget, parent nostr.PubKey) (targets []string, hint string) {
	if sys == nil {
		return nil, ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	targets = UserWriteRelays()
	if len(targets) == 0 {
		targets = slices.Clone(Relays())
	}
	write, read, _ := nip65Lists(ctx, t.Author)
	targets = append(targets, socialPublicRelays(ctx, read, socialInboxRelays)...)
	if h := socialPublicRelays(ctx, write, 1); len(h) > 0 {
		hint = h[0]
	}
	if parent != nostr.ZeroPK && parent != t.Author {
		_, parentRead, _ := nip65Lists(ctx, parent)
		targets = append(targets, socialPublicRelays(ctx, parentRead, socialInboxRelays)...)
	}
	return normalizedUnique(targets), hint
}

// socialPublicRelays is up to n entries of list that are public relays.
func socialPublicRelays(ctx context.Context, list []string, n int) []string {
	var out []string
	for _, raw := range list {
		if len(out) == n {
			break
		}
		if u, err := napExplicitRelay(ctx, raw); err == nil {
			out = append(out, u)
		}
	}
	return out
}

// socialSigner is the service signer and its public key.
func socialSigner() (nostr.Keyer, nostr.PubKey, error) {
	k, pk := identitySnapshot()
	if k == nil || pk == nostr.ZeroPK {
		return nil, nostr.ZeroPK, ErrServiceSignerUnavailable
	}
	return k, pk, nil
}

// socialAcquire takes the one social action slot; release gives it back.
func socialAcquire(ctx context.Context) (release func(), err error) {
	select {
	case socialActions <- struct{}{}:
		return func() { <-socialActions }, nil
	case <-ctx.Done():
		return nil, ErrServiceTimeout
	}
}

// socialSignPublish signs tmpl with k and publishes it to targets, keeping
// it locally first. A signing past socialSignTimeout or ctx is a timeout.
// A signer's answer is not trusted: another key, a changed template, a bad
// id or a bad signature is unavailable, and then nothing is stored or
// published. Only the relay counts come back.
func socialSignPublish(ctx context.Context, k nostr.Keyer, pk nostr.PubKey, tmpl nostr.Event, targets []string) (nostr.Event, int, int, error) {
	evt, err := socialSign(ctx, k, pk, tmpl)
	if err != nil {
		return nostr.Event{}, 0, 0, err
	}
	pubCtx, cancel := context.WithTimeout(ctx, socialPublishTimeout)
	result := publishSigned(pubCtx, evt, targets)
	cancel()
	published, _ := result["published"].(int)
	failed, _ := result["failed"].(int)
	log.Info().Uint16("kind", uint16(evt.Kind)).Int("published", published).Int("failed", failed).Msg("social event published")
	return evt, published, failed, nil
}

// socialSign signs tmpl with k. A signing past socialSignTimeout or ctx is
// a timeout; another key, a changed template, a bad id or a bad signature
// is unavailable.
func socialSign(ctx context.Context, k nostr.Keyer, pk nostr.PubKey, tmpl nostr.Event) (nostr.Event, error) {
	evt := tmpl
	evt.Tags = slices.Clone(tmpl.Tags)
	signCtx, cancel := context.WithTimeout(ctx, socialSignTimeout)
	err := k.SignEvent(signCtx, &evt)
	expired := signCtx.Err() != nil || ctx.Err() != nil
	cancel()
	if expired {
		log.Warn().Uint16("kind", uint16(tmpl.Kind)).Msg("social event signing timed out")
		return nostr.Event{}, ErrServiceTimeout
	}
	if err != nil || evt.PubKey != pk || !socialSameTemplate(evt, tmpl) || !evt.CheckID() || !evt.VerifySignature() {
		log.Warn().Uint16("kind", uint16(tmpl.Kind)).Msg("social event was not signed")
		return nostr.Event{}, ErrServiceUnavailable
	}
	return evt, nil
}

// socialSameTemplate says whether the signer kept what it was asked to sign.
func socialSameTemplate(evt, tmpl nostr.Event) bool {
	return evt.Kind == tmpl.Kind && evt.CreatedAt == tmpl.CreatedAt && evt.Content == tmpl.Content &&
		slices.EqualFunc(evt.Tags, tmpl.Tags, func(a, b nostr.Tag) bool { return slices.Equal(a, b) })
}

// ─── templates ──────────────────────────────────────────────────────────

func socialKindTag(kind nostr.Kind) string { return strconv.Itoa(int(kind)) }

// socialReactionEvent is a NIP-25 kind 7 on version of t: the "e" tag (with
// the relay hint when there is one), "p" the author, "k" the napplet kind,
// "a" the address except for a snapshot, which has none, and the NIP-30
// "emoji" tag for a :shortcode: reaction.
func socialReactionEvent(t socialTarget, version, hint, content, emojiURL string, at nostr.Timestamp) nostr.Event {
	e := nostr.Tag{"e", version}
	if hint != "" {
		e = append(e, hint)
	}
	tags := nostr.Tags{e, {"p", t.Author.Hex()}, {"k", socialKindTag(t.Kind)}}
	if !t.Snapshot {
		tags = append(tags, nostr.Tag{"a", t.Address})
	}
	if shortcode := socialShortcode(content); shortcode != "" {
		tags = append(tags, nostr.Tag{"emoji", shortcode, emojiURL})
	}
	return nostr.Event{Kind: nostr.KindReaction, CreatedAt: at, Tags: tags, Content: content}
}

// socialCommentEvent is a NIP-22 kind 1111 on t. The root scope is the
// address ("E" the version for a snapshot) with its kind and author. A
// top-level comment's parent is the napplet itself, by address and version
// event; a reply's parent is the comment it answers.
func socialCommentEvent(t socialTarget, version string, parent *nostr.Event, content string) nostr.Event {
	kind := socialKindTag(t.Kind)
	author := t.Author.Hex()
	root := nostr.Tag{"A", t.Address}
	if t.Snapshot {
		root = nostr.Tag{"E", version}
	}
	tags := nostr.Tags{root, {"K", kind}, {"P", author}}
	if parent == nil {
		if !t.Snapshot {
			tags = append(tags, nostr.Tag{"a", t.Address})
		}
		tags = append(tags, nostr.Tag{"e", version}, nostr.Tag{"k", kind}, nostr.Tag{"p", author})
	} else {
		tags = append(tags,
			nostr.Tag{"e", parent.ID.Hex()},
			nostr.Tag{"k", socialKindTag(nostr.KindComment)},
			nostr.Tag{"p", parent.PubKey.Hex()},
		)
	}
	return nostr.Event{Kind: nostr.KindComment, CreatedAt: nostr.Now(), Tags: tags, Content: content}
}

// socialDeletionEvent is a NIP-09 kind 5 naming targets in order, then
// each of their kinds once, ascending.
func socialDeletionEvent(targets []nostr.Event) nostr.Event {
	tags := nostr.Tags{}
	var kinds []nostr.Kind
	for _, evt := range targets {
		tags = append(tags, nostr.Tag{"e", evt.ID.Hex()})
		if !slices.Contains(kinds, evt.Kind) {
			kinds = append(kinds, evt.Kind)
		}
	}
	slices.Sort(kinds)
	for _, kind := range kinds {
		tags = append(tags, nostr.Tag{"k", socialKindTag(kind)})
	}
	return nostr.Event{Kind: nostr.KindDeletion, CreatedAt: nostr.Now(), Tags: tags, Content: ""}
}

// socialShareEntity is the canonical NIP-19 entity of t for a share's
// nostr: URI: an naddr, or an nevent for a snapshot (without a kind, which
// the pinned encoder cannot write and NIP-19 makes optional). hint, when
// set, is its one relay; it only ever goes inside the signed event. ok is
// false when t cannot be encoded, such as a d tag over 255 bytes.
func socialShareEntity(t socialTarget, hint string) (string, bool) {
	var relays []string
	if hint != "" {
		relays = []string{hint}
	}
	if t.Snapshot {
		if len(t.VersionIDs) == 0 {
			return "", false
		}
		id, err := nostr.IDFromHex(t.VersionIDs[0])
		if err != nil {
			return "", false
		}
		return nip19.EncodeNevent(id, relays, t.Author), true
	}
	ptr, err := ParseCanonicalServiceAddress(t.Address)
	if err != nil || len(ptr.Identifier) > socialNIP19Value {
		return "", false
	}
	return nip19.EncodeNaddr(t.Author, t.Kind, ptr.Identifier, relays), true
}

// socialShareEvent is a NIP-18 quote note sharing t: the text without its
// trailing whitespace, a blank line and the nostr: URI (the bare URI when
// the text is blank). It quotes t with "q" (a snapshot's positional id,
// relay and author), tags the author with "p" and names the address with
// "a" except for a snapshot.
func socialShareEvent(t socialTarget, hint, entity, content string, at nostr.Timestamp) nostr.Event {
	text := strings.TrimRightFunc(content, unicode.IsSpace)
	body := "nostr:" + entity
	if strings.TrimSpace(text) != "" {
		body = text + "\n\nnostr:" + entity
	}
	author := t.Author.Hex()
	var tags nostr.Tags
	if t.Snapshot {
		tags = nostr.Tags{{"q", t.VersionIDs[0], hint, author}, {"p", author}}
	} else {
		q := nostr.Tag{"q", t.Address}
		if hint != "" {
			q = append(q, hint)
		}
		tags = nostr.Tags{q, {"p", author}, {"a", t.Address}}
	}
	return nostr.Event{Kind: nostr.KindTextNote, CreatedAt: at, Tags: tags, Content: body}
}

// ─── lookups ────────────────────────────────────────────────────────────

// socialVersion is the version event a reaction or top-level comment names:
// eventID when it is a known version, else the installed one, else the
// newest archived one. NIP-25 requires an "e" tag, so without a known
// version there is nothing conformant to publish.
func socialVersion(t socialTarget, eventID string) (string, error) {
	if eventID != "" {
		if !slices.Contains(t.VersionIDs, eventID) {
			return "", ErrServiceNotFound
		}
		return eventID, nil
	}
	if t.Installed != "" {
		return t.Installed, nil
	}
	if len(t.VersionIDs) > 0 {
		return t.VersionIDs[0], nil
	}
	return "", ErrServiceNotFound
}

// socialOwnLive is pk's live held events of kind on t, newest first.
func socialOwnLive(t socialTarget, kind nostr.Kind, pk nostr.PubKey) []nostr.Event {
	held := socialHeld(t, kind)
	var own []nostr.Event
	for _, evt := range socialLive(held, socialDeletions(held)) {
		if evt.PubKey == pk {
			own = append(own, evt)
		}
	}
	slices.SortFunc(own, socialOrder)
	return own
}

// socialRemoveLocal drops events from the local store; their kind 5 stays
// as the tombstone.
func socialRemoveLocal(events []nostr.Event) []string {
	ids := []string{}
	for _, evt := range events {
		if err := sys.Store.DeleteEvent(evt.ID); err != nil {
			log.Warn().Err(err).Msg("deleted social event could not be removed")
		}
		ids = append(ids, evt.ID.Hex())
	}
	return ids
}

// socialAction is the shared preamble of every social action after its own
// parameter checks: the store, the deadline, the signer and the action
// slot, in that order, so the error a client sees is deterministic.
func socialAction(ctx context.Context) (nostr.Keyer, nostr.PubKey, func(), error) {
	if sys == nil {
		return nil, nostr.ZeroPK, nil, ErrServiceUnavailable
	}
	if ctx.Err() != nil {
		return nil, nostr.ZeroPK, nil, ErrServiceTimeout
	}
	k, pk, err := socialSigner()
	if err != nil {
		return nil, nostr.ZeroPK, nil, err
	}
	release, err := socialAcquire(ctx)
	if err != nil {
		return nil, nostr.ZeroPK, nil, err
	}
	return k, pk, release, nil
}

// ─── actions ────────────────────────────────────────────────────────────

// ServiceSocialVote sets the user's vote on a napplet: "up", "down" or
// "none", on one version when eventID is given. A vote already in place is
// unchanged and nothing is published. Otherwise up and down publish a new
// kind 7, and only once that reached a relay a kind 5 for the previous own
// votes (at most ten); none publishes only that kind 5. The new vote shows
// locally either way.
func ServiceSocialVote(ctx context.Context, address, vote, eventID string) (ServiceSocialVoteResult, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceSocialVoteResult{}, err
	}
	if (vote != "up" && vote != "down" && vote != "none") ||
		(eventID != "" && !hex64.MatchString(eventID)) || (vote == "none" && eventID != "") {
		return ServiceSocialVoteResult{}, ErrServiceInvalidSocial
	}
	k, pk, release, err := socialAction(ctx)
	if err != nil {
		return ServiceSocialVoteResult{}, err
	}
	defer release()

	result := ServiceSocialVoteResult{Address: address, Outcome: "unchanged", Deleted: []string{}}
	own := socialOwnLive(t, nostr.KindReaction, pk)
	var prior []nostr.Event
	for _, evt := range own {
		if isVoteContent(evt.Content) {
			prior = append(prior, evt)
		}
	}
	current := socialOwnState(t, own, pk)
	if current.Vote == vote {
		same := eventID == ""
		for _, evt := range prior {
			if evt.ID.Hex() == current.VoteEventID {
				same = same || socialVersionOf(evt, t) == eventID
			}
		}
		if same {
			result.EventID = current.VoteEventID
			return result, nil
		}
	}
	if len(prior) > socialVoteDeleteLimit {
		prior = prior[:socialVoteDeleteLimit]
	}

	targets, hint := socialPublishRelays(ctx, t, nostr.ZeroPK)
	result.Outcome = "published"
	if vote != "none" {
		version, err := socialVersion(t, eventID)
		if err != nil {
			return ServiceSocialVoteResult{}, err
		}
		// the new vote must win locally even against a vote of this second
		at := nostr.Now()
		if len(prior) > 0 && prior[0].CreatedAt+1 > at {
			at = prior[0].CreatedAt + 1
		}
		content := "+"
		if vote == "down" {
			content = "-"
		}
		signed, published, failed, err := socialSignPublish(ctx, k, pk, socialReactionEvent(t, version, hint, content, "", at), targets)
		if err != nil {
			return ServiceSocialVoteResult{}, err
		}
		socialGeneration.Add(1)
		result.EventID, result.Published, result.Failed = signed.ID.Hex(), published, failed
		// NIP-25 clients take the newest vote; withdrawing the old ones
		// before any relay holds the new one could leave no vote at all
		if published == 0 || len(prior) == 0 {
			return result, nil
		}
		if _, _, _, err := socialSignPublish(ctx, k, pk, socialDeletionEvent(prior), targets); err != nil {
			log.Warn().Msg("previous votes were not withdrawn")
			return result, nil
		}
		result.Deleted = socialRemoveLocal(prior)
		socialGeneration.Add(1)
		return result, nil
	}

	_, published, failed, err := socialSignPublish(ctx, k, pk, socialDeletionEvent(prior), targets)
	if err != nil {
		return ServiceSocialVoteResult{}, err
	}
	result.Deleted = socialRemoveLocal(prior)
	result.Published, result.Failed = published, failed
	socialGeneration.Add(1)
	return result, nil
}

// ServiceSocialReact publishes an emoji reaction (not a vote) on a
// napplet, on one version when eventID is given. A NIP-30 :shortcode: needs
// its https image url, and only a shortcode may have one.
func ServiceSocialReact(ctx context.Context, address, emoji, emojiURL, eventID string) (ServiceSocialPublishResult, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	if isVoteContent(emoji) || validateReaction(emoji, emojiURL) != "" ||
		strings.IndexFunc(emoji, func(r rune) bool { return unicode.Is(unicode.Cf, r) }) >= 0 ||
		(emojiURL != "" && socialEmojiURL(emojiURL) == "") ||
		(eventID != "" && !hex64.MatchString(eventID)) {
		return ServiceSocialPublishResult{}, ErrServiceInvalidSocial
	}
	k, pk, release, err := socialAction(ctx)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	defer release()

	version, err := socialVersion(t, eventID)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	targets, hint := socialPublishRelays(ctx, t, nostr.ZeroPK)
	signed, published, failed, err := socialSignPublish(ctx, k, pk, socialReactionEvent(t, version, hint, emoji, emojiURL, nostr.Now()), targets)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	socialGeneration.Add(1)
	return ServiceSocialPublishResult{Address: address, Outcome: "published", EventID: signed.ID.Hex(), Published: published, Failed: failed}, nil
}

// ServiceSocialComment publishes a NIP-22 comment on a napplet, or a reply
// to parentID, which must be a live comment held for the same napplet.
func ServiceSocialComment(ctx context.Context, address, content, parentID string) (ServiceSocialPublishResult, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	if strings.TrimSpace(content) == "" || len(content) > socialMaxContent ||
		(parentID != "" && !hex64.MatchString(parentID)) {
		return ServiceSocialPublishResult{}, ErrServiceInvalidSocial
	}
	k, pk, release, err := socialAction(ctx)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	defer release()

	var parent *nostr.Event
	parentAuthor := nostr.ZeroPK
	if parentID != "" {
		held := socialHeld(t, nostr.KindComment)
		for _, evt := range socialLive(held, socialDeletions(held)) {
			if evt.ID.Hex() == parentID {
				parent = &evt
				break
			}
		}
		if parent == nil {
			return ServiceSocialPublishResult{}, ErrServiceNotFound
		}
		parentAuthor = parent.PubKey
	}
	version := ""
	if parent == nil || t.Snapshot {
		if version, err = socialVersion(t, ""); err != nil {
			return ServiceSocialPublishResult{}, err
		}
	}
	targets, _ := socialPublishRelays(ctx, t, parentAuthor)
	signed, published, failed, err := socialSignPublish(ctx, k, pk, socialCommentEvent(t, version, parent, content), targets)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	socialGeneration.Add(1)
	return ServiceSocialPublishResult{Address: address, Outcome: "published", EventID: signed.ID.Hex(), Published: published, Failed: failed}, nil
}

// ServiceSocialShare shares a napplet in a NIP-18 quote note: the text is
// published as given except for trailing whitespace, followed by the
// napplet's nostr: URI. It needs no known version, since the note names the
// address. The result never echoes the text or the URI.
func ServiceSocialShare(ctx context.Context, address, content string) (ServiceSocialPublishResult, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	if len(content) > socialShareContentMax {
		return ServiceSocialPublishResult{}, ErrServiceInvalidSocial
	}
	if _, ok := socialShareEntity(t, ""); !ok {
		return ServiceSocialPublishResult{}, ErrServiceInvalidSocial
	}
	k, pk, release, err := socialAction(ctx)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	defer release()

	targets, hint := socialPublishRelays(ctx, t, nostr.ZeroPK)
	if len(hint) > socialNIP19Value {
		hint = ""
	}
	entity, ok := socialShareEntity(t, hint)
	if !ok {
		return ServiceSocialPublishResult{}, ErrServiceInvalidSocial
	}
	signed, published, failed, err := socialSignPublish(ctx, k, pk, socialShareEvent(t, hint, entity, content, nostr.Now()), targets)
	if err != nil {
		return ServiceSocialPublishResult{}, err
	}
	socialGeneration.Add(1)
	return ServiceSocialPublishResult{Address: address, Outcome: "published", EventID: signed.ID.Hex(), Published: published, Failed: failed}, nil
}

// ServiceSocialDelete withdraws one of the user's own live reactions,
// comments or shared notes on a napplet with a NIP-09 kind 5, and drops it
// locally while keeping the kind 5 as its tombstone. An unknown id, someone
// else's event and one linked to another napplet are the same not found.
func ServiceSocialDelete(ctx context.Context, address, eventID string) (ServiceSocialDeleteResult, error) {
	t, err := newSocialTarget(address)
	if err != nil {
		return ServiceSocialDeleteResult{}, err
	}
	if !hex64.MatchString(eventID) {
		return ServiceSocialDeleteResult{}, ErrServiceInvalidSocial
	}
	k, pk, release, err := socialAction(ctx)
	if err != nil {
		return ServiceSocialDeleteResult{}, err
	}
	defer release()

	var target *nostr.Event
	for _, kind := range socialDeletableKinds {
		for _, evt := range socialOwnLive(t, kind, pk) {
			if evt.ID.Hex() == eventID {
				target = &evt
				break
			}
		}
		if target != nil {
			break
		}
	}
	if target == nil {
		return ServiceSocialDeleteResult{}, ErrServiceNotFound
	}
	targets, _ := socialPublishRelays(ctx, t, nostr.ZeroPK)
	signed, published, failed, err := socialSignPublish(ctx, k, pk, socialDeletionEvent([]nostr.Event{*target}), targets)
	if err != nil {
		return ServiceSocialDeleteResult{}, err
	}
	socialRemoveLocal([]nostr.Event{*target})
	socialGeneration.Add(1)
	return ServiceSocialDeleteResult{Address: address, Outcome: "published", DeletionID: signed.ID.Hex(), Published: published, Failed: failed}, nil
}

// ─── wire types ─────────────────────────────────────────────────────────

// ServiceSocialVoteResult is napplet.vote's result. EventID is the new
// vote, or the vote already in place when unchanged; Deleted lists the
// previous own votes withdrawn.
type ServiceSocialVoteResult struct {
	Address   string   `json:"address"`
	Outcome   string   `json:"outcome"`
	EventID   string   `json:"event_id,omitempty"`
	Deleted   []string `json:"deleted"`
	Published int      `json:"published"`
	Failed    int      `json:"failed"`
}

// ServiceSocialPublishResult is napplet.react's, napplet.comment's and
// napplet.share's result.
type ServiceSocialPublishResult struct {
	Address   string `json:"address"`
	Outcome   string `json:"outcome"`
	EventID   string `json:"event_id"`
	Published int    `json:"published"`
	Failed    int    `json:"failed"`
}

// ServiceSocialDeleteResult is napplet.delete's result.
type ServiceSocialDeleteResult struct {
	Address    string `json:"address"`
	Outcome    string `json:"outcome"`
	DeletionID string `json:"deletion_id"`
	Published  int    `json:"published"`
	Failed     int    `json:"failed"`
}
