package backend

import (
	"context"
	"slices"
	"sync"
	"time"

	"fiatjaf.com/nostr"
)

// Napp updates are the same manifest event (kind:35130, or 35129 for a napplet), same author and d-tag, with a
// newer created_at: there are no version numbers, only newer publications.
// Discovery keeps the in-memory list fresh passively; CheckForUpdates goes
// out and asks the source relays about every installed napp at once.

// CheckForUpdates looks for a newer manifest of every installed napp — on
// the discovery relays and on each author's outbox relays — and marks the
// napps it found new versions for, or whose latest version is invalid.
// Non-blocking: watch UpdateCheckRunning and the per-napp UpdateAvailable and
// Unavailable fields in the state for the outcome.
func CheckForUpdates() {
	// the check runs from a copy of the installed list: an install or
	// uninstall starting meanwhile doesn't change what this round asks
	stateMu.Lock()
	napps := make([]Napp, 0, len(state.InstalledNapps))
	for _, n := range state.InstalledNapps {
		napps = append(napps, n)
	}
	stateMu.Unlock()
	if len(napps) == 0 {
		return
	}

	updateChecking.Store(true)
	defer func() {
		updateChecking.Store(false)
		notifyState()
	}()
	notifyState()

	states := checkAllUpdates(napps)
	if len(states) == 0 {
		return
	}
	var newer, unavailable []string
	for id, entry := range states {
		switch {
		case entry == nil:
		case entry.Unavailable != "":
			unavailable = append(unavailable, id)
		default:
			newer = append(newer, id)
		}
	}
	if len(newer) > 0 {
		log.Info().Strs("ids", newer).Msg("update check found new versions")
	}
	if len(unavailable) > 0 {
		log.Info().Strs("ids", unavailable).Msg("update check found invalid latest versions")
	}
	mergeUpdateStates(states)
}

// checkAllUpdates asks the relays about every napp at once and works out
// each one's update state from what came back (see updateStates).
func checkAllUpdates(napps []Napp) map[string]*Napp {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return updateStates(napps, fetchManifestEvents(ctx, napps))
}

// fetchManifestEvents gathers every manifest event the relays hold for the
// addresses of napps, unvalidated: selection (latestByAddress) decides what
// counts. It asks the discovery relays for all of them at once, by kind,
// author and d, and each author's outbox relays for each napp's own
// manifest. Every event every relay sent is kept: a single-result query
// returns whichever relay answered first, not the NIP-01 winner. Tests swap
// it to hand events in directly.
var fetchManifestEvents = func(ctx context.Context, napps []Napp) []nostr.Event {
	if sys == nil || len(napps) == 0 {
		return nil
	}

	var (
		mu  sync.Mutex
		out []nostr.Event
		wg  sync.WaitGroup
	)
	collect := func(urls []string, f nostr.Filter) {
		if len(urls) == 0 {
			return
		}
		for re := range sys.Pool.FetchMany(ctx, urls, f, nostr.SubscriptionOptions{
			Label: "verdana-napp-update",
		}) {
			mu.Lock()
			out = append(out, re.Event)
			mu.Unlock()
		}
	}

	// the discovery relays: named napps and napplets by d, and root napplets
	// (no d tag, which a #d filter would never match) by author alone
	relays := Relays()
	var ds []string
	var named, roots []Napp
	for _, n := range napps {
		if addressable(n.ManifestKind()) {
			named = append(named, n)
			ds = append(ds, n.D)
		} else {
			roots = append(roots, n)
		}
	}
	if len(named) > 0 {
		wg.Go(func() {
			collect(relays, nostr.Filter{
				Kinds:   napKinds,
				Authors: nappAuthors(named),
				Tags:    nostr.TagMap{"d": ds},
			})
		})
	}
	if len(roots) > 0 {
		wg.Go(func() {
			collect(relays, nostr.Filter{
				Kinds:   []nostr.Kind{KindRootNapplet},
				Authors: nappAuthors(roots),
			})
		})
	}

	// each napp's own manifest on its author's outbox relays, a few at a time
	sem := make(chan struct{}, 8)
	for _, n := range napps {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			collect(sys.FetchWriteRelays(ctx, n.Author), manifestFilter(n))
		})
	}
	wg.Wait()
	return out
}

// updateStates works out what the update set should say about each
// installed record from the events fetched for it: the NIP-01 winner of the
// record's address among them is the latest version (D-10). The result
// holds an entry only for records with at least one authentic event at
// their address; one with nothing found keeps whatever state it had.
//   - a winner older than the installed record changes nothing but clears a
//     stale entry (nil): the installed version is the latest
//   - an invalid winner is an unavailable entry: the installed copy keeps
//     running, and no update is offered
//   - a valid winner that is NIP-01-newer is the update
//   - otherwise the installed version is the latest (nil)
func updateStates(installed []Napp, events []nostr.Event) map[string]*Napp {
	winners := latestByAddress{}
	for _, evt := range events {
		winners.add(evt)
	}
	states := make(map[string]*Napp, len(installed))
	for _, n := range installed {
		evt, ok := winners[n.Address()]
		if !ok {
			continue
		}
		states[n.ID] = updateState(n, nappFromLatest(evt))
	}
	return states
}

// updateState is the update-set entry for an installed record given the
// latest version of its address: the unavailable entry, the newer version,
// or nil when the installed version is the latest. Either entry carries the
// id the launcher knows the record by.
func updateState(installed, latest Napp) *Napp {
	if nappNewer(installed, latest) {
		return nil
	}
	if latest.Unavailable != "" {
		latest.ID = installed.ID
		return &latest
	}
	if nappNewer(latest, installed) {
		latest.ID = installed.ID
		return &latest
	}
	return nil
}

// ─── the launch-time check ───────────────────────────────────────

// launchCheckEvery is how often launching one napplet may look for a newer
// version of it: a launch inside the window after the last check of that
// napplet asks nobody.
var launchCheckEvery = 30 * time.Minute

// launchCheckClock is the clock the throttle reads; tests swap it.
var launchCheckClock = time.Now

// updateCheckOnline says whether a background check could reach anyone: a
// system and at least one discovery relay. Tests swap it.
var updateCheckOnline = func() bool { return sys != nil && len(Relays()) > 0 }

var (
	launchChecksMu sync.Mutex
	launchChecks   = make(map[string]time.Time) // napp id -> last check start
)

// launchUpdateCheck looks for a newer version of an installed napplet that
// was just opened, in the background (D-19, CONFORMANCE A11): the launch
// never waits for it. It runs at most once per napplet per
// launchCheckEvery, never sets UpdateCheckRunning, and shows nothing but its
// result: through the update set, the installed record gains
// UpdateAvailable or Unavailable, or loses a stale entry. Offline, a failed
// or timed-out lookup, or no authentic event found leave the previous state
// as it was and only reach the log.
func launchUpdateCheck(n Napp) {
	if !n.IsNapplet() {
		return
	}
	if _, ok := InstalledNapp(n.ID); !ok || !updateCheckOnline() {
		return
	}

	now := launchCheckClock()
	launchChecksMu.Lock()
	if last, ok := launchChecks[n.ID]; ok && now.Sub(last) < launchCheckEvery {
		launchChecksMu.Unlock()
		return
	}
	launchChecks[n.ID] = now
	launchChecksMu.Unlock()

	// tracked with the other background passes, so tests can wait it out
	// before they swap the globals it reads
	backgroundSyncs.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("napp", n.ID).Msg("launch-time update check panicked")
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		latest, found := latestManifest(ctx, n)
		if !found {
			log.Debug().Str("napp", n.ID).Err(ctx.Err()).Msg("launch-time update check found nothing")
			return
		}

		// compared against the record as it is now: an update or uninstall
		// may have landed while the relays were asked
		current, ok := InstalledNapp(n.ID)
		if !ok {
			return
		}
		entry := updateState(current, latest)
		switch {
		case entry == nil:
		case entry.Unavailable != "":
			log.Info().Str("napp", n.ID).Str("event", entry.EventID).Msg("launch-time check found an invalid latest version")
		default:
			log.Info().Str("napp", n.ID).Str("event", entry.EventID).Msg("launch-time check found a new version")
		}
		mergeUpdateState(current.ID, entry)
	})
}

// ─── applying an update ──────────────────────────────────────────

// Update re-downloads an installed napp's files from its blossom servers. It
// requires knowing a newer version: the one a check round found, or the
// relay lookup this triggers when there is none. Only the NIP-01 latest
// version is ever installed, and only when it is valid.
func Update(id string) {
	n, ok := InstalledNapp(id)
	if !ok {
		SetFetchErr("napp " + id + " is not installed")
		return
	}

	setBusy(id, true)
	defer setBusy(id, false)

	latest := newerVersion(n)
	if latest == nil {
		SetFetchErr("no update found for " + n.Label())
		return
	}

	applyUpdate(n, *latest)
}

// applyUpdate does the shared re-download: fetch every path of newer into the
// napp's install dir, then record it as the installed version. Called with
// setBusy held. newer needs the full event shape; Paths and Servers are the
// parts that matter for the download itself.
func applyUpdate(current, newer Napp) {
	if newer.Unavailable != "" {
		// newerVersion never returns one; this keeps any other caller from
		// downloading an invalid manifest's files
		log.Warn().Str("napp", current.ID).Str("event", newer.EventID).
			Msg("refusing to update to an invalid latest version")
		SetFetchErr("update failed: " + errUnavailable.Error())
		return
	}
	base, err := nappBaseDir(current.ID)
	if err != nil {
		log.Error().Err(err).Str("napp", current.ID).Msg("update failed")
		SetFetchErr("update failed: " + err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	servers := newer.Servers
	if len(servers) == 0 {
		servers = newer.BlossomServers(ctx)
	}
	if err := fetchNappAssets(ctx, newer, base, servers); err != nil {
		log.Error().Err(err).Str("napp", current.ID).Msg("update failed")
		SetFetchErr("update failed: " + err.Error())
		return
	}

	// adopt the new event wholesale (new paths, new metadata), keeping the
	// id the launcher knows it by (the id is author~d, so it is already the
	// same — this only guards against a weird event)
	newer.ID = current.ID
	newer.UpdateAvailable = nil
	stateMu.Lock()
	previous, had := state.InstalledNapps[current.ID]
	state.InstalledNapps[current.ID] = newer
	delete(state.LastLaunched, current.ID)
	saveState()
	stateMu.Unlock()
	if !had {
		previous = current
	}

	// the previously available update is now the installed version
	mergeUpdateState(current.ID, nil)

	// the superseded version's storage and config are nobody's any more
	// (D-05); a window still running it keeps them until it closes (D-24)
	if scope, err := nappletScope(newer); err == nil {
		cancelPendingReclaim(scope)
	}
	if previous.IsNapplet() && previous.ArtifactHash != newer.ArtifactHash {
		reclaimNapplet(previous, instancesForNapp(current.ID))
	}

	refreshInstalled()
	log.Info().Str("napp", current.ID).Msg("update complete")
}

// newerVersion returns the version Update installs: the NIP-01 latest
// version of the napp's address when it is valid and newer than what is
// installed. A valid update the last check found is used as is; otherwise
// the relays are asked right now. A latest version that turns out invalid
// marks the napp unavailable and gives nothing to install.
func newerVersion(n Napp) *Napp {
	if latest, ok := updateSet.Load().Load(n.ID); ok && latest.Unavailable == "" && nappNewer(latest, n) {
		return &latest
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	latest, found := latestManifest(ctx, n)
	if !found {
		return nil
	}
	entry := updateState(n, latest)
	if entry == nil {
		// the installed version is the latest: a stale entry goes
		mergeUpdateState(n.ID, nil)
		return nil
	}
	mergeUpdateState(n.ID, entry)
	if entry.Unavailable != "" {
		return nil
	}
	return entry
}

// latestManifest is the NIP-01 latest version of n's address among what the
// relays hold, read with nappFromLatest: an invalid one comes back with
// Unavailable set. found is false when no authentic event of the address
// came back (offline, a timeout, or nothing published).
func latestManifest(ctx context.Context, n Napp) (Napp, bool) {
	winners := latestByAddress{}
	for _, evt := range fetchManifestEvents(ctx, []Napp{n}) {
		winners.add(evt)
	}
	evt, ok := winners[n.Address()]
	if !ok {
		return Napp{}, false
	}
	return nappFromLatest(evt), true
}

// ─── helpers ─────────────────────────────────────────────────────

// nappAuthors is the distinct authors of napps, in first-seen order.
func nappAuthors(napps []Napp) []nostr.PubKey {
	out := make([]nostr.PubKey, 0, len(napps))
	for _, n := range napps {
		if !slices.Contains(out, n.Author) {
			out = append(out, n.Author)
		}
	}
	return out
}

func nappsByAuthor(napps []Napp, author nostr.PubKey) []Napp {
	out := make([]Napp, 0, 1)
	for _, n := range napps {
		if n.Author == author {
			out = append(out, n)
		}
	}
	return out
}

// manifestFilter asks for the current manifest of one installed napp or
// napplet: its own kind only (an author may publish a napp and a napplet
// under the same d tag), and the d tag when the kind has one.
func manifestFilter(n Napp) nostr.Filter {
	f := nostr.Filter{
		Kinds:   []nostr.Kind{n.ManifestKind()},
		Authors: []nostr.PubKey{n.Author},
		Limit:   1,
	}
	if addressable(n.ManifestKind()) {
		f.Tags = nostr.TagMap{"d": []string{n.D}}
	}
	return f
}
