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

func TestSystemSearchEntriesSkipUnavailableDiscovery(t *testing.T) {
	broken := Napp{ID: "broken", Name: "x’; Start-Process calc; ’", Format: FormatNapplet, Unavailable: "latest version is invalid"}
	installedBroken := Napp{ID: "kept", Name: "Kept", Format: FormatNapplet, Unavailable: "latest version is invalid"}

	got := systemSearchEntries(State{
		Discovery: []Napp{broken, installedBroken},
		Installed: []Napp{installedBroken},
	})
	// the unavailable discovery entry is left out; an installed copy whose
	// latest event is invalid still runs, so it keeps its entry
	if len(got) != 1 || got[0].ID != "kept" {
		t.Fatalf("search entries = %+v", got)
	}
}
