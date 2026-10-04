package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"verdana/backend/webview"
)

// The napplet runtime's Go half. A napplet window's host page (webview's
// napplet-host.js) forwards every NAP envelope its sandboxed frame posts as a
// nap.msg rpc; this file decides what each one means. Replies and everything
// the launcher has to say unprompted (relay events, inc events, theme and
// identity changes) go back the same way: a __nap_push eval, which the host
// page re-posts into the frame.
//
// A session starts only when the host page says so (the nap.start rpc, made
// before the napplet's frame exists); nothing the frame posts can start, reset
// or replay one. The napplet learns its domains from window.napplet itself
// (NIP-5D presence detection): there is no handshake.
//
// Envelopes are handled one at a time, in the order the napplet sent them
// (a subscribe is never overtaken by its own close). A handler that has to
// wait — on the network, on the user — does so in its own goroutine, tied to
// the session, and replies when it is done.

// napDomains are the NAP domains the launcher implements, and so the ones
// the shim installs window.napplet.<domain> for. It is the launcher's policy,
// never the napplet's R/O tags (WEB-NAPPLET.md forbids gating on those).
var napDomains = []string{
	"relay", "identity", "storage", "resource", "common",
	"theme", "inc", "intent", "link", "upload", "outbox", "media",
	"config", "notify",
}

// napSession is what one napplet window has going: the subscriptions and
// channels a reset or a close has to tear down.
type napSession struct {
	mu sync.Mutex

	// dispatchMu makes a handler's synchronous part atomic with the session
	// lifecycle. napDispatch holds it shared from its gen check until the
	// handler returns; napStart, napReset and napClosed take it exclusively
	// before they tear the session down. nap.start runs on the host page's
	// rpc goroutine, not on the worker, so without it a handler that already
	// passed the gen check could write its topic, subscription or channel
	// into the next session's fresh state (a stale inc.subscribe would then
	// count as the new document being ready for an intent). Work a handler
	// hands to c.async is outside the lock: it runs on the session context,
	// which the teardown cancels, and its replies are gen-checked. Lock
	// order: dispatchMu before mu.
	dispatchMu sync.RWMutex

	// established flips when the trusted host page starts a session
	// (nap.start), before it creates the napplet's frame. Every envelope
	// before it is dropped, and the frame has no way to set it: it can only
	// post envelopes, which reach Go as nap.msg.
	established bool

	// gen counts sessions in this window. nap.start and nap.reset (a dev
	// reload or a replaced document) both end the current one and move gen
	// on, and a late answer for the old one must not reach the new
	// document.
	gen    int
	ctx    context.Context
	cancel context.CancelFunc

	// relay and outbox subscriptions by subId (outbox ids carry a prefix,
	// outboxSubKey); each entry is owned by the pump it started
	subs map[string]*napSub
	// inc topics this napplet listens on (a set: the shim subscribes once per
	// handler but unsubscribes once per topic)
	topics map[string]bool
	// open resource requests by id, for resource.cancel
	fetches map[string]*resourceFetch
	// uploads are scoped to one document/session. A reload cancels active
	// network work and makes its upload ids unreachable to the new document.
	uploads map[string]*napUploadStatus
	// the session's answers to its standing questions ("may it fetch from
	// the web", "may it read encrypted messages"): asked once per session.
	// grants holds only what the user (or a rule) answered; asking is the
	// question in flight per permission, which concurrent requests wait on
	// instead of asking again (sessionGrant). Both are guarded by mu.
	grants map[Permission]bool
	asking map[Permission]*grantQuestion
	// notifications are the OS notifications created by this document. The
	// handles are session-owned so a reload or closed iframe dismisses them.
	notifications  map[string]NotificationHandle
	notifySeq      int
	notifyChannels map[string]notificationChannel
	notifyBadge    uint
	// media sessions by canonical id; shell-owned ones hold a player that a
	// reset stops. mediaSeq numbers ids and is never reset, so an id from an
	// old document never names a session in a new one.
	media    map[string]*mediaSession
	mediaSeq int
	// configSubscribed is config.subscribe having been sent: the window
	// gets config.values pushes. (config.openSettings and notify.send are
	// rate-limited by limits, which no reset touches.)
	configSubscribed bool

	// limits are the window's rate limits (nap_limits.go). Created once
	// with the session and never reset: a reload must not refill them.
	limits *napLimiter

	// queue serializes envelopes; started lazily by the first one
	queue chan *napCall
	once  sync.Once

	// beforeHandler, when set, runs on the worker after the gen check and
	// right before the handler: a test hook for parking a handler mid
	// dispatch. Set it before the session's first envelope (the worker is
	// started by that one); it is always nil outside tests.
	beforeHandler func(c *napCall)
	// pumpHook, when set, runs in place of a relay or outbox subscription's
	// pump: a test hook for a subscription that stays open with no relays,
	// or one whose exit is held back. Set it like beforeHandler; it is
	// always nil outside tests.
	pumpHook func(ctx context.Context, subID string)
}

// napSub is one relay or outbox subscription. The pointer is its identity:
// a pump that ends removes its own entry only, never a later subscription
// that reused the id after a close (the same pattern as resourceTrack).
type napSub struct {
	cancel context.CancelFunc
	// done is closed once the pump has stopped and its cleanup has run
	done chan struct{}
}

func newNapSession() *napSession {
	s := &napSession{limits: newNapLimiter()}
	s.resetLocked()
	return s
}

// resetLocked tears the session's state down and starts a fresh generation.
// The caller holds s.mu (or owns s exclusively).
func (s *napSession) resetLocked() {
	if s.cancel != nil {
		s.cancel()
	}
	for _, sub := range s.subs {
		sub.cancel()
	}
	for _, fetch := range s.fetches {
		fetch.cancel()
	}
	s.gen++
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.subs = make(map[string]*napSub)
	s.topics = make(map[string]bool)
	s.fetches = make(map[string]*resourceFetch)
	for _, upload := range s.uploads {
		if upload.cancel != nil {
			upload.cancel()
		}
	}
	s.uploads = make(map[string]*napUploadStatus)
	s.grants = make(map[Permission]bool)
	// a question still in flight belongs to the old session: its asker is
	// dismissed by the cancelled context, and its waiters see the new gen
	s.asking = make(map[Permission]*grantQuestion)
	for _, n := range s.notifications {
		_ = n.Dismiss()
	}
	s.notifications = make(map[string]NotificationHandle)
	s.notifyChannels = make(map[string]notificationChannel)
	s.notifyBadge = 0
	for _, ms := range s.media {
		ms.stop()
	}
	s.media = make(map[string]*mediaSession)
	s.configSubscribed = false
	s.established = false
}

// napCall is one envelope from the napplet.
type napCall struct {
	ci  *Instance
	gen int
	ctx context.Context
	// route is the declared route napDispatch found for Type (nap_route.go);
	// nil until then
	route *napRoute

	Type string
	// ID is echoed back verbatim: the shim's ids are uuid strings, but
	// nothing here needs to know that. napEnqueue only lets through a JSON
	// string or number of at most napMaxIDBytes bytes (D-11).
	ID json.RawMessage
	// SubID is the envelope's exact "subId" when it is a valid string: what
	// a subscription's failure echoes
	SubID string
	raw   json.RawMessage
	// received is when napEnqueue read the envelope (napNow)
	received time.Time

	// answered flips on the request's first answer (reply, replyAs, failWith
	// or drop). Exactly one answer reaches the napplet: a later one is
	// dropped, and a request left unanswered is failed for its handler once
	// that has returned (napDispatch, c.async). A napCall is only ever passed
	// by pointer, so the flags are shared by everyone holding the call.
	answered atomic.Bool
	// handedOff is set by c.async: the answer is the async closure's job now
	handedOff atomic.Bool
	// approved is set once the call passed its declared gate; the gated sinks
	// refuse a call without it
	approved atomic.Bool
}

// napHandler handles one envelope type. It runs on the session's queue, so
// it must not block: anything slow goes through c.async. Its synchronous
// part also runs under dispatchMu.RLock, so napStart and napClosed wait for
// it: a handler that prompts or fetches inline would stall the host page's
// nap.start and WindowClosed (on Android, the main thread in onDestroy).
//
// Handlers are registered through handleNap (nap_route.go), which joins each
// with its declared route: the gate it needs and the shape it fails in.
type napHandler func(c *napCall)

func (c *napCall) decode(v any) error { return json.Unmarshal(c.raw, v) }

// envelope builds a message of the given type, carrying this call's id.
func (c *napCall) envelope(typ string, fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		out[k] = v
	}
	out["type"] = typ
	if len(c.ID) > 0 {
		out["id"] = c.ID
	}
	return out
}

// reply answers with <type>.result.
func (c *napCall) reply(fields map[string]any) {
	if !c.answered.CompareAndSwap(false, true) {
		c.secondReply(c.Type + ".result")
		return
	}
	c.send(c.Type+".result", fields)
}

// replyAs answers with an explicit type (the resource and relay error types,
// a subscription's closed push).
func (c *napCall) replyAs(typ string, fields map[string]any) {
	if !c.answered.CompareAndSwap(false, true) {
		c.secondReply(typ)
		return
	}
	c.send(typ, fields)
}

// send pushes an answer that has already claimed the call.
func (c *napCall) send(typ string, fields map[string]any) {
	c.ci.napPushGen(c.gen, c.envelope(typ, fields))
}

func (c *napCall) secondReply(typ string) {
	napSampled().Warn().Str("type", c.Type).Str("reply", typ).Msg("second reply to a NAP request dropped")
}

// drop marks the call answered without sending anything: a cancelled
// request whose late answer NAP-RESOURCE says to drop.
func (c *napCall) drop() { c.answered.Store(true) }

// async runs fn off the queue, on the session's context: a reset or a closed
// window cancels it, and whatever it replies after that is dropped. A request
// fn leaves unanswered is failed once fn returns, unless its route is
// reply-less or a subscription.
func (c *napCall) async(fn func(ctx context.Context)) {
	c.handedOff.Store(true)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("type", c.Type).Msg("NAP handler panicked")
				c.failWith(napErrInternal)
			}
		}()
		fn(c.ctx)
		if c.route != nil && c.route.autoFails() && !c.answered.Load() {
			c.failWith(napErrInternal)
		}
	}()
}

// safeGo is the only other way NAP code may start a goroutine (02-04's guard
// enforces it): fn runs with a recover that logs the panic and, when the
// goroutine works for a call, fails that call (a no-op once it is answered).
// Goroutines that are not a call's answer, such as a push to a peer, pass nil.
func safeGo(c *napCall, what string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("in", what).Msg("NAP goroutine panicked")
				if c != nil {
					c.failWith(napErrInternal)
				}
			}
		}()
		fn()
	}()
}

// napBurst bounds the log lines a napplet can cause at will (unknown types,
// second replies, publish failures): a flood must not become a log flood.
var napBurst = &zerolog.BurstSampler{Burst: 5, Period: time.Minute}

// napSampled is the logger for those lines.
func napSampled() *zerolog.Logger {
	l := log.Sample(napBurst)
	return &l
}

// ─── pushing into the napplet ────────────────────────────────────

// napPush sends envelopes to the napplet in this window, in one eval.
func (ci *Instance) napPush(envs ...any) {
	if ci.nap == nil || len(envs) == 0 {
		return
	}
	ci.nap.mu.Lock()
	gen := ci.nap.gen
	ci.nap.mu.Unlock()
	ci.napPushGen(gen, envs...)
}

// napPushGen pushes only if the session is still the one gen names, and
// reports whether it did.
func (ci *Instance) napPushGen(gen int, envs ...any) bool {
	if ci.nap == nil || len(envs) == 0 {
		return false
	}
	ci.nap.mu.Lock()
	live := ci.nap.gen == gen && ci.nap.established
	ci.nap.mu.Unlock()
	if !live {
		return false
	}
	var payload any = envs
	if len(envs) == 1 {
		payload = envs[0]
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("could not encode a NAP push")
		return false
	}
	// the check above and the send are not atomic: nap.start can run in
	// between, and the host page can have the next session's frame up by the
	// time this lands. So the push names its session, and the host page drops
	// any push for a session other than the one nap.start gave it.
	ci.eval("window.__nap_push && window.__nap_push(" + strconv.Itoa(gen) + ", " + jsString(string(raw)) + ")")
	return true
}

// ─── the rpcs the host page makes ────────────────────────────────

// napRPC answers the host page. Nothing else is reachable from a napplet
// window: see bridgeRPC. The lifecycle rpcs (nap.boot, nap.start, nap.loaded,
// nap.reset) come only from the host page's own binding, never from the
// napplet's frame, which can reach Go only through the host page, and only as
// nap.msg. nap.reset ends the session on a dev reload and whenever the host
// page finds its frame's document replaced (napReset).
func napRPC(ci *Instance, method, params string) (any, error) {
	switch method {
	case "nap.boot":
		return nappletBoot(ci)
	case "nap.start":
		gen, err := ci.napStart()
		if err != nil {
			return nil, err
		}
		// the host page tags its frame with this and drops pushes for any
		// other session (napPushGen)
		return map[string]any{"gen": gen}, nil
	case "nap.loaded":
		ci.napLoaded()
		return nil, nil
	case "nap.msg":
		ci.napEnqueue(params)
		return nil, nil
	case "nap.reset":
		ci.napReset()
		return nil, nil
	case "nap.openSettings":
		// the gear in the host page's chrome, never the napplet: its frame
		// cannot reach these rpcs
		return nil, openSettings(ci.napp.ID, "")
	}
	return nil, fmt.Errorf("unsupported method: %s", method)
}

// napEnqueue reads one envelope and puts it on the session's queue. Go is the
// authority on what a napplet may send (D-09, D-10, D-11): it checks every
// bound itself after decoding, whatever the host page did. What cannot be
// answered (not an envelope, an unknown type, a bad id, a missing correlator)
// is dropped without a word: unknown input must never tell a napplet anything
// (NIP-5D). What can be answered but is refused (colliding keys, too large,
// over the envelope bucket, a full queue) gets its route's failure.
//
// It runs inline on the window's reader, which also carries prompt answers
// (HandleMessage), so nothing in it may block: a flood that fills the queue
// is answered rate-limited instead of waiting for room (D-16).
func (ci *Instance) napEnqueue(params string) {
	s := ci.nap
	if s == nil {
		return
	}
	if len(params) > napMaxParams {
		napSampled().Warn().Str("napplet", ci.napp.ID).Int("len", len(params)).Msg("dropping an oversized NAP message")
		return
	}
	// every envelope counts against the window's bucket, whatever becomes of
	// it (D-14): the token is taken before anything is parsed, so an envelope
	// that is refused (colliding keys, too large) or dropped (unknown type,
	// bad id, no correlator) pays for itself too (WR-05). Over the bucket, a
	// request that can be answered is answered rate-limited once its head is
	// known; everything else is dropped.
	overRate := !s.limits.allowEnvelope()
	// the host page sends the envelope as a JSON string (rpc params are
	// JSON); accept the object form too
	raw := json.RawMessage(params)
	var str string
	if json.Unmarshal(raw, &str) == nil {
		raw = json.RawMessage(str)
	}
	if len(raw) > napMaxEnvelope {
		napSampled().Warn().Str("napplet", ci.napp.ID).Int("len", len(raw)).Msg("dropping an oversized NAP envelope")
		return
	}
	head, ok := parseNapHead(raw)
	if !ok {
		return
	}
	route := napRoutes[head.typ]
	if route == nil {
		// unknown types are dropped silently, never answered (NIP-5D)
		napSampled().Debug().Str("type", head.typ).Str("napplet", ci.napp.ID).Msg("ignoring unknown NAP message")
		return
	}
	// an id that is not a short string or number cannot be echoed (D-11),
	// and a request whose answer needs a correlator it lacks cannot be
	// answered at all (A16)
	if head.badID {
		return
	}
	switch route.correlator() {
	case "id":
		if head.id == nil {
			return
		}
	case "subId":
		if head.subID == "" {
			return
		}
	}

	s.once.Do(func() {
		s.queue = make(chan *napCall, napQueueSlots)
		go ci.napWorker()
	})

	s.mu.Lock()
	c := &napCall{
		ci: ci, gen: s.gen, ctx: s.ctx, route: route,
		Type: head.typ, ID: head.id, SubID: head.subID, raw: raw, received: napNow(),
	}
	s.mu.Unlock()

	// over the envelope bucket (charged above); a reply-less type is simply
	// dropped
	if overRate {
		napSampled().Debug().Str("type", c.Type).Str("napplet", ci.napp.ID).Msg("NAP envelope over the window's rate limit")
		c.failWith(napErrRateLimited)
		return
	}
	// keys a case-insensitive decode would merge could make the envelope one
	// type here and another to its handler (D-10)
	if head.collide {
		c.failWith(napErrInvalid)
		return
	}
	if len(raw) > route.maxBytes() {
		c.failWith(napErrTooLarge)
		return
	}

	select {
	case <-ci.gone:
		return
	default:
	}
	select {
	case s.queue <- c:
	default:
		napSampled().Warn().Str("type", c.Type).Str("napplet", ci.napp.ID).Msg("NAP queue full, answering rate-limited")
		c.failWith(napErrRateLimited)
	}
}

func (ci *Instance) napWorker() {
	s := ci.nap
	for {
		select {
		case <-ci.gone:
			return
		case c := <-s.queue:
			ci.napDispatch(c)
		}
	}
}

// napDispatch handles one envelope. There is no lifecycle type: a session
// starts only through nap.start, so a frame-sent shell.ready (or anything
// else the shim does not define) is an unknown type like any other, dropped
// silently (NIP-5D).
func (ci *Instance) napDispatch(c *napCall) {
	s := ci.nap
	// held until the handler returns, so no session starts or ends between
	// the gen check and the handler's writes to session state
	s.dispatchMu.RLock()
	defer s.dispatchMu.RUnlock()
	s.mu.Lock()
	ok := s.established
	// the call was read under an older session: let it go
	stale := c.gen != s.gen
	c.gen, c.ctx = s.gen, s.ctx
	s.mu.Unlock()
	if !ok || stale {
		return
	}

	// napEnqueue found the route; a call made by hand (tests) looks it up
	route := c.route
	if route == nil {
		route = napRoutes[c.Type]
	}
	if route == nil {
		// unknown types are dropped silently, never answered
		napSampled().Debug().Str("type", c.Type).Str("napplet", ci.napp.ID).Msg("ignoring unknown NAP message")
		return
	}
	c.route = route
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("type", c.Type).Msg("NAP handler panicked")
			c.failWith(napErrInternal)
		}
	}()
	// the route's category bucket (D-14), charged before anything else
	// about the request is looked at; a reply-less type over it is dropped
	if !s.limits.allow(route.limit, 1) {
		napSampled().Debug().Str("type", c.Type).Str("napplet", ci.napp.ID).Msg("NAP request over its category rate limit")
		c.failWith(napErrRateLimited)
		return
	}
	// the gate step (D-04): a stored denial answers in the route's denial
	// shape and the handler never runs. It only reads what the user already
	// decided, never asks: this runs under dispatchMu.
	if ci.napDeniedUpFront(route.gate) {
		c.failWith(napErrDenied)
		return
	}
	if s.beforeHandler != nil {
		s.beforeHandler(c)
	}
	route.h(c)
	// a handler that returned without answering, and without handing the
	// answer to c.async, forgot it: the napplet gets the failure instead of
	// waiting (relay.* has no shim timeout at all)
	if route.autoFails() && !c.answered.Load() && !c.handedOff.Load() {
		c.failWith(napErrInternal)
	}
}

// napDeniedUpFront says whether the user already refused what a Session or
// PerCall route needs: a stored "deny" rule for the napp, or this session's
// answer of no. Open routes need nothing, and Dynamic ones depend on the
// payload, so they are never refused here.
func (ci *Instance) napDeniedUpFront(g napGate) bool {
	if g.kind != napGateSession && g.kind != napGatePerCall {
		return false
	}
	if rule, ok := lookupRule(RuleKey{Napp: ci.napp.ID, Permission: g.perm}); ok && rule.Decision == DecisionDeny {
		return true
	}
	s := ci.nap
	s.mu.Lock()
	granted, decided := s.grants[g.perm]
	s.mu.Unlock()
	return decided && !granted
}

// napStart answers nap.start: the host page is about to create a fresh frame
// for the napplet's document, and this opens the session that document talks
// to. Whatever the window had before is torn down first. The generation bump
// is what keeps the outgoing document out (D-07): an envelope it queued
// before the restart carries the old gen and napDispatch drops it, and a late
// answer for the old session is dropped by napPushGen, or, when it already
// passed that check, by the host page, which knows the new session's gen from
// this answer and ignores pushes tagged with any other.
func (ci *Instance) napStart() (int, error) {
	s := ci.nap
	if s == nil {
		return 0, errors.New("not a napplet window")
	}
	// waits out a handler the worker is inside of (napSession.dispatchMu)
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	s.mu.Lock()
	ci.napTeardownLocked("napplet reset")
	s.established = true
	gen := s.gen
	s.mu.Unlock()
	log.Info().Str("napplet", ci.napp.ID).Str("instance", ci.instance).Int("gen", gen).Msg("napplet session started")
	return gen, nil
}

// napLoaded answers nap.loaded and pushes notify.controls. The host page
// sends it once per frame, on the frame's first load: the trigger is the
// load event because the upstream notify shim keeps no last value, so a
// push before the napplet's top-level scripts registered onControls would be
// lost, and load fires after them. It is only that trigger, never a session
// start.
//
// A document that replaces the frame's own (a reload, a navigation) is never
// loaded into this session: the host page removes that frame, ends the
// session with nap.reset and boots a new frame with a new session (D-01), so
// every document still gets exactly one push. Go itself answers every
// nap.loaded it gets while a session is established.
func (ci *Instance) napLoaded() {
	s := ci.nap
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.established {
		s.mu.Unlock()
		return
	}
	gen := s.gen
	s.mu.Unlock()
	ci.napPushGen(gen, map[string]any{"type": "notify.controls", "controls": host.NotificationControls()})
}

// napReset answers nap.reset: it ends the session the host page's frame
// belonged to. The host page sends it in two cases: a dev reload (new bytes
// are coming, dev.go) and a replaced document (D-02): the frame loaded a
// second time, so something replaced the document it was booted with, and
// the host page has already dropped that frame. Either way the teardown
// happens here, before any new document exists: subscriptions, fetches,
// uploads, prompts (owned by the session context), inc topics and grants
// end, and the gen moves on, so a call the old document queued is stale.
// The host page's next nap.start opens the new session; until then
// established is false, and every envelope is dropped and nothing is
// pushed.
func (ci *Instance) napReset() {
	if ci.nap == nil {
		return
	}
	ci.nap.dispatchMu.Lock()
	defer ci.nap.dispatchMu.Unlock()
	ci.nap.mu.Lock()
	ci.napTeardownLocked("napplet reset")
	gen := ci.nap.gen
	ci.nap.mu.Unlock()
	log.Info().Str("napplet", ci.napp.ID).Str("instance", ci.instance).Int("gen", gen).Msg("napplet session reset")
}

// napTeardownLocked ends the current session: subscriptions, resource
// fetches, inc topics and channels. The caller holds ci.nap.mu.
func (ci *Instance) napTeardownLocked(reason string) {
	incForget(ci, reason)
	ci.nap.resetLocked()
}

// napClosed is WindowClosed's part for a napplet window.
func (ci *Instance) napClosed() {
	if ci.nap == nil {
		return
	}
	ci.nap.dispatchMu.Lock()
	defer ci.nap.dispatchMu.Unlock()
	ci.nap.mu.Lock()
	ci.napTeardownLocked("peer destroyed")
	ci.nap.mu.Unlock()
}

// ─── the document ────────────────────────────────────────────────

// nappletBoot answers nap.boot: the napplet's verified HTML, wrapped for the
// sandboxed frame.
func nappletBoot(ci *Instance) (any, error) {
	html, err := nappletDocumentForInstance(ci)
	if err != nil {
		log.Error().Err(err).Str("napplet", ci.napp.ID).Msg("napplet will not boot")
		return nil, err
	}
	doc, err := buildSrcdoc(html, napDomains)
	if err != nil {
		return nil, err
	}
	return map[string]any{"srcdoc": doc, "title": ci.napp.Label()}, nil
}

func nappletDocumentForInstance(ci *Instance) ([]byte, error) {
	if ci.previewDocument != nil {
		return append([]byte(nil), ci.previewDocument...), nil
	}
	return nappletDocument(ci.napp)
}

// nappletDocument reads the napplet's one file: from a dev napplet's folder,
// or from the install dir, re-checked against the event's hash right before
// it runs, so bytes changed on disk after install never execute.
func nappletDocument(n Napp) ([]byte, error) {
	if d := devLookup(n.ID); d != nil {
		if d.dir == "" {
			return nil, errors.New("dev napplets load from a folder")
		}
		return os.ReadFile(filepath.Join(d.dir, "index.html"))
	}
	base, err := nappBaseDir(n.ID)
	if err != nil {
		return nil, fmt.Errorf("napplet %s: %w", n.ID, err)
	}
	data, err := os.ReadFile(filepath.Join(base, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("napplet %s is not installed", n.ID)
	}
	// the file that runs is index.html: for a WEB-NAPPLET its hash is the x
	// tag, for a NIP-5D manifest it is index.html's own path tag (whose
	// aggregate was checked against x when the event was read)
	want := n.IndexHash()
	sum := sha256.Sum256(data)
	if want == "" || hex.EncodeToString(sum[:]) != want {
		return nil, fmt.Errorf("napplet %s: installed file does not match its hash", n.ID)
	}
	return data, nil
}

// buildSrcdoc is the napplet's document as the host page loads it: the
// launcher's preamble (CSP, shim, activation and the document-start marker)
// and then the napplet's bytes. webview.NappletSrcdoc builds it and says why
// it is built that way; it lives beside the host page that reads the marker.
func buildSrcdoc(html []byte, domains []string) (string, error) {
	return webview.NappletSrcdoc(html, domains)
}

// grantQuestion is a session question in flight: the first request that
// needs the permission asks, and the others wait on done for its answer.
type grantQuestion struct {
	done chan struct{}
	ok   bool
	// decided is false when the question ended without an answer that
	// counts (dismissed, refused by the prompt bounds): nothing was recorded
	// and a waiter has to ask for itself
	decided bool
}

// sessionGrant asks once per session (or not at all, once the user said
// "always") whether the napplet may do something it will want to do over
// and over: fetching, decrypting. Every caller after the first gets the same
// answer without another prompt.
//
// Only an explicit answer is recorded (02-RESEARCH Pattern 9). A question
// that was dismissed or refused leaves the permission undecided, so the next
// request asks again instead of being denied for the rest of the session. A
// caller waiting on someone else's question returns as soon as its own ctx
// ends, and asks for itself when that question ended without an answer.
// s.mu is only held to read and write grants and asking, never across the
// prompt.
func (c *napCall) sessionGrant(ctx context.Context, perm Permission, title, detail string) (bool, error) {
	s := c.ci.nap
	for {
		if ctx.Err() != nil {
			return false, errPromptDismissed
		}
		s.mu.Lock()
		if s.gen != c.gen {
			s.mu.Unlock()
			return false, errPromptDismissed
		}
		if ok, decided := s.grants[perm]; decided {
			s.mu.Unlock()
			return ok, nil
		}
		if q := s.asking[perm]; q != nil {
			s.mu.Unlock()
			select {
			case <-q.done:
				if q.decided {
					return q.ok, nil
				}
				// the asker was dismissed: ask again under our own deadline
				continue
			case <-ctx.Done():
				return false, errPromptDismissed
			}
		}
		q := &grantQuestion{done: make(chan struct{})}
		s.asking[perm] = q
		s.mu.Unlock()

		ok, err := askApproval(ctx, c.ci, perm, title, detail, "")

		s.mu.Lock()
		stale := s.gen != c.gen
		if err == nil && !stale {
			s.grants[perm] = ok
			q.ok, q.decided = ok, true
		}
		if s.asking[perm] == q {
			delete(s.asking, perm)
		}
		s.mu.Unlock()
		close(q.done)
		if err == nil && stale {
			// answered for a session that has ended since: nothing may
			// happen for it
			return false, errPromptDismissed
		}
		return ok, err
	}
}
