package napaddr_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"github.com/btcsuite/btcd/btcutil/bech32"
	"kwakore/backend"
	"kwakore/backend/desktopentry"
	"kwakore/backend/napaddr"
)

var (
	testPK  = nostr.KeyOne.Public()
	testHex = nostr.KeyOne.Public().Hex()
)

// tlv is one naddr TLV entry for hand-crafted codes.
type tlv struct {
	typ   byte
	value []byte
}

func encodeTLV(t testing.TB, hrp string, entries ...tlv) string {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		buf.WriteByte(e.typ)
		buf.WriteByte(byte(len(e.value)))
		buf.Write(e.value)
	}
	return encodeRaw(t, hrp, buf.Bytes())
}

func encodeRaw(t testing.TB, hrp string, data []byte) string {
	t.Helper()
	bits5, err := bech32.ConvertBits(data, 8, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	s, err := bech32.Encode(hrp, bits5)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func kindBytes(k uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, k)
	return b
}

func TestParseCanonicalPassesThrough(t *testing.T) {
	for _, input := range []string{
		"35129:" + testHex + ":notes",
		"35130:" + testHex + ":napp",
		"15129:" + testHex + ":",
	} {
		addr, err := napaddr.Parse(input)
		if err != nil || addr.Canonical != input || addr.Relays != nil {
			t.Fatalf("%q: %+v %v", input, addr, err)
		}
	}
	addr, _ := napaddr.Parse("35129:" + testHex + ":with:colons")
	if addr.Identifier != "with:colons" {
		t.Fatalf("identifier %q", addr.Identifier)
	}
}

func TestParseNaddrForms(t *testing.T) {
	relays := []string{"wss://relay.napplet.soy", "wss://relay.example.com"}
	naddr := nip19.EncodeNaddr(testPK, 35129, "n-143146b0d6f", relays)
	want := "35129:" + testHex + ":n-143146b0d6f"
	for _, input := range []string{
		naddr,
		"nostr:" + naddr,
		"NOSTR:" + naddr,
		strings.ToUpper(naddr),
		"nostr:" + strings.ToUpper(naddr),
		"Nostr:" + naddr,
	} {
		addr, err := napaddr.Parse(input)
		if err != nil || addr.Canonical != want || addr.Identifier != "n-143146b0d6f" || !slices.Equal(addr.Relays, relays) {
			t.Fatalf("%q: %+v %v", input, addr, err)
		}
	}

	root := nip19.EncodeNaddr(testPK, 15129, "", nil)
	if addr, err := napaddr.Parse(root); err != nil || addr.Canonical != "15129:"+testHex+":" || addr.Relays != nil {
		t.Fatalf("root napplet: %+v %v", addr, err)
	}
	if addr, err := napaddr.Parse(nip19.EncodeNaddr(testPK, 35130, "napp", nil)); err != nil || addr.Canonical != "35130:"+testHex+":napp" {
		t.Fatalf("napp: %+v %v", addr, err)
	}
}

func TestParseRejects(t *testing.T) {
	naddr := nip19.EncodeNaddr(testPK, 35129, "notes", []string{"wss://relay.napplet.soy"})
	flipped := []byte(naddr)
	if flipped[20] == 'q' {
		flipped[20] = 'p'
	} else {
		flipped[20] = 'q'
	}
	canonicalPrefix := "35129:" + testHex + ":"
	invalid := map[string]string{
		"empty":            "",
		"garbage":          "hello",
		"bare hrp":         "naddr1",
		"checksum":         string(flipped),
		"mixed case":       strings.ToUpper(naddr[:1]) + naddr[1:],
		"web link":         "https://njump.me/" + naddr,
		"nostr canonical":  "nostr:" + canonicalPrefix + "d",
		"leading space":    " " + naddr,
		"trailing newline": naddr + "\n",
		"double scheme":    "nostr:nostr:" + naddr,
		"scheme only":      "nostr:",
		"oversized":        strings.Repeat("a", desktopentry.MaxAddressLen+1),
		"padded canonical": canonicalPrefix + strings.Repeat("x", desktopentry.MaxAddressLen),
		"not a point":      nip19.EncodeNaddr(nostr.PubKey(bytes.Repeat([]byte{0xbb}, 32)), 35129, "d", nil),
		"embedded naddr":   nip19.EncodeNaddr(testPK, 35129, "naddr1"+strings.Repeat("q", 30), nil),
		"trailing space d": nip19.EncodeNaddr(testPK, 35129, "notes ", nil),
		"non-utf8 d":       nip19.EncodeNaddr(testPK, 35129, "no\xfftes", nil),
		"non-utf8 canon":   canonicalPrefix + "no\xfftes",
		"15129 with d":     nip19.EncodeNaddr(testPK, 15129, "x", nil),
		"35129 empty d":    nip19.EncodeNaddr(testPK, 35129, "", nil),
		"kind 30023":       nip19.EncodeNaddr(testPK, 30023, "post", nil),
		"15129 no d tlv":   encodeTLV(t, "naddr", tlv{2, testPK[:]}, tlv{3, kindBytes(15129)}),
		"truncated entry":  encodeRaw(t, "naddr", append(tlvBytes(tlv{0, []byte("d")}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}), 1, 10, 'w')),
		"dangling byte":    encodeRaw(t, "naddr", append(tlvBytes(tlv{0, []byte("d")}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}), 1)),
		"short kind":       encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{2, testPK[:]}, tlv{3, []byte{0x89, 0x39}}),
		"short author":     encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{2, testPK[:31]}, tlv{3, kindBytes(35129)}),
		"duplicate d":      encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{0, []byte("e")}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}),
		"duplicate author": encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{2, testPK[:]}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}),
		"duplicate kind":   encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}, tlv{3, kindBytes(35129)}),
		"missing d":        encodeTLV(t, "naddr", tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}),
		"missing author":   encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{3, kindBytes(35129)}),
		"missing kind":     encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{2, testPK[:]}),
		"other hrp":        encodeTLV(t, "naddrx", tlv{0, []byte("d")}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}),
	}
	for name, input := range invalid {
		if addr, err := napaddr.Parse(input); !errors.Is(err, napaddr.ErrInvalid) {
			t.Errorf("%s: %+v %v", name, addr, err)
		}
	}

	var id nostr.ID
	id[0] = 1
	unsupported := map[string]string{
		"npub":     nip19.EncodeNpub(testPK),
		"nprofile": nip19.EncodeNprofile(testPK, []string{"wss://relay.example.com"}),
		"note":     encodeRaw(t, "note", id[:]),
		"nevent":   nip19.EncodeNevent(id, nil, testPK),
		"nsec":     nip19.EncodeNsec(nostr.KeyOne),
		"nrelay":   "nrelay1qqqqqqqq",
		"broken":   "nsec1notevenbech32",
	}
	for name, code := range unsupported {
		for _, input := range []string{code, strings.ToUpper(code), "nostr:" + code, "NOSTR:" + strings.ToUpper(code)} {
			if addr, err := napaddr.Parse(input); !errors.Is(err, napaddr.ErrUnsupported) {
				t.Errorf("%s %q: %+v %v", name, input, addr, err)
			}
		}
	}
}

func tlvBytes(entries ...tlv) []byte {
	var buf bytes.Buffer
	for _, e := range entries {
		buf.WriteByte(e.typ)
		buf.WriteByte(byte(len(e.value)))
		buf.Write(e.value)
	}
	return buf.Bytes()
}

func TestParseIgnoresUnknownTLV(t *testing.T) {
	code := encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{9, []byte("whatever")}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)})
	if addr, err := napaddr.Parse(code); err != nil || addr.Canonical != "35129:"+testHex+":d" {
		t.Fatalf("unknown type: %+v %v", addr, err)
	}
}

func TestParseFiltersRelayHints(t *testing.T) {
	bad := []string{
		"relay.damus.io",
		"http://x.example",
		"wss://u:p@x.example",
		"wss://x.example?q=1",
		"wss://x.example?",
		"wss://x.example#f",
		"wss://x.example/a b",
		"wss://x.example/\x01",
		"ws:opaque",
	}
	good := []string{"wss://a.example", "ws://b.example/path", "WSS://c.example"}
	hints := append(append([]string{}, bad...), good...)
	hints = append(hints, "wss://a.example")
	addr, err := napaddr.Parse(nip19.EncodeNaddr(testPK, 35129, "d", hints))
	if err != nil || !slices.Equal(addr.Relays, good) {
		t.Fatalf("filtered: %+v %v", addr, err)
	}
	var many []string
	for i := range 12 {
		many = append(many, fmt.Sprintf("wss://r%d.example", i))
	}
	addr, err = napaddr.Parse(nip19.EncodeNaddr(testPK, 35129, "d", many))
	if err != nil || !slices.Equal(addr.Relays, many[:napaddr.MaxRelayHints]) {
		t.Fatalf("capped: %+v %v", addr, err)
	}
	addr, err = napaddr.Parse(nip19.EncodeNaddr(testPK, 35129, "d", bad))
	if err != nil || addr.Relays != nil {
		t.Fatalf("only bad hints: %+v %v", addr, err)
	}
	// an naddr TLV cannot carry more than 255 bytes, so this one is only
	// checked directly
	tooLong := "wss://" + strings.Repeat("a", napaddr.MaxRelayHintLen)
	if !napaddr.ValidRelayHint(tooLong[:napaddr.MaxRelayHintLen]) {
		t.Error("255-byte hint refused")
	}
	for _, s := range append(bad, tooLong, "") {
		if napaddr.ValidRelayHint(s) {
			t.Errorf("hint %q accepted", s)
		}
	}
	for _, s := range good {
		if !napaddr.ValidRelayHint(s) {
			t.Errorf("hint %q refused", s)
		}
	}
}

func TestSafeIdentifier(t *testing.T) {
	for _, d := range []string{"n-143146b0d6f", "with space", "Ünïcødé ✓", ""} {
		if !napaddr.SafeIdentifier(d) {
			t.Errorf("%q refused", d)
		}
	}
	for _, d := range []string{"a\tb", "a\nb", "a\x00b", "a\x7fb", "a\u0085b", "a\u202eb", "a\u200bb", "a\ufeffb", "a\u00adb", "a\u2028b", "a\u2029b", "a\xffb"} {
		if napaddr.SafeIdentifier(d) {
			t.Errorf("%q accepted", d)
		}
	}
}

func parseCorpus(t *testing.T) []string {
	relays := []string{"wss://relay.napplet.soy", "relay.damus.io", "wss://relay.example.com"}
	naddr := nip19.EncodeNaddr(testPK, 35129, "n-143146b0d6f", relays)
	return []string{
		"35129:" + testHex + ":notes",
		"35130:" + testHex + ":napp",
		"15129:" + testHex + ":",
		"35129:" + testHex + ":new\nline",
		"35129:" + testHex + ":Ünïcødé ✓ \u202eevil",
		naddr,
		"nostr:" + naddr,
		"NOSTR:" + naddr,
		strings.ToUpper(naddr),
		"nostr:" + strings.ToUpper(naddr),
		nip19.EncodeNaddr(testPK, 15129, "", nil),
		nip19.EncodeNaddr(testPK, 35130, "napp", []string{"wss://a.example"}),
		nip19.EncodeNaddr(testPK, 35129, "tab\tin d", nil),
		nip19.EncodeNaddr(testPK, 35129, "with:colon", nil),
		nip19.EncodeNaddr(testPK, 15129, "x", nil),
		nip19.EncodeNaddr(testPK, 35129, "", nil),
		nip19.EncodeNaddr(testPK, 30023, "post", nil),
		"https://njump.me/" + naddr,
		" " + naddr,
		naddr + "\n",
		"nostr:35129:" + testHex + ":d",
		encodeTLV(t, "naddr", tlv{0, []byte("d")}, tlv{9, []byte("x")}, tlv{2, testPK[:]}, tlv{3, kindBytes(35129)}),
		nip19.EncodeNpub(testPK),
		"",
	}
}

// Every address the CLI accepts must name exactly the napplet the backend
// reads from the same input, and the canonical form it sends must pass the
// daemon's rule, or a pasted naddr could install something else.
func TestParseMatchesBackend(t *testing.T) {
	inputs := parseCorpus(t)
	rng := rand.New(rand.NewPCG(26, 1007))
	bases := []string{inputs[5], inputs[0]}
	alphabet := []byte("023456789acdefghjklmnpqrstuvwxyzQPZRY:nostr \n\t")
	for i := range 2000 {
		b := []byte(bases[i%2])
		for range 1 + rng.IntN(3) {
			if len(b) == 0 {
				break
			}
			j := rng.IntN(len(b))
			switch rng.IntN(3) {
			case 0:
				b[j] = alphabet[rng.IntN(len(alphabet))]
			case 1:
				b = append(b[:j], b[j+1:]...)
			default:
				b = append(b[:j], append([]byte{alphabet[rng.IntN(len(alphabet))]}, b[j:]...)...)
			}
		}
		inputs = append(inputs, string(b))
	}
	accepted := 0
	for _, input := range inputs {
		addr, err := napaddr.Parse(input)
		if err != nil {
			continue
		}
		accepted++
		if desktopentry.CanonicalAddress(addr.Canonical) != nil {
			t.Fatalf("%q: canonical %q refused by desktopentry", input, addr.Canonical)
		}
		svc, err := backend.ParseCanonicalServiceAddress(addr.Canonical)
		if err != nil {
			t.Fatalf("%q: canonical %q refused by service: %v", input, addr.Canonical, err)
		}
		ptr, err := backend.ParseNappAddress(input)
		if err != nil {
			t.Fatalf("%q: backend refused: %v", input, err)
		}
		if ptr.Kind != svc.Kind || ptr.PublicKey != svc.PublicKey || ptr.Identifier != svc.Identifier || ptr.Identifier != addr.Identifier {
			t.Fatalf("%q: backend %+v, cli %+v", input, ptr, addr)
		}
		// each kept hint is in the backend's list, in the same order
		rest := ptr.Relays
		for _, h := range addr.Relays {
			k := slices.Index(rest, h)
			if k < 0 {
				t.Fatalf("%q: hint %q not in backend relays %v", input, h, ptr.Relays)
			}
			rest = rest[k+1:]
		}
	}
	if accepted < 10 {
		t.Fatalf("only %d inputs accepted", accepted)
	}
}
