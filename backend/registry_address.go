package backend

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

// Napps and napplets are addressable: an naddr (or its "<kind>:<pubkey>:<d>"
// coordinate) names one for good, across every update. The launcher can take
// one from the discovery filter, a startup argument or a nostr: link, look it
// up on relays and install or open it.

// naddrPattern finds an naddr inside whatever was pasted: a bare code, a
// nostr: URI or a web link that carries one in its path.
var naddrPattern = regexp.MustCompile(`naddr1[02-9ac-hj-np-z]{20,}`)

// ParseNappAddress reads a napp or napplet address: an naddr, a nostr: URI
// or web link containing one, or a "<kind>:<pubkey hex>:<d>" coordinate. The
// kind must be one the launcher runs.
func ParseNappAddress(input string) (nostr.EntityPointer, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nostr.EntityPointer{}, errors.New("empty address")
	}

	var ptr nostr.EntityPointer
	if code := naddrPattern.FindString(strings.ToLower(s)); code != "" {
		prefix, data, err := nip19.Decode(code)
		if err != nil || prefix != "naddr" {
			return nostr.EntityPointer{}, fmt.Errorf("invalid naddr: %w", err)
		}
		ptr = data.(nostr.EntityPointer)
	} else {
		p, err := nostr.ParseAddrString(strings.TrimPrefix(s, "nostr:"))
		if err != nil {
			return nostr.EntityPointer{}, errors.New("not a napp address")
		}
		ptr = p
	}

	if !isNapKind(ptr.Kind) {
		return nostr.EntityPointer{}, fmt.Errorf("kind %d is not a napp or napplet", ptr.Kind)
	}
	if addressable(ptr.Kind) && ptr.Identifier == "" {
		return nostr.EntityPointer{}, errors.New("address has no d tag")
	}
	return ptr, nil
}

// IsNappAddress says whether input reads as an address, for a UI deciding
// between filtering its list and looking something up.
func IsNappAddress(input string) bool {
	_, err := ParseNappAddress(input)
	return err == nil
}

// addressFilter is the query for the current manifest at ptr. A root
// napplet (kind 15129) is replaceable, so it has no d tag to match.
func addressFilter(ptr nostr.EntityPointer) nostr.Filter {
	f := nostr.Filter{Kinds: []nostr.Kind{ptr.Kind}, Authors: []nostr.PubKey{ptr.PublicKey}}
	if addressable(ptr.Kind) {
		f.Tags = nostr.TagMap{"d": []string{ptr.Identifier}}
	}
	return f
}

// matchesAddress says whether a napp is the one ptr names.
func (n Napp) matchesAddress(ptr nostr.EntityPointer) bool {
	if n.Author != ptr.PublicKey || n.ManifestKind() != ptr.Kind {
		return false
	}
	return !addressable(ptr.Kind) || n.D == ptr.Identifier
}

// Naddr is the napp's address as an naddr, for sharing it.
func (n Napp) Naddr() string {
	if n.Author == (nostr.PubKey{}) {
		return ""
	}
	return nip19.EncodeNaddr(n.Author, n.ManifestKind(), n.D, nil)
}

// resolveTimeout bounds one address lookup across every relay asked.
const resolveTimeout = 15 * time.Second

// ResolveNappAddress finds the current manifest at an address, its NIP-01
// latest event (registry_select.go), in the local store, on the address's
// relay hints, the author's write relays and the launcher's relays. When
// that event is invalid the result is an unavailable entry (Unavailable
// set), never an older valid version. Blocking.
func ResolveNappAddress(ctx context.Context, input string) (Napp, error) {
	ptr, err := ParseNappAddress(input)
	if err != nil {
		return Napp{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	events, err := addressEvents(ctx, ptr)
	if err != nil {
		return Napp{}, err
	}
	best, found := pickAddress(ptr, events)
	if !found {
		return Napp{}, errors.New("no napp or napplet found at that address")
	}
	if best.AuthorName == "" {
		best.AuthorName = best.AuthorShortName()
	}
	return best, nil
}

// addressEvents gathers the manifest events at ptr from the local store and
// the relays. It is a variable so tests can stand in for the relays.
var addressEvents = func(ctx context.Context, ptr nostr.EntityPointer) ([]nostr.Event, error) {
	if sys == nil {
		return nil, errors.New("not started")
	}
	var events []nostr.Event
	filter := addressFilter(ptr)
	for evt := range sys.Store.QueryEvents(filter, 10) {
		events = append(events, evt)
	}

	// relay hints are only a place to look, and only public ws(s) ones are
	// asked: a pasted link mustn't make the launcher dial into the network
	// it sits on
	urls := []string{}
	for _, r := range ptr.Relays {
		if u, err := napExplicitRelay(ctx, r); err == nil {
			urls = nostr.AppendUnique(urls, u)
		}
	}
	for _, r := range sys.FetchWriteRelays(ctx, ptr.PublicKey) {
		urls = nostr.AppendUnique(urls, r)
	}
	for _, r := range Relays() {
		urls = nostr.AppendUnique(urls, r)
	}
	if len(urls) > 0 {
		for re := range sys.Pool.FetchMany(ctx, urls, filter, nostr.SubscriptionOptions{Label: "verdana-address"}) {
			events = append(events, re.Event)
		}
	}
	return events, nil
}

// pickAddress is the NIP-01 winner among the authentic events for the
// address ptr names: events by another author, of another kind or (for an
// addressable kind) with another d are ignored, whatever a relay sent. found
// is false when nothing authentic matched; an invalid winner is found, as an
// unavailable entry.
func pickAddress(ptr nostr.EntityPointer, events []nostr.Event) (Napp, bool) {
	latest := latestByAddress{}
	for _, evt := range events {
		if evt.PubKey != ptr.PublicKey || evt.Kind != ptr.Kind ||
			(addressable(ptr.Kind) && evt.Tags.GetD() != ptr.Identifier) {
			continue
		}
		latest.add(evt)
	}
	// every event left has the one address, so there is at most one winner
	for _, evt := range latest {
		return nappFromLatest(evt), true
	}
	return Napp{}, false
}

// ─── the launcher's lookup ───────────────────────────────────────

// AddressLookup is the state of the last address typed into the discovery
// filter: still being looked up, found (NappID, also listed in Discovery) or
// failed (Err).
type AddressLookup struct {
	Query   string `json:"query"`
	Pending bool   `json:"pending"`
	NappID  string `json:"nappId,omitempty"`
	Err     string `json:"err,omitempty"`
}

// LookupAddress starts looking up an address typed into the discovery
// filter, if it is one and isn't already being (or been) looked up. The
// result shows up in State.Lookup, and the napp itself in State.Discovery,
// where the usual install and open buttons apply to it. Cheap to call on
// every keystroke.
func LookupAddress(input string) {
	q := strings.TrimSpace(input)
	if !IsNappAddress(q) {
		ls.mu.Lock()
		changed := ls.lookup != nil
		ls.lookup = nil
		ls.mu.Unlock()
		if changed {
			notifyState()
		}
		return
	}

	ls.mu.Lock()
	if ls.lookup != nil && ls.lookup.Query == q {
		ls.mu.Unlock()
		return
	}
	ls.lookup = &AddressLookup{Query: q, Pending: true}
	ls.mu.Unlock()
	notifyState()

	go func() {
		n, err := ResolveNappAddress(context.Background(), q)
		if err == nil {
			rememberResolved(n)
		}
		ls.mu.Lock()
		if ls.lookup == nil || ls.lookup.Query != q {
			// the filter moved on while we were looking
			ls.mu.Unlock()
			return
		}
		ls.lookup = &AddressLookup{Query: q}
		if err != nil {
			ls.lookup.Err = err.Error()
		} else {
			ls.lookup.NappID = n.ID
		}
		ls.mu.Unlock()
		notifyState()
	}()
}

// rememberResolved lists a napp found by its address among the discovered
// ones (where installing and the detail view look napps up), and keeps it
// there across discovery refreshes.
func rememberResolved(n Napp) {
	ls.mu.Lock()
	if ls.resolved == nil {
		ls.resolved = make(map[string]Napp)
	}
	if prev, ok := ls.resolved[n.ID]; !ok || nappNewer(n, prev) {
		ls.resolved[n.ID] = n
	}
	ls.discovery = ls.withResolved(ls.discovery)
	ls.sortDiscovery()
	ls.mu.Unlock()
	notifyState()
}

// withResolved adds the napps found by address to a discovery list, or
// swaps in the resolved copy where it wins over the listed one by the NIP-01
// order (nappNewer).
func (l *launcherState) withResolved(list []Napp) []Napp {
	if len(l.resolved) == 0 {
		return list
	}
	out := append([]Napp(nil), list...)
	seen := make(map[string]int, len(out))
	for i, n := range out {
		seen[n.ID] = i
	}
	for _, n := range l.resolved {
		if i, ok := seen[n.ID]; ok {
			if nappNewer(n, out[i]) {
				out[i] = n
			}
			continue
		}
		out = append(out, n)
	}
	return out
}

// ─── opening ─────────────────────────────────────────────────────

// OpenAddress opens the napp or napplet at an address: it is launched right
// away when installed, and otherwise looked up and, once the user agrees,
// installed and launched. Blocking; meant for links and startup arguments,
// which come from outside the launcher, hence the question before
// installing anything.
func OpenAddress(input string) error {
	ptr, err := ParseNappAddress(input)
	if err != nil {
		return err
	}
	// a link that cold-started the launcher waits for its login, so the
	// window it opens can already sign (see waitStartupLogin)
	ctx, cancel := context.WithTimeout(context.Background(), shortcutActionTimeout)
	waitStartupLogin(ctx)
	cancel()

	if n, ok := installedAt(ptr); ok {
		Launch(n)
		return nil
	}

	n, err := ResolveNappAddress(context.Background(), input)
	if err != nil {
		return err
	}
	return openResolved(n)
}

// openResolved is OpenAddress once the address resolved and nothing is
// installed there: list it, then ask, install and launch. An unavailable
// entry is listed, so the store can say why, but never offered.
func openResolved(n Napp) error {
	rememberResolved(n)
	if n.Unavailable != "" {
		return errUnavailable
	}
	if !askInstall(n) {
		// a no is an answer, not a failure
		return nil
	}
	if err := InstallNapp(n); err != nil {
		return err
	}
	Launch(n)
	return nil
}

// InstallAddress looks an address up and installs (or updates) what it
// names, without asking: the caller is the user acting in the launcher.
// Blocking.
func InstallAddress(input string) (Napp, error) {
	n, err := ResolveNappAddress(context.Background(), input)
	if err != nil {
		return Napp{}, err
	}
	return installResolved(n)
}

// installResolved is InstallAddress once the address resolved: list it and
// install it, unless it is unavailable.
func installResolved(n Napp) (Napp, error) {
	rememberResolved(n)
	if n.Unavailable != "" {
		return Napp{}, errUnavailable
	}
	if err := InstallNapp(n); err != nil {
		return Napp{}, err
	}
	return n, nil
}

// installedAt is the installed napp at an address, if there is one.
func installedAt(ptr nostr.EntityPointer) (Napp, bool) {
	for _, n := range installedNapps() {
		if n.matchesAddress(ptr) {
			return n, true
		}
	}
	return Napp{}, false
}

// askInstall asks the user whether a napp that came in by address may be
// installed. Never remembered: each link is its own question.
func askInstall(n Napp) bool {
	what := "napp"
	if n.IsNapplet() {
		what = "napplet"
	}
	detail := "By " + n.AuthorShortName() + "."
	if n.Description != "" {
		detail = n.Description + "\n\n" + detail
	}
	p := newPrompt("Verdana", "Install and open the "+what+" "+n.Label()+"?", detail, n.Naddr(), nil)
	log.Info().Str("napp", n.ID).Msg("asking to install a napp opened by address")
	enqueuePrompt(p)
	return p.wait().OK
}
