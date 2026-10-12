package backend

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/sdk"
)

// Fetching, keeping and pruning napplet social events. A refresh only runs
// when a client asks for one (ServiceSocial with refresh), and asks a small
// relay set for the reactions, comments and mentions that name the target, then for
// NIP-09 deletions of what is held. Everything it keeps passed the same
// checks: a real id and signature, an exact tag match, sane size and time,
// and for a reaction NIP-25's content rules. Zap receipts are asked of the
// author's zap provider only and verified by registry_social_zap.go. Relay
// URLs never leave this file: results carry only a per-category status.

// socialRelays is where a refresh asks: the author's inbox and outbox (four
// each, public relays only, since an author's list is not to be trusted),
// the discovery relays and the user's read relays, at most twelve. Tests
// replace it.
var socialRelays = func(ctx context.Context, t socialTarget) []string {
	if sys == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	write, read, _ := nip65Lists(ctx, t.Author)
	var urls []string
	for _, list := range [][]string{read, write} {
		n := 0
		for _, raw := range list {
			if n == 4 {
				break
			}
			if u, err := napExplicitRelay(ctx, raw); err == nil {
				urls = append(urls, u)
				n++
			}
		}
	}
	urls = append(urls, discoveryRelays()...)
	urls = append(urls, UserReadRelays()...)
	urls = normalizedUnique(urls)
	if len(urls) > socialMaxRelays {
		urls = urls[:socialMaxRelays]
	}
	return urls
}

const (
	socialMaxRelays = 12
	socialMaxTags   = 200
	// socialMaxContent caps any social event's content at ingest.
	socialMaxContent = 8 << 10
	// socialMaxReaction caps a reaction's content at ingest, in bytes.
	socialMaxReaction = 64
	// socialLocalLimit bounds one local store query.
	socialLocalLimit = 4000
	// socialChunk is how many ids one deletion query names.
	socialChunk = 100
	// socialIndexKey is the KV entry holding every address's sync status.
	socialIndexKey = "kwakore:social:index"
	// socialMemoTTL bounds how long a memoized aggregate is reused, since
	// other paths may also write kind 7, 1111, 1 and 9735 events to the store.
	socialMemoTTL = 15 * time.Second
)

// socialLimits bound what a refresh fetches and what is kept. Tests shrink
// them.
var socialLimits = struct {
	Reactions, Comments, Mentions, Zaps         int
	AuthorComments, AuthorEmoji, AuthorMentions int
	IndexEntries, PerFilter                     int
}{
	Reactions:      2000,
	Comments:       1000,
	Mentions:       500,
	Zaps:           1000,
	AuthorComments: 50,
	AuthorEmoji:    20,
	AuthorMentions: 50,
	IndexEntries:   200,
	PerFilter:      500,
}

// socialRefreshFloor is how soon a category can be refreshed again; inside
// it a refresh returns what is held.
var socialRefreshFloor = 30 * time.Second

// socialFutureSkew is how far ahead of the clock created_at may be, so a
// future-dated vote cannot pin an author's latest vote.
const socialFutureSkew = 15 * time.Minute

// socialGeneration changes whenever held social events change, so a
// memoized aggregate is never reused across a change made here.
var socialGeneration atomic.Uint64

// socialKind is a category's event kind.
func socialKind(category string) nostr.Kind {
	switch category {
	case SocialComments:
		return nostr.KindComment
	case SocialMentions:
		return nostr.KindTextNote
	case SocialZaps:
		return nostr.KindZap
	}
	return nostr.KindReaction
}

// socialKinds are the kinds held per address: reactions, comments,
// mentions and zap receipts.
var socialKinds = []nostr.Kind{nostr.KindReaction, nostr.KindComment, nostr.KindTextNote, nostr.KindZap}

// socialFilters are the relay filters of one category. A snapshot has no
// address filter, and no filter ever carries an empty tag list, which some
// relays read as no constraint at all. Zaps have none here: they are asked
// of the author's provider only (socialZapFilters).
func socialFilters(t socialTarget, category string) []nostr.Filter {
	if category == SocialZaps {
		return nil
	}
	kind := socialKind(category)
	aName, eName, _ := socialTagNames(kind)
	var filters []nostr.Filter
	if !t.Snapshot && t.Address != "" {
		filters = append(filters, nostr.Filter{Kinds: []nostr.Kind{kind}, Tags: nostr.TagMap{aName: {t.Address}}, Limit: socialLimits.PerFilter})
	}
	if refs := socialRefValues(t, kind); len(refs) > 0 {
		filters = append(filters, nostr.Filter{Kinds: []nostr.Kind{kind}, Tags: nostr.TagMap{eName: refs}, Limit: socialLimits.PerFilter})
	}
	return filters
}

// socialIngestable says whether a relay's event may be kept for t. A zap
// receipt never is here: only socialZapIngestable, which knows the
// provider, keeps receipts.
func socialIngestable(evt nostr.Event, t socialTarget, now time.Time) bool {
	if evt.Kind == nostr.KindZap {
		return false
	}
	if !evt.CheckID() || !evt.VerifySignature() || !socialMatches(evt, t) {
		return false
	}
	if evt.CreatedAt.Time().After(now.Add(socialFutureSkew)) || len(evt.Tags) > socialMaxTags || len(evt.Content) > socialMaxContent {
		return false
	}
	if evt.Kind == nostr.KindReaction {
		if len(evt.Content) > socialMaxReaction || strings.IndexFunc(evt.Content, func(r rune) bool { return unicode.Is(unicode.Cf, r) }) >= 0 {
			return false
		}
		if evt.Content == "" {
			return true
		}
		href := ""
		if shortcode := socialShortcode(evt.Content); shortcode != "" {
			for _, tag := range evt.Tags {
				if len(tag) >= 3 && tag[0] == "emoji" && tag[1] == shortcode {
					href = tag[2]
					break
				}
			}
		}
		return validateReaction(evt.Content, href) == ""
	}
	return true
}

// socialTombstoned says whether the store holds evt's author's deletion of
// it. The #e index is a prefix match, so the tag is checked exactly.
func socialTombstoned(store eventstore.Store, evt nostr.Event) bool {
	id := evt.ID.Hex()
	for d := range store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}, Authors: []nostr.PubKey{evt.PubKey}, Tags: nostr.TagMap{"e": {id}}}, 50) {
		if d.PubKey != evt.PubKey {
			continue
		}
		for _, tag := range d.Tags {
			if len(tag) >= 2 && tag[0] == "e" && tag[1] == id {
				return true
			}
		}
	}
	return false
}

// socialIngest keeps every ingestable, not tombstoned event of events.
func socialIngest(t socialTarget, events []nostr.Event) {
	now := time.Now()
	changed := false
	for _, evt := range events {
		if !socialIngestable(evt, t, now) || socialTombstoned(sys.Store, evt) {
			continue
		}
		switch err := sys.Store.SaveEvent(evt); {
		case err == nil:
			changed = true
		case errors.Is(err, eventstore.ErrDupEvent):
		default:
			log.Warn().Err(err).Msg("social event could not be saved")
		}
	}
	if changed {
		socialGeneration.Add(1)
	}
}

// socialHeld is every locally held event of kind that names t exactly. The
// address index is blind past 100 bytes, so a long address falls back to
// the author's p/P tag, and every hit is post-filtered.
func socialHeld(t socialTarget, kind nostr.Kind) []nostr.Event {
	if sys == nil {
		return nil
	}
	aName, eName, ok := socialTagNames(kind)
	if !ok {
		return nil
	}
	var queries []nostr.TagMap
	if !t.Snapshot && len(t.Address) <= 100 {
		queries = append(queries, nostr.TagMap{aName: {t.Address}})
	}
	// the index cannot hold an address over 100 bytes; the p fallback
	// below finds those quotes instead
	refs := slices.DeleteFunc(socialRefValues(t, kind), func(v string) bool { return len(v) > 100 })
	if len(refs) > 0 {
		queries = append(queries, nostr.TagMap{eName: refs})
	}
	if !t.Snapshot && len(t.Address) > 100 {
		queries = append(queries, nostr.TagMap{"p": {t.Author.Hex()}})
		if kind == nostr.KindComment {
			queries = append(queries, nostr.TagMap{"P": {t.Author.Hex()}})
		}
	}
	seen := map[nostr.ID]bool{}
	var out []nostr.Event
	for _, tags := range queries {
		for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{kind}, Tags: tags}, socialLocalLimit) {
			if seen[evt.ID] || evt.Kind != kind || !socialMatches(evt, t) {
				continue
			}
			seen[evt.ID] = true
			out = append(out, evt)
		}
	}
	return out
}

func socialChunks(ids []string) [][]string {
	var out [][]string
	for len(ids) > 0 {
		n := min(socialChunk, len(ids))
		out = append(out, ids[:n])
		ids = ids[n:]
	}
	return out
}

// socialDeletions is every locally held kind 5 that names one of held by
// its exact id. socialLive decides which of them count.
func socialDeletions(held []nostr.Event) []nostr.Event {
	if sys == nil || len(held) == 0 {
		return nil
	}
	want := make(map[string]bool, len(held))
	ids := make([]string, 0, len(held))
	for _, evt := range held {
		id := evt.ID.Hex()
		if !want[id] {
			want[id] = true
			ids = append(ids, id)
		}
	}
	seen := map[nostr.ID]bool{}
	var out []nostr.Event
	for _, chunk := range socialChunks(ids) {
		for d := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}, Tags: nostr.TagMap{"e": chunk}}, socialLocalLimit) {
			if seen[d.ID] || d.Kind != nostr.KindDeletion {
				continue
			}
			for _, tag := range d.Tags {
				if len(tag) >= 2 && tag[0] == "e" && want[tag[1]] {
					seen[d.ID] = true
					out = append(out, d)
					break
				}
			}
		}
	}
	return out
}

// ─── refresh ────────────────────────────────────────────────────────────

var (
	socialFlightsMu sync.Mutex
	socialFlights   = map[string]chan struct{}{}
	// socialSlots lets two addresses refresh at a time.
	socialSlots = make(chan struct{}, 2)
)

// socialFetched is one category's relay answer.
type socialFetched struct {
	events []nostr.Event
	status string
}

// socialFetch runs filters on relays in parallel and grades the answer the
// way fetchHistory does.
func socialFetch(ctx context.Context, relays []string, filters []nostr.Filter) socialFetched {
	result := socialFetched{status: "unavailable"}
	if len(relays) == 0 || len(filters) == 0 {
		return result
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	answered, allAnswered, timedOut := false, true, false
	for _, filter := range filters {
		wg.Go(func() {
			queryCtx, stop := context.WithCancel(ctx)
			defer stop()
			q := fetchRelays(queryCtx, relays, filter, "kwakore-social")
			var events []nostr.Event
			limited := false
			for re := range q.Events() {
				if len(events) < socialLimits.PerFilter {
					events = append(events, re.Event)
				} else {
					limited = true
					stop()
				}
			}
			mu.Lock()
			result.events = append(result.events, events...)
			answered = answered || q.Answered()
			allAnswered = allAnswered && q.AllAnswered() && !limited
			timedOut = timedOut || !q.Complete()
			mu.Unlock()
		})
	}
	wg.Wait()
	switch {
	case allAnswered:
		result.status = "answered"
	case answered || len(result.events) > 0:
		result.status = "partial"
	case ctx.Err() != nil || timedOut:
		result.status = "timeout"
	}
	return result
}

// socialRefresh asks relays for t's categories, at most once per address at
// a time and two addresses at a time, skipping categories synced within the
// floor. It only fails when ctx ends.
func socialRefresh(ctx context.Context, t socialTarget, categories []string) error {
	for {
		socialFlightsMu.Lock()
		flight, busy := socialFlights[t.Address]
		if !busy {
			flight = make(chan struct{})
			socialFlights[t.Address] = flight
		}
		socialFlightsMu.Unlock()
		if !busy {
			defer func() {
				socialFlightsMu.Lock()
				delete(socialFlights, t.Address)
				socialFlightsMu.Unlock()
				close(flight)
			}()
			break
		}
		// someone is refreshing this address: wait, then look at the floor
		select {
		case <-flight:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	status := socialSyncStatus(t.Address)
	var due []string
	for _, category := range categories {
		if s := status[category]; s.SyncedAt != nil && time.Since(time.Unix(*s.SyncedAt, 0)) < socialRefreshFloor {
			continue
		}
		due = append(due, category)
	}
	if len(due) == 0 {
		return nil
	}

	select {
	case socialSlots <- struct{}{}:
		defer func() { <-socialSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}

	relays := socialRelays(ctx, t)
	statuses := map[string]string{}
	if len(relays) == 0 {
		for _, category := range due {
			statuses[category] = "unavailable"
		}
		socialRecordSync(t, statuses, nil)
		return ctx.Err()
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var fetched []nostr.Event
	// zaps are asked of the author's provider and kept apart, since only
	// socialZapIngest knows that provider; zapProvider is the index record:
	// nil leaves it, "" clears it
	var (
		zapEvents   []nostr.Event
		zapEndpoint *socialZapEndpoint
		zapProvider *string
	)
	for _, category := range due {
		wg.Go(func() {
			if category == SocialZaps {
				lookupCtx, cancel := context.WithTimeout(ctx, socialZapLookupTimeout)
				z, err := socialZapLookup(lookupCtx, t.Author)
				cancel()
				status, record := "unavailable", (*string)(nil)
				var result socialFetched
				switch {
				case errors.Is(err, ErrServiceNotZappable):
					record = new(string)
				case err == nil:
					result = socialFetch(ctx, relays, socialZapFilters(t, z.provider))
					status = result.status
					hex := z.provider.Hex()
					record = &hex
				}
				mu.Lock()
				statuses[category] = status
				zapProvider = record
				if err == nil {
					zapEvents, zapEndpoint = result.events, &z
				}
				mu.Unlock()
				return
			}
			result := socialFetch(ctx, relays, socialFilters(t, category))
			mu.Lock()
			statuses[category] = result.status
			fetched = append(fetched, result.events...)
			mu.Unlock()
		})
	}
	wg.Wait()
	socialIngest(t, fetched)
	if zapEndpoint != nil {
		socialZapIngest(t, zapEvents, *zapEndpoint)
	}
	socialDeletionPass(ctx, t, relays, due)
	socialEnforceCaps(t)
	socialRecordSync(t, statuses, zapProvider)
	socialGeneration.Add(1)
	return ctx.Err()
}

// socialDeletionPass asks relays for kind 5s naming the held events of the
// refreshed categories, and applies those signed by the event's own author:
// the target goes and the deletion stays as a tombstone that blocks a later
// re-ingest. Anyone else's kind 5 is never stored. Zap receipts are
// skipped: a receipt is the provider's record of a payment, so NIP-09 is
// never applied to them.
func socialDeletionPass(ctx context.Context, t socialTarget, relays []string, categories []string) {
	authors := map[string]nostr.PubKey{}
	var ids []string
	for _, category := range categories {
		if category == SocialZaps {
			continue
		}
		for _, evt := range socialHeld(t, socialKind(category)) {
			id := evt.ID.Hex()
			if _, ok := authors[id]; !ok {
				authors[id] = evt.PubKey
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	slots := make(chan struct{}, 4)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var found []nostr.Event
	for _, chunk := range socialChunks(ids) {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			queryCtx, stop := context.WithCancel(ctx)
			defer stop()
			q := fetchRelays(queryCtx, relays, nostr.Filter{Kinds: []nostr.Kind{nostr.KindDeletion}, Tags: nostr.TagMap{"e": chunk}, Limit: 500}, "kwakore-social")
			var events []nostr.Event
			for re := range q.Events() {
				if len(events) == 500 {
					stop()
					continue
				}
				events = append(events, re.Event)
			}
			mu.Lock()
			found = append(found, events...)
			mu.Unlock()
		})
	}
	wg.Wait()
	now := time.Now()
	changed := false
	for _, d := range found {
		if d.Kind != nostr.KindDeletion || len(d.Tags) > socialMaxTags || d.CreatedAt.Time().After(now.Add(socialFutureSkew)) ||
			!d.CheckID() || !d.VerifySignature() {
			continue
		}
		applied := false
		for _, tag := range d.Tags {
			if len(tag) < 2 || tag[0] != "e" {
				continue
			}
			author, held := authors[tag[1]]
			if !held || author != d.PubKey {
				continue
			}
			id, err := nostr.IDFromHex(tag[1])
			if err != nil {
				continue
			}
			if err := sys.Store.DeleteEvent(id); err != nil {
				log.Warn().Err(err).Msg("deleted social event could not be removed")
				continue
			}
			delete(authors, tag[1])
			applied = true
		}
		if !applied {
			continue
		}
		changed = true
		if err := sys.Store.SaveEvent(d); err != nil && !errors.Is(err, eventstore.ErrDupEvent) {
			log.Warn().Err(err).Msg("social tombstone could not be saved")
		}
	}
	if changed {
		socialGeneration.Add(1)
	}
}

// socialOwnKey is the user's public key, or the zero key without one.
func socialOwnKey() nostr.PubKey {
	_, pk := identitySnapshot()
	return pk
}

// socialKeepOwn says whether evt is the user's own and so never dropped by
// a cap, an eviction or a prune: signed by own, or a zap receipt whose
// request own signed.
func socialKeepOwn(evt nostr.Event, own nostr.PubKey) bool {
	if own == nostr.ZeroPK {
		return false
	}
	if evt.PubKey == own {
		return true
	}
	if evt.Kind == nostr.KindZap {
		sender, ok := socialZapSender(evt)
		return ok && sender == own
	}
	return false
}

// socialEnforceCaps keeps at most AuthorEmoji emoji reactions,
// AuthorComments comments and AuthorMentions mentions per author, then at
// most Reactions reactions, Comments comments, Mentions mentions and Zaps
// zap receipts for the address, dropping the oldest. The user's own events
// are never dropped.
func socialEnforceCaps(t socialTarget) {
	own := socialOwnKey()
	keep := func(evt nostr.Event) bool { return socialKeepOwn(evt, own) }
	var drop []nostr.ID
	for _, kind := range socialKinds {
		held := socialHeld(t, kind)
		var perAuthor, total int
		switch kind {
		case nostr.KindReaction:
			perAuthor, total = socialLimits.AuthorEmoji, socialLimits.Reactions
		case nostr.KindComment:
			perAuthor, total = socialLimits.AuthorComments, socialLimits.Comments
		case nostr.KindZap:
			total = socialLimits.Zaps
		default:
			perAuthor, total = socialLimits.AuthorMentions, socialLimits.Mentions
		}
		slices.SortFunc(held, socialOrder) // newest first
		counts := map[nostr.PubKey]int{}
		var kept []nostr.Event
		for _, evt := range held {
			// comments and notes (even empty ones) are always capped per
			// author, reactions unless they are votes, and receipts never:
			// one provider signs them all
			var capped bool
			switch kind {
			case nostr.KindReaction:
				capped = !isVoteContent(evt.Content)
			case nostr.KindComment, nostr.KindTextNote:
				capped = true
			}
			if capped && !keep(evt) {
				counts[evt.PubKey]++
				if counts[evt.PubKey] > perAuthor {
					drop = append(drop, evt.ID)
					continue
				}
			}
			kept = append(kept, evt)
		}
		// oldest first: created_at asc, then id desc
		for i := len(kept) - 1; i >= 0 && len(kept) > total; i-- {
			if keep(kept[i]) {
				continue
			}
			drop = append(drop, kept[i].ID)
			kept = slices.Delete(kept, i, i+1)
		}
	}
	for _, id := range drop {
		if err := sys.Store.DeleteEvent(id); err != nil {
			log.Warn().Err(err).Msg("capped social event could not be removed")
		}
	}
	if len(drop) > 0 {
		socialGeneration.Add(1)
	}
}

// socialPruneEvents removes t's held reactions, comments, mentions and zap
// receipts except the user's own. Tombstones stay: they only block a
// re-ingest.
func socialPruneEvents(t socialTarget) {
	own := socialOwnKey()
	changed := false
	for _, kind := range socialKinds {
		for _, evt := range socialHeld(t, kind) {
			if socialKeepOwn(evt, own) {
				continue
			}
			if err := sys.Store.DeleteEvent(evt.ID); err != nil {
				log.Warn().Err(err).Msg("social event could not be pruned")
				continue
			}
			changed = true
		}
	}
	if changed {
		socialGeneration.Add(1)
	}
}

// pruneNappletSocial forgets an uninstalled napplet's social data: its held
// events (never the user's own) and its index entry. Failures are logged
// and never reach the uninstall result.
func pruneNappletSocial(address string) {
	if sys == nil {
		return
	}
	t, err := newSocialTarget(address)
	if err != nil {
		return
	}
	socialPruneEvents(t)
	socialIndexMu.Lock()
	defer socialIndexMu.Unlock()
	err = sys.KVStore.Update([]byte(socialIndexKey), func(raw []byte) ([]byte, error) {
		index := socialIndexDecode(raw)
		if _, ok := index[address]; !ok {
			return raw, nil
		}
		delete(index, address)
		return json.Marshal(index)
	})
	if err != nil {
		log.Warn().Err(err).Msg("social index entry could not be removed")
	}
}

// ─── status index ───────────────────────────────────────────────────────

var socialIndexMu sync.Mutex

type socialIndexCategory struct {
	SyncedAt int64  `json:"synced_at"`
	Status   string `json:"status"`
}

type socialIndexEntry struct {
	Installed bool `json:"installed"`
	// Touched orders entries for eviction: when the address last synced, in
	// unix nanoseconds so refreshes in one second still order.
	Touched    int64                          `json:"touched"`
	Categories map[string]socialIndexCategory `json:"categories"`
	// ZapProvider is the hex key whose receipts count, found by the last
	// zaps refresh; "" when the author was not zappable or never looked up.
	ZapProvider string `json:"zap_provider,omitempty"`
}

func socialIndexDecode(raw []byte) map[string]socialIndexEntry {
	index := map[string]socialIndexEntry{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &index); err != nil {
			log.Warn().Err(err).Msg("social index unreadable, starting over")
			return map[string]socialIndexEntry{}
		}
	}
	return index
}

// socialInstalledAddresses is the canonical address of every installed napplet.
func socialInstalledAddresses() map[string]bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	out := make(map[string]bool, len(state.InstalledNapps))
	for _, n := range state.InstalledNapps {
		out[n.Address()] = true
	}
	return out
}

// socialRecordSync stores the statuses of a refresh of t and its zap
// provider record (nil leaves it, "" clears it, a hex key sets it), then
// evicts the least recently synced uninstalled addresses past IndexEntries
// and prunes their events.
func socialRecordSync(t socialTarget, statuses map[string]string, zapProvider *string) {
	if sys == nil {
		return
	}
	installed := socialInstalledAddresses()
	now := time.Now()
	var evicted []string
	socialIndexMu.Lock()
	err := sys.KVStore.Update([]byte(socialIndexKey), func(raw []byte) ([]byte, error) {
		evicted = nil
		index := socialIndexDecode(raw)
		entry := index[t.Address]
		if entry.Categories == nil {
			entry.Categories = map[string]socialIndexCategory{}
		}
		for category, status := range statuses {
			entry.Categories[category] = socialIndexCategory{SyncedAt: now.Unix(), Status: status}
		}
		if zapProvider != nil {
			entry.ZapProvider = *zapProvider
		}
		entry.Installed = installed[t.Address]
		entry.Touched = now.UnixNano()
		index[t.Address] = entry
		if len(index) > socialLimits.IndexEntries {
			var candidates []string
			for address := range index {
				if address != t.Address && !installed[address] {
					candidates = append(candidates, address)
				}
			}
			slices.SortFunc(candidates, func(a, b string) int {
				if c := cmp.Compare(index[a].Touched, index[b].Touched); c != 0 {
					return c
				}
				return cmp.Compare(a, b)
			})
			for _, address := range candidates {
				if len(index) <= socialLimits.IndexEntries {
					break
				}
				delete(index, address)
				evicted = append(evicted, address)
			}
		}
		return json.Marshal(index)
	})
	socialIndexMu.Unlock()
	if err != nil {
		log.Warn().Err(err).Msg("social index could not be saved")
		return
	}
	for _, address := range evicted {
		if target, err := newSocialTarget(address); err == nil {
			socialPruneEvents(target)
		}
	}
}

// socialSyncStatus is every category's last refresh of address;
// not_requested with a null synced_at when there was none.
func socialSyncStatus(address string) map[string]ServiceSocialSync {
	out := map[string]ServiceSocialSync{}
	var index map[string]socialIndexEntry
	if sys != nil {
		raw, err := sys.KVStore.Get([]byte(socialIndexKey))
		if err != nil {
			log.Warn().Err(err).Msg("social index could not be read")
		}
		index = socialIndexDecode(raw)
	}
	entry := index[address]
	for _, category := range SocialCategories() {
		c, ok := entry.Categories[category]
		if !ok || c.Status == "" {
			out[category] = ServiceSocialSync{Status: "not_requested"}
			continue
		}
		at := c.SyncedAt
		out[category] = ServiceSocialSync{Status: c.Status, SyncedAt: &at}
	}
	return out
}

// socialZapProviderOf is the zap provider the last zaps refresh of address
// recorded, or the zero key without one.
func socialZapProviderOf(address string) nostr.PubKey {
	if sys == nil {
		return nostr.ZeroPK
	}
	raw, err := sys.KVStore.Get([]byte(socialIndexKey))
	if err != nil {
		log.Warn().Err(err).Msg("social index could not be read")
		return nostr.ZeroPK
	}
	entry, ok := socialIndexDecode(raw)[address]
	if !ok || entry.ZapProvider == "" {
		return nostr.ZeroPK
	}
	pk, err := nostr.PubKeyFromHex(entry.ZapProvider)
	if err != nil {
		return nostr.ZeroPK
	}
	return pk
}

// ─── reads ──────────────────────────────────────────────────────────────

type socialMemoEntry struct {
	key       string
	at        time.Time
	summary   ServiceSocialSummary
	reactions []nostr.Event
}

var socialMemo = struct {
	sync.Mutex
	entries map[string]socialMemoEntry
}{entries: map[string]socialMemoEntry{}}

func socialResetMemo() {
	socialMemo.Lock()
	socialMemo.entries = map[string]socialMemoEntry{}
	socialMemo.Unlock()
}

// socialAggregate is t's summary without Sync and Own, and its live
// reactions for the viewer's own state, memoized by the generation, the
// version ids and the zap provider. Deletions apply to reactions, comments
// and mentions (kinds 7, 1111 and 1), never to zap receipts.
func socialAggregate(t socialTarget) (ServiceSocialSummary, []nostr.Event) {
	provider := socialZapProviderOf(t.Address)
	key := fmt.Sprintf("%d|%s|%s", socialGeneration.Load(), strings.Join(t.VersionIDs, ","), provider.Hex())
	socialMemo.Lock()
	entry, ok := socialMemo.entries[t.Address]
	socialMemo.Unlock()
	if ok && entry.key == key && time.Since(entry.at) < socialMemoTTL {
		return entry.summary, entry.reactions
	}
	reactions := socialHeld(t, nostr.KindReaction)
	comments := socialHeld(t, nostr.KindComment)
	mentions := socialHeld(t, nostr.KindTextNote)
	deletions := socialDeletions(slices.Concat(reactions, comments, mentions))
	summary := socialSummary(t, reactions, comments, deletions, nil)
	summary.Mentions = mentionSummary(t, mentions, deletions)
	summary.Zaps = zapSummary(socialZapsOf(t, socialHeld(t, nostr.KindZap), provider))
	live := socialLive(reactions, deletions)
	socialMemo.Lock()
	if len(socialMemo.entries) >= 64 {
		socialMemo.entries = map[string]socialMemoEntry{}
	}
	socialMemo.entries[t.Address] = socialMemoEntry{key: key, at: time.Now(), summary: summary, reactions: live}
	socialMemo.Unlock()
	return summary, live
}

// socialViewer is the signer's public key, or nil without a signer.
func socialViewer() *nostr.PubKey {
	k, pk := identitySnapshot()
	if k == nil || pk == nostr.ZeroPK {
		return nil
	}
	return &pk
}

// socialAuthorName is pk's display name from the local profile cache, made
// one safe line; never the network and never a picture.
func socialAuthorName(pk nostr.PubKey) string {
	if sys == nil {
		return ""
	}
	for evt := range sys.Store.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{0}, Authors: []nostr.PubKey{pk}}, 1) {
		meta, err := sdk.ParseMetadata(evt)
		if err != nil {
			return ""
		}
		name := meta.DisplayName
		if strings.TrimSpace(name) == "" {
			name = meta.Name
		}
		name, _ = socialText(name, socialAuthorNameRunes)
		name = strings.Map(func(r rune) rune {
			if r == '\n' || r == '\t' {
				return ' '
			}
			return r
		}, name)
		return strings.TrimSpace(name)
	}
	return ""
}

func socialAuthorNames[T any](items []T, author func(*T) (string, *string)) {
	names := map[string]string{}
	for i := range items {
		hex, dst := author(&items[i])
		name, ok := names[hex]
		if !ok {
			if pk, err := nostr.PubKeyFromHex(hex); err == nil {
				name = socialAuthorName(pk)
			}
			names[hex] = name
		}
		*dst = name
	}
}
