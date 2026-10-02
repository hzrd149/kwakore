package main

import (
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

func TestTrayUserLabel(t *testing.T) {
	if got := trayUserLabel("", ""); got != "Not logged in" {
		t.Errorf("logged out: %q", got)
	}
	pk := nostr.Generate().Public().Hex()
	if got := trayUserLabel("alice", pk); got != "alice" {
		t.Errorf("named: %q", got)
	}
	got := trayUserLabel(pk, pk)
	if !strings.HasPrefix(got, "npub1") || !strings.Contains(got, "…") || len([]rune(got)) != 15 {
		t.Errorf("hex fallback: %q", got)
	}
}
