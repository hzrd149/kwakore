package backend

import (
	"slices"
	"testing"
)

func TestSystemSearchEntriesIncludeDiscoveryAndInstalledNapplets(t *testing.T) {
	discovered := Napp{ID: "discovered", Name: "Paint", Description: "Draw", Format: FormatNapplet, AuthorName: "Ada"}
	installed := Napp{ID: "installed", Name: "Notes", Format: FormatNapplet}
	napp := Napp{ID: "napp", Name: "Native app"}

	got := systemSearchEntries(State{
		Discovery: []Napp{discovered, discovered, napp},
		Installed: []Napp{installed},
	})
	ids := make([]string, len(got))
	for i := range got {
		ids[i] = got[i].ID
	}
	if !slices.Equal(ids, []string{"installed", "discovered"}) {
		t.Fatalf("search entry IDs = %v", ids)
	}
	if got[1].Description != "Draw — by Ada" {
		t.Fatalf("search description = %q", got[1].Description)
	}
}
