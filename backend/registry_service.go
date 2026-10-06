package backend

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ServiceVersion is the public installed manifest version.
type ServiceVersion struct {
	EventID      string `json:"event_id"`
	CreatedAt    int64  `json:"created_at"`
	ArtifactHash string `json:"artifact_hash"`
}

// ServiceDescriptor contains only catalog fields safe for local clients.
type ServiceDescriptor struct {
	Address   string         `json:"address"`
	Name      string         `json:"name"`
	Format    string         `json:"format"`
	Available bool           `json:"available"`
	Version   ServiceVersion `json:"version"`
}

type ServicePage struct {
	Items      []ServiceDescriptor `json:"items"`
	Total      int                 `json:"total"`
	NextOffset *int                `json:"next_offset"`
}

var (
	ErrDiscoveryUnavailable = errors.New("discovery unavailable")
	ErrDiscoveryTimeout     = errors.New("discovery timeout")
	ErrDiscoveryConflict    = errors.New("discovery superseded")
)

type ServiceDiscoveryPage struct {
	ServicePage
	FetchedAt *time.Time `json:"fetched_at"`
	Complete  bool       `json:"complete"`
}

func ServiceDiscover(ctx context.Context, query string, refresh bool, offset, limit int) (ServiceDiscoveryPage, error) {
	if refresh {
		if err := RefreshDiscovery(ctx); err != nil {
			return ServiceDiscoveryPage{}, err
		}
	}
	discoverMu.Lock()
	items := append([]Napp(nil), catalog...)
	fetched := catalogFetched
	discoverMu.Unlock()
	result := ServiceDiscoveryPage{FetchedAt: fetched, Complete: fetched != nil}
	query = strings.ToLower(strings.TrimSpace(query))
	descriptors := make([]ServiceDescriptor, 0, len(items))
	for _, n := range items {
		if n.MatchesQuery(query) {
			descriptors = append(descriptors, serviceDescriptor(n))
		}
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Address < descriptors[j].Address })
	result.ServicePage = servicePage(descriptors, offset, limit)
	return result, nil
}

func ServiceInstalled(offset, limit int) ServicePage {
	stateMu.Lock()
	type storedDescriptor struct {
		key  string
		napp Napp
	}
	stored := make([]storedDescriptor, 0, len(state.InstalledNapps))
	for key, n := range state.InstalledNapps {
		stored = append(stored, storedDescriptor{key: key, napp: n})
	}
	stateMu.Unlock()
	type sortedDescriptor struct {
		key        string
		descriptor ServiceDescriptor
	}
	sorted := make([]sortedDescriptor, len(stored))
	for i, row := range stored {
		sorted[i] = sortedDescriptor{key: row.key, descriptor: serviceDescriptor(row.napp)}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].descriptor.Address == sorted[j].descriptor.Address {
			return sorted[i].key < sorted[j].key
		}
		return sorted[i].descriptor.Address < sorted[j].descriptor.Address
	})
	items := make([]ServiceDescriptor, len(sorted))
	for i, row := range sorted {
		items[i] = row.descriptor
	}
	return servicePage(items, offset, limit)
}

func serviceDescriptor(n Napp) ServiceDescriptor {
	format := "napp"
	if n.IsNapplet() {
		format = FormatNapplet
	}
	name := make([]rune, 0, 256)
	for _, r := range n.Name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		name = append(name, r)
		if len(name) == 256 {
			break
		}
	}
	return ServiceDescriptor{
		Address: n.Address(), Name: string(name), Format: format,
		Available: n.Unavailable == "",
		Version:   ServiceVersion{EventID: n.EventID, CreatedAt: int64(n.CreatedAt), ArtifactHash: n.ArtifactHash},
	}
}

func servicePage(items []ServiceDescriptor, offset, limit int) ServicePage {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	result := ServicePage{Items: []ServiceDescriptor{}, Total: len(items)}
	if offset >= len(items) {
		return result
	}
	end := offset + limit
	if end < offset || end > len(items) {
		end = len(items)
	}
	result.Items = items[offset:end]
	if end < len(items) {
		result.NextOffset = &end
	}
	return result
}
