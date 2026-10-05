package backend

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
)

var (
	// discoverMu orders a refresh against the one it replaces: the old run
	// only publishes while holding it and while its context is still live,
	// so nothing it found lands on top of the new run's list.
	discoverMu     sync.Mutex
	cancelDiscover context.CancelFunc
)

// discoveryFlushInterval is how often napps arriving from the relays are
// pushed to the launcher while discovery runs. The list fills in as each
// relay answers instead of waiting for the slowest one, and a relay that
// dumps hundreds of events at once still redraws the UI only a few times a
// second.
const discoveryFlushInterval = 250 * time.Millisecond

func Discover() {
	discoverMu.Lock()
	if cancelDiscover != nil {
		cancelDiscover()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelDiscover = cancel
	setFetching(true)
	discoverMu.Unlock()

	// a first login's relay list is still on its way: give it a moment, so
	// this run asks the user's relays too instead of a second run redoing it
	waitUserRelays(userRelayWait)
	urls := discoveryRelays()
	go refreshFollows(ctx)
	log.Info().Strs("relays", urls).Strs("settings", Relays()).Strs("user", UserWriteRelays()).
		Bool("userRelays", DiscoverOnUserRelays()).Msg("fetching napps from relays")

	events, eose := sys.Pool.SubscribeManyNotifyEOSE(ctx, urls,
		nostr.Filter{
			Kinds: napKinds,
		},
		nostr.SubscriptionOptions{
			Label:          "verdana-discovery",
			MaxWaitForEOSE: time.Second * 20,
		},
	)

	collectDiscovery(events, eose, discoveryFlushInterval, func(list []Napp, done bool) {
		discoverMu.Lock()
		defer discoverMu.Unlock()
		if ctx.Err() != nil {
			return
		}
		setDiscovery(list)
		if done {
			log.Info().Int("count", len(list)).Msg("fetch complete")
			setFetching(false)
			go SyncSystemSearch()
		}
	})

	log.Info().Err(context.Cause(ctx)).Msg("discovery subscription ended")
}

// refreshFollows loads who the user follows for the discovery tab's friends
// filter. It rides along with each discovery run, so "Refresh" picks up a
// follow list changed elsewhere too.
func refreshFollows(ctx context.Context) {
	pk, ok := currentUser()
	if !ok {
		return
	}
	authors := FollowedAuthors(ctx)
	if ctx.Err() != nil || len(authors) == 0 {
		return
	}
	hexes := make([]string, len(authors))
	for i, a := range authors {
		hexes[i] = a.Hex()
	}
	setFollows(pk.Hex(), hexes)
}

// collectDiscovery reads napps off a discovery subscription until it
// closes, handing publish the list so far every flush interval while new
// ones keep arriving, and with done set once every relay has sent EOSE (or
// been given up on) or the subscription ends. The events of one address
// collapse into its NIP-01 winner (registry_select.go), which is listed
// even when it is invalid: then as unavailable, never as an older version.
func collectDiscovery(events <-chan nostr.RelayEvent, eose <-chan struct{}, flush time.Duration, publish func(list []Napp, done bool)) {
	var list []Napp
	latest := latestByAddress{}
	index := make(map[string]int) // address -> position in list
	dirty, finished := false, false

	ticker := time.NewTicker(flush)
	defer ticker.Stop()

	for {
		select {
		case re, ok := <-events:
			if !ok {
				if dirty || !finished {
					publish(list, true)
				}
				return
			}
			if !latest.add(re.Event) {
				continue
			}
			n := nappFromLatest(re.Event)
			addr := eventAddress(re.Event)
			if i, seen := index[addr]; seen {
				list[i] = n
			} else {
				index[addr] = len(list)
				list = append(list, n)
			}
			dirty = true
		case <-ticker.C:
			if dirty {
				publish(append([]Napp(nil), list...), finished)
				dirty = false
			}
		case <-eose:
			eose = nil
			finished = true
			publish(append([]Napp(nil), list...), true)
			dirty = false
		}
	}
}

// nappFromNappEvent reads a kind:35130 napp manifest.
func nappFromNappEvent(evt nostr.Event) Napp {
	d := evt.Tags.GetD()
	n := Napp{
		D:         d,
		ID:        evt.PubKey.Hex()[:16] + "~" + d,
		Author:    evt.PubKey,
		CreatedAt: evt.CreatedAt,
	}

	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}

		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "title":
			n.Name = tag[1]
		case "description":
			n.Description = tag[1]
		case "icon":
			n.Icon = tag[1]
		case "action":
			n.Actions = append(n.Actions, tag[1])
		case "requires":
			n.Requires = append(n.Requires, tag[1])
		case "path":
			if len(tag) < 3 {
				continue
			}
			n.Paths = append(n.Paths, NappPath{Path: tag[1], Sha256: tag[2]})
		case "server":
			n.Servers = append(n.Servers, tag[1])
		case "initial_size", "initial-size":
			if len(tag) >= 3 {
				w, werr := strconv.Atoi(strings.TrimSpace(tag[1]))
				h, herr := strconv.Atoi(strings.TrimSpace(tag[2]))
				if werr == nil && herr == nil {
					if s, ok := sanitizeInitialSize(w, h); ok {
						n.InitialSize = &s
					}
				}
			}
		}
	}

	if n.Name == "" {
		n.Name = d
	}

	return n
}
