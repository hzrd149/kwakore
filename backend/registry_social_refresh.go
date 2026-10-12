package backend

// Background social refresh. The fetch policy is a locked decision:
// installed napplets are fetched after an install, update or version
// selection and refreshed every social_refresh (a setting; "0" turns the
// periodic part off). Napplets that are not installed are fetched only when
// a client asks. A refresh that changes the held events, and each of the
// user's own social actions, emits social.updated so clients re-read
// instead of polling.
//
// Only the daemon runs this: StartServiceSocialRefresh is called next to
// the update checks, and the install hooks need both the daemon's service
// config and a running loop, so neither tests nor other embedders ever reach
// a relay through it. Every refresh goes through socialRefresh, so the
// per-address singleflight, the two-address semaphore and the 30-second
// floor bound the load; the loop adds one napplet at a time, a stagger and
// a per-napplet timeout.

import (
	"context"
	"slices"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"kwakore/backend/serviceconfig"
)

var (
	// socialRefreshDelay is how long after start the first pass waits.
	socialRefreshDelay = 5 * time.Minute
	// socialRefreshStagger separates two napplets within a pass.
	socialRefreshStagger = 10 * time.Second
	// socialRefreshOfflineRetry caps the wait before the next try when a
	// pass was due while no relay could be reached.
	socialRefreshOfflineRetry = 15 * time.Minute
	// socialRefreshTimeout bounds the refresh of one napplet.
	socialRefreshTimeout = 60 * time.Second

	// socialRefreshEvery is the current interval; 0 means the periodic
	// refresh is off. It is read on every tick, so a settings change takes
	// effect at once.
	socialRefreshEvery = func() time.Duration {
		if serviceConfig == nil {
			return 0
		}
		d, ok := serviceconfig.SocialRefreshInterval(serviceConfig.Effective().SocialRefresh)
		if !ok {
			return 0
		}
		return d
	}

	// socialRefreshOne refreshes every category of one napplet address.
	socialRefreshOne = func(ctx context.Context, address string) {
		t, err := newSocialTarget(address)
		if err != nil {
			log.Debug().Msg("background social refresh skipped an address")
			return
		}
		if err := socialRefresh(ctx, t, SocialCategories()); err != nil {
			log.Debug().Msg("background social refresh ended early")
		}
	}

	// prefetchNappletSocial says whether an install, update or selection
	// fetches the napplet's social data in the background. Only the daemon
	// does, like prefetchNappletIcons; tests swap it.
	prefetchNappletSocial = func() bool { return serviceConfig != nil }

	// socialRefreshWake carries SocialRefreshChanged to the loop.
	socialRefreshWake = make(chan struct{}, 1)
)

// socialLoop is the running loop, if any. Hook refreshes join hooks while
// ctx is set; the loop clears ctx under mu before it waits on hooks, so Add
// never races Wait.
var socialLoop struct {
	mu    sync.Mutex
	ctx   context.Context
	hooks sync.WaitGroup
}

// SocialRefreshChanged tells the refresh loop that social_refresh changed,
// so it applies the new interval at once. It never blocks.
func SocialRefreshChanged() {
	select {
	case socialRefreshWake <- struct{}{}:
	default:
	}
}

// socialHookBegin joins a hook refresh to the running loop. It returns the
// loop's context and the func that ends the hook, or false when no loop runs.
func socialHookBegin() (context.Context, func(), bool) {
	socialLoop.mu.Lock()
	defer socialLoop.mu.Unlock()
	if socialLoop.ctx == nil {
		return nil, nil, false
	}
	socialLoop.hooks.Add(1)
	return socialLoop.ctx, socialLoop.hooks.Done, true
}

// StartServiceSocialRefresh runs the periodic social refresh on ctx, which
// the daemon cancels when it shuts down. The first pass runs
// socialRefreshDelay after start; each later pass is due one interval after
// the previous one started. No pass runs while the interval is 0 or while
// no relay could be reached (the next try is then at most
// socialRefreshOfflineRetry later). wait returns once the loop and every
// hook refresh started while it ran have ended.
func StartServiceSocialRefresh(ctx context.Context) (wait func()) {
	socialLoop.mu.Lock()
	socialLoop.ctx = ctx
	socialLoop.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			socialLoop.mu.Lock()
			socialLoop.ctx = nil
			socialLoop.mu.Unlock()
			socialLoop.hooks.Wait()
		}()
		due := time.Now().Add(socialRefreshDelay)
		var last time.Time
		for {
			d := socialRefreshEvery()
			var tick <-chan time.Time
			var timer *time.Timer
			if d > 0 {
				timer = time.NewTimer(time.Until(due))
				tick = timer.C
			}
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case <-socialRefreshWake:
				if timer != nil {
					timer.Stop()
				}
				// before the first pass the initial delay stands
				if !last.IsZero() {
					due = last.Add(socialRefreshEvery())
				}
			case <-tick:
				d = socialRefreshEvery()
				if d == 0 {
					continue
				}
				if !updateCheckOnline() {
					due = time.Now().Add(min(d, socialRefreshOfflineRetry))
					continue
				}
				last = time.Now()
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Error().Interface("panic", r).Msg("background social refresh panicked")
						}
					}()
					socialRefreshPass(ctx)
				}()
				due = last.Add(d)
			}
		}
	}()
	return wg.Wait
}

// socialRefreshPass refreshes every installed napplet, snapshots included,
// one at a time in address order, socialRefreshStagger apart. It stops
// early when ctx ends or the interval turned 0.
func socialRefreshPass(ctx context.Context) {
	var addresses []string
	for _, n := range installedNapplets() {
		addresses = append(addresses, n.Address())
	}
	slices.Sort(addresses)
	addresses = slices.Compact(addresses)
	for i, address := range addresses {
		if i > 0 {
			timer := time.NewTimer(socialRefreshStagger)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if ctx.Err() != nil || socialRefreshEvery() == 0 {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Msg("background social refresh of a napplet panicked")
				}
			}()
			refreshCtx, cancel := context.WithTimeout(ctx, socialRefreshTimeout)
			defer cancel()
			socialRefreshOne(refreshCtx, address)
		}()
	}
}

// scheduleNappletSocial fetches every social category of n in the
// background after a committed install, update or selection, then marks it
// installed in the status index. It only runs in the daemon while the
// refresh loop runs, whose wait drains it before the stores close. It is
// not gated on being online: the install itself just reached the network.
func scheduleNappletSocial(n Napp) {
	if !n.IsNapplet() || !prefetchNappletSocial() {
		return
	}
	loopCtx, done, ok := socialHookBegin()
	if !ok {
		return
	}
	address, id := n.Address(), n.ID
	refresh, timeout := socialRefreshOne, socialRefreshTimeout
	backgroundSyncs.Go(func() {
		defer done()
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("napp", id).Msg("napplet social fetch panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(loopCtx, timeout)
		defer cancel()
		refresh(ctx, address)
		socialMarkInstalled(address)
	})
}

// socialCategoryOf is the category of an event kind, the inverse of
// socialKind, or "" for a kind no category holds.
func socialCategoryOf(kind nostr.Kind) string {
	switch kind {
	case nostr.KindReaction:
		return SocialReactions
	case nostr.KindComment:
		return SocialComments
	case nostr.KindTextNote:
		return SocialMentions
	case nostr.KindZap:
		return SocialZaps
	}
	return ""
}

// socialUpdated emits social.updated for address with the sorted, distinct
// categories, when there are any. It carries nothing else: never a URL,
// path, key or error.
func socialUpdated(address string, categories ...string) {
	var fields []string
	for _, c := range categories {
		if c != "" {
			fields = append(fields, c)
		}
	}
	slices.Sort(fields)
	fields = slices.Compact(fields)
	if len(fields) == 0 {
		return
	}
	emitServiceEvent(ServiceEvent{Type: EventSocialUpdated, Address: address, Fields: fields})
}
