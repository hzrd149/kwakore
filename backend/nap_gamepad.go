package backend

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"

	"kwakore/backend/gamepad"
)

// NAP-GAMEPAD draft, napplet/naps PR #108, commit 0b28f77.
// The daemon owns the physical reader, subscriptions and focus arbitration.
const gamepadControlType = "__kwakore.gamepad"

type gamepadButton = gamepad.Button
type gamepadSlot = gamepad.Pad
type gamepadSample = gamepad.Snapshot

var gamepadRun = gamepad.Run

type gamepadSubscriber struct {
	gen      int
	last     string
	focusSeq uint64
	focused  bool
	order    uint64
}

type gamepadBroker struct {
	// Lock order: broker.mu before napSession.mu. Lifecycle removes the
	// subscription before taking napSession.mu, under dispatchMu exclusively.
	mu          sync.Mutex
	subs        map[*Instance]*gamepadSubscriber
	cancel      context.CancelFunc
	focus       *Instance
	epoch       uint64
	focusSerial uint64
	sample      gamepadSample
}

var napGamepads = gamepadBroker{}

func init() {
	handleNap(map[string]napHandler{
		"gamepad.subscribe":   napGamepadSubscribe,
		"gamepad.unsubscribe": napGamepadUnsubscribe,
	})
}

func validGamepadSubscription(c *napCall) bool {
	// These messages have no payload or correlator. Ignore malformed messages
	// silently, including attempts to send a sample through the NAP lane.
	var fields map[string]json.RawMessage
	return json.Unmarshal(c.raw, &fields) == nil && len(fields) == 1 && fields["type"] != nil
}

func napGamepadSubscribe(c *napCall) {
	if !validGamepadSubscription(c) {
		return
	}
	b := &napGamepads
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs == nil {
		b.subs = make(map[*Instance]*gamepadSubscriber)
		b.sample = gamepadSample{Available: true, Pads: []*gamepadSlot{}}
	}
	if sub := b.subs[c.ci]; sub != nil {
		sub.last = ""
	} else {
		b.subs[c.ci] = &gamepadSubscriber{gen: c.gen}
	}
	if b.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		b.cancel = cancel
		b.epoch++
		epoch := b.epoch
		run := gamepadRun
		safeGo(nil, "gamepad monitor", func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					b.accept(epoch, gamepadSample{Reason: "unavailable", Pads: []*gamepadSlot{}})
					panic(recovered) // safeGo logs the failure after input is neutralized
				}
			}()
			run(ctx, func(sample gamepadSample) { b.accept(epoch, sample) })
		})
	}
	b.controlLocked(c.ci, true)
	b.pushLocked(c.ci) // every accepted subscribe gets a complete snapshot
}

func napGamepadUnsubscribe(c *napCall) {
	if validGamepadSubscription(c) {
		napGamepads.remove(c.ci)
	}
}

func (b *gamepadBroker) controlLocked(ci *Instance, subscribed bool) {
	if sub := b.subs[ci]; sub != nil {
		ci.napPushGen(sub.gen, map[string]any{
			"type": gamepadControlType, "subscribed": subscribed,
		})
	}
}

func (b *gamepadBroker) remove(ci *Instance) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[ci] == nil {
		return
	}
	b.controlLocked(ci, false)
	delete(b.subs, ci)
	b.selectFocusLocked()
	for subscriber := range b.subs {
		b.pushLocked(subscriber)
	}
	if len(b.subs) == 0 {
		b.cancel()
		b.cancel = nil
		b.epoch++
		b.subs = nil
		b.sample = gamepadSample{}
	}
}

// update is a trusted host RPC, never a NAP route: the sandboxed napplet
// cannot call it, supply controller values, or grant itself input focus.
func (b *gamepadBroker) update(ci *Instance, params string) error {
	if len(params) > 64<<10 {
		return errors.New("gamepad update too large")
	}
	var msg struct {
		Gen      int    `json:"gen"`
		Focused  *bool  `json:"focused"`
		FocusSeq uint64 `json:"focusSeq"`
	}
	if err := json.Unmarshal([]byte(params), &msg); err != nil {
		return errors.New("invalid gamepad update")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	sub := b.subs[ci]
	if sub == nil || sub.gen != msg.Gen {
		return nil
	}
	if msg.Focused != nil && msg.FocusSeq > sub.focusSeq {
		sub.focusSeq = msg.FocusSeq
		sub.focused = *msg.Focused
		if sub.focused {
			b.focusSerial++
			sub.order = b.focusSerial
		}
		b.selectFocusLocked()
	}
	for subscriber := range b.subs {
		b.pushLocked(subscriber)
	}
	return nil
}

// Child processes can report focus transitions in different orders. Keep
// the latest candidate from each so a late blur cannot strand the new window
// without input. Each trusted host also masks snapshots against local focus.
func (b *gamepadBroker) selectFocusLocked() {
	b.focus = nil
	var newest uint64
	for ci, sub := range b.subs {
		if sub.focused && sub.order > newest {
			b.focus = ci
			newest = sub.order
		}
	}
}

func (b *gamepadBroker) accept(epoch uint64, sample gamepadSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel == nil || b.epoch != epoch {
		return
	}
	b.sample = sanitizeGamepadSample(sample)
	for ci := range b.subs {
		b.pushLocked(ci)
	}
}

func (b *gamepadBroker) pushLocked(ci *Instance) {
	sub := b.subs[ci]
	focused := b.sample.Available && b.focus == ci
	pads := b.sample.Pads
	if !focused {
		pads = neutralGamepads(pads)
	}
	env := struct {
		Type string `json:"type"`
		gamepadSample
		Focused bool `json:"focused"`
	}{"gamepad.state", gamepadSample{Available: b.sample.Available, Reason: b.sample.Reason, Pads: pads}, focused}
	raw, _ := json.Marshal(env)
	if string(raw) == sub.last {
		return
	}
	sub.last = string(raw)
	ci.napPushGen(sub.gen, env)
}

func neutralGamepads(pads []*gamepadSlot) []*gamepadSlot {
	out := make([]*gamepadSlot, len(pads))
	for i, pad := range pads {
		if pad == nil {
			continue
		}
		copy := *pad
		copy.Timestamp = 0
		copy.Axes = make([]float64, len(pad.Axes))
		copy.Buttons = make([]gamepadButton, len(pad.Buttons))
		out[i] = &copy
	}
	return out
}

func sanitizeGamepadSample(sample gamepadSample) gamepadSample {
	if !sample.Available {
		if sample.Reason != "blocked" {
			sample.Reason = "unavailable"
		}
		sample.Pads = []*gamepadSlot{}
		return sample
	}
	sample.Reason = ""
	sample.Pads = sample.Pads[:min(len(sample.Pads), 8)]
	if sample.Pads == nil {
		sample.Pads = []*gamepadSlot{}
	}
	for index, pad := range sample.Pads {
		if pad == nil || !pad.Connected {
			sample.Pads[index] = nil
			continue
		}
		pad.Index = index
		pad.ID = string([]rune(pad.ID)[:min(len([]rune(pad.ID)), 128)])
		pad.Mapping = strings.TrimSpace(pad.Mapping)
		pad.Mapping = string([]rune(pad.Mapping)[:min(len([]rune(pad.Mapping)), 16)])
		if math.IsNaN(pad.Timestamp) || math.IsInf(pad.Timestamp, 0) {
			pad.Timestamp = 0
		}
		pad.Timestamp = max(0, pad.Timestamp)
		pad.Axes = append([]float64{}, pad.Axes[:min(len(pad.Axes), 16)]...)
		pad.Buttons = append([]gamepadButton{}, pad.Buttons[:min(len(pad.Buttons), 32)]...)
		for i, axis := range pad.Axes {
			pad.Axes[i] = gamepadClamp(axis, -1)
		}
		for i := range pad.Buttons {
			pad.Buttons[i].Value = gamepadClamp(pad.Buttons[i].Value, 0)
		}
	}
	return sample
}

func gamepadClamp(value, lower float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return max(lower, min(1, value))
}
