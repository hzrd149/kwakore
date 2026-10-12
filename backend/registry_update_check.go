package backend

// The service's update check (napplet.updates.check): it asks the relays
// about installed napplets and records what it finds in the in-memory
// update set, which napplet.installed reads as update_available. It never
// installs anything: napplet.update stays the only way to apply an update,
// and it re-verifies the version before it does.

import (
	"context"
	"sort"
	"sync"
	"time"
)

// ServiceUpdateItem is one installed napplet with a newer valid version.
type ServiceUpdateItem struct {
	Address          string         `json:"address"`
	InstalledVersion ServiceVersion `json:"installed_version"`
	AvailableVersion ServiceVersion `json:"available_version"`
}

// ServiceUpdateCheckResult lists the installed napplets checked that have a
// newer valid version. Complete is false when some napplet's relays did not
// answer; that napplet keeps its previous state.
type ServiceUpdateCheckResult struct {
	Items    []ServiceUpdateItem `json:"items"`
	Complete bool                `json:"complete"`
}

var (
	// serviceUpdateCheckMu serializes checks.
	serviceUpdateCheckMu sync.Mutex
	// serviceUpdateCheckTimeout bounds every lookup of one check together.
	serviceUpdateCheckTimeout = 60 * time.Second
	// serviceUpdateCheckDelay and serviceUpdateCheckEvery schedule the
	// background check; tests shorten them.
	serviceUpdateCheckDelay = 2 * time.Minute
	serviceUpdateCheckEvery = 6 * time.Hour
	// serviceUpdateCheck is one background check; tests replace it.
	serviceUpdateCheck = func(ctx context.Context) error {
		_, err := ServiceCheckUpdates(ctx, "")
		return err
	}
)

// serviceUpdateCheckLookups is how many napplets are looked up at once.
const serviceUpdateCheckLookups = 8

// serviceUpdateVersion is the newer valid version the update set holds for
// installed record n, or nil. It reads a stamped copy; the record itself is
// never changed.
func serviceUpdateVersion(n Napp) *ServiceVersion {
	c := n
	stampUpdateState(&c, updateSet.Load())
	if c.UpdateAvailable == nil || c.UpdateAvailable.Unavailable != "" {
		return nil
	}
	v := serviceVersion(*c.UpdateAvailable)
	return &v
}

// serviceUpdateItems lists the napplets of napps with a newer valid
// version, sorted by address. It is never nil.
func serviceUpdateItems(napps []Napp) []ServiceUpdateItem {
	items := []ServiceUpdateItem{}
	for _, n := range napps {
		if !n.IsNapplet() {
			continue
		}
		v := serviceUpdateVersion(n)
		if v == nil {
			continue
		}
		items = append(items, ServiceUpdateItem{Address: n.Address(), InstalledVersion: serviceVersion(n), AvailableVersion: *v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Address < items[j].Address })
	return items
}

// installedNapplets copies the installed napplet records.
func installedNapplets() []Napp {
	stateMu.Lock()
	defer stateMu.Unlock()
	out := make([]Napp, 0, len(state.InstalledNapps))
	for _, n := range state.InstalledNapps {
		if n.IsNapplet() {
			out = append(out, n)
		}
	}
	return out
}

// ServiceCheckUpdates asks the relays for the latest manifest of every
// installed napplet, or of the one at address, and records the result in
// the update set. It always asks: a cached entry is replaced by what the
// relays say now. Unanswered lookups keep their previous entry. It never
// installs anything.
func ServiceCheckUpdates(ctx context.Context, address string) (ServiceUpdateCheckResult, error) {
	serviceUpdateCheckMu.Lock()
	defer serviceUpdateCheckMu.Unlock()
	if address != "" {
		if _, err := ParseCanonicalServiceAddress(address); err != nil {
			return ServiceUpdateCheckResult{}, err
		}
	}
	if ctx.Err() != nil {
		return ServiceUpdateCheckResult{}, ErrServiceTimeout
	}
	targets := installedNapplets()
	if address != "" {
		var match []Napp
		for _, n := range targets {
			if n.Address() == address {
				match = append(match, n)
			}
		}
		if len(match) == 0 {
			return ServiceUpdateCheckResult{}, ErrServiceNotFound
		}
		targets = match
	}
	if len(targets) == 0 {
		return ServiceUpdateCheckResult{Items: []ServiceUpdateItem{}, Complete: true}, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, serviceUpdateCheckTimeout)
	defer cancel()
	answers := make([]struct {
		answered bool
		change   *Napp
	}, len(targets))
	sem := make(chan struct{}, serviceUpdateCheckLookups)
	var wg sync.WaitGroup
	for i, n := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Str("napp", n.ID).Msg("update check lookup panicked")
				}
			}()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-lookupCtx.Done():
				return
			}
			events, answered := fetchServiceManifestEvents(lookupCtx, n)
			if !answered {
				return
			}
			winners := latestByAddress{}
			for _, evt := range events {
				winners.add(evt)
			}
			answers[i].answered = true
			if evt, ok := winners[n.Address()]; ok {
				answers[i].change = updateState(n, nappFromLatest(evt))
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ServiceUpdateCheckResult{}, ErrServiceTimeout
	}

	changes := make(map[string]*Napp, len(targets))
	complete := true
	for i, n := range targets {
		if !answers[i].answered {
			complete = false
			continue
		}
		changes[n.ID] = answers[i].change
	}
	if len(changes) == 0 {
		return ServiceUpdateCheckResult{}, ErrServiceUnavailable
	}
	mergeUpdateStates(changes)

	items := serviceUpdateItems(targets)
	if len(items) > 0 {
		log.Info().Int("count", len(items)).Msg("update check found new versions")
	}
	return ServiceUpdateCheckResult{Items: items, Complete: complete}, nil
}

// StartServiceUpdateChecks runs the update check in the background on ctx,
// which the daemon cancels when it shuts down: once after
// serviceUpdateCheckDelay, then every serviceUpdateCheckEvery, and only
// while a relay could be reached. Updates are never applied. wait returns
// once the worker has stopped.
func StartServiceUpdateChecks(ctx context.Context) (wait func()) {
	delay, every, check := serviceUpdateCheckDelay, serviceUpdateCheckEvery, serviceUpdateCheck
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		run := func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Msg("background update check panicked")
				}
			}()
			if !updateCheckOnline() {
				return
			}
			if err := check(ctx); err != nil {
				log.Debug().Err(err).Msg("background update check failed")
			}
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		run()
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				run()
			}
		}
	}()
	return wg.Wait
}
