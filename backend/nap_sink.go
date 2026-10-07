package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nipb7/blossom"
	"kwakore/backend/netguard"
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

// promptCtx is what a prompt raised for this call lives in (DEC-1, D-20):
// the session's context, so a teardown (nap.start, nap.reset, a closed
// window) cancels it, bounded by the route's prompt deadline counted from
// when the envelope arrived. Past that the shim stopped waiting (30 s, 5 s
// for storage) and an answer would reach nobody; relay publishes and
// upload.upload, which the shim does not time out, get promptTimeout. The
// elapsed time is read on napNow, the clock the limits use, so a test that
// freezes it still gets the whole deadline.
func (c *napCall) promptCtx() (context.Context, context.CancelFunc) {
	parent := c.ctx
	if parent == nil {
		parent = context.Background()
	}
	deadline := napDeadlineDefault
	if r := c.declaredRoute(); r != nil {
		deadline = r.promptDeadline()
	}
	if !c.received.IsZero() {
		deadline -= napNow().Sub(c.received)
	}
	return context.WithTimeout(parent, deadline)
}

// approve asks the user (or the rules) whether this one call may do what
// perm covers. Only a PerCall route on perm, or a Dynamic route listing it,
// may ask. On yes the call's sinks open up.
//
// The prompt lives in c.promptCtx(): it is cancelled as dismissed when the
// request's deadline passes or its session ends, and the bounds may refuse
// it. Both come back as the error (errPromptDismissed, errPromptLimited),
// which a handler answers with c.failForPrompt.
func (c *napCall) approve(perm Permission, title, detail, code string) (bool, error) {
	if !c.gateDeclares(perm, false) {
		c.undeclared(perm, "approve")
		return false, nil
	}
	ctx, cancel := c.promptCtx()
	defer cancel()
	ok, err := askApproval(ctx, c.ci, perm, title, detail, code)
	if err != nil || !ok {
		return false, err
	}
	c.approved.Store(true)
	return true, nil
}

// grant asks the session's standing question for perm (once per session,
// see sessionGrant). Only a Session route on perm, or a Dynamic route
// listing it, may ask. On yes the call's sinks open up. Its prompt, or its
// wait on another request's prompt, lives in c.promptCtx() like approve's.
func (c *napCall) grant(perm Permission, title, detail string) (bool, error) {
	if !c.gateDeclares(perm, true) {
		c.undeclared(perm, "grant")
		return false, nil
	}
	ctx, cancel := c.promptCtx()
	defer cancel()
	ok, err := c.sessionGrant(ctx, perm, title, detail)
	if err != nil || !ok {
		return false, err
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

// failForPrompt answers a request whose prompt ended without an answer, in
// the route's shape: refused by the prompt bounds is rate-limited (D-15),
// dismissed (deadline, session end, promptTimeout) is a denial like the
// user's no, and anything else is internal-error.
func (c *napCall) failForPrompt(err error) {
	switch {
	case err == nil:
	case errors.Is(err, errPromptLimited):
		c.failWith(napErrRateLimited)
	case errors.Is(err, errPromptDismissed):
		c.failWith(napErrDenied)
	default:
		c.failWith(napErrInternal)
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

// encrypt encrypts plaintext for to with the user's key, under NIP-04 or
// NIP-44 (scheme "nip04" or "nip44").
func (c *napCall) encrypt(ctx context.Context, scheme, plaintext string, to nostr.PubKey) (string, error) {
	if !c.sinkAllowed("encrypt") {
		return "", errSinkRefused
	}
	k, _ := identitySnapshot()
	if k == nil {
		return "", errors.New("not-signed-in")
	}
	switch scheme {
	case "nip04":
		return k.Nip04Encrypt(ctx, plaintext, to)
	case "nip44":
		return k.Encrypt(ctx, plaintext, to)
	}
	return "", errors.New("unsupported encryption")
}

// sign signs evt with the user's key.
func (c *napCall) sign(ctx context.Context, evt *nostr.Event) error {
	if !c.sinkAllowed("sign") {
		return errSinkRefused
	}
	k, _ := identitySnapshot()
	if k == nil {
		return errors.New("not-signed-in")
	}
	return k.SignEvent(ctx, evt)
}

// napPublishSigned is publishSigned, the store-then-relays publish; tests
// replace it so a publish needs no relay.
var napPublishSigned = publishSigned

// publish sends a signed event to targets and reports per relay, as
// publishSigned does. A refused call gets nil.
func (c *napCall) publish(ctx context.Context, evt nostr.Event, targets []string) map[string]any {
	if !c.sinkAllowed("publish") {
		return nil
	}
	return napPublishSigned(ctx, evt, targets)
}

// uploadAuth signs a Blossom authorization for one blob with keyer.
func (c *napCall) uploadAuth(ctx context.Context, keyer nostr.Keyer, hash string) (string, error) {
	if !c.sinkAllowed("uploadAuth") {
		return "", errSinkRefused
	}
	return napUploadAuth(ctx, keyer, hash)
}

// uploadToServer PUTs a blob to one Blossom server.
func (c *napCall) uploadToServer(ctx context.Context, server string, data []byte, mimeType, auth string) (*blossom.BlobDescriptor, error) {
	if !c.sinkAllowed("uploadToServer") {
		return nil, errSinkRefused
	}
	return napUploadToServer(ctx, server, data, mimeType, auth)
}

// the Blossom upload's two steps, as package vars so tests can stand in for
// the signer and the server
var (
	// napUploadAuth signs the Blossom (BUD-02) authorization for one blob.
	// ctx has no deadline: a remote signer may wait on the user for as long
	// as it likes.
	napUploadAuth = func(ctx context.Context, keyer nostr.Keyer, hash string) (string, error) {
		now := nostr.Now()
		evt := nostr.Event{
			Kind: 24242, CreatedAt: now, Content: "Upload blob",
			Tags: nostr.Tags{
				{"t", "upload"}, {"x", hash},
				// generous, since the user may take a while to sign: the
				// authorization is good for this one blob only
				{"expiration", strconv.FormatInt(int64(now)+napUploadAuthTTL, 10)},
			},
		}
		if err := keyer.SignEvent(ctx, &evt); err != nil {
			return "", err
		}
		j, err := json.Marshal(evt)
		if err != nil {
			return "", err
		}
		return "Nostr " + base64.StdEncoding.EncodeToString(j), nil
	}
	napUploadToServer = func(ctx context.Context, server string, data []byte, mimeType, auth string) (*blossom.BlobDescriptor, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, server+"/upload", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mimeType)
		req.Header.Set("Authorization", auth)
		resp, err := napUploadClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 {
			reason := resp.Header.Get("X-Reason")
			if reason == "" {
				reason = preview(string(body), 200)
			}
			return nil, fmt.Errorf("%s: %s", resp.Status, reason)
		}
		var descriptor blossom.BlobDescriptor
		if err := json.Unmarshal(body, &descriptor); err != nil {
			return nil, fmt.Errorf("unreadable blob descriptor: %w", err)
		}
		return &descriptor, nil
	}
)

// notify shows a system notification.
func (c *napCall) notify(req NotificationRequest) (NotificationHandle, error) {
	if !c.sinkAllowed("notify") {
		return nil, errSinkRefused
	}
	return host.SendNotification(req)
}

// requestNotifyPermission asks the platform for notification permission (on
// Android, the OS prompt). A refused call gets false.
func (c *napCall) requestNotifyPermission() bool {
	if !c.sinkAllowed("requestNotifyPermission") {
		return false
	}
	return host.RequestNotificationPermission()
}

// fetch downloads a napplet-chosen https URL for the napplet.
func (c *napCall) fetch(ctx context.Context, target string) (resourceResult, error) {
	if !c.sinkAllowed("fetch") {
		return resourceResult{}, errSinkRefused
	}
	return httpsResource(ctx, target)
}

// fetchBlossom downloads one Blossom blob candidate, with its own deadline
// per server so one stalling server does not eat the whole request.
//
// It is the one sink that does not need c.approved, on purpose: Blossom
// fetches have never asked, and the resource routes' Dynamic reason records
// that their consent is RES-02 (Phase 7: "Blossom fetches get the same
// consent and host policy as https fetches"). Until then the exception is
// here, in the gate layer, where it can be seen, and the test hook still
// sees every call.
func (c *napCall) fetchBlossom(ctx context.Context, target string) (resourceResult, error) {
	napSinkSeen("fetchBlossom", c.approved.Load())
	actx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return httpsResource(actx, target)
}

// blossomHas asks a Blossom server whether it has a blob, without downloading
// it: a HEAD through resourceClient (public addresses only, on every hop),
// with its own short deadline.
//
// Like fetchBlossom it does not need c.approved, on purpose:
// media.session.create looks a napplet's blossomHash up on the user's own
// Blossom servers before its PermMedia grant, to know what the player would
// be asked to open. Bringing Blossom under the same consent and host policy
// as https is RES-02 and MDIA-01/MDIA-02 (Phase 7). Until then the exception
// is here, in the gate layer, where it can be seen, and the test hook still
// sees every call.
func (c *napCall) blossomHas(ctx context.Context, target string) bool {
	napSinkSeen("blossomHas", c.approved.Load())
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "kwakore-napplet-resource")
	resp, err := resourceClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// httpsResource downloads target over https through resourceClient (public
// addresses only, on every hop) and types the bytes with sniffResource.
func httpsResource(ctx context.Context, target string) (resourceResult, error) {
	ctx, cancel := context.WithTimeout(ctx, resourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return resourceResult{}, rerr("invalid-request", err.Error())
	}
	req.Header.Set("User-Agent", "kwakore-napplet-resource")
	resp, err := resourceClient.Do(req)
	if err != nil {
		if errors.Is(err, netguard.ErrPrivateAddress) || strings.Contains(err.Error(), netguard.ErrPrivateAddress.Error()) {
			return resourceResult{}, rerr("blocked-by-policy", "not a public address")
		}
		if ctx.Err() != nil {
			return resourceResult{}, rerr("timeout", "")
		}
		return resourceResult{}, rerr("network-error", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return resourceResult{}, rerr("not-found", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return resourceResult{}, rerr("network-error", resp.Status)
	}
	if resp.ContentLength > resourceMaxBytes {
		return resourceResult{}, rerr("too-large", "")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, resourceMaxBytes+1))
	if err != nil {
		return resourceResult{}, rerr("network-error", err.Error())
	}
	if len(data) > resourceMaxBytes {
		return resourceResult{}, rerr("too-large", "")
	}
	mime, err := sniffResource(data, resp.Header.Get("Content-Type"))
	if err != nil {
		return resourceResult{}, err
	}
	return resourceResult{data: data, mime: mime}, nil
}

// playMedia starts the user's external media player on a resolved source.
func (c *napCall) playMedia(req MediaRequest, onState func(MediaState)) (MediaPlayer, error) {
	if !c.sinkAllowed("playMedia") {
		return nil, errSinkRefused
	}
	return host.MediaPlay(req, onState)
}
