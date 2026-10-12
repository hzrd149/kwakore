package backend

import (
	"context"
	"sort"
	"time"

	"fiatjaf.com/nostr"
)

// ProfileDetail is what a profile tab shows: whatever the launcher knows
// about a pubkey, without blocking the render loop to get it.
type ProfileDetail struct {
	Pubkey      string `json:"pubkey"`
	Npub        string `json:"npub"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	ShortName   string `json:"shortName"`
	About       string `json:"about"`
	Picture     string `json:"picture"`
	NIP05       string `json:"nip05"`
	Website     string `json:"website"`
}

// FetchProfileDetail returns what the sdk has cached or can fetch about a
// pubkey: name, about, picture, nip05 and friends. It blocks (with its own
// timeout), so call it off the render loop.
func FetchProfileDetail(pubkeyHex string) ProfileDetail {
	pk, err := nostr.PubKeyFromHex(pubkeyHex)
	if err != nil {
		return ProfileDetail{Pubkey: pubkeyHex}
	}
	if sys == nil {
		return ProfileDetail{Pubkey: pk.Hex()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pm := sys.FetchProfileMetadata(ctx, pk)
	indexUser(pm)
	return ProfileDetail{
		Pubkey:      pk.Hex(),
		Npub:        pm.Npub(),
		Name:        pm.Name,
		DisplayName: pm.DisplayName,
		ShortName:   pm.ShortName(),
		About:       pm.About,
		Picture:     pm.Picture,
		NIP05:       pm.NIP05,
		Website:     pm.Website,
	}
}

// FetchAuthorNapps lists the napps and napplets an author published: from
// the author's own write relays plus the launcher's discovery relays. Each
// address shows once, as its NIP-01 latest event (an unavailable entry when
// that event is invalid), so an author that republished the same napp shows
// up once. It blocks (with its own timeout), so call it off the render loop.
func FetchAuthorNapps(pubkeyHex string) []Napp {
	pk, err := nostr.PubKeyFromHex(pubkeyHex)
	if err != nil || sys == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	urls := sys.FetchWriteRelays(ctx, pk)
	for _, r := range Relays() {
		urls = nostr.AppendUnique(urls, r)
	}
	if len(urls) == 0 {
		return nil
	}

	filter := nostr.Filter{Kinds: napKinds, Authors: []nostr.PubKey{pk}}
	var events []nostr.Event
	for evt := range sys.Store.QueryEvents(filter, 200) {
		events = append(events, evt)
	}
	for re := range fetchRelays(ctx, urls, filter, "kwakore-author-napps").Events() {
		events = append(events, re.Event)
	}

	out := authorNapps(pk, events)
	// author names resolve in the background and are stamped here too
	for i := range out {
		if out[i].AuthorName == "" {
			out[i].AuthorName = out[i].AuthorShortName()
		}
	}
	return out
}

// authorNapps is one entry per address among pk's authentic manifest
// events, newest first: keyed by address, not d, because a napp and a
// napplet may share a d tag.
func authorNapps(pk nostr.PubKey, events []nostr.Event) []Napp {
	latest := latestByAddress{}
	for _, evt := range events {
		if evt.PubKey == pk {
			latest.add(evt)
		}
	}
	out := make([]Napp, 0, len(latest))
	for _, evt := range latest {
		out = append(out, nappFromLatest(evt))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ─── napplet.detail ──────────────────────────────────────────────

// ServiceDetailResult is the public detail of one napplet address: its
// descriptor plus install state and the versions known locally. It holds no
// path, icon, server or blob URL.
type ServiceDetailResult struct {
	ServiceDescriptor
	Kind             int              `json:"kind"`
	Installed        bool             `json:"installed"`
	InstalledVersion *ServiceVersion  `json:"installed_version"`
	UpdateAvailable  *ServiceVersion  `json:"update_available"`
	RequiredDomains  []string         `json:"required_domains"`
	OptionalDomains  []string         `json:"optional_domains"`
	Versions         []ServiceVersion `json:"versions"`
}

// serviceDetailVersions caps the versions a detail lists, and the manifest
// events read from the local store for it.
const serviceDetailVersions = 20

// ServiceDetail describes the napplet at a canonical address from local
// knowledge only, never the relays: the installed record, else the cached
// discovery catalog, else the newest valid manifest in the local event
// store.
func ServiceDetail(address string) (ServiceDetailResult, error) {
	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil {
		return ServiceDetailResult{}, err
	}

	var installed *Napp
	stateMu.Lock()
	for _, n := range state.InstalledNapps {
		if n.Address() == address {
			c := n
			installed = &c
			break
		}
	}
	stateMu.Unlock()

	var listed *Napp
	discoverMu.Lock()
	for _, n := range catalog {
		if n.Address() == address {
			c := n
			listed = &c
			break
		}
	}
	discoverMu.Unlock()

	stored := storedManifests(ptr)

	var chosen Napp
	switch {
	case installed != nil:
		chosen = *installed
	case listed != nil:
		chosen = *listed
	case len(stored) > 0:
		chosen = stored[0]
	default:
		return ServiceDetailResult{}, ErrServiceNotFound
	}

	result := ServiceDetailResult{
		ServiceDescriptor: serviceDescriptor(chosen),
		Kind:              int(ptr.Kind),
		RequiredDomains:   cloneDomains(chosen.RequiredDomains),
		OptionalDomains:   cloneDomains(chosen.OptionalDomains),
	}
	versions := make([]ServiceVersion, 0, 4+len(stored))
	if installed != nil {
		v := serviceVersion(*installed)
		result.Installed, result.InstalledVersion = true, &v
		result.UpdateAvailable = serviceUpdateVersion(*installed)
		versions = append(versions, v)
		if result.UpdateAvailable != nil {
			versions = append(versions, *result.UpdateAvailable)
		}
	}
	if listed != nil {
		versions = append(versions, serviceVersion(*listed))
	}
	for _, n := range stored {
		versions = append(versions, serviceVersion(n))
	}
	result.Versions = uniqueVersions(versions)
	return result, nil
}

func cloneDomains(domains []string) []string {
	if len(domains) == 0 {
		return []string{}
	}
	return append([]string(nil), domains...)
}

// uniqueVersions dedupes versions by event id and sorts them newest first,
// at most serviceDetailVersions of them. It is never nil.
func uniqueVersions(versions []ServiceVersion) []ServiceVersion {
	seen := make(map[string]bool, len(versions))
	out := make([]ServiceVersion, 0, len(versions))
	for _, v := range versions {
		if v.EventID == "" || seen[v.EventID] {
			continue
		}
		seen[v.EventID] = true
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].EventID < out[j].EventID
	})
	if len(out) > serviceDetailVersions {
		out = out[:serviceDetailVersions]
	}
	return out
}

// storedManifests is the valid manifests of ptr's address in the local
// event store, newest first. Each event must hash to its id, carry a valid
// signature and match the author, kind and d of the address.
func storedManifests(ptr nostr.EntityPointer) []Napp {
	if sys == nil {
		return nil
	}
	// The store answers a filter naming a replaceable kind with its latest
	// event alone, which may be the invalid one; the kind is therefore left
	// out of the filter and checked below.
	filter := nostr.Filter{Authors: []nostr.PubKey{ptr.PublicKey}}
	maxEvents := 10 * serviceDetailVersions
	if addressable(ptr.Kind) {
		filter.Tags = nostr.TagMap{"d": []string{ptr.Identifier}}
		maxEvents = serviceDetailVersions
	}
	var out []Napp
	for evt := range sys.Store.QueryEvents(filter, maxEvents) {
		if evt.Kind != ptr.Kind || evt.PubKey != ptr.PublicKey || !evt.CheckID() || !evt.VerifySignature() {
			continue
		}
		if addressable(ptr.Kind) && evt.Tags.GetD() != ptr.Identifier {
			continue
		}
		n := nappFromLatest(evt)
		if n.Unavailable != "" {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].EventID < out[j].EventID
	})
	if len(out) > serviceDetailVersions {
		out = out[:serviceDetailVersions]
	}
	return out
}

// LookupNapp finds a napp the launcher already knows — installed, discovered
// or dev — by id, so a detail tab can refresh its copy every frame.
func LookupNapp(id string) (Napp, bool) {
	if n, ok := InstalledNapp(id); ok {
		// stamped like Snapshot's entry: the saved record never carries
		// UpdateAvailable or Unavailable, and a napp page built from it
		// could never offer Update or say the latest version is invalid
		stampUpdateState(&n, updateSet.Load())
		return n, true
	}
	if n, ok := DiscoveredNapp(id); ok {
		return n, true
	}
	if d := devLookup(id); d != nil {
		return d.napp, true
	}
	return Napp{}, false
}
