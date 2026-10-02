//go:build linux

package osintegration

import (
	"os"
	"sort"
	"strings"
	"verdana/backend"

	"github.com/godbus/dbus/v5"
)

const (
	searchProviderBus       = "com.verdana.Verdana.SearchProvider"
	searchProviderInterface = "org.gnome.Shell.SearchProvider2"
	searchProviderPath      = dbus.ObjectPath("/com/verdana/Verdana/SearchProvider")
	searchProviderLimit     = 20
)

type searchProvider struct {
	onSearch func(query string)
}

// StartSearchProvider exposes the GNOME Shell SearchProvider2 interface for
// this launcher process; searches the user opens in Verdana go to onSearch. Failure is non-fatal: Verdana also runs on Linux
// desktops which have no session bus or do not implement GNOME search.
func StartSearchProvider(onSearch func(query string)) func() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Debug().Err(err).Msg("GNOME search provider unavailable")
		return func() {}
	}
	provider := &searchProvider{onSearch: onSearch}
	if err := conn.Export(provider, searchProviderPath, searchProviderInterface); err != nil {
		conn.Close()
		log.Warn().Err(err).Msg("could not export GNOME search provider")
		return func() {}
	}
	reply, err := conn.RequestName(searchProviderBus, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		log.Debug().Err(err).Uint32("reply", uint32(reply)).Msg("GNOME search provider name unavailable")
		return func() {}
	}
	log.Info().Msg("GNOME search provider ready")
	return func() {
		_, _ = conn.ReleaseName(searchProviderBus)
		_ = conn.Close()
	}
}

func (p *searchProvider) GetInitialResultSet(terms []string) ([]string, *dbus.Error) {
	if !backend.GNOMESearchEnabled() {
		return []string{}, nil
	}
	ids := searchNappletIDs(backend.Snapshot(), terms)
	logSearchQuery("initial", terms, nil, ids)
	return ids, nil
}

func (p *searchProvider) GetSubsearchResultSet(previous []string, terms []string) ([]string, *dbus.Error) {
	if !backend.GNOMESearchEnabled() {
		return []string{}, nil
	}
	ids := searchNappletIDs(backend.Snapshot(), terms)
	logSearchQuery("subsearch", terms, previous, ids)
	return ids, nil
}

func (searchProvider) GetResultMetas(ids []string) ([]map[string]dbus.Variant, *dbus.Error) {
	napplets := nappletsByID(backend.Snapshot())
	metas := make([]map[string]dbus.Variant, 0, len(ids))
	for _, id := range ids {
		n, ok := napplets[id]
		if !ok {
			continue
		}
		description := n.Description
		if n.AuthorName != "" {
			if description != "" {
				description += " — "
			}
			description += "by " + n.AuthorName
		}
		metas = append(metas, map[string]dbus.Variant{
			"id":          dbus.MakeVariant(n.ID),
			"name":        dbus.MakeVariant(n.Name),
			"description": dbus.MakeVariant(description),
			"gicon":       dbus.MakeVariant("application-x-executable"),
		})
	}
	if searchDebugEnabled() {
		log.Info().Str("search_method", "metas").Strs("requested", ids).
			Int("returned", len(metas)).Msg("GNOME search request")
	}
	return metas, nil
}

func (searchProvider) ActivateResult(id string, _ []string, _ uint32) *dbus.Error {
	if searchDebugEnabled() {
		log.Info().Str("search_method", "activate").Str("result", id).Msg("GNOME search request")
	}
	backend.TryNappletFromDiscovery(id)
	return nil
}

func (p searchProvider) LaunchSearch(terms []string, _ uint32) *dbus.Error {
	if searchDebugEnabled() {
		log.Info().Str("search_method", "launch").Strs("terms", terms).Msg("GNOME search request")
	}
	p.onSearch(strings.Join(terms, " "))
	return nil
}

func searchDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VERDANA_SEARCH_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func logSearchQuery(method string, terms, previous, results []string) {
	if !searchDebugEnabled() {
		return
	}
	log.Info().Str("search_method", method).Strs("terms", terms).
		Strs("previous", previous).Strs("results", results).
		Msg("GNOME search request")
}

func searchNappletIDs(st backend.State, terms []string) []string {
	query := make([]string, 0, len(terms))
	for _, term := range terms {
		if term = strings.ToLower(strings.TrimSpace(term)); term != "" {
			query = append(query, term)
		}
	}
	if len(query) == 0 {
		return []string{}
	}

	type match struct {
		id        string
		name      string
		installed bool
	}
	installed := make(map[string]bool, len(st.Installed))
	for _, n := range st.Installed {
		installed[n.ID] = true
	}
	matches := make([]match, 0)
	seen := make(map[string]bool)
	for _, list := range [][]backend.Napp{st.Discovery, st.Installed} {
		for _, n := range list {
			if seen[n.ID] || !n.IsNapplet() || !matchesAllTerms(n, query) {
				continue
			}
			seen[n.ID] = true
			matches = append(matches, match{id: n.ID, name: strings.ToLower(n.Name), installed: installed[n.ID]})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		iPrefix := strings.HasPrefix(matches[i].name, query[0])
		jPrefix := strings.HasPrefix(matches[j].name, query[0])
		if iPrefix != jPrefix {
			return iPrefix
		}
		if matches[i].installed != matches[j].installed {
			return matches[i].installed
		}
		return matches[i].name < matches[j].name
	})
	if len(matches) > searchProviderLimit {
		matches = matches[:searchProviderLimit]
	}
	ids := make([]string, len(matches))
	for i := range matches {
		ids[i] = matches[i].id
	}
	return ids
}

func matchesAllTerms(n backend.Napp, terms []string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		n.Name,
		n.Description,
		n.Author.Hex(),
		n.AuthorName,
	}, "\n"))
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

func nappletsByID(st backend.State) map[string]backend.Napp {
	out := make(map[string]backend.Napp, len(st.Discovery)+len(st.Installed))
	for _, list := range [][]backend.Napp{st.Discovery, st.Installed} {
		for _, n := range list {
			if n.IsNapplet() {
				out[n.ID] = n
			}
		}
	}
	return out
}
