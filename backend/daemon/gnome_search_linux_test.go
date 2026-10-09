//go:build linux

package daemon

import (
	"slices"
	"testing"

	"kwakore/backend"
)

func TestGNOMESearchMatchesCatalogTerms(t *testing.T) {
	items := []backend.Napp{
		{ID: "paint", Name: "Paint Box", Description: "pixel art", Format: backend.FormatNapplet},
		{ID: "notes", Name: "Paint Notes", Description: "writing", Format: backend.FormatNapplet},
		{ID: "native", Name: "Paint Studio"},
	}
	if got := searchNappletIDs(items, []string{"paint", "pixel"}); !slices.Equal(got, []string{"paint"}) {
		t.Fatalf("results: %v", got)
	}
	if got := searchNappletIDs(items, nil); len(got) != 0 {
		t.Fatalf("empty query: %v", got)
	}
}
