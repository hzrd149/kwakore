package backend

import (
	"errors"
	"slices"
	"sync"
)

// The gate layer (D-02). This file and nap_route.go are the only NAP code
// that may ask the user anything or touch what the user cares about. A
// handler reaches consent only through the gate helpers below (approve,
// grant, hasGrant), which ask only for what its route declared, and it
// reaches every sensitive side effect (opening a link, signing, encrypting,
// publishing, uploading, fetching a napplet-chosen URL, notifying, playing
// media) only through a sink method on its call. A sink refuses, logs the
// bug and answers in the route's denial shape unless the call passed its
// gate first (napCall.approved), so a handler that forgets to ask fails
// closed instead of acting. nap_guard_test.go keeps it that way: no other
// nap_*.go file may name a prompt, a raw sink or a bare goroutine.

// errSinkRefused is what a sink returns when its call never passed its gate.
// The refusal has already been answered; the caller only has to stop.
var errSinkRefused = errors.New("NAP sink refused: the call did not pass its gate")

var (
	napSinkHookMu sync.RWMutex
	// napSinkHook, when set, sees every sink a call reaches and whether the
	// call was approved: a test hook, always nil outside tests. Set it
	// through setNapSinkHook (tests), never directly.
	napSinkHook func(sink string, approved bool)
)

// napSinkSeen reports a sink to the test hook.
func napSinkSeen(sink string, approved bool) {
	napSinkHookMu.RLock()
	h := napSinkHook
	napSinkHookMu.RUnlock()
	if h != nil {
		h(sink, approved)
	}
}

// ─── gates ───────────────────────────────────────────────────────

// declaredRoute is the call's route: napDispatch sets it, and a call built by
// hand (tests) looks it up.
func (c *napCall) declaredRoute() *napRoute {
	if c.route != nil {
		return c.route
	}
	return napRoutes[c.Type]
}

// gateDeclares says whether the call's route declared perm for the kind of
// question asked: a session question (grant, hasGrant) needs a Session gate
// on perm, a per-call one (approve) a PerCall gate on perm, and a Dynamic
// gate allows either for the permissions it lists. Open routes declare
// nothing, so they may ask nothing.
func (c *napCall) gateDeclares(perm Permission, session bool) bool {
	r := c.declaredRoute()
	if r == nil || perm == "" {
		return false
	}
	g := r.gate
	switch g.kind {
	case napGateSession:
		return session && g.perm == perm
	case napGatePerCall:
		return !session && g.perm == perm
	case napGateDynamic:
		return slices.Contains(g.perms, perm)
	}
	return false
}

// undeclared refuses a gate question the route never declared. It is a bug
// in the handler, not something the napplet chose, so it is logged at Error
// and answered as a denial without asking anyone.
func (c *napCall) undeclared(perm Permission, how string) {
	log.Error().Str("type", c.Type).Str("perm", string(perm)).Str("ask", how).
		Msg("NAP gate asked outside its declaration")
	c.failWith(napErrDenied)
}

// approve asks the user (or the rules) whether this one call may do what
// perm covers. Only a PerCall route on perm, or a Dynamic route listing it,
// may ask. On yes the call's sinks open up.
//
// The error is for prompts that end without an answer (02-06 makes them
// cancellable and bounded); a handler answers it with c.failForPrompt.
func (c *napCall) approve(perm Permission, title, detail, code string) (bool, error) {
	if !c.gateDeclares(perm, false) {
		c.undeclared(perm, "approve")
		return false, nil
	}
	if !askApproval(c.ci, perm, title, detail, code) {
		return false, nil
	}
	c.approved.Store(true)
	return true, nil
}

// grant asks the session's standing question for perm (once per session,
// see sessionGrant). Only a Session route on perm, or a Dynamic route
// listing it, may ask. On yes the call's sinks open up.
func (c *napCall) grant(perm Permission, title, detail string) (bool, error) {
	if !c.gateDeclares(perm, true) {
		c.undeclared(perm, "grant")
		return false, nil
	}
	if !c.sessionGrant(perm, title, detail) {
		return false, nil
	}
	c.approved.Store(true)
	return true, nil
}

// hasGrant checks a Session gate without ever prompting (notify.send): yes
// when this session was granted perm, or, before the session decided, when
// a stored rule allows it. On yes the call's sinks open up.
func (c *napCall) hasGrant(perm Permission) bool {
	if !c.gateDeclares(perm, true) {
		c.undeclared(perm, "hasGrant")
		return false
	}
	s := c.ci.nap
	s.mu.Lock()
	granted, decided := s.grants[perm]
	stale := s.gen != c.gen
	s.mu.Unlock()
	if stale {
		return false
	}
	if !decided {
		rule, ok := lookupRule(RuleKey{Napp: c.ci.napp.ID, Permission: perm})
		granted = ok && rule.Decision.granted()
	}
	if granted {
		c.approved.Store(true)
	}
	return granted
}

// failForPrompt answers a request whose prompt ended without an answer.
// Every such ending is a denial until 02-06 gives them their own codes.
func (c *napCall) failForPrompt(err error) {
	if err != nil {
		c.failWith(napErrDenied)
	}
}

// sinkAllowed is every sink's first step: it lets the call through only when
// it passed its gate. A refusal is a handler bug: it is logged at Error and
// answered in the route's denial shape, and the sink does nothing.
func (c *napCall) sinkAllowed(name string) bool {
	ok := c.approved.Load()
	napSinkSeen(name, ok)
	if ok {
		return true
	}
	log.Error().Str("type", c.Type).Str("sink", name).Msg("NAP sink reached without passing its gate")
	c.failWith(napErrDenied)
	return false
}

// ─── sinks ───────────────────────────────────────────────────────

// openLink hands an approved http(s) link to the platform's browser.
func (c *napCall) openLink(url string) error {
	if !c.sinkAllowed("openLink") {
		return errSinkRefused
	}
	return openExternalLink(url)
}
