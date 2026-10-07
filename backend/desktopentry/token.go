// Package desktopentry carries an installed napplet's canonical address
// through a native Linux desktop entry and back to the control CLI.
//
// The address holds the author's d tag, which may contain spaces, quotes,
// percent signs, newlines or any other byte. None of that may reach a
// desktop entry's key names or Exec syntax, so the address travels as one
// inert launch token: the unpadded base64url of its bytes. The CLI command
// `kwakore launch-token TOKEN` decodes the token, checks that it names a
// full canonical address and sends the ordinary napplet.launch request.
//
// The package is a leaf: the CLI imports it, and it must not pull in the
// backend root (relay pool, stores) to do so.
package desktopentry

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

// MaxAddressLen is the longest canonical address the service accepts.
const MaxAddressLen = 4096

// MaxTokenLen is the longest launch token, the encoding of MaxAddressLen
// bytes. A longer token is refused before it is decoded.
var MaxTokenLen = base64.RawURLEncoding.EncodedLen(MaxAddressLen)

var (
	// ErrInvalidAddress is returned for anything that is not a full
	// canonical "<kind>:<pubkey hex>:<d>" address of a napp or napplet.
	ErrInvalidAddress = errors.New("invalid canonical address")
	// ErrInvalidToken is returned for a malformed, oversized, padded or
	// otherwise non-canonical launch token.
	ErrInvalidToken = errors.New("invalid launch token")
)

var tokenEncoding = base64.RawURLEncoding.Strict()

// naddrPattern is the backend's ParseNappAddress pattern. When it matches,
// the backend reads the embedded naddr instead of the coordinate, so such an
// input can never be canonical.
var naddrPattern = regexp.MustCompile(`naddr1[02-9ac-hj-np-z]{20,}`)

// CanonicalAddress reports whether address is exactly what the service's
// ParseCanonicalServiceAddress accepts: a kind the launcher runs written in
// plain decimal, a lowercase hex x-only public key that is a valid curve
// point, and a d tag that is present for the addressable kinds and absent
// for the root napplet. The backend's equivalence test pins the two
// together; this copy exists so the CLI can refuse a bad token before it
// dials without linking the whole backend.
func CanonicalAddress(address string) error {
	if len(address) == 0 || len(address) > MaxAddressLen || strings.TrimSpace(address) != address {
		return ErrInvalidAddress
	}
	kind, rest, ok := strings.Cut(address, ":")
	if !ok {
		return ErrInvalidAddress
	}
	pubkey, identifier, ok := strings.Cut(rest, ":")
	if !ok || len(pubkey) != 64 {
		return ErrInvalidAddress
	}
	switch kind {
	case "35130", "35129":
		if identifier == "" {
			return ErrInvalidAddress
		}
	case "15129":
		if identifier != "" {
			return ErrInvalidAddress
		}
	default:
		return ErrInvalidAddress
	}
	raw, err := hex.DecodeString(pubkey)
	if err != nil || hex.EncodeToString(raw) != pubkey {
		return ErrInvalidAddress
	}
	if _, err := schnorr.ParsePubKey(raw); err != nil {
		return ErrInvalidAddress
	}
	if naddrPattern.MatchString(strings.ToLower(address)) {
		return ErrInvalidAddress
	}
	return nil
}

// EncodeToken returns the launch token for a canonical address.
func EncodeToken(address string) (string, error) {
	if err := CanonicalAddress(address); err != nil {
		return "", err
	}
	return tokenEncoding.EncodeToString([]byte(address)), nil
}

// DecodeToken turns a launch token back into its canonical address. The
// length and alphabet are checked before any decoding, so padding, the
// standard alphabet, whitespace and oversized input are refused outright;
// the decode is strict and must re-encode to the same token, so there is
// exactly one token per address.
func DecodeToken(token string) (string, error) {
	if len(token) == 0 || len(token) > MaxTokenLen {
		return "", ErrInvalidToken
	}
	for i := 0; i < len(token); i++ {
		if !tokenByte(token[i]) {
			return "", ErrInvalidToken
		}
	}
	raw, err := tokenEncoding.DecodeString(token)
	if err != nil || tokenEncoding.EncodeToString(raw) != token {
		return "", ErrInvalidToken
	}
	address := string(raw)
	if CanonicalAddress(address) != nil {
		return "", ErrInvalidToken
	}
	return address, nil
}

func tokenByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}
