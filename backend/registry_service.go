package backend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"fiatjaf.com/nostr"
)

func ParseCanonicalServiceAddress(input string) (nostr.EntityPointer, error) {
	if len(input) == 0 || len(input) > 4096 {
		return nostr.EntityPointer{}, ErrServiceInvalidAddress
	}
	ptr, err := ParseNappAddress(input)
	if err != nil || fmt.Sprintf("%d:%s:%s", ptr.Kind, ptr.PublicKey.Hex(), ptr.Identifier) != input || (!addressable(ptr.Kind) && ptr.Identifier != "") {
		return nostr.EntityPointer{}, ErrServiceInvalidAddress
	}
	return ptr, nil
}

var (
	ErrServiceInvalidAddress = errors.New("invalid service address")
	ErrServiceNotFound       = errors.New("service address not found")
	ErrServiceBusy           = errors.New("service mutation busy")
	ErrServiceUnavailable    = errors.New("service mutation unavailable")
	ErrServiceNoUpdate       = errors.New("no update")
	ErrServiceTimeout        = errors.New("service mutation timeout")
	ErrServiceConflict       = errors.New("service mutation conflict")
	ErrServicePartialCleanup = errors.New("service uninstall partial cleanup")
)

type ServiceInstallResult struct {
	Address          string         `json:"address"`
	Outcome          string         `json:"outcome"`
	InstalledVersion ServiceVersion `json:"installed_version"`
}

type ServiceUpdateResult struct {
	Address          string         `json:"address"`
	Outcome          string         `json:"outcome"`
	PreviousVersion  ServiceVersion `json:"previous_version"`
	InstalledVersion ServiceVersion `json:"installed_version"`
}

type ServiceUninstallResult struct {
	Address         string         `json:"address"`
	Outcome         string         `json:"outcome,omitempty"`
	PreviousVersion ServiceVersion `json:"previous_version,omitempty"`
	RecordRemoved   bool           `json:"-"`
	CleanupComplete bool           `json:"cleanup_complete"`
}

func ServiceUninstall(ctx context.Context, address string) (ServiceUninstallResult, error) {
	if _, err := ParseCanonicalServiceAddress(address); err != nil {
		return ServiceUninstallResult{}, err
	}
	if ctx.Err() != nil {
		return ServiceUninstallResult{}, ErrServiceTimeout
	}
	stateMu.Lock()
	var id string
	for key, n := range state.InstalledNapps {
		if n.Address() == address {
			id = key
			break
		}
	}
	stateMu.Unlock()
	if id == "" {
		return ServiceUninstallResult{}, ErrServiceNotFound
	}
	result, err := uninstallNapp(id)
	if errors.Is(err, errNotInstalled) {
		return ServiceUninstallResult{}, ErrServiceNotFound
	}
	if errors.Is(err, ErrServicePartialCleanup) {
		return result, err
	}
	return result, serviceMutationError(ctx, err)
}

func serviceVersion(n Napp) ServiceVersion {
	return ServiceVersion{EventID: n.EventID, CreatedAt: int64(n.CreatedAt), ArtifactHash: n.ArtifactHash}
}

func serviceMutationError(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ErrServiceTimeout
	case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
		return ErrServiceTimeout
	case errors.Is(err, errBusy):
		return ErrServiceBusy
	case errors.Is(err, errOlderVersion):
		return ErrServiceConflict
	case errors.Is(err, errUnavailable):
		return ErrServiceUnavailable
	default:
		return ErrServiceUnavailable
	}
}

// ServiceInstall installs, or updates, the napplet at a canonical address.
// relays are optional hints from the address's naddr: only extra places to
// look for this one install. addressEvents asks just the public ws(s) ones
// (napExplicitRelay), and nothing stores them, since pickAddress builds the
// Napp from the event alone.
func ServiceInstall(ctx context.Context, address string, relays []string) (ServiceInstallResult, error) {
	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil {
		return ServiceInstallResult{}, err
	}
	ptr.Relays = slices.Clone(relays)
	n, err := resolveNappPointer(ctx, ptr)
	if err != nil {
		if ctx.Err() != nil {
			return ServiceInstallResult{}, ErrServiceTimeout
		}
		if errors.Is(err, ErrNappAddressNotFound) {
			return ServiceInstallResult{}, ErrServiceNotFound
		}
		return ServiceInstallResult{}, ErrServiceUnavailable
	}
	if n.Unavailable != "" {
		return ServiceInstallResult{}, ErrServiceUnavailable
	}
	result, err := InstallNappContext(ctx, n)
	return result, serviceMutationError(ctx, err)
}

func ServiceUpdate(ctx context.Context, address string) (ServiceUpdateResult, error) {
	if _, err := ParseCanonicalServiceAddress(address); err != nil {
		return ServiceUpdateResult{}, err
	}
	stateMu.Lock()
	var id string
	for key, n := range state.InstalledNapps {
		if n.Address() == address {
			id = key
			break
		}
	}
	stateMu.Unlock()
	if id == "" {
		return ServiceUpdateResult{}, ErrServiceNotFound
	}
	result, err := updateNappContext(ctx, id, address, true)
	if errors.Is(err, errNotInstalled) {
		return ServiceUpdateResult{}, ErrServiceNotFound
	}
	if errors.Is(err, ErrServiceNoUpdate) {
		return ServiceUpdateResult{}, ErrServiceNoUpdate
	}
	return result, serviceMutationError(ctx, err)
}

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
