//go:build linux

package daemon

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"kwakore/backend"
)

const (
	gnomeSearchBus       = "org.kwakore.SearchProvider"
	gnomeSearchInterface = "org.gnome.Shell.SearchProvider2"
	gnomeSearchPath      = dbus.ObjectPath("/org/kwakore/SearchProvider")
)

type gnomeSearchProvider struct{ service *Service }

func (s *Service) startGNOMESearch() {
	s.mu.Lock()
	if s.searchBus != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	provider := &gnomeSearchProvider{service: s}
	if err := conn.Export(provider, gnomeSearchPath, gnomeSearchInterface); err != nil {
		_ = conn.Close()
		return
	}
	reply, err := conn.RequestName(gnomeSearchBus, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return
	}
	s.mu.Lock()
	s.searchBus = conn
	s.mu.Unlock()
}

// StartSearchCatalogRefresh keeps discovery available to GNOME without
// delaying D-Bus queries on relay responses.
func (s *Service) StartSearchCatalogRefresh() {
	s.searchRefreshOnce.Do(func() { go s.refreshGNOMECatalog() })
}

// StartDesktopSearch reconciles provider files and starts the live D-Bus
// endpoint after the control socket is listening for activation.
func (s *Service) StartDesktopSearch() {
	if err := s.searchHost.SetGNOMESearchIntegration(s.manager.Effective().GNOMESearch); err != nil {
		s.recordError("gnome_search", "GNOME search provider registration failed")
	}
	s.startGNOMESearch()
	if s.manager.Effective().GNOMESearch {
		s.StartSearchCatalogRefresh()
	}
}

func (s *Service) refreshGNOMECatalog() {
	// A live query never waits on relays. Refresh in the background and keep
	// the last complete catalog available across individual search requests.
	for {
		if s.manager.Effective().GNOMESearch {
			ctx, cancel := context.WithTimeout(s.workContext, 30*time.Second)
			_ = backend.RefreshDiscovery(ctx)
			cancel()
		}
		select {
		case <-s.workContext.Done():
			return
		case <-time.After(15 * time.Minute):
		}
	}
}

func (p *gnomeSearchProvider) GetInitialResultSet(terms []string) ([]string, *dbus.Error) {
	return p.results(terms), nil
}

func (p *gnomeSearchProvider) GetSubsearchResultSet(_ []string, terms []string) ([]string, *dbus.Error) {
	return p.results(terms), nil
}

func (p *gnomeSearchProvider) results(terms []string) []string {
	if !p.service.manager.Effective().GNOMESearch {
		return []string{}
	}
	return searchNappletIDs(backend.SystemSearchCatalog(), terms)
}

func searchNappletIDs(items []backend.Napp, terms []string) []string {
	query := make([]string, 0, len(terms))
	for _, term := range terms {
		if s := strings.ToLower(strings.TrimSpace(term)); s != "" {
			query = append(query, s)
		}
	}
	if len(query) == 0 {
		return []string{}
	}
	type match struct {
		id, name  string
		installed bool
	}
	installed := map[string]bool{}
	for _, n := range backend.SystemSearchCatalog() {
		if _, ok := backend.InstalledNapp(n.ID); ok {
			installed[n.ID] = true
		}
	}
	matches := make([]match, 0)
	for _, n := range items {
		if !n.IsNapplet() {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{n.Name, n.Description, n.AuthorName, n.Author.Hex()}, "\n"))
		found := true
		for _, term := range query {
			if !strings.Contains(haystack, term) {
				found = false
				break
			}
		}
		if found {
			matches = append(matches, match{n.ID, strings.ToLower(n.Name), installed[n.ID]})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := strings.HasPrefix(matches[i].name, query[0]), strings.HasPrefix(matches[j].name, query[0])
		if a != b {
			return a
		}
		if matches[i].installed != matches[j].installed {
			return matches[i].installed
		}
		if matches[i].name != matches[j].name {
			return matches[i].name < matches[j].name
		}
		return matches[i].id < matches[j].id
	})
	if len(matches) > 20 {
		matches = matches[:20]
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.id
	}
	return ids
}

func (p *gnomeSearchProvider) GetResultMetas(ids []string) ([]map[string]dbus.Variant, *dbus.Error) {
	if !p.service.manager.Effective().GNOMESearch {
		return []map[string]dbus.Variant{}, nil
	}
	known := map[string]backend.Napp{}
	for _, n := range backend.SystemSearchCatalog() {
		known[n.ID] = n
	}
	out := make([]map[string]dbus.Variant, 0, len(ids))
	for _, id := range ids {
		n, ok := known[id]
		if !ok {
			continue
		}
		description := n.Description
		if n.AuthorName != "" {
			description += " — by " + n.AuthorName
		}
		out = append(out, map[string]dbus.Variant{
			"id": dbus.MakeVariant(id), "name": dbus.MakeVariant(n.Label()),
			"description": dbus.MakeVariant(description), "gicon": dbus.MakeVariant("application-x-executable"),
		})
	}
	return out, nil
}

func (p *gnomeSearchProvider) ActivateResult(id string, _ []string, _ uint32) *dbus.Error {
	if !p.service.manager.Effective().GNOMESearch {
		return nil
	}
	for _, n := range backend.SystemSearchCatalog() {
		if n.ID == id {
			go backend.TryNappletFromDiscovery(id)
			break
		}
	}
	return nil
}

func (p *gnomeSearchProvider) LaunchSearch(_ []string, _ uint32) *dbus.Error { return nil }
