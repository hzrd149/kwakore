//go:build linux

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"sync"
	"time"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

// The event hub fans service events out to subscribed connections. Publish
// never blocks: each subscriber has a bounded queue, and once it is full the
// subscriber's events are counted, not queued, until its writer has drained
// the queue and sent one events.overflow notice. A writer stalled past the
// 30-second write deadline loses its connection.

var (
	// maxEventSubscribers caps concurrent subscriptions (Busy past it),
	// leaving most of the 64 connection slots for requests.
	maxEventSubscribers = 16
	// eventQueueSize is each subscriber's queue length.
	eventQueueSize = 256
)

var errTooManySubscribers = errors.New("too many event subscribers")

type eventHub struct {
	mu   sync.Mutex
	subs map[*eventSub]struct{}
}

type eventSub struct {
	// types is the subscribed set; nil means every type
	types map[string]bool
	queue chan backend.ServiceEvent
	// dropped counts the events missed since the queue filled; guarded by
	// the hub's mu
	dropped int
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[*eventSub]struct{})}
}

// publish stamps e and queues it for every subscriber that wants its type.
// It never blocks, and its time stamps rise in delivery order.
func (h *eventHub) publish(e backend.ServiceEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e.Time = time.Now().UTC()
	for sub := range h.subs {
		if sub.types != nil && !sub.types[e.Type] {
			continue
		}
		if sub.dropped > 0 {
			sub.dropped++
			continue
		}
		select {
		case sub.queue <- e:
		default:
			sub.dropped = 1
		}
	}
}

// subscribe registers a subscriber for types (nil for all).
func (h *eventHub) subscribe(types []string) (*eventSub, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= maxEventSubscribers {
		return nil, errTooManySubscribers
	}
	sub := &eventSub{queue: make(chan backend.ServiceEvent, eventQueueSize)}
	if types != nil {
		sub.types = make(map[string]bool, len(types))
		for _, t := range types {
			sub.types[t] = true
		}
	}
	h.subs[sub] = struct{}{}
	return sub, nil
}

// remove drops a subscriber; removing one twice is harmless.
func (h *eventHub) remove(sub *eventSub) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

func (h *eventHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// next is the subscriber's next event: a queued one, else the overflow
// notice once the queue has drained after drops, else whatever arrives
// first. ok is false when ctx or done ends the wait.
func (h *eventHub) next(ctx context.Context, done <-chan struct{}, sub *eventSub) (backend.ServiceEvent, bool) {
	for {
		select {
		case e := <-sub.queue:
			return e, true
		default:
		}
		h.mu.Lock()
		// publish adds nothing while dropped is set, so an empty queue here
		// means every queued event before the drops was delivered
		if sub.dropped > 0 && len(sub.queue) == 0 {
			e := backend.ServiceEvent{Type: controlprotocol.EventOverflow, Time: time.Now().UTC(), Dropped: sub.dropped}
			sub.dropped = 0
			h.mu.Unlock()
			return e, true
		}
		pending := len(sub.queue) > 0
		h.mu.Unlock()
		if pending {
			continue
		}
		select {
		case e := <-sub.queue:
			return e, true
		case <-ctx.Done():
			return backend.ServiceEvent{}, false
		case <-done:
			return backend.ServiceEvent{}, false
		}
	}
}

// connStream is a connection's subscription, set by events.subscribe and
// read by the connection handler once the frame's response is written.
type connStream struct {
	mu  sync.Mutex
	sub *eventSub
}

func (c *connStream) subscription() *eventSub {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sub
}

type connStreamKey struct{}

// streamEvents writes the subscriber's events to conn as notifications until
// the peer goes (ctx), the service shuts down, or a write fails. It holds no
// lease, so Close never waits for it; Listener.Close closes conn, which ends
// a blocked write.
func (s *Service) streamEvents(ctx context.Context, conn *net.UnixConn, sub *eventSub) {
	defer s.events.remove(sub)
	var done <-chan struct{}
	if s.workContext != nil {
		done = s.workContext.Done()
	}
	for {
		e, ok := s.events.next(ctx, done, sub)
		if !ok {
			return
		}
		b, err := json.Marshal(controlprotocol.Notification{JSONRPC: "2.0", Method: controlprotocol.EventMethod, Params: e})
		if err != nil {
			continue
		}
		if writeResponse(conn, b) != nil {
			return
		}
	}
}

// subscribeEvents is the events.subscribe method.
func (s *Service) subscribeEvents(ctx context.Context, params json.RawMessage) (any, *controlprotocol.Error) {
	types, rpcErr := decodeEventTypes(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	done, beginErr := s.Begin()
	if beginErr != nil {
		return nil, controlprotocol.FixedError(controlprotocol.Closing)
	}
	defer done()
	stream, _ := ctx.Value(connStreamKey{}).(*connStream)
	if stream == nil || s.events == nil {
		return nil, controlprotocol.FixedError(controlprotocol.Unavailable)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.sub != nil {
		return nil, controlprotocol.FixedError(controlprotocol.Conflict)
	}
	sub, err := s.events.subscribe(types)
	if err != nil {
		return nil, controlprotocol.FixedError(controlprotocol.Busy)
	}
	stream.sub = sub
	subscribed := types
	if subscribed == nil {
		subscribed = controlprotocol.EventTypes()
	}
	return struct {
		Subscribed bool     `json:"subscribed"`
		Types      []string `json:"types"`
	}{true, subscribed}, nil
}

// decodeEventTypes reads {types?}: absent or [] subscribes to every type
// (nil); otherwise 1 to 10 distinct subscribable types, returned sorted.
func decodeEventTypes(params json.RawMessage) ([]string, *controlprotocol.Error) {
	invalid := controlprotocol.FixedError(controlprotocol.InvalidParams)
	if err := controlprotocol.ValidateNamedParams(params, "types"); err != nil {
		return nil, err
	}
	if len(params) == 0 {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return nil, invalid
	}
	raw, ok := fields["types"]
	if !ok {
		return nil, nil
	}
	var types []string
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &types) != nil {
		return nil, invalid
	}
	if len(types) == 0 {
		return nil, nil
	}
	known := controlprotocol.EventTypes()
	if len(types) > len(known) {
		return nil, invalid
	}
	for i, t := range types {
		if !slices.Contains(known, t) || slices.Contains(types[:i], t) {
			return nil, invalid
		}
	}
	slices.Sort(types)
	return types, nil
}
