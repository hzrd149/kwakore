package backend

import (
	"context"
	"errors"
	"sort"
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
	return ServiceDiscoveryPage{ServicePage: ServicePage{Items: []ServiceDescriptor{}}}, nil
}

func ServiceInstalled(offset, limit int) ServicePage {
	stateMu.Lock()
	items := make([]ServiceDescriptor, 0, len(state.InstalledNapps))
	for _, n := range state.InstalledNapps {
		items = append(items, serviceDescriptor(n))
	}
	stateMu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].Address < items[j].Address })
	return servicePage(items, offset, limit)
}

func serviceDescriptor(n Napp) ServiceDescriptor {
	format := n.Format
	if format == "" {
		format = "napp"
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
