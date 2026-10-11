package backend

// Bounded multi-relay requests.
//
// The rule, in the user's words: "if a user has an offline relay it does not
// break the whole app or hold up the syncing process. when making requests to
// multiple relays we should ensure that the request is complete 15 seconds
// after the fastest relay returns EOSE and all requests have a 30s timeout".
//
// So a request to several relays finishes at the earliest of:
//   - every relay has sent EOSE or failed (refused, CLOSED, dropped);
//   - relayEOSEGrace after the first real EOSE from any relay;
//   - relayQueryTimeout after it started.
//
// A shorter caller ctx still ends it sooner; nothing here extends a deadline.
//
// The pool's FetchMany and SubscribeManyNotifyEOSE cannot do this. They never
// expose a per-relay EOSE, so the grace window has nothing to start on, and
// SubscribeManyNotifyEOSE retries an unreachable relay forever and only counts
// it as done when ctx ends, so one offline relay keeps its EOSE channel open
// for good. The helper therefore fans each (relay, filter) target out itself
// with EnsureRelay and relay.Subscribe, keeping the pool's middleware and a
// shared per-ID dedup.

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"fiatjaf.com/nostr"
	"github.com/puzpuzpuz/xsync/v3"
)

const (
	// relayQueryTimeout is the hard cap on any multi-relay request.
	relayQueryTimeout = 30 * time.Second
	// relayEOSEGrace is how long slower relays get after the first EOSE.
	relayEOSEGrace = 15 * time.Second
)

// relayLimits carries the two durations of the rule, so tests can pass short
// ones to the core without touching a global.
type relayLimits struct{ timeout, grace time.Duration }

var defaultRelayLimits = relayLimits{relayQueryTimeout, relayEOSEGrace}

// errRelayTimeout is the fixed error a publish reports for a relay that had
// not answered when the cap ran out.
var errRelayTimeout = errors.New("relay did not answer in time")

// relayQuery is one bounded request to several relays.
type relayQuery struct {
	events   chan nostr.RelayEvent
	done     chan struct{}
	answered atomic.Bool
	complete atomic.Bool
}

// Events delivers each event once, whichever relay sent it first. For a
// one-shot query it closes after Done; for a live query it stays open until
// the caller's ctx ends.
func (q *relayQuery) Events() <-chan nostr.RelayEvent { return q.events }

// Done closes when the rule says the stored events are in: every relay sent
// EOSE or failed, the grace after the first EOSE ran out, the cap was reached
// or the caller's ctx ended.
func (q *relayQuery) Done() <-chan struct{} { return q.done }

// Answered reports whether any relay sent a real EOSE. It only ever turns
// true, so an empty result is authoritative only when it is.
func (q *relayQuery) Answered() bool { return q.answered.Load() }

// Complete reports whether every relay sent EOSE or failed before the grace
// or the cap ended the request. It is final once Done is closed.
func (q *relayQuery) Complete() bool { return q.complete.Load() }

func finishedQuery() *relayQuery {
	q := &relayQuery{events: make(chan nostr.RelayEvent), done: make(chan struct{})}
	q.complete.Store(true)
	close(q.done)
	close(q.events)
	return q
}

// queryRelays runs every target as its own subscription under the rule. A
// one-shot query ends each subscription at its EOSE and all of them at Done;
// a live query keeps them open (reconnecting with a backoff) until ctx ends,
// and only its Done follows the rule.
func queryRelays(ctx context.Context, pool *nostr.Pool, limits relayLimits, targets []nostr.DirectedFilter, label string, live bool) *relayQuery {
	norm := make([]nostr.DirectedFilter, 0, len(targets))
	for _, df := range targets {
		if url := nostr.NormalizeURL(df.Relay); url != "" {
			norm = append(norm, nostr.DirectedFilter{Filter: df.Filter, Relay: url})
		}
	}
	if len(norm) == 0 {
		return finishedQuery()
	}

	q := &relayQuery{events: make(chan nostr.RelayEvent), done: make(chan struct{})}

	seen := xsync.NewMapOf[nostr.ID, struct{}]()
	opts := nostr.SubscriptionOptions{
		Label: label,
		CheckDuplicate: func(id nostr.ID, relay string) bool {
			if _, dup := seen.LoadOrStore(id, struct{}{}); dup {
				if mh := pool.DuplicateMiddleware; mh != nil {
					mh(relay, id)
				}
				return true
			}
			return false
		},
		// the library otherwise sends a fake EOSE after 7s that looks just like
		// a real one; only a relay's real EOSE may start the grace window
		MaxWaitForEOSE: time.Duration(math.MaxInt64),
	}

	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if !live {
		runCtx, cancel = context.WithCancel(ctx)
	}

	finished := make(chan bool, len(norm))
	var wg sync.WaitGroup
	wg.Add(len(norm))
	for _, df := range norm {
		go func() {
			defer wg.Done()
			queryTarget(runCtx, pool, q, df, opts, label, live, finished)
		}()
	}

	go func() {
		capTimer := time.NewTimer(limits.timeout)
		defer capTimer.Stop()
		var graceTimer *time.Timer
		var grace <-chan time.Time
		reported, cut := 0, false
	wait:
		for {
			select {
			case ok := <-finished:
				reported++
				if ok && graceTimer == nil {
					graceTimer = time.NewTimer(limits.grace)
					grace = graceTimer.C
				}
				if reported == len(norm) {
					q.complete.Store(true)
					break wait
				}
			case <-grace:
				cut = true
				break wait
			case <-capTimer.C:
				cut = true
				break wait
			case <-ctx.Done():
				break wait
			}
		}
		if graceTimer != nil {
			graceTimer.Stop()
		}
		close(q.done)
		if !live {
			cancel()
		}
		if cut {
			log.Debug().Str("label", label).Int("targets", len(norm)).Int("reported", reported).
				Msg("relay query cut short")
		}
	}()

	go func() {
		// every forwarder has returned, so nothing can send on a closed channel;
		// waiting for done too means Complete is final by the time a consumer
		// sees Events close
		wg.Wait()
		<-q.done
		close(q.events)
	}()

	return q
}

// queryTarget runs one (relay, filter) target, reporting to finished exactly
// once: true on its first real EOSE, false on its first failure.
func queryTarget(ctx context.Context, pool *nostr.Pool, q *relayQuery, df nostr.DirectedFilter, opts nostr.SubscriptionOptions, label string, live bool, finished chan<- bool) {
	url, filter := df.Relay, df.Filter
	reported := false
	report := func(ok bool) {
		if !reported {
			reported = true
			finished <- ok // buffered for every target, never blocks
		}
	}
	fail := func(err error) {
		log.Debug().Str("relay", url).Str("label", label).Err(err).Msg("relay query target failed")
		report(false)
	}

	backoff := 3 * time.Second
	for ctx.Err() == nil {
		if mh := pool.QueryMiddleware; mh != nil && filter.Kinds != nil && filter.Authors != nil {
			for _, kind := range filter.Kinds {
				for _, author := range filter.Authors {
					mh(url, author, kind)
				}
			}
		}

		relay, err := relayConnect(ctx, pool, url)
		var sub *nostr.Subscription
		if err == nil {
			sub, err = relay.Subscribe(ctx, filter, opts)
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fail(err)
		} else {
			backoff = 3 * time.Second
			if !readTarget(ctx, pool, q, sub, relay, url, label, live, report) {
				return
			}
			// the connection dropped under a live query: from now on only new
			// events matter, as the pool does
			filter.Since = nostr.Now()
		}

		if !live {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(10*time.Minute, backoff*17/10)
	}
}

// readTarget forwards one subscription's events. It returns true only when a
// live query's connection dropped and the target should reconnect.
func readTarget(ctx context.Context, pool *nostr.Pool, q *relayQuery, sub *nostr.Subscription, relay *nostr.Relay, url, label string, live bool, report func(bool)) bool {
	closed := func(reason string) {
		// permanent, as in the pool. There is no AUTH retry: kwakore sets no
		// AuthRequiredHandler, so an auth-required CLOSED cannot be answered.
		log.Debug().Str("relay", url).Str("label", label).Str("reason", reason).Msg("relay closed query")
		report(false)
	}
	for {
		select {
		case evt, more := <-sub.Events:
			if !more {
				// a CLOSED queues its reason before it ends the subscription
				select {
				case reason := <-sub.ClosedReason:
					closed(reason)
					return false
				default:
				}
				if ctx.Err() != nil {
					return false
				}
				log.Debug().Str("relay", url).Str("label", label).Msg("relay dropped query")
				report(false)
				return live
			}
			ie := nostr.RelayEvent{Event: evt, Relay: relay}
			if mh := pool.EventMiddleware; mh != nil {
				mh(ie)
			}
			select {
			case q.events <- ie:
			case <-ctx.Done():
				return false
			}
		case <-sub.EndOfStoredEvents:
			q.answered.Store(true)
			report(true)
			if !live {
				sub.Unsub()
				return false
			}
		case reason := <-sub.ClosedReason:
			closed(reason)
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// relayConnect is pool.EnsureRelay bounded by ctx. EnsureRelay dials on the
// pool's own context, so a hanging dial would otherwise hold the request for
// the library's 7s; here it is abandoned when ctx ends, and its goroutine
// finishes on its own when that dial gives up.
func relayConnect(ctx context.Context, pool *nostr.Pool, url string) (*nostr.Relay, error) {
	type result struct {
		relay *nostr.Relay
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		relay, err := pool.EnsureRelay(url)
		ch <- result{relay, err}
	}()
	select {
	case res := <-ch:
		return res.relay, res.err
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

// fetchRelays is a one-shot query of filter on every distinct relay in urls.
// It replaces the pool's FetchMany.
func fetchRelays(ctx context.Context, urls []string, filter nostr.Filter, label string) *relayQuery {
	if sys == nil {
		return finishedQuery()
	}
	return queryRelays(ctx, sys.Pool, defaultRelayLimits, directedOnce(urls, filter), label, false)
}

// fetchDirected is a one-shot query of each (relay, filter) target. It
// replaces the pool's BatchedQueryMany.
func fetchDirected(ctx context.Context, dfs []nostr.DirectedFilter, label string) *relayQuery {
	if sys == nil {
		return finishedQuery()
	}
	return queryRelays(ctx, sys.Pool, defaultRelayLimits, dfs, label, false)
}

// subscribeRelays is a live subscription of filter on every distinct relay in
// urls whose Done follows the rule. It replaces the pool's
// SubscribeManyNotifyEOSE.
func subscribeRelays(ctx context.Context, urls []string, filter nostr.Filter, label string) *relayQuery {
	if sys == nil {
		return finishedQuery()
	}
	return queryRelays(ctx, sys.Pool, defaultRelayLimits, directedOnce(urls, filter), label, true)
}

// directedOnce gives filter one target per distinct normalized relay.
func directedOnce(urls []string, filter nostr.Filter) []nostr.DirectedFilter {
	seen := make(map[string]bool, len(urls))
	dfs := make([]nostr.DirectedFilter, 0, len(urls))
	for _, u := range urls {
		nm := nostr.NormalizeURL(u)
		if nm == "" || seen[nm] {
			continue
		}
		seen[nm] = true
		dfs = append(dfs, nostr.DirectedFilter{Filter: filter, Relay: nm})
	}
	return dfs
}

// publishWithin publishes evt to every distinct relay in urls and reports one
// result per relay, ending by timeout: a relay that has not answered by then
// is reported with errRelayTimeout. The caller's first spelling of a URL is
// kept, and a URL that does not parse fails at once.
func publishWithin(ctx context.Context, pool *nostr.Pool, timeout time.Duration, urls []string, evt nostr.Event) <-chan nostr.PublishResult {
	var list, bad []string
	seen := make(map[string]bool, len(urls))
	for _, u := range urls {
		nm := nostr.NormalizeURL(u)
		if nm == "" {
			bad = append(bad, u)
			continue
		}
		if seen[nm] {
			continue
		}
		seen[nm] = true
		list = append(list, u)
	}

	out := make(chan nostr.PublishResult, len(list)+len(bad))
	for _, u := range bad {
		out <- nostr.PublishResult{Error: errors.New("invalid relay url"), RelayURL: u}
	}

	pctx, cancel := context.WithTimeout(ctx, timeout)
	pending := make(map[string]bool, len(list))
	for _, u := range list {
		pending[nostr.NormalizeURL(u)] = true
	}
	// the library buffers this channel for every relay, so the PublishMany
	// goroutines left behind at the cap never block
	inner := pool.PublishMany(pctx, list, evt)

	go func() {
		defer close(out)
		defer cancel()
		forward := func(res nostr.PublishResult) {
			nm := nostr.NormalizeURL(res.RelayURL)
			if pending[nm] {
				delete(pending, nm)
				out <- res
			}
		}
		for {
			select {
			case res, ok := <-inner:
				if !ok {
					return
				}
				forward(res)
			case <-pctx.Done():
				// take whatever already arrived, then time out the rest
			arrived:
				for {
					select {
					case res, ok := <-inner:
						if !ok {
							break arrived
						}
						forward(res)
					default:
						break arrived
					}
				}
				for _, u := range list {
					if pending[nostr.NormalizeURL(u)] {
						out <- nostr.PublishResult{Error: errRelayTimeout, RelayURL: u}
					}
				}
				return
			}
		}
	}()
	return out
}

// publishRelays publishes evt to urls under relayQueryTimeout.
func publishRelays(ctx context.Context, urls []string, evt nostr.Event) <-chan nostr.PublishResult {
	if sys == nil {
		out := make(chan nostr.PublishResult)
		close(out)
		return out
	}
	return publishWithin(ctx, sys.Pool, relayQueryTimeout, urls, evt)
}
