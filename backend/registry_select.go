package backend

import (
	"bytes"
	"fmt"

	"fiatjaf.com/nostr"
)

// Every registry path that turns relay events into the napps and napplets
// the launcher lists picks the current manifest of an address the same way,
// the NIP-01 way: the latest created_at wins, and on a tie the lowest event
// id ("In case of replaceable events with the same timestamp, the event with
// the lowest id (first in lexical order) should be retained"). Only that
// winner is validated. When it is invalid the address is unavailable: an
// older valid version is never used instead, so neither an author nor a
// relay can roll users back by publishing a broken newer event (REG-01).

// isNapKind says whether k is a manifest kind the launcher follows.
func isNapKind(k nostr.Kind) bool {
	for _, nk := range napKinds {
		if nk == k {
			return true
		}
	}
	return false
}

// eventAddress is the NIP-01 address an event replaces under:
// <kind>:<pubkey hex>:<d>. A root napplet (15129) is one per author, so a d
// tag it happens to carry is ignored and never splits or merges root
// napplets. For addressable kinds d is the first d tag, the one relays index.
func eventAddress(evt nostr.Event) string {
	d := ""
	if addressable(evt.Kind) {
		d = evt.Tags.GetD()
	}
	return fmt.Sprintf("%d:%s:%s", evt.Kind, evt.PubKey.Hex(), d)
}

// newerEvent says whether a beats b under NIP-01: a later created_at, or the
// same one and a lower id. Both ids must already have passed CheckID, which
// latestByAddress.add makes sure of. Lowercase hex ids sort the same as
// their bytes.
func newerEvent(a, b nostr.Event) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	return bytes.Compare(a.ID[:], b.ID[:]) < 0
}

// latestByAddress keeps the NIP-01 winner of each address among authentic
// manifest events.
type latestByAddress map[string]nostr.Event

// add offers evt and says whether it is now its address's winner. An event
// is only considered when its kind is a manifest kind, its id field is the
// hash of its body and its signature holds. CheckID is the one that matters
// for ties: VerifySignature recomputes the id from the body and never looks
// at the id field, so without it a relay could rewrite the id of a validly
// signed event to 00…0 and win every tie.
func (m latestByAddress) add(evt nostr.Event) bool {
	if !isNapKind(evt.Kind) || !evt.CheckID() || !evt.VerifySignature() {
		return false
	}
	addr := eventAddress(evt)
	if cur, ok := m[addr]; ok && !newerEvent(evt, cur) {
		return false
	}
	m[addr] = evt
	return true
}

// nappFromLatest reads the winner of an address. Only this event is
// validated: when it is invalid the result is an unavailable entry, never
// an older valid version of the same address.
func nappFromLatest(evt nostr.Event) Napp {
	n, ok := nappFromEvent(evt)
	if !ok {
		return unavailableNapp(evt, "Its manifest is malformed")
	}
	n.EventID = evt.ID.Hex()
	return n
}

// unavailableNapp is the listing of an address whose latest event is
// invalid. It carries enough to be shown and recognised (the address as its
// id, kind, author, d, time and event id) and nothing to install or run: no
// paths, servers or actions.
func unavailableNapp(evt nostr.Event, reason string) Napp {
	n := Napp{
		ID:          eventAddress(evt),
		Author:      evt.PubKey,
		CreatedAt:   evt.CreatedAt,
		EventID:     evt.ID.Hex(),
		Unavailable: reason,
	}
	if addressable(evt.Kind) {
		n.D = evt.Tags.GetD()
	}
	if evt.Kind == KindNapplet || evt.Kind == KindRootNapplet {
		n.Format = FormatNapplet
		n.Kind = evt.Kind
	}
	return n
}

// nappNewer is newerEvent on Napp records that came from different lookups:
// a later CreatedAt wins, and on the same CreatedAt the lower EventID, when
// both are known. Two records of the same second where either lacks an
// EventID (one saved before EventID existed) are not newer than each other,
// so such a record is never replaced back and forth.
func nappNewer(a, b Napp) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	if a.EventID == "" || b.EventID == "" {
		return false
	}
	return a.EventID < b.EventID
}
