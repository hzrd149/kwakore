package desktopentry_test

import (
	"encoding/base64"
	"math/rand/v2"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"verdana/backend"
	"verdana/backend/desktopentry"
)

func tokenAddressCorpus() []string {
	pk := nostr.KeyOne.Public().Hex()
	naddr := nip19.EncodeNaddr(nostr.KeyOne.Public(), 35129, "d", nil)
	return []string{
		"35129:" + pk + ":notes",
		"35130:" + pk + ":napp",
		"15129:" + pk + ":",
		"35129:" + pk + ":with:colons",
		"35129:" + pk + ":new\nline in the middle",
		"35129:" + pk + ":tab\tand %f %u $(rm -rf ~) `id` \"quote\" 'single' \\back",
		"35129:" + pk + ":Ünïcødé ✓ ‮evil",
		"35129:" + pk + ":" + strings.Repeat("x", desktopentry.MaxAddressLen-len("35129:"+pk+":")),
		"35129:" + pk + ":" + strings.Repeat("x", desktopentry.MaxAddressLen-len("35129:"+pk+":")+1),
		"35129:" + pk + ":trailing ",
		"35129:" + pk + ":trailing\n",
		" 35129:" + pk + ":leading",
		"35129:" + strings.ToUpper(pk) + ":upper",
		"035129:" + pk + ":zero",
		"+35129:" + pk + ":plus",
		"1:" + pk + ":kind",
		"30023:" + pk + ":kind",
		"35129:" + pk + ":",
		"15129:" + pk + ":d",
		"35129:" + pk,
		"35129:" + pk[:63] + ":short",
		"35129:" + strings.Repeat("f", 64) + ":not-a-point",
		"35129:" + strings.Repeat("0", 64) + ":zero-point",
		"nostr:35129:" + pk + ":prefixed",
		naddr,
		"35129:" + pk + ":" + naddr,
		"35129:" + pk + ":NADDR1" + strings.Repeat("Q", 30),
		"",
	}
}

// The CLI's pre-dial check and the daemon's ParseCanonicalServiceAddress
// must agree on every input, or a desktop entry could carry an address the
// daemon refuses (or the CLI could refuse one the daemon accepts).
func TestTokenCanonicalMatchesService(t *testing.T) {
	inputs := tokenAddressCorpus()
	rng := rand.New(rand.NewPCG(9, 3))
	base := inputs[0]
	alphabet := []byte("0123456789abcdefABCDEF: \n\t%naddr1q")
	for range 2000 {
		b := []byte(base)
		for range 1 + rng.IntN(3) {
			i := rng.IntN(len(b))
			switch rng.IntN(3) {
			case 0:
				b[i] = alphabet[rng.IntN(len(alphabet))]
			case 1:
				b = append(b[:i], b[i+1:]...)
			default:
				b = append(b[:i], append([]byte{alphabet[rng.IntN(len(alphabet))]}, b[i:]...)...)
			}
			if len(b) == 0 {
				break
			}
		}
		inputs = append(inputs, string(b))
	}
	accepted := 0
	for _, input := range inputs {
		_, serviceErr := backend.ParseCanonicalServiceAddress(input)
		localErr := desktopentry.CanonicalAddress(input)
		if (serviceErr == nil) != (localErr == nil) {
			t.Fatalf("disagreement on %q: service=%v local=%v", input, serviceErr, localErr)
		}
		if localErr == nil {
			accepted++
			token, err := desktopentry.EncodeToken(input)
			if err != nil {
				t.Fatalf("encode %q: %v", input, err)
			}
			if got, err := desktopentry.DecodeToken(token); err != nil || got != input {
				t.Fatalf("round trip %q: %q %v", input, got, err)
			}
		} else if _, err := desktopentry.EncodeToken(input); err == nil {
			t.Fatalf("encoded invalid address %q", input)
		}
	}
	if accepted < 8 {
		t.Fatalf("corpus accepted only %d addresses", accepted)
	}
}

func TestTokenRejectsMalformed(t *testing.T) {
	address := "35129:" + nostr.KeyOne.Public().Hex() + ":notes"
	token, err := desktopentry.EncodeToken(address)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(token, "=+/ \n") || strings.Contains(token, address) {
		t.Fatalf("token is not inert: %q", token)
	}
	// A 73-byte address ends in two characters whose last one carries four
	// unused bits; setting one of them is a second spelling of the same
	// bytes, which the strict decoder refuses.
	odd := base64.RawURLEncoding.EncodeToString([]byte("35129:" + nostr.KeyOne.Public().Hex() + ":no"))
	trailing := odd[:len(odd)-1] + string(odd[len(odd)-1]+1)
	for name, bad := range map[string]string{
		"empty":         "",
		"padded":        base64.URLEncoding.EncodeToString([]byte(address)),
		"std alphabet":  strings.NewReplacer("-", "+", "_", "/").Replace(base64.RawURLEncoding.EncodeToString([]byte(address + "?>?"))),
		"whitespace":    token[:10] + " " + token[10:],
		"newline":       token + "\n",
		"truncated":     token[:len(token)-1],
		"trailing bits": trailing,
		"raw address":   address,
		"oversized":     strings.Repeat("A", desktopentry.MaxTokenLen+4),
		"noncanonical":  base64.RawURLEncoding.EncodeToString([]byte("nostr:" + address)),
		"uppercase":     base64.RawURLEncoding.EncodeToString([]byte(strings.ToUpper(address))),
		"unicode":       "Mz" + "é" + token[3:],
	} {
		if got, err := desktopentry.DecodeToken(bad); err == nil {
			t.Fatalf("%s: accepted %q as %q", name, bad, got)
		}
	}
}
