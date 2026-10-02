//go:build linux

package main

import (
	"slices"
	"testing"
	"verdana/backend"

	"fiatjaf.com/nostr"
)

func TestSearchNappletIDsMatchesAllTermsAndOnlyNapplets(t *testing.T) {
	author := nostr.Generate().Public()
	paint := backend.Napp{ID: "paint", Name: "Pocket Paint", Description: "Draw pixel art", Format: backend.FormatNapplet, Author: author}
	notes := backend.Napp{ID: "notes", Name: "Paint Notes", Description: "Write words", Format: backend.FormatNapplet, Author: author}
	napp := backend.Napp{ID: "native", Name: "Paint Studio"}
	st := backend.State{Discovery: []backend.Napp{notes, napp, paint}}

	got := searchNappletIDs(st, []string{"paint", "pixel"}, map[string]bool{author.Hex(): true})
	if !slices.Equal(got, []string{"paint"}) {
		t.Fatalf("search results = %v, want [paint]", got)
	}
}

func TestSearchNappletIDsRanksPrefixThenInstalled(t *testing.T) {
	author := nostr.Generate().Public()
	installed := backend.Napp{ID: "installed", Name: "My Paint", Format: backend.FormatNapplet}
	prefix := backend.Napp{ID: "prefix", Name: "Paint Box", Format: backend.FormatNapplet, Author: author}
	other := backend.Napp{ID: "other", Name: "Finger Paint", Format: backend.FormatNapplet, Author: author}
	st := backend.State{
		Installed: []backend.Napp{installed},
		Discovery: []backend.Napp{other, prefix, installed},
	}

	got := searchNappletIDs(st, []string{"paint"}, map[string]bool{author.Hex(): true})
	want := []string{"prefix", "installed", "other"}
	if !slices.Equal(got, want) {
		t.Fatalf("search results = %v, want %v", got, want)
	}
}

func TestSearchNappletIDsRequiresQuery(t *testing.T) {
	n := backend.Napp{ID: "all", Name: "Everything", Format: backend.FormatNapplet}
	if got := searchNappletIDs(backend.State{Discovery: []backend.Napp{n}}, nil, nil); len(got) != 0 {
		t.Fatalf("empty query returned %v", got)
	}
}

func TestSearchNappletIDsExcludesUnfollowedRemoteAuthors(t *testing.T) {
	followed := nostr.Generate().Public()
	stranger := nostr.Generate().Public()
	st := backend.State{Discovery: []backend.Napp{
		{ID: "friend", Name: "Paint Friend", Format: backend.FormatNapplet, Author: followed},
		{ID: "stranger", Name: "Paint Stranger", Format: backend.FormatNapplet, Author: stranger},
	}}

	got := searchNappletIDs(st, []string{"paint"}, map[string]bool{followed.Hex(): true})
	if !slices.Equal(got, []string{"friend"}) {
		t.Fatalf("search results = %v, want [friend]", got)
	}
}

func TestSearchDebugEnabled(t *testing.T) {
	t.Setenv("VERDANA_SEARCH_DEBUG", "true")
	if !searchDebugEnabled() {
		t.Fatal("VERDANA_SEARCH_DEBUG=true did not enable tracing")
	}
	t.Setenv("VERDANA_SEARCH_DEBUG", "0")
	if searchDebugEnabled() {
		t.Fatal("VERDANA_SEARCH_DEBUG=0 enabled tracing")
	}
}
