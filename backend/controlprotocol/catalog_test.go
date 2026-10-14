package controlprotocol

import (
	"slices"
	"testing"
)

// The catalog grows only additively within protocol version 1.
func TestMethodCatalogCount(t *testing.T) {
	names := MethodNames()
	if len(names) != 43 {
		t.Fatalf("catalog has %d methods, want 43", len(names))
	}
	for _, name := range []string{"napplet.icon", "napplet.social", "napplet.reactions", "napplet.comments", "napplet.mentions", "napplet.vote", "napplet.react", "napplet.comment", "napplet.delete", "napplet.share", "napplet.zaps", "napplet.zap"} {
		if !slices.Contains(names, name) {
			t.Fatalf("catalog lacks %s", name)
		}
	}
}
