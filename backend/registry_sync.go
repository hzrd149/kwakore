package backend

// The installed-list sync keeps one list of installed napplets, with their
// pinned versions, across the user's machines. Every machine whose
// configuration sets the same sync_id reads and writes one NIP-78 event
// (kind 30078) signed by the service signer: its only tag is d = sync_id,
// and its content is the list, NIP-44 encrypted to the user's own key, so
// relays learn neither which napplets the user runs nor how many (D-encrypt).
//
// A pass is a three-way merge. The base is the list this machine last agreed
// on with the relays (state.Sync.Base), local is what is installed here and
// remote is the list on the relays. An address dropped on either side since
// the base is dropped (D-mirror: a remote removal uninstalls the napplet here
// too, and a removal beats a concurrent update). Where both sides have an
// address, the newer pin wins, and a pin whose age is unknown never beats
// the local one, so nothing is ever downgraded (D-no-downgrade): a newer
// local version is kept and republished. With no base (first enable) the
// merge is a union and removes nothing.
//
// What is persisted, beside the base, is the last remote list known to be on
// the relays and its event: an unchanged remote event is not decrypted again,
// a stale relay copy is ignored, and a missing event is never read as
// "everything was removed". The base leaves out installs that failed and
// keeps uninstalls that failed, so the next pass retries both instead of
// reversing them. Content that cannot be decrypted or parsed is left alone:
// nothing is applied, published or saved because of it.
//
// Nothing a pass does reaches the control protocol; failures go to the
// journal only. The worker and its triggers are in registry_sync_loop.go.

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"fiatjaf.com/nostr"
)

const (
	// installSyncKind is the NIP-78 application-specific data kind.
	installSyncKind = nostr.KindApplicationSpecificData
	// installSyncSignerTimeout bounds every encrypt, decrypt and sign call:
	// an offline bunker must not hold a pass, or the daemon's shutdown,
	// forever (the revocable keyer waits for calls in flight).
	installSyncSignerTimeout = 20 * time.Second
	// installSyncMaxEntries caps a decoded list.
	installSyncMaxEntries = 1000
	// installSyncLookupTimeout bounds relay-list and pin lookups.
	installSyncLookupTimeout = 15 * time.Second
	// nip44MaxPlaintext is the largest plaintext NIP-44 v2 encrypts.
	nip44MaxPlaintext = 65535
)

// syncEntry is one napplet in the encrypted list.
type syncEntry struct {
	Address string `json:"address"`
	EventID string `json:"event_id"`
}

// syncList is the plaintext of the encrypted list.
type syncList struct {
	V        int         `json:"v"`
	Napplets []syncEntry `json:"napplets"`
}

// syncPin is one address's pinned version. CreatedAt 0 means unknown.
type syncPin struct {
	EventID   string
	CreatedAt nostr.Timestamp
}

// installSyncState is what a pass persists in state.json. The base resets
// when the sync_id or the signed-in account changes.
type installSyncState struct {
	ID        string          `json:"id"`
	Pubkey    string          `json:"pubkey"`
	EventID   string          `json:"event_id,omitempty"`
	CreatedAt nostr.Timestamp `json:"created_at,omitempty"`
	// Pending is set when the last publish reached no relay, so the next
	// pass publishes again.
	Pending bool `json:"pending,omitempty"`
	// Remote is the last list known to be on the relays.
	Remote []syncEntry `json:"remote,omitempty"`
	// Base is the last list this machine agreed on.
	Base []syncEntry `json:"base,omitempty"`
}

var (
	errSyncPinNotFound = errors.New("synced napplet version not found")
	errSyncPinMismatch = errors.New("synced napplet version does not match its pin")
	errSyncListInvalid = errors.New("invalid synced napplet list")
)

// ─── seams ───────────────────────────────────────────────────────

// Tests replace these, so they never resolve real relay lists or download.
var (
	installSyncID = func() string {
		if serviceConfig == nil {
			return ""
		}
		return serviceConfig.Effective().SyncID
	}

	// installSyncRelays are where the list is read and published: the
	// user's write relays, else the configured relays.
	installSyncRelays = func(ctx context.Context, pk nostr.PubKey) []string {
		if sys == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(ctx, installSyncLookupTimeout)
		defer cancel()
		if urls := sys.FetchWriteRelays(ctx, pk); len(urls) > 0 {
			return urls
		}
		return Relays()
	}

	fetchInstallSyncEvent = func(ctx context.Context, pk nostr.PubKey, id string) *nostr.Event {
		if sys == nil {
			return nil
		}
		return fetchLatestOn(ctx, installSyncRelays(ctx, pk), installSyncFilter(pk, id))
	}

	// fetchSyncedPin finds the manifest event with the pinned id: the local
	// store first, then the author's write relays and the configured ones.
	fetchSyncedPin = func(ctx context.Context, ptr nostr.EntityPointer, id nostr.ID) *nostr.Event {
		if sys == nil {
			return nil
		}
		for evt := range sys.Store.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			if evt.ID == id {
				return &evt
			}
		}
		ctx, cancel := context.WithTimeout(ctx, installSyncLookupTimeout)
		defer cancel()
		urls := sys.FetchWriteRelays(ctx, ptr.PublicKey)
		for _, r := range Relays() {
			urls = nostr.AppendUnique(urls, r)
		}
		for re := range fetchRelays(ctx, urls, nostr.Filter{IDs: []nostr.ID{id}}, "kwakore-sync-pin").Events() {
			if re.Event.ID == id {
				evt := re.Event
				return &evt
			}
		}
		return nil
	}

	syncInstallNapp    = InstallNappContext
	syncServiceInstall = ServiceInstall
	installSyncPin     = installSyncedPin
	// installSyncRemove uninstalls an address; one that is already gone, or
	// whose record went with only its file cleanup left over, is done.
	installSyncRemove = func(ctx context.Context, address string) error {
		_, err := ServiceUninstall(ctx, address)
		if errors.Is(err, ErrServiceNotFound) || errors.Is(err, ErrServicePartialCleanup) {
			return nil
		}
		return err
	}
)

func installSyncFilter(pk nostr.PubKey, id string) nostr.Filter {
	return nostr.Filter{
		Kinds:   []nostr.Kind{installSyncKind},
		Authors: []nostr.PubKey{pk},
		Tags:    nostr.TagMap{"d": []string{id}},
	}
}

// ─── kicks, the applying guard, the last publish ─────────────────

// installSyncKick asks the worker for a pass. One buffered slot: kicks that
// arrive while one is pending fold into it.
var installSyncKick = make(chan struct{}, 1)

// kickInstallSync never blocks: ServiceSigner calls it with its mutex held.
func kickInstallSync() {
	select {
	case installSyncKick <- struct{}{}:
	default:
	}
}

// The addresses a pass is installing or uninstalling right now. Their
// local-change notifications are the sync's own and schedule no pass.
var (
	installSyncApplyingMu sync.Mutex
	installSyncApplying   = map[string]int{}
)

func markInstallSyncApplying(address string) {
	installSyncApplyingMu.Lock()
	installSyncApplying[address]++
	installSyncApplyingMu.Unlock()
}

func doneInstallSyncApplying(address string) {
	installSyncApplyingMu.Lock()
	if installSyncApplying[address] <= 1 {
		delete(installSyncApplying, address)
	} else {
		installSyncApplying[address]--
	}
	installSyncApplyingMu.Unlock()
}

func installSyncIsApplying(address string) bool {
	installSyncApplyingMu.Lock()
	defer installSyncApplyingMu.Unlock()
	return installSyncApplying[address] > 0
}

// installSyncLastPublished is the hex id of this machine's last list event,
// which the live subscription then sees come back and ignores.
var (
	installSyncPublishedMu   sync.Mutex
	installSyncLastPublished string
)

func setInstallSyncLastPublished(id string) {
	installSyncPublishedMu.Lock()
	installSyncLastPublished = id
	installSyncPublishedMu.Unlock()
}

func lastInstallSyncPublished() string {
	installSyncPublishedMu.Lock()
	defer installSyncPublishedMu.Unlock()
	return installSyncLastPublished
}

// ─── the merge ───────────────────────────────────────────────────

// syncPinNewer says whether a is a newer version than b. A pin whose age is
// unknown is never newer, nor older: the local one then stays.
func syncPinNewer(a, b syncPin) bool {
	if a.CreatedAt == 0 || b.CreatedAt == 0 {
		return false
	}
	return nappNewer(Napp{CreatedAt: a.CreatedAt, EventID: a.EventID}, Napp{CreatedAt: b.CreatedAt, EventID: b.EventID})
}

// mergeSyncLists is the three-way merge of the package comment.
func mergeSyncLists(base, local, remote map[string]syncPin) map[string]syncPin {
	out := make(map[string]syncPin, len(local)+len(remote))
	addresses := make(map[string]bool, len(base)+len(local)+len(remote))
	for _, m := range []map[string]syncPin{base, local, remote} {
		for a := range m {
			addresses[a] = true
		}
	}
	for a := range addresses {
		l, inLocal := local[a]
		r, inRemote := remote[a]
		_, inBase := base[a]
		switch {
		case inLocal && inRemote:
			if syncPinNewer(r, l) {
				out[a] = r
			} else {
				out[a] = l
			}
		case inBase:
			// dropped on one side since the lists last agreed
		case inLocal:
			out[a] = l
		case inRemote:
			out[a] = r
		}
	}
	return out
}

// ─── the encrypted event ─────────────────────────────────────────

// encodeSyncList is the v1 plaintext, sorted by address.
func encodeSyncList(pins map[string]syncPin) ([]byte, error) {
	list := syncList{V: 1, Napplets: syncEntries(pins)}
	return json.Marshal(list)
}

// syncEntries is pins as entries sorted by address, never nil.
func syncEntries(pins map[string]syncPin) []syncEntry {
	entries := make([]syncEntry, 0, len(pins))
	for a, p := range pins {
		entries = append(entries, syncEntry{Address: a, EventID: p.EventID})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Address < entries[j].Address })
	return entries
}

// syncPins is entries as pins of unknown age.
func syncPins(entries []syncEntry) map[string]syncPin {
	pins := make(map[string]syncPin, len(entries))
	for _, e := range entries {
		pins[e.Address] = syncPin{EventID: e.EventID}
	}
	return pins
}

// decodeSyncList reads a v1 plaintext strictly: one bad entry rejects the
// whole list. Unknown fields are ignored.
func decodeSyncList(plaintext []byte) (map[string]syncPin, error) {
	var raw struct {
		V        int             `json:"v"`
		Napplets json.RawMessage `json:"napplets"`
	}
	if err := json.Unmarshal(plaintext, &raw); err != nil || raw.V != 1 || len(raw.Napplets) == 0 || raw.Napplets[0] != '[' {
		return nil, errSyncListInvalid
	}
	var entries []syncEntry
	if err := json.Unmarshal(raw.Napplets, &entries); err != nil || len(entries) > installSyncMaxEntries {
		return nil, errSyncListInvalid
	}
	pins := make(map[string]syncPin, len(entries))
	for _, e := range entries {
		if !syncableAddress(e.Address) || !lowerHexID(e.EventID) {
			return nil, errSyncListInvalid
		}
		if _, dup := pins[e.Address]; dup {
			return nil, errSyncListInvalid
		}
		pins[e.Address] = syncPin{EventID: e.EventID}
	}
	return pins, nil
}

// syncableAddress says whether address is a canonical napplet address.
func syncableAddress(address string) bool {
	ptr, err := ParseCanonicalServiceAddress(address)
	return err == nil && (ptr.Kind == KindNapplet || ptr.Kind == KindRootNapplet)
}

// lowerHexID says whether s is a 64-character lowercase hex event id.
func lowerHexID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// sealSyncList encrypts the list to the user's own key.
func sealSyncList(ctx context.Context, k nostr.Keyer, pk nostr.PubKey, pins map[string]syncPin) (string, error) {
	plaintext, err := encodeSyncList(pins)
	if err != nil {
		return "", err
	}
	if len(plaintext) > nip44MaxPlaintext {
		return "", errors.New("synced napplet list too large to encrypt")
	}
	ctx, cancel := context.WithTimeout(ctx, installSyncSignerTimeout)
	defer cancel()
	return k.Encrypt(ctx, string(plaintext), pk)
}

// openSyncList checks that evt is the user's own list event for id and
// decrypts and decodes it.
func openSyncList(ctx context.Context, k nostr.Keyer, pk nostr.PubKey, id string, evt nostr.Event) (map[string]syncPin, error) {
	if evt.Kind != installSyncKind || evt.PubKey != pk || evt.Tags.GetD() != id || !evt.CheckID() || !evt.VerifySignature() {
		return nil, errSyncListInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, installSyncSignerTimeout)
	defer cancel()
	plaintext, err := k.Decrypt(ctx, evt.Content, pk)
	if err != nil {
		return nil, err
	}
	return decodeSyncList([]byte(plaintext))
}

// ─── pins ────────────────────────────────────────────────────────

// resolveSyncedPin is the napplet record of the exact manifest event a pin
// names, checked like any lookup: author, kind and d, then id and
// signature.
func resolveSyncedPin(ctx context.Context, address, eventID string) (Napp, error) {
	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil || (ptr.Kind != KindNapplet && ptr.Kind != KindRootNapplet) || !lowerHexID(eventID) {
		return Napp{}, errSyncPinMismatch
	}
	id, err := nostr.IDFromHex(eventID)
	if err != nil {
		return Napp{}, errSyncPinMismatch
	}
	evt := fetchSyncedPin(ctx, ptr, id)
	if evt == nil {
		return Napp{}, errSyncPinNotFound
	}
	if evt.ID != id {
		return Napp{}, errSyncPinMismatch
	}
	n, found := pickAddress(ptr, []nostr.Event{*evt})
	if !found || n.EventID != eventID {
		return Napp{}, errSyncPinMismatch
	}
	// kept, so the install and the next passes find it locally
	if sys != nil {
		stored := false
		for range sys.Store.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			stored = true
		}
		if !stored {
			if err := sys.Store.SaveEvent(*evt); err != nil {
				log.Debug().Str("address", address).Err(err).Msg("synced napplet version not stored")
			}
		}
	}
	if n.AuthorName == "" {
		n.AuthorName = n.AuthorShortName()
	}
	return n, nil
}

// installSyncedPin installs the pinned version of address. A pin that is
// nowhere to be found installs the newest version instead; one that does
// not match its event installs nothing. A newer version installed already
// counts as done: it is never downgraded.
func installSyncedPin(ctx context.Context, address, eventID string) error {
	n, err := resolveSyncedPin(ctx, address, eventID)
	if errors.Is(err, errSyncPinNotFound) {
		_, err := syncServiceInstall(ctx, address, nil)
		if errors.Is(err, ErrServiceConflict) {
			return nil
		}
		return err
	}
	if err != nil {
		log.Warn().Str("address", address).Str("event", eventID).Err(err).Msg("synced napplet version rejected")
		return err
	}
	if n.Unavailable != "" {
		return ErrServiceUnavailable
	}
	_, err = syncInstallNapp(ctx, n)
	err = serviceMutationError(ctx, err)
	if errors.Is(err, ErrServiceConflict) {
		return nil
	}
	return err
}

// localSyncPins is what is installed here and can be synced: napplets with
// a canonical address and a known manifest event.
func localSyncPins() map[string]syncPin {
	stateMu.Lock()
	defer stateMu.Unlock()
	pins := make(map[string]syncPin, len(state.InstalledNapps))
	for _, n := range state.InstalledNapps {
		if !n.IsNapplet() || !lowerHexID(n.EventID) {
			continue
		}
		a := n.Address()
		if !syncableAddress(a) {
			continue
		}
		pins[a] = syncPin{EventID: n.EventID, CreatedAt: n.CreatedAt}
	}
	return pins
}

// samePins says whether a and b pin the same addresses to the same events.
func samePins(a, b map[string]syncPin) bool {
	if len(a) != len(b) {
		return false
	}
	for addr, p := range a {
		if q, ok := b[addr]; !ok || q.EventID != p.EventID {
			return false
		}
	}
	return true
}

// ─── one pass ────────────────────────────────────────────────────

// installSyncMu serializes passes.
var installSyncMu sync.Mutex

// runInstallSync is one sync pass. It only logs; nothing it does is
// reported to a client.
func runInstallSync(ctx context.Context) {
	installSyncMu.Lock()
	defer installSyncMu.Unlock()

	id := installSyncID()
	k, pk := identitySnapshot()
	if id == "" || k == nil || pk == nostr.ZeroPK || sys == nil {
		return
	}

	stateMu.Lock()
	var st installSyncState
	if state.Sync != nil {
		st = *state.Sync
		st.Remote = append([]syncEntry(nil), state.Sync.Remote...)
		st.Base = append([]syncEntry(nil), state.Sync.Base...)
	}
	stateMu.Unlock()
	if st.ID != id || st.Pubkey != pk.Hex() {
		// a new list or another account: nothing agreed on yet
		st = installSyncState{ID: id, Pubkey: pk.Hex()}
	}

	// the remote list
	evt := fetchInstallSyncEvent(ctx, pk, id)
	if ctx.Err() != nil {
		return
	}
	var remote map[string]syncPin
	remoteMissing, fresh := evt == nil, false
	switch {
	case evt == nil:
		remote = syncPins(st.Remote)
	case evt.ID.Hex() == st.EventID || evt.CreatedAt < st.CreatedAt:
		// already merged, or a stale copy
		remote = syncPins(st.Remote)
	default:
		pins, err := openSyncList(ctx, k, pk, id, *evt)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn().Str("event", evt.ID.Hex()).Err(err).Msg("install sync list unreadable, leaving it alone")
			return
		}
		remote, fresh = pins, true
	}

	// the ages of the pins that differ from the local ones decide the merge
	local := localSyncPins()
	for a, r := range remote {
		l, ok := local[a]
		if !ok {
			continue
		}
		if r.EventID == l.EventID {
			remote[a] = l
			continue
		}
		if n, err := resolveSyncedPin(ctx, a, r.EventID); err == nil {
			r.CreatedAt = n.CreatedAt
			remote[a] = r
		}
	}

	target := mergeSyncLists(syncPins(st.Base), local, remote)

	// apply, in address order
	failedInstall := map[string]bool{}
	failedUninstall := map[string]bool{}
	attempted := false
	for _, e := range syncEntries(target) {
		if l, ok := local[e.Address]; ok && l.EventID == e.EventID {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		attempted = true
		markInstallSyncApplying(e.Address)
		err := installSyncPin(ctx, e.Address, e.EventID)
		doneInstallSyncApplying(e.Address)
		if err != nil {
			failedInstall[e.Address] = true
			logInstallSyncFailure(e.Address, err, "synced napplet install failed")
		}
	}
	for _, e := range syncEntries(local) {
		if _, ok := target[e.Address]; ok {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		attempted = true
		markInstallSyncApplying(e.Address)
		err := installSyncRemove(ctx, e.Address)
		doneInstallSyncApplying(e.Address)
		if err != nil {
			failedUninstall[e.Address] = true
			logInstallSyncFailure(e.Address, err, "synced napplet uninstall failed")
		}
	}
	if ctx.Err() != nil {
		return
	}

	// the base the next pass merges against: a failed install of something
	// new is left out, so the remote pin installs again; a failed update or
	// uninstall keeps the address, so it is retried rather than reversed
	publishList := target
	base := make(map[string]syncPin, len(target))
	for a, p := range target {
		if failedInstall[a] {
			if l, ok := local[a]; ok {
				base[a] = l
			}
			continue
		}
		base[a] = p
	}
	for a := range failedUninstall {
		base[a] = local[a]
	}

	next := installSyncState{ID: id, Pubkey: pk.Hex(), Remote: syncEntries(remote), Base: syncEntries(base)}
	if evt != nil && (fresh || evt.ID.Hex() == st.EventID) {
		next.EventID, next.CreatedAt = evt.ID.Hex(), evt.CreatedAt
	} else {
		next.EventID, next.CreatedAt = st.EventID, st.CreatedAt
	}

	if !samePins(publishList, remote) || (remoteMissing && len(publishList) > 0) || st.Pending {
		if attempted {
			// another machine may have changed the list while this one
			// applied; publishing now would overwrite its change
			again := fetchInstallSyncEvent(ctx, pk, id)
			if ctx.Err() != nil {
				return
			}
			if again != nil && (evt == nil || again.ID != evt.ID) && again.ID.Hex() != st.EventID {
				log.Info().Msg("install sync list changed during a pass, merging again")
				kickInstallSync()
				return
			}
		}
		content, err := sealSyncList(ctx, k, pk, publishList)
		if err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("install sync list not sealed")
			}
			return
		}
		createdAt := nostr.Now()
		if evt != nil && evt.CreatedAt+1 > createdAt {
			createdAt = evt.CreatedAt + 1
		}
		if st.CreatedAt+1 > createdAt {
			createdAt = st.CreatedAt + 1
		}
		out := nostr.Event{Kind: installSyncKind, CreatedAt: createdAt, Tags: nostr.Tags{{"d", id}}, Content: content}
		signCtx, cancel := context.WithTimeout(ctx, installSyncSignerTimeout)
		err = k.SignEvent(signCtx, &out)
		cancel()
		if err != nil || out.PubKey != pk {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("install sync list not signed")
			}
			return
		}
		setInstallSyncLastPublished(out.ID.Hex())
		pubCtx, cancel := context.WithTimeout(ctx, relayQueryTimeout)
		result := publishSigned(pubCtx, out, installSyncRelays(pubCtx, pk))
		cancel()
		published, _ := result["published"].(int)
		if published == 0 {
			log.Warn().Msg("install sync list reached no relay, publishing again on the next pass")
		}
		next.EventID, next.CreatedAt = out.ID.Hex(), out.CreatedAt
		next.Pending = published == 0
		next.Remote = syncEntries(publishList)
	}

	// the settings or the account may have changed while this pass ran
	if installSyncID() != id {
		return
	}
	if _, now := identitySnapshot(); now != pk {
		return
	}
	stateMu.Lock()
	state.Sync = &next
	_ = saveState()
	stateMu.Unlock()
}

func logInstallSyncFailure(address string, err error, msg string) {
	if errors.Is(err, ErrServiceBusy) {
		log.Debug().Str("address", address).Err(err).Msg(msg)
		return
	}
	log.Warn().Str("address", address).Err(err).Msg(msg)
}
