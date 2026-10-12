package controlprotocol

import (
	"slices"
	"testing"
)

// The catalog grows only additively within protocol version 1.
func TestMethodCatalogCount(t *testing.T) {
	names := MethodNames()
	if len(names) != 38 {
		t.Fatalf("catalog has %d methods, want 38", len(names))
	}
	for _, name := range []string{"napplet.icon", "napplet.social", "napplet.reactions", "napplet.comments", "napplet.vote", "napplet.react", "napplet.comment", "napplet.delete"} {
		if !slices.Contains(names, name) {
			t.Fatalf("catalog lacks %s", name)
		}
	}
}
