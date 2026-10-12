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
	"unicode/utf8"

	"fiatjaf.com/nostr"
)

// ParseCanonicalServiceAddress reads the one spelling the control protocol
// accepts: "<kind>:<pubkey hex>:<identifier>" exactly as Napp.Address
// writes it. The identifier is the d tag for 35130 and 35129, empty for a
// root napplet (15129), and the 64 lowercase hex event id for a snapshot
// (5129). desktopentry.CanonicalAddress is the CLI's copy of this rule.
func ParseCanonicalServiceAddress(input string) (nostr.EntityPointer, error) {
	if len(input) == 0 || len(input) > 4096 {
		return nostr.EntityPointer{}, ErrServiceInvalidAddress
	}
	ptr, err := ParseNappAddress(input)
	if err != nil || fmt.Sprintf("%d:%s:%s", ptr.Kind, ptr.PublicKey.Hex(), ptr.Identifier) != input {
		return nostr.EntityPointer{}, ErrServiceInvalidAddress
	}
	switch ptr.Kind {
	case KindRootNapplet:
		if ptr.Identifier != "" {
			return nostr.EntityPointer{}, ErrServiceInvalidAddress
		}
	case KindSnapshotNapplet:
		if !hex64.MatchString(ptr.Identifier) {
			return nostr.EntityPointer{}, ErrServiceInvalidAddress
		}
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
	case errors.Is(err, ErrServiceNotFound), errors.Is(err, ErrServiceInvalidAddress), errors.Is(err, ErrServiceConflict), errors.Is(err, ErrServiceTimeout), errors.Is(err, ErrServiceBusy):
		return err
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
	if current, ok := InstalledNapp(n.ID); ok && current.Pinned {
		r, err := selectResolvedContext(ctx, n, true, nil, false)
		return ServiceInstallResult{Address: r.Address, Outcome: r.Outcome, InstalledVersion: r.InstalledVersion}, serviceMutationError(ctx, err)
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
// The manifest fields after Version are bounded and sanitized: tokens are
// checked against their grammar, intent ids holding control or format
// runes are dropped rather than altered, every list is capped, and the
// icon appears only as its hash. Every list is non-nil, so it is [] on the
// wire.
type ServiceDescriptor struct {
	Address           string         `json:"address"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Author            string         `json:"author"`
	AuthorName        string         `json:"author_name,omitempty"`
	Format            string         `json:"format"`
	Available         bool           `json:"available"`
	UnavailableReason string         `json:"unavailable_reason,omitempty"`
	Sources           []string       `json:"sources,omitempty"`
	Version           ServiceVersion `json:"version"`
	Pinned            bool           `json:"pinned"`

	Kind            int             `json:"kind"`
	Schema          string          `json:"schema"`
	Icon            *ServiceIconRef `json:"icon"`
	Archetypes      []string        `json:"archetypes"`
	Intents         []ServiceIntent `json:"intents"`
	RequiredDomains []string        `json:"required_domains"`
	OptionalDomains []string        `json:"optional_domains"`
	MissingDomains  []string        `json:"missing_domains"`
	Compatible      bool            `json:"compatible"`
	Lineage         *ServiceLineage `json:"lineage"`
}

// ServiceIconRef is a manifest's icon tag: its hash and declared type, never a
// URL. napplet.icon serves the verified bytes.
type ServiceIconRef struct {
	Sha256 string `json:"sha256"`
	Mime   string `json:"mime"`
}

// ServiceIntent is one advertised intent and its parameter names.
type ServiceIntent struct {
	ID     string   `json:"id"`
	Params []string `json:"params"`
}

// ServiceLineage is a snapshot's a/A provenance: canonical 35129/15129
// addresses, never resolved or trusted. A missing one is null.
type ServiceLineage struct {
	Parent *string `json:"parent"`
	Root   *string `json:"root"`
}

// Descriptor bounds.
const (
	maxDescriptorTokens     = 32 // archetypes
	maxDescriptorIntents    = 64
	maxDescriptorParams     = 16
	maxDescriptorDomains    = 64
	maxDescriptorTokenBytes = 64
	maxDescriptorIntentID   = 256
)

// wireSchema maps the internal manifest schema to its wire label. The
// internal values are stored in state.json and never change: SchemaWebNapplet
// ("web-napplet") is the current NIP-5D manifest, shown as "nip5d", and
// SchemaNIP5D ("nip5d") the legacy path-tag manifest, shown as "legacy".
func wireSchema(n Napp) string {
	switch {
	case !n.IsNapplet():
		return "napp"
	case n.NappletSchema == SchemaNIP5D:
		return "legacy"
	default:
		return "nip5d"
	}
}

// descriptorTokens keeps the members of list that are domain tokens of at
// most maxDescriptorTokenBytes, in order, at most limit of them.
func descriptorTokens(list []string, limit int) []string {
	out := make([]string, 0, min(len(list), limit))
	for _, s := range list {
		if len(out) == limit {
			break
		}
		if len(s) <= maxDescriptorTokenBytes && domainToken.MatchString(s) {
			out = append(out, s)
		}
	}
	return out
}

// descriptorIntents keeps the conventions whose id is short, valid UTF-8
// and free of control and format runes, with their well-formed params.
func descriptorIntents(list []NappletConvention) []ServiceIntent {
	out := make([]ServiceIntent, 0, min(len(list), maxDescriptorIntents))
	for _, c := range list {
		if len(out) == maxDescriptorIntents {
			break
		}
		if c.ID == "" || len(c.ID) > maxDescriptorIntentID || !utf8.ValidString(c.ID) ||
			serviceDisplayText(c.ID, maxDescriptorIntentID) != c.ID {
			continue
		}
		params := make([]string, 0, min(len(c.Params), maxDescriptorParams))
		for _, p := range c.Params {
			if len(params) == maxDescriptorParams {
				break
			}
			if len(p) <= maxDescriptorTokenBytes && conventionParam.MatchString(p) {
				params = append(params, p)
			}
		}
		out = append(out, ServiceIntent{ID: c.ID, Params: params})
	}
	return out
}

// descriptorLineage is a snapshot's lineage, each value checked again.
func descriptorLineage(n Napp) *ServiceLineage {
	if !n.IsSnapshot() {
		return nil
	}
	var l ServiceLineage
	if n.Parent != "" && validLineageAddress(n.Parent) {
		p := n.Parent
		l.Parent = &p
	}
	if n.Root != "" && validLineageAddress(n.Root) {
		r := n.Root
		l.Root = &r
	}
	if l.Parent == nil && l.Root == nil {
		return nil
	}
	return &l
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

// ServiceDiscoverFilter narrows napplet.discover. Query is the
// case-insensitive search; every other field set is an exact match on the
// built descriptor, and all of them must hold: Archetype a member of
// archetypes, Intent an intents[].id, Domain a required or optional domain,
// Compatible equal to compatible, and Kind (when non-zero) equal to kind.
type ServiceDiscoverFilter struct {
	Query      string
	Archetype  string
	Intent     string
	Domain     string
	Compatible *bool
	Kind       int
}

// match says whether d passes every set field but Query.
func (f ServiceDiscoverFilter) match(d ServiceDescriptor) bool {
	if f.Archetype != "" && !slices.Contains(d.Archetypes, f.Archetype) {
		return false
	}
	if f.Intent != "" && !slices.ContainsFunc(d.Intents, func(i ServiceIntent) bool { return i.ID == f.Intent }) {
		return false
	}
	if f.Domain != "" && !slices.Contains(d.RequiredDomains, f.Domain) && !slices.Contains(d.OptionalDomains, f.Domain) {
		return false
	}
	if f.Compatible != nil && d.Compatible != *f.Compatible {
		return false
	}
	return f.Kind == 0 || d.Kind == f.Kind
}

func ServiceDiscover(ctx context.Context, filter ServiceDiscoverFilter, refresh bool, offset, limit int) (ServiceDiscoveryPage, error) {
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
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	descriptors := make([]ServiceDescriptor, 0, len(items))
	for _, n := range items {
		if !n.MatchesQuery(query) {
			continue
		}
		if d := serviceDescriptor(n); filter.match(d) {
			descriptors = append(descriptors, d)
		}
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Address < descriptors[j].Address })
	result.ServicePage = servicePage(descriptors, offset, limit)
	return result, nil
}

// ServiceInstalledItem is an installed napplet's descriptor plus the newer
// valid version the last update check found for it. UpdateAvailable is
// always present on the wire, as a Version or null.
type ServiceInstalledItem struct {
	ServiceDescriptor
	UpdateAvailable *ServiceVersion `json:"update_available"`
}

type ServiceInstalledPage struct {
	Items      []ServiceInstalledItem `json:"items"`
	Total      int                    `json:"total"`
	NextOffset *int                   `json:"next_offset"`
}

func ServiceInstalled(offset, limit int) ServiceInstalledPage {
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
	type sortedItem struct {
		key  string
		item ServiceInstalledItem
	}
	sorted := make([]sortedItem, len(stored))
	for i, row := range stored {
		// the descriptor comes from the unstamped record, so available keeps
		// its meaning; the update entry is read from a stamped copy
		sorted[i] = sortedItem{key: row.key, item: ServiceInstalledItem{ServiceDescriptor: serviceDescriptor(row.napp), UpdateAvailable: serviceUpdateVersion(row.napp)}}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].item.Address == sorted[j].item.Address {
			return sorted[i].key < sorted[j].key
		}
		return sorted[i].item.Address < sorted[j].item.Address
	})
	items := make([]ServiceInstalledItem, len(sorted))
	for i, row := range sorted {
		items[i] = row.item
	}
	page, total, next := pageOf(items, offset, limit)
	return ServiceInstalledPage{Items: page, Total: total, NextOffset: next}
}

func serviceDescriptor(n Napp) ServiceDescriptor {
	format := "napp"
	if n.IsNapplet() {
		format = FormatNapplet
	}
	authorName := ""
	if sys != nil && n.Author != nostr.ZeroPK {
		if metadata, ok := sys.MetadataCache.Get(n.Author); ok {
			authorName = metadata.ShortName()
		}
	}
	var icon *ServiceIconRef
	if hasNappletIcon(n) {
		icon = &ServiceIconRef{Sha256: n.IconSha, Mime: n.IconMime}
	}
	// compatible reads the unfiltered list: a missing domain that is not
	// shown is still missing
	rawMissing := n.MissingDomains()
	missing := descriptorTokens(rawMissing, maxDescriptorDomains)
	return ServiceDescriptor{
		Address: n.Address(), Name: serviceDisplayText(n.Name, 256), Description: serviceDisplayText(n.Description, 1024),
		Author: n.Author.Hex(), AuthorName: serviceDisplayText(authorName, 128), Format: format,
		Available: n.Unavailable == "", UnavailableReason: publicUnavailableReason(n.Unavailable),
		Sources: append([]string(nil), n.Sources...),
		Version: ServiceVersion{EventID: n.EventID, CreatedAt: int64(n.CreatedAt), ArtifactHash: n.ArtifactHash},
		Pinned:  n.Pinned || n.IsSnapshot(),

		Kind:            int(n.ManifestKind()),
		Schema:          wireSchema(n),
		Icon:            icon,
		Archetypes:      descriptorTokens(n.Roles, maxDescriptorTokens),
		Intents:         descriptorIntents(n.Conventions),
		RequiredDomains: descriptorTokens(n.RequiredDomains, maxDescriptorDomains),
		OptionalDomains: descriptorTokens(n.OptionalDomains, maxDescriptorDomains),
		MissingDomains:  missing,
		Compatible:      len(rawMissing) == 0,
		Lineage:         descriptorLineage(n),
	}
}

func publicUnavailableReason(reason string) string {
	switch reason {
	case "":
		return ""
	case reasonFileList, reasonHashes, reasonRequiredTags, reasonConventions, reasonManifest:
		return reason
	default:
		return reasonManifest
	}
}

func serviceDisplayText(raw string, limit int) string {
	var b strings.Builder
	count := 0
	for _, r := range raw {
		if count >= limit {
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

func servicePage(items []ServiceDescriptor, offset, limit int) ServicePage {
	page, total, next := pageOf(items, offset, limit)
	return ServicePage{Items: page, Total: total, NextOffset: next}
}

// pageOf is one page of sorted items: never nil, with the offset of the next
// page when more remain.
func pageOf[T any](items []T, offset, limit int) ([]T, int, *int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	if offset >= len(items) {
		return []T{}, len(items), nil
	}
	end := offset + limit
	if end < offset || end > len(items) {
		end = len(items)
	}
	var next *int
	if end < len(items) {
		next = &end
	}
	return items[offset:end], len(items), next
}
