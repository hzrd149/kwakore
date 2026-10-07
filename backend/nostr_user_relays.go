package backend

import (
	"context"
	"slices"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/sdk"
)

// The logged-in user's own relay list (NIP-65, kind 10002), loaded in the
// background on login and kept fresh while the session lasts. Discovery asks
// its write relays (when enabled in Settings) and NAP-OUTBOX reads it from
// memory instead of looking it up on every request. It is persisted, so the
// next start has it before any relay answers.

// userRelayList is the user's NIP-65 list as last seen. A zero CreatedAt with
// LoadedAt set means it was looked for and none was found.
type userRelayList struct {
	Pubkey    nostr.PubKey    `json:"pubkey"`
	Write     []string        `json:"write"`
	Read      []string        `json:"read"`
	CreatedAt nostr.Timestamp `json:"created_at"`
	LoadedAt  time.Time       `json:"loaded_at"`
}

// userRelayWait is how long a discovery run waits for a first login's relay
// list before going ahead without it.
const userRelayWait = 3 * time.Second

var ur struct {
	mu     sync.Mutex
	pubkey nostr.PubKey // whose list is being followed
	list   *userRelayList
	cancel context.CancelFunc
	// ready is closed once a list (persisted or fetched) is known
	ready     chan struct{}
	readyOnce func()
	// discovered is the user write set the last discovery run used, so a
	// change only reruns discovery when it would ask different relays
	discovered []string
}

// userRelayTasks tracks the goroutines following a list, so tests can wait
// for them to wind down after stopUserRelays.
var userRelayTasks sync.WaitGroup

// rediscover reruns discovery; a var so tests can watch for it.
var rediscover = func() {
	if sys != nil {
		go Discover()
	}
}

// relayListFromEvent reads a kind 10002's "r" tags: no marker is both
// directions, "read" or "write" just that one.
func relayListFromEvent(evt nostr.Event) (write, read []string) {
	write, read = []string{}, []string{}
	for _, tag := range evt.Tags {
		if len(tag) < 2 || tag[0] != "r" {
			continue
		}
		u := nostr.NormalizeURL(tag[1])
		if u == "" {
			continue
		}
		marker := ""
		if len(tag) > 2 {
			marker = tag[2]
		}
		if marker != "read" {
			write = nostr.AppendUnique(write, u)
		}
		if marker != "write" {
			read = nostr.AppendUnique(read, u)
		}
	}
	return write, read
}

// startUserRelays begins following pk's relay list: the persisted one at
// once, then what the relays have, then any update while the session lasts.
// It replaces whatever was followed before.
func startUserRelays(pk nostr.PubKey) {
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})

	ur.mu.Lock()
	if ur.cancel != nil {
		ur.cancel()
	}
	ur.cancel = cancel
	ur.pubkey = pk
	ur.list = nil
	ur.ready = ready
	ur.readyOnce = sync.OnceFunc(func() { close(ready) })
	ur.discovered = nil
	stateMu.Lock()
	if p := state.UserRelays; p != nil && p.Pubkey == pk {
		cp := *p
		ur.list = &cp
		ur.readyOnce()
	}
	stateMu.Unlock()
	markReady := ur.readyOnce
	ur.mu.Unlock()
	notifyState()

	s := sys
	if s == nil {
		markReady()
		return
	}
	userRelayTasks.Add(1)
	go func() {
		defer userRelayTasks.Done()
		defer markReady()
		fctx, fcancel := context.WithTimeout(ctx, 15*time.Second)
		rl := s.FetchRelayList(fctx, pk)
		fcancel()
		if ctx.Err() != nil {
			return
		}
		if rl.Event != nil {
			applyUserRelayEvent(*rl.Event)
		} else {
			markUserRelaysMissing(pk)
		}
		markReady()
		log.Info().Strs("write", UserWriteRelays()).Strs("read", UserReadRelays()).Msg("user relay list loaded")

		watchUserRelays(ctx, s, pk)
	}()
}

// stopUserRelays forgets the followed list, in memory and on disk: the user
// logged out.
func stopUserRelays() {
	ur.mu.Lock()
	if ur.cancel != nil {
		ur.cancel()
		ur.cancel = nil
	}
	ur.pubkey = nostr.ZeroPK
	ur.list = nil
	ur.ready = nil
	ur.discovered = nil
	ur.mu.Unlock()

	stateMu.Lock()
	if state.UserRelays != nil {
		state.UserRelays = nil
		saveState()
	}
	stateMu.Unlock()
}

// watchUserRelays listens for new versions of pk's list on the relay-list
// relays and the user's own write relays until ctx ends.
func watchUserRelays(ctx context.Context, s *sdk.System, pk nostr.PubKey) {
	urls := normalizedUnique(append(append([]string(nil), s.RelayListRelays.URLs...), UserWriteRelays()...))
	filter := nostr.Filter{
		Kinds:   []nostr.Kind{nostr.KindRelayListMetadata},
		Authors: []nostr.PubKey{pk},
		Limit:   1,
	}
	for _, u := range urls {
		userRelayTasks.Add(1)
		go func() {
			defer userRelayTasks.Done()
			listenUserRelays(ctx, s, u, filter)
		}()
	}
}

// listenUserRelays keeps one relay's subscription open, reconnecting with
// backoff as the bunker listener does.
func listenUserRelays(ctx context.Context, s *sdk.System, url string, filter nostr.Filter) {
	backoff := time.Second
	for ctx.Err() == nil {
		relay, err := s.Pool.EnsureRelay(url)
		if err == nil {
			var sub *nostr.Subscription
			sub, err = relay.Subscribe(ctx, filter, nostr.SubscriptionOptions{Label: "kwakore-user-relays"})
			if err == nil {
				backoff = time.Second
				readUserRelays(ctx, s, sub)
			}
		}
		if err != nil {
			log.Debug().Err(err).Str("relay", url).Msg("relay list relay unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(10*time.Minute, backoff*2)
	}
}

func readUserRelays(ctx context.Context, s *sdk.System, sub *nostr.Subscription) {
	defer sub.Unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.ClosedReason:
			return
		case evt, ok := <-sub.Events:
			if !ok {
				return
			}
			if applyUserRelayEvent(evt) {
				// so the sdk's lookups see it too
				if _, err := s.Store.ReplaceEvent(evt); err != nil {
					log.Debug().Err(err).Msg("failed to store relay list")
				}
				dropCached(s.RelayListCache, evt.PubKey)
			}
		}
	}
}

// applyUserRelayEvent takes a kind 10002 from the followed user if it is
// newer than the list held, and says whether it was.
func applyUserRelayEvent(evt nostr.Event) bool {
	if evt.Kind != nostr.KindRelayListMetadata {
		return false
	}
	write, read := relayListFromEvent(evt)

	ur.mu.Lock()
	if ur.pubkey == nostr.ZeroPK || evt.PubKey != ur.pubkey {
		ur.mu.Unlock()
		return false
	}
	if ur.list != nil && ur.list.CreatedAt != 0 && evt.CreatedAt <= ur.list.CreatedAt {
		ur.mu.Unlock()
		return false
	}
	ur.list = &userRelayList{
		Pubkey:    evt.PubKey,
		Write:     write,
		Read:      read,
		CreatedAt: evt.CreatedAt,
		LoadedAt:  time.Now(),
	}
	settled(ur.list)
	return true
}

// markUserRelaysMissing records that pk has no relay list, unless one is
// already held: an empty answer may only mean the relays were unreachable.
func markUserRelaysMissing(pk nostr.PubKey) {
	ur.mu.Lock()
	if ur.pubkey != pk || ur.list != nil {
		ur.mu.Unlock()
		return
	}
	ur.list = &userRelayList{Pubkey: pk, Write: []string{}, Read: []string{}, LoadedAt: time.Now()}
	settled(ur.list)
}

// settled persists a new list and reruns discovery if it changes what
// discovery asks. ur.mu must be held; it is released.
func settled(list *userRelayList) {
	cp := *list
	ran := ur.discovered != nil
	changed := ran && !slices.Equal(ur.discovered, cp.Write)
	ur.mu.Unlock()

	stateMu.Lock()
	state.UserRelays = &cp
	saveState()
	stateMu.Unlock()
	notifyState()

	if changed && DiscoverOnUserRelays() {
		rediscover()
	}
}

// userRelays is the logged-in user's list, if one has been loaded.
func userRelays() (userRelayList, bool) {
	pk, ok := currentUser()
	if !ok {
		return userRelayList{}, false
	}
	ur.mu.Lock()
	defer ur.mu.Unlock()
	if ur.list == nil || ur.list.Pubkey != pk {
		return userRelayList{}, false
	}
	return *ur.list, true
}

// UserWriteRelays are the logged-in user's NIP-65 write (outbox) relays.
func UserWriteRelays() []string {
	l, _ := userRelays()
	return append([]string(nil), l.Write...)
}

// UserReadRelays are the logged-in user's NIP-65 read (inbox) relays.
func UserReadRelays() []string {
	l, _ := userRelays()
	return append([]string(nil), l.Read...)
}

// waitUserRelays waits, up to d, for the followed list to be known.
func waitUserRelays(d time.Duration) {
	ur.mu.Lock()
	ready := ur.ready
	ur.mu.Unlock()
	if ready == nil {
		return
	}
	select {
	case <-ready:
	case <-time.After(d):
	}
}

// discoveryRelays are where discovery looks: the Settings relays and, when
// enabled, the user's write relays. It notes the user relays it used.
func discoveryRelays() []string {
	urls := normalizedUnique(Relays())
	var mine []string
	if DiscoverOnUserRelays() {
		mine = UserWriteRelays()
		for _, u := range mine {
			urls = nostr.AppendUnique(urls, u)
		}
	}
	ur.mu.Lock()
	if mine == nil {
		mine = []string{}
	}
	ur.discovered = mine
	ur.mu.Unlock()
	return urls
}

// outboxFallback is where NAP-OUTBOX asks when nothing better is known: the
// user's own relays, or the Settings relays without them.
func outboxFallback() []string {
	l, ok := userRelays()
	if !ok {
		return Relays()
	}
	urls := normalizedUnique(append(append([]string(nil), l.Read...), l.Write...))
	if len(urls) == 0 {
		return Relays()
	}
	return urls
}
