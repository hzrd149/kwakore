package backend

import (
	"context"
	"errors"
	"os"
	"time"

	"fiatjaf.com/nostr"
	"kwakore/backend/napconfig"
)

const maxSelectionRevision = uint64(1<<53 - 1)

type ServiceSelectionResult struct {
	Address          string          `json:"address"`
	Outcome          string          `json:"outcome"`
	Direction        string          `json:"direction"`
	PreviousVersion  *ServiceVersion `json:"previous_version"`
	InstalledVersion ServiceVersion  `json:"installed_version"`
	Pinned           bool            `json:"pinned"`
	DataReset        bool            `json:"data_reset"`
}

var fetchVersionEvent = func(ctx context.Context, ptr nostr.EntityPointer, id nostr.ID) *nostr.Event {
	if evt := archivedEvent(id); evt != nil {
		return evt
	}
	if sys == nil {
		return nil
	}
	for evt := range sys.Store.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		if evt.ID == id && evt.CheckID() && evt.VerifySignature() {
			return &evt
		}
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	urls := append([]string(nil), Relays()...)
	for _, hint := range ptr.Relays {
		if relay, err := napExplicitRelay(ctx, hint); err == nil {
			urls = nostr.AppendUnique(urls, relay)
		}
	}
	for _, relay := range sys.FetchWriteRelays(ctx, ptr.PublicKey) {
		urls = nostr.AppendUnique(urls, relay)
	}
	for re := range fetchRelays(ctx, urls, nostr.Filter{IDs: []nostr.ID{id}}, "kwakore-version").Events() {
		if re.Event.ID == id && re.Event.CheckID() && re.Event.VerifySignature() {
			evt := re.Event
			return &evt
		}
	}
	return nil
}

func resolveSelectedVersion(ctx context.Context, address, eventID string, relays []string) (Napp, error) {
	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil || !isNappletKind(ptr.Kind) || !lowerHexID(eventID) {
		return Napp{}, ErrServiceInvalidAddress
	}
	if ptr.Kind == KindSnapshotNapplet && ptr.Identifier != eventID {
		return Napp{}, ErrServiceConflict
	}
	if ctx.Err() != nil {
		return Napp{}, ErrServiceTimeout
	}
	ptr.Relays = relays
	id, _ := nostr.IDFromHex(eventID)
	evt := fetchVersionEvent(ctx, ptr, id)
	if ctx.Err() != nil {
		return Napp{}, ErrServiceTimeout
	}
	if evt == nil {
		return Napp{}, ErrServiceNotFound
	}
	if evt.ID != id {
		return Napp{}, ErrServiceConflict
	}
	n, found := pickAddress(ptr, []nostr.Event{*evt})
	if !found || n.EventID != eventID {
		return Napp{}, ErrServiceConflict
	}
	if n.Unavailable != "" {
		return Napp{}, ErrServiceUnavailable
	}
	return n, nil
}

func ServiceInstallVersion(ctx context.Context, address, eventID string, relays []string) (ServiceInstallResult, error) {
	n, err := resolveSelectedVersion(ctx, address, eventID, relays)
	if err != nil {
		return ServiceInstallResult{}, err
	}
	r, err := selectResolvedContext(ctx, n, false, nil, false)
	return ServiceInstallResult{Address: r.Address, Outcome: r.Outcome, InstalledVersion: r.InstalledVersion}, serviceMutationError(ctx, err)
}

func ServiceSelect(ctx context.Context, address, eventID string) (ServiceSelectionResult, error) {
	n, err := resolveSelectedVersion(ctx, address, eventID, nil)
	if err != nil {
		return ServiceSelectionResult{}, err
	}
	r, err := selectResolvedContext(ctx, n, true, nil, false)
	return r, serviceMutationError(ctx, err)
}

func selectionPin(n Napp) syncPin {
	return syncPin{EventID: n.EventID, CreatedAt: n.CreatedAt, Pinned: n.Pinned, Revision: n.SelectionRevision, SelectionID: n.SelectionID}
}

// Logical revision plus a random tie breaker orders user choices independently
// of publication dates and unsynchronized wall clocks. stateMu is held.
func stampSelectionLocked(n *Napp) error {
	var revision uint64
	if current, ok := state.InstalledNapps[n.ID]; ok {
		revision = current.SelectionRevision
	}
	if state.Sync != nil {
		for _, list := range [][]syncEntry{state.Sync.Base, state.Sync.Remote} {
			for _, e := range list {
				if e.Address == n.Address() {
					revision = max(revision, e.Revision)
				}
			}
		}
	}
	if revision >= maxSelectionRevision {
		return ErrServiceConflict
	}
	n.Pinned, n.SelectionRevision, n.SelectionID = true, revision+1, randomID()
	return nil
}

// The caller supplies either a user choice (remote nil) or an authenticated
// synced choice. busyHeld is used by the latest-only update path after lookup.
func selectResolvedContext(ctx context.Context, next Napp, requireInstalled bool, remote *syncPin, busyHeld bool) (ServiceSelectionResult, error) {
	if ctx.Err() != nil {
		return ServiceSelectionResult{}, ErrServiceTimeout
	}
	if !next.IsNapplet() || next.Unavailable != "" {
		return ServiceSelectionResult{}, ErrServiceUnavailable
	}
	if !busyHeld {
		if !trySetBusy(next.ID) {
			return ServiceSelectionResult{}, errBusy
		}
		defer setBusy(next.ID, false)
	}
	if mutationExists(next.ID) {
		return ServiceSelectionResult{}, errBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	stateMu.Lock()
	previous, had := state.InstalledNapps[next.ID]
	stateMu.Unlock()
	if requireInstalled && !had {
		return ServiceSelectionResult{}, ErrServiceNotFound
	}
	if had && previous.Address() != next.Address() {
		return ServiceSelectionResult{}, ErrServiceConflict
	}
	if had && previous.EventID != next.EventID && len(runningForNapp(next.ID)) > 0 {
		return ServiceSelectionResult{}, errBusy
	}
	// Re-selecting the same established pin neither resets data nor downloads.
	if had && previous.EventID == next.EventID && previous.Pinned && (remote == nil || samePinSelection(selectionPin(previous), *remote)) {
		return selectionResult(previous, previous, true, false), nil
	}
	base, err := nappBaseDir(next.ID)
	if err != nil {
		return ServiceSelectionResult{}, err
	}
	if had {
		retainInstalledArtifacts(previous, base)
	}
	servers := []string(nil)
	if !haveVerifiedArtifacts(next) {
		servers = next.BlossomServers(ctx)
	}
	staging, err := stageNappFiles(ctx, next, base, servers)
	if err != nil {
		return ServiceSelectionResult{}, err
	}
	defer os.RemoveAll(staging)
	// The launch path holds the same lock from its record read through window
	// registration. A switch can never miss a concurrently opening old window.
	reclaimMu.Lock()
	stateMu.Lock()
	current, exists := state.InstalledNapps[next.ID]
	if exists != had || (exists && current.EventID != previous.EventID) {
		stateMu.Unlock()
		reclaimMu.Unlock()
		return ServiceSelectionResult{}, ErrServiceConflict
	}
	if ctx.Err() != nil {
		stateMu.Unlock()
		reclaimMu.Unlock()
		return ServiceSelectionResult{}, ErrServiceTimeout
	}
	reset := had && previous.EventID != next.EventID
	if reset {
		for _, ci := range allInstances() {
			if ci.napp.ID == next.ID {
				stateMu.Unlock()
				reclaimMu.Unlock()
				return ServiceSelectionResult{}, errBusy
			}
		}
	}
	if remote != nil {
		if had && !samePinSelection(selectionPin(current), *remote) && !syncPinNewer(*remote, selectionPin(current)) {
			stateMu.Unlock()
			reclaimMu.Unlock()
			return ServiceSelectionResult{}, ErrServiceConflict
		}
		next.Pinned, next.SelectionRevision, next.SelectionID = remote.Pinned, remote.Revision, remote.SelectionID
	} else if err := stampSelectionLocked(&next); err != nil {
		stateMu.Unlock()
		reclaimMu.Unlock()
		return ServiceSelectionResult{}, err
	}
	next.UpdateAvailable = nil
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	previous, had, err = commitInstallMutation(next.ID, "select", staging, base, next, reset)
	stateMu.Unlock()
	reclaimMu.Unlock()
	if err != nil {
		return ServiceSelectionResult{}, err
	}
	result := selectionResult(previous, next, had, reset)
	mergeUpdateState(next.ID, nil)
	refreshInstalled()
	scheduleNappletIconCache(next)
	scheduleNappletSocial(next)
	installSyncLocalChange(next.Address())
	event := ServiceEvent{Type: EventNappletUpdated, Address: next.Address(), Outcome: result.Outcome, PreviousVersion: result.PreviousVersion, InstalledVersion: serviceVersionPtr(result.InstalledVersion)}
	event.Pinned, event.DataReset = &result.Pinned, &result.DataReset
	if !had {
		event.Type = EventNappletInstalled
	}
	emitServiceEvent(event)
	noteUpdatesChanged()
	return result, nil
}

func selectionResult(previous, next Napp, had, reset bool) ServiceSelectionResult {
	r := ServiceSelectionResult{Address: next.Address(), Outcome: "installed", Direction: "installed", InstalledVersion: serviceVersion(next), Pinned: next.Pinned || next.IsSnapshot(), DataReset: reset}
	if had {
		v := serviceVersion(previous)
		r.PreviousVersion = &v
		r.Outcome, r.Direction = "selected", "forward"
		if previous.EventID == next.EventID {
			r.Outcome, r.Direction = "unchanged", "unchanged"
		} else if nappNewer(previous, next) {
			r.Direction = "backward"
		}
	}
	return r
}

// Called only after the state token commits, with stateMu held and no live
// window for this address. Failure leaves the journal and blocks launch until
// recovery completes; an uncommitted selection never deletes saved data.
func resetSelectionDataLocked(previous, next Napp) error {
	seen := map[string]bool{}
	var cleanupErr error
	for _, n := range []Napp{previous, next} {
		scope, err := nappletScope(n)
		if err != nil {
			return err
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		cleanupErr = errors.Join(cleanupErr, reclaimStoreKey(scope))
		for _, instance := range instancesForNapp(n.ID) {
			key, err := nappletStorageKey(n, "instance", instance)
			if err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			} else {
				cleanupErr = errors.Join(cleanupErr, reclaimStoreKey(key))
			}
		}
		cleanupErr = errors.Join(cleanupErr, napconfig.Forget(scope))
	}
	return cleanupErr
}
