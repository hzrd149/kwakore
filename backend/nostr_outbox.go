package backend

import (
	"context"
	"errors"
	"slices"
	"sync"

	"fiatjaf.com/nostr"
)

// Outbox routing (NIP-65) for NAP-OUTBOX: where to read an author's events
// (their write relays), where to reach a person (their read relays) and where
// one signed event has to go. The napplet only says what it wants; every
// relay is picked or validated here. Relay lists are read from kind 10002
// only, and a relay taken from anyone's list but the user's own must be a
// public relay: naming an author is not a way to make the daemon dial a
// loopback or LAN host.

const (
	// outboxMaxAuthors bounds how many authors' relay lists one request
	// looks up; the rest are asked on the fallback relays
	outboxMaxAuthors = 256
	// outboxRelaysPerAuthor is how many of an author's relays are asked
	outboxRelaysPerAuthor = 3
	// outboxMaxDirected bounds the relay connections one filter fans out to
	outboxMaxDirected = 60
	// outboxMaxInboxes bounds the people one publish delivers to
	outboxMaxInboxes = 32
	// outboxLookups is how many relay lists are looked up at once
	outboxLookups = 16
)

// relayListFunc is an author's NIP-65 relay list: write (outbox) and read
// (inbox) relays, and whether a list was found at all.
type relayListFunc func(ctx context.Context, pk nostr.PubKey) (write, read []string, found bool)

// nip65Lists reads relay lists through the sdk (local store, cache, then the
// relay-list relays). The user's own comes from memory once it is loaded.
func nip65Lists(ctx context.Context, pk nostr.PubKey) (write, read []string, found bool) {
	if l, ok := userRelays(); ok && l.Pubkey == pk {
		return l.Write, l.Read, len(l.Write)+len(l.Read) > 0
	}
	rl := sys.FetchRelayList(ctx, pk)
	for _, r := range rl.Items {
		u := nostr.NormalizeURL(r.URL)
		if u == "" {
			continue
		}
		if r.Outbox {
			write = nostr.AppendUnique(write, u)
		}
		if r.Inbox {
			read = nostr.AppendUnique(read, u)
		}
	}
	return write, read, len(rl.Items) > 0
}

// relayCheck validates a relay URL that came from someone else's say (a
// napplet, or another person's relay list) and returns it normalized. The
// daemon's is napExplicitRelay.
type relayCheck func(ctx context.Context, raw string) (string, error)

// relayRouting is what read routing needs to know. The sdk's own outbox and
// inbox lookups read a relay-hint database that only network events feed:
// after a restart it is empty, and for anyone it doesn't know the sdk
// stands in hardcoded third-party relays. Routing therefore reads the kind
// 10002 lists themselves (lists), checks every relay that comes from
// another person's list (check), trusts the signed-in user's own list
// (self: their relays are their choice, as publishing already assumes) and
// sends whoever has no usable list to fallback. Tests build one from stubs;
// the daemon's is liveRelayRouting.
type relayRouting struct {
	lists relayListFunc
	check relayCheck
	// self is the signed-in user, zero when signed out
	self nostr.PubKey
	// fallback is where reads go when NIP-65 data is missing
	fallback []string
}

// liveRelayRouting is the routing the daemon uses right now: lists from
// kind 10002, the public-host check, the signed-in user and their fallback.
func liveRelayRouting() relayRouting {
	self, _ := currentUser()
	return relayRouting{
		lists:    nip65Lists,
		check:    napExplicitRelay,
		self:     self,
		fallback: outboxFallback(),
	}
}

// relays are up to n of pk's usable write or read relays, in list order.
// Another person's relays that fail the check are skipped, as a bad entry
// in a list is not the napplet's fault; empty means none are usable.
func (rt relayRouting) relays(ctx context.Context, pk nostr.PubKey, direction string, n int) []string {
	write, read, _ := rt.lists(ctx, pk)
	urls := write
	if direction == "read" {
		urls = read
	}
	out := []string{}
	for _, u := range urls {
		if len(out) >= n {
			break
		}
		if u = nostr.NormalizeURL(u); u == "" {
			continue
		}
		if pk != rt.self {
			v, err := rt.check(ctx, u)
			if err != nil || v == "" {
				continue
			}
			u = v
		}
		out = nostr.AppendUnique(out, u)
	}
	return out
}

// lookup resolves many people at once; the check (DNS for a hostname)
// runs inside the bounded concurrent lookups too.
func (rt relayRouting) lookup(ctx context.Context, pks []nostr.PubKey, direction string, n int) map[nostr.PubKey][]string {
	return lookupAll(ctx, pks, func(ctx context.Context, pk nostr.PubKey) []string {
		return rt.relays(ctx, pk, direction, n)
	})
}

// outboxRelayPlan is NAP-OUTBOX's OutboxRelayPlan.
type outboxRelayPlan struct {
	Relays         []string `json:"relays"`
	Source         string   `json:"source"`
	MissingAuthors []string `json:"missingAuthors,omitempty"`
}

// outboxPlan is the plan reads use: up to outboxRelaysPerAuthor usable
// write ("write") or read ("read") relays per author, from their NIP-65
// lists. Authors with none, or past outboxMaxAuthors, are missing, and
// reads send them to the fallback relays, so those follow when any author
// is missing. Source is nip65 when any author contributed a relay.
func outboxPlan(ctx context.Context, rt relayRouting, authors []nostr.PubKey, direction string) outboxRelayPlan {
	plan := outboxRelayPlan{Relays: []string{}, Source: "fallback"}
	looked := authors
	if len(looked) > outboxMaxAuthors {
		looked = looked[:outboxMaxAuthors]
	}
	found := rt.lookup(ctx, looked, direction, outboxRelaysPerAuthor)
	for i, pk := range authors {
		urls := found[pk]
		if i >= outboxMaxAuthors || len(urls) == 0 {
			plan.MissingAuthors = append(plan.MissingAuthors, pk.Hex())
			continue
		}
		plan.Source = "nip65"
		for _, u := range urls {
			plan.Relays = nostr.AppendUnique(plan.Relays, u)
		}
	}
	if len(plan.MissingAuthors) > 0 {
		for _, u := range normalizedUnique(rt.fallback) {
			plan.Relays = nostr.AppendUnique(plan.Relays, u)
		}
	}
	return plan
}

// directFilter splits a filter by relay, the outbox model proper: each
// author's relays get the filter for just the authors who write there, and
// every relay in full gets the whole filter. Authors no kept relay covers
// (no relays known, or dropped past outboxMaxDirected) go to the fallback
// relays, as does a filter with nowhere else to go.
func directFilter(f nostr.Filter, outboxes map[nostr.PubKey][]string, full, fallback []string) []nostr.DirectedFilter {
	full = normalizedUnique(full)
	byRelay := map[string][]nostr.PubKey{}
	for _, pk := range f.Authors {
		for _, u := range outboxes[pk] {
			if u = nostr.NormalizeURL(u); u != "" && !slices.Contains(full, u) {
				byRelay[u] = append(byRelay[u], pk)
			}
		}
	}

	// the busiest relays first, so the cap drops the ones that serve the
	// fewest authors; ties by url, for a stable order
	relays := make([]string, 0, len(byRelay))
	for u := range byRelay {
		relays = append(relays, u)
	}
	slices.SortFunc(relays, func(a, b string) int {
		if d := len(byRelay[b]) - len(byRelay[a]); d != 0 {
			return d
		}
		if a < b {
			return -1
		}
		return 1
	})
	budget := outboxMaxDirected - len(full)
	if budget < 0 {
		budget = 0
	}
	if len(relays) > budget {
		relays = relays[:budget]
	}

	out := make([]nostr.DirectedFilter, 0, len(relays)+len(full))
	covered := map[nostr.PubKey]bool{}
	for _, u := range relays {
		sub := f.Clone()
		sub.Authors = byRelay[u]
		for _, pk := range sub.Authors {
			covered[pk] = true
		}
		out = append(out, nostr.DirectedFilter{Filter: sub, Relay: u})
	}
	for _, u := range full {
		out = append(out, nostr.DirectedFilter{Filter: f, Relay: u})
	}

	var uncovered []nostr.PubKey
	for _, pk := range f.Authors {
		if !covered[pk] {
			uncovered = append(uncovered, pk)
		}
	}
	rest := f
	switch {
	case len(uncovered) > 0:
		rest = f.Clone()
		rest.Authors = uncovered
	case len(out) > 0:
		return out
	}
	for _, u := range normalizedUnique(fallback) {
		if !slices.Contains(full, u) {
			out = append(out, nostr.DirectedFilter{Filter: rest, Relay: u})
		}
	}
	return out
}

// outboxDirected is where a napplet's filter is asked, per NIP-65: its
// authors on up to three of their usable write relays each, and, with the
// whole filter, the napplet's validated relay hints, the hinted authors'
// write relays (only when the filter names no authors) and the tagged
// people's read relays. An author with no usable relays is asked on the
// fallback relays for just their events; a hinted or tagged person with
// none sends the whole filter there too, as does a filter naming nobody.
func outboxDirected(ctx context.Context, rt relayRouting, f nostr.Filter, hintAuthors []nostr.PubKey, hintRelays []string) []nostr.DirectedFilter {
	authors := f.Authors
	if len(authors) > outboxMaxAuthors {
		authors = authors[:outboxMaxAuthors]
	}
	outboxes := rt.lookup(ctx, authors, "write", outboxRelaysPerAuthor)

	full := append([]string(nil), hintRelays...)
	missing := false
	// in slice order, not map order, so the routes come out the same way
	// every time
	collect := func(pks []nostr.PubKey, found map[nostr.PubKey][]string) {
		for _, pk := range pks {
			if len(found[pk]) == 0 {
				missing = true
			}
			full = append(full, found[pk]...)
		}
	}
	if len(f.Authors) == 0 {
		if len(hintAuthors) > outboxMaxAuthors {
			hintAuthors = hintAuthors[:outboxMaxAuthors]
		}
		collect(hintAuthors, rt.lookup(ctx, hintAuthors, "write", outboxRelaysPerAuthor))
	}
	if ps, ok := f.Tags["p"]; ok {
		var tagged []nostr.PubKey
		for _, p := range ps {
			if pk, err := nostr.PubKeyFromHex(p); err == nil && len(tagged) < 12 {
				tagged = append(tagged, pk)
			}
		}
		collect(tagged, rt.lookup(ctx, tagged, "read", outboxRelaysPerAuthor))
	}
	// the fallback SHOULD covers read targets: whoever was named without a
	// usable list is looked for there, with the whole filter
	if missing {
		full = append(full, rt.fallback...)
	}
	return directFilter(f, outboxes, full, rt.fallback)
}

// lookupAll runs a relay lookup for many people at once (the sdk batches the
// relay-list fetches underneath).
func lookupAll(ctx context.Context, pks []nostr.PubKey, fn func(context.Context, nostr.PubKey) []string) map[nostr.PubKey][]string {
	out := make(map[nostr.PubKey][]string, len(pks))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, outboxLookups)
	for _, pk := range pks {
		wg.Add(1)
		sem <- struct{}{}
		go func(pk nostr.PubKey) {
			defer func() { <-sem; wg.Done() }()
			urls := fn(ctx, pk)
			mu.Lock()
			out[pk] = urls
			mu.Unlock()
		}(pk)
	}
	wg.Wait()
	return out
}

func normalizedUnique(urls []string) []string {
	out := make([]string, 0, len(urls))
	for _, u := range urls {
		if u = nostr.NormalizeURL(u); u != "" {
			out = nostr.AppendUnique(out, u)
		}
	}
	return out
}

// errNoInbox is a toInboxes author with no NIP-65 read relays to deliver to.
var errNoInbox = errors.New("relay list unavailable")

// errPolicyDenied is an explicit relay the launcher won't connect to.
var errPolicyDenied = errors.New("policy denied")

// outboxFanout is the relay set for one outbox.publish: the user's write
// relays (toOutbox), every toInboxes person's read relays, and the
// napplet's explicit relays. Each inbox relay comes from someone else's
// relay list, so it gets the same public-host check as an explicit one;
// inboxes maps each person to the relays kept for them. A person with no
// usable inbox is an error: delivering to them was asked, not hoped for,
// and the fallback never stands in for a recipient's inbox. A user with no
// relay list publishes to the fallback relays instead. Recipients' lists
// are looked up at once; targets keep toInboxes order.
func outboxFanout(ctx context.Context, lists relayListFunc, check relayCheck,
	user nostr.PubKey, toOutbox bool, toInboxes []nostr.PubKey, explicit, fallback []string,
) (targets []string, inboxes map[nostr.PubKey][]string, err error) {
	targets = []string{}
	if toOutbox {
		write, _, _ := lists(ctx, user)
		if len(write) == 0 {
			write = normalizedUnique(fallback)
		}
		for _, u := range write {
			targets = nostr.AppendUnique(targets, u)
		}
	}
	if len(toInboxes) > outboxMaxInboxes {
		return nil, nil, errors.New("too many recipients")
	}
	// every resolved, allowed inbox relay is a target, so these are not
	// capped per person
	found := lookupAll(ctx, toInboxes, func(ctx context.Context, pk nostr.PubKey) []string {
		_, read, _ := lists(ctx, pk)
		kept := []string{}
		for _, u := range read {
			if v, err := check(ctx, u); err == nil && v != "" {
				kept = nostr.AppendUnique(kept, v)
			}
		}
		return kept
	})
	inboxes = make(map[nostr.PubKey][]string, len(toInboxes))
	for _, pk := range toInboxes {
		if len(found[pk]) == 0 {
			return nil, nil, errNoInbox
		}
		inboxes[pk] = found[pk]
		for _, u := range found[pk] {
			targets = nostr.AppendUnique(targets, u)
		}
	}
	for _, raw := range explicit {
		u, err := check(ctx, raw)
		if err != nil {
			return nil, nil, errPolicyDenied
		}
		targets = nostr.AppendUnique(targets, u)
	}
	if len(targets) == 0 {
		return nil, nil, errors.New("no relays to publish to")
	}
	return targets, inboxes, nil
}
