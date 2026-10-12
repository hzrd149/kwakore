package controlprotocol

import (
	"slices"
	"testing"
)

// The catalog grows only additively within protocol version 1.
func TestMethodCatalogCount(t *testing.T) {
	names := MethodNames()
	if len(names) != 34 {
		t.Fatalf("catalog has %d methods, want 34", len(names))
	}
	for _, name := range []string{"napplet.icon", "napplet.social", "napplet.reactions", "napplet.comments"} {
		if !slices.Contains(names, name) {
			t.Fatalf("catalog lacks %s", name)
		}
	}
}
