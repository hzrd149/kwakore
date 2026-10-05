package backend

import (
	"encoding/json"
	"strings"
	"testing"
)

// hostileShortcutID is a napplet id whose d tag tries to add a second Exec
// line to any .desktop file it is written into raw.
var hostileShortcutID = "35129:" + strings.Repeat("ab", 32) + ":x\nExec=/bin/evil"

func TestLaunchTokenRoundTrip(t *testing.T) {
	token := LaunchToken(hostileShortcutID)
	if !strings.HasPrefix(token, "=") {
		t.Fatalf("launch token does not start with '=': %q", token)
	}
	for _, r := range token[1:] {
		ok := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			t.Fatalf("launch token holds %q outside base64url: %q", r, token)
		}
	}

	entries, err := parseBundleToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].NappID != hostileShortcutID {
		t.Fatalf("launch token parsed to %#v", entries)
	}

	// shortcut files written by earlier builds carry the raw id
	entries, err = parseBundleToken("abcdef0123456789~notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].NappID != "abcdef0123456789~notes" {
		t.Fatalf("legacy raw id parsed to %#v", entries)
	}

	for _, bad := range []string{"=", "=!!not-base64!!", "=a"} {
		if _, err := parseBundleToken(bad); err == nil {
			t.Fatalf("malformed launch token %q parsed", bad)
		}
	}

	// a bundle naming the hostile napplet survives the token split
	bundle := []ShortcutEntry{{
		NappID:  hostileShortcutID,
		Actions: []ShortcutAction{{Type: "open", Payload: json.RawMessage(`{"x":"a b"}`)}},
	}}
	encoded := bundleToken(bundle)
	fields := strings.Fields(encoded)
	if len(fields) != 2 || fields[0] != token || !strings.HasPrefix(fields[1], "+") {
		t.Fatalf("bundle token is not the launch token and one action: %q", encoded)
	}
	back, err := parseBundleToken(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].NappID != hostileShortcutID || len(back[0].Actions) != 1 ||
		back[0].Actions[0].Type != "open" || string(back[0].Actions[0].Payload) != `{"x":"a b"}` {
		t.Fatalf("bundle token round-tripped to %#v", back)
	}
}

func TestBundleTokenLegacyMixedFields(t *testing.T) {
	// an earlier build's token: a raw id followed by a bare action name
	entries, err := parseBundleToken("abcdef0123456789~notes +open " + LaunchToken("dev~x y"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].NappID != "abcdef0123456789~notes" ||
		len(entries[0].Actions) != 1 || entries[0].Actions[0].Type != "open" ||
		entries[1].NappID != "dev~x y" {
		t.Fatalf("mixed token parsed to %#v", entries)
	}
}
