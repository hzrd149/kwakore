package controlprotocol

import (
	"slices"
	"testing"
)

// The catalog grows only additively within protocol version 1.
func TestMethodCatalogCount(t *testing.T) {
	names := MethodNames()
	if len(names) != 30 {
		t.Fatalf("catalog has %d methods, want 30", len(names))
	}
	if !slices.Contains(names, "napplet.icon") {
		t.Fatal("catalog lacks napplet.icon")
	}
}
