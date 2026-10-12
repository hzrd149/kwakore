// Package napaddr reads the napplet addresses people and agents hand to the
// kwakore CLI and turns them into the canonical "<kind>:<pubkey hex>:<d>"
// coordinate the control protocol requires.
//
// The wire stays canonical-only: the daemon accepts nothing else, so a
// pasted naddr1… or nostr:naddr1… is decoded here, before the CLI dials,
// and checked with the same rule the daemon applies
// (desktopentry.CanonicalAddress, pinned to the backend's
// ParseCanonicalServiceAddress). An naddr's relay hints are filtered and
// returned beside the address for the one request that may carry them.
//
// A kind 5129 snapshot has no naddr: it is named by its event, so an
// nevent1… (or note1…) is read too. An nevent that carries its author
// converts to the canonical "5129:<author hex>:<event id hex>" here; a
// note1 or an nevent without an author names only the event, and Parse
// returns ErrSnapshotLookup with the event id for the caller to resolve.
//
// The package is a leaf. It imports only the standard library,
// kwakore/backend/desktopentry and the bech32 codec, so the CLI does not
// link the backend root (relay pool, stores) or the nostr library.
package napaddr

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"kwakore/backend/desktopentry"
)

// AcceptedForms names the address forms Parse reads, for error messages.
// It is ASCII only so a JSON encoder writes it unescaped.
var AcceptedForms = []string{"KIND:PUBKEY_HEX:D", "naddr1...", "nostr:naddr1...", "nevent1...", "note1..."}

var (
	// ErrInvalid is returned for anything that is not an address of a napp
	// or napplet in one of the accepted forms.
	ErrInvalid = errors.New("invalid napplet address")
	// ErrUnsupported is returned for a NIP-19 entity that cannot name a
	// napplet (npub, nprofile, nsec, nrelay, or an nevent of another kind).
	// The first four are recognized by their prefix and never decoded.
	ErrUnsupported = errors.New("unsupported nip-19 entity")
	// ErrSnapshotLookup is returned with an Address holding only the event
	// id (and an nevent's relay hints) for a note1 or an nevent without an
	// author: the snapshot's author, and so its canonical address, must be
	// looked up first.
	ErrSnapshotLookup = errors.New("snapshot address needs a lookup")
)

// snapshotKind is the kind of a snapshot napplet, the only kind an nevent
// may name here.
const snapshotKind = 5129

const (
	// MaxRelayHints is the most relay hints kept from one naddr.
	MaxRelayHints = 8
	// MaxRelayHintLen is the longest relay hint, the most an naddr relay
	// TLV entry can hold.
	MaxRelayHintLen = 255
)

// Address is a parsed napplet address.
type Address struct {
	// Canonical is "<kind>:<lowercase pubkey hex>:<d>", or empty with
	// ErrSnapshotLookup.
	Canonical string
	// Identifier is the d tag, empty for a root napplet, and the event id
	// hex for a snapshot.
	Identifier string
	// Relays are the naddr's or nevent's usable relay hints, nil for a
	// canonical input.
	Relays []string
}

// unsupportedPrefixes are the NIP-19 entities that never name a napplet.
var unsupportedPrefixes = []string{"npub1", "nprofile1", "nsec1", "nrelay1"}

const (
	tlvIdentifier = 0
	tlvRelay      = 1
	tlvAuthor     = 2
	tlvKind       = 3
)

// Parse reads a canonical coordinate, a bare naddr or a nostr: URI holding
// an naddr, or a snapshot's nevent or note (bare or as a nostr: URI; see
// the package comment and ErrSnapshotLookup). The scheme is matched in any letter case; the bech32 part must
// be all lowercase or all uppercase. Web links, surrounding whitespace and
// anything else are ErrInvalid. Relay hints never make an naddr invalid:
// the ones that fail ValidRelayHint are dropped.
func Parse(input string) (Address, error) {
	if len(input) == 0 || len(input) > desktopentry.MaxAddressLen || !utf8.ValidString(input) {
		return Address{}, ErrInvalid
	}
	if desktopentry.CanonicalAddress(input) == nil {
		_, rest, _ := strings.Cut(input, ":")
		_, identifier, _ := strings.Cut(rest, ":")
		return Address{Canonical: input, Identifier: identifier}, nil
	}

	code := input
	if len(code) >= 6 && strings.EqualFold(code[:6], "nostr:") {
		code = code[6:]
	}
	lower := strings.ToLower(code)
	for _, prefix := range unsupportedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return Address{}, ErrUnsupported
		}
	}
	switch {
	case strings.HasPrefix(lower, "nevent1"):
		return parseNevent(code)
	case strings.HasPrefix(lower, "note1"):
		return parseNote(code)
	case !strings.HasPrefix(lower, "naddr1"):
		return Address{}, ErrInvalid
	}

	hrp, data5, err := bech32.DecodeNoLimit(code)
	if err != nil || hrp != "naddr" {
		return Address{}, ErrInvalid
	}
	data, err := bech32.ConvertBits(data5, 5, 8, false)
	if err != nil {
		return Address{}, ErrInvalid
	}

	var (
		identifier            string
		author                []byte
		kind                  uint32
		hasD, hasAuth, hasKnd bool
		relays                []string
	)
	for len(data) > 0 {
		if len(data) < 2 || len(data) < 2+int(data[1]) {
			return Address{}, ErrInvalid
		}
		typ, value := data[0], data[2:2+int(data[1])]
		data = data[2+int(data[1]):]
		switch typ {
		case tlvIdentifier:
			if hasD {
				return Address{}, ErrInvalid
			}
			identifier, hasD = string(value), true
		case tlvRelay:
			relays = append(relays, string(value))
		case tlvAuthor:
			if hasAuth || len(value) != 32 {
				return Address{}, ErrInvalid
			}
			author, hasAuth = value, true
		case tlvKind:
			if hasKnd || len(value) != 4 {
				return Address{}, ErrInvalid
			}
			kind, hasKnd = binary.BigEndian.Uint32(value), true
		}
	}
	if !hasD || !hasAuth || !hasKnd {
		return Address{}, ErrInvalid
	}

	canonical := fmt.Sprintf("%d:%s:%s", kind, hex.EncodeToString(author), identifier)
	if !utf8.ValidString(canonical) || desktopentry.CanonicalAddress(canonical) != nil {
		return Address{}, ErrInvalid
	}
	return Address{Canonical: canonical, Identifier: identifier, Relays: relayHints(relays)}, nil
}

// relayHints keeps the usable, distinct hints, at most MaxRelayHints.
func relayHints(relays []string) []string {
	var hints []string
	for _, r := range relays {
		if len(hints) == MaxRelayHints {
			break
		}
		if !ValidRelayHint(r) || slices.Contains(hints, r) {
			continue
		}
		hints = append(hints, r)
	}
	return hints
}

// decodeBech32 decodes code under hrp into bytes.
func decodeBech32(code, hrp string) ([]byte, bool) {
	got, data5, err := bech32.DecodeNoLimit(code)
	if err != nil || got != hrp {
		return nil, false
	}
	data, err := bech32.ConvertBits(data5, 5, 8, false)
	if err != nil {
		return nil, false
	}
	return data, true
}

// parseNevent reads an nevent naming a snapshot: an event id, optional
// relay hints, an optional author and an optional kind, which must be 5129
// when present. With the author it is converted to the canonical address;
// without one only the id is known (ErrSnapshotLookup).
func parseNevent(code string) (Address, error) {
	data, ok := decodeBech32(code, "nevent")
	if !ok {
		return Address{}, ErrInvalid
	}
	var (
		id, author             []byte
		kind                   uint32
		hasID, hasAuth, hasKnd bool
		relays                 []string
	)
	for len(data) > 0 {
		if len(data) < 2 || len(data) < 2+int(data[1]) {
			return Address{}, ErrInvalid
		}
		typ, value := data[0], data[2:2+int(data[1])]
		data = data[2+int(data[1]):]
		switch typ {
		case 0:
			if hasID || len(value) != 32 {
				return Address{}, ErrInvalid
			}
			id, hasID = value, true
		case tlvRelay:
			relays = append(relays, string(value))
		case tlvAuthor:
			if hasAuth || len(value) != 32 {
				return Address{}, ErrInvalid
			}
			author, hasAuth = value, true
		case tlvKind:
			if hasKnd || len(value) != 4 {
				return Address{}, ErrInvalid
			}
			kind, hasKnd = binary.BigEndian.Uint32(value), true
		}
	}
	if !hasID {
		return Address{}, ErrInvalid
	}
	if hasKnd && kind != snapshotKind {
		return Address{}, ErrUnsupported
	}
	idHex := hex.EncodeToString(id)
	hints := relayHints(relays)
	if !hasAuth {
		return Address{Identifier: idHex, Relays: hints}, ErrSnapshotLookup
	}
	canonical := fmt.Sprintf("%d:%s:%s", snapshotKind, hex.EncodeToString(author), idHex)
	if desktopentry.CanonicalAddress(canonical) != nil {
		return Address{}, ErrInvalid
	}
	return Address{Canonical: canonical, Identifier: idHex, Relays: hints}, nil
}

// parseNote reads a note1, which names only an event id (ErrSnapshotLookup).
func parseNote(code string) (Address, error) {
	data, ok := decodeBech32(code, "note")
	if !ok || len(data) != 32 {
		return Address{}, ErrInvalid
	}
	return Address{Identifier: hex.EncodeToString(data)}, ErrSnapshotLookup
}

// ValidRelayHint says whether s is a usable relay hint: a ws:// or wss://
// URL of at most MaxRelayHintLen bytes with a host and no credentials,
// query, fragment, whitespace or control bytes. Whether the host is public
// is decided later, at fetch time.
func ValidRelayHint(s string) bool {
	if len(s) == 0 || len(s) > MaxRelayHintLen || !utf8.ValidString(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] == 0x7f {
			return false
		}
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		return false
	}
	return u.Host != "" && u.User == nil && u.Opaque == "" && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery
}

// SafeIdentifier says whether a d tag is safe to act on from the command
// line: valid UTF-8 with no control or format characters (which can hide
// or reorder text in a terminal) and no line or paragraph separators.
func SafeIdentifier(d string) bool {
	if !utf8.ValidString(d) {
		return false
	}
	for _, r := range d {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}
