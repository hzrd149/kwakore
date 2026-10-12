package controlprotocol

import (
	"slices"
	"testing"
)

// The catalog grows only additively within protocol version 1.
func TestMethodCatalogCount(t *testing.T) {
	names := MethodNames()
	if len(names) != 40 {
		t.Fatalf("catalog has %d methods, want 40", len(names))
	}
	for _, name := range []string{"napplet.icon", "napplet.social", "napplet.reactions", "napplet.comments", "napplet.mentions", "napplet.vote", "napplet.react", "napplet.comment", "napplet.delete", "napplet.share"} {
		if !slices.Contains(names, name) {
			t.Fatalf("catalog lacks %s", name)
		}
	}
}
