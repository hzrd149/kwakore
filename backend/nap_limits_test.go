package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nipb7/blossom"
	"golang.org/x/time/rate"
)

// ─── test rig ────────────────────────────────────────────────────

// freezeNapNow stops the limiters' clock for the test and returns a way to
// move it forward (Pitfall 7).
func freezeNapNow(t *testing.T) func(time.Duration) {
	t.Helper()
	saved := napNow
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	napNow = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	t.Cleanup(func() { napNow = saved })
	return func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}
}

// withLimits replaces a window's buckets. Call it before the window's first
// envelope: the worker it starts reads them.
func withLimits(t *testing.T, ci *Instance, l *napLimiter) {
	t.Helper()
	saved := ci.nap.limits
	ci.nap.limits = l
	t.Cleanup(func() { ci.nap.limits = saved })
}

// limitsWith is the defaults with some class buckets replaced.
func limitsWith(envelope napLimitSpec, classes map[napLimitClass]napLimitSpec) *napLimiter {
	specs := napLimitSpecs
	for class, spec := range classes {
		specs[class] = spec
	}
	return newNapLimiterWith(envelope, specs)
}

// waitID returns the push of a type answering id.
func waitID(t *testing.T, rec *recTransport, typ, id string) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		for _, p := range rec.find(typ) {
			if p["id"] == id {
				return p
			}
		}
		select {
		case <-rec.notify:
		case <-deadline:
			t.Fatalf("no %s for %q; got %v", typ, id, rec.types())
		}
	}
}

// limitLinkHost opens links and counts them, safely across async handlers.
type limitLinkHost struct {
	noopHost
	mu     sync.Mutex
	opened int
}

func (h *limitLinkHost) OpenLink(string) error {
	h.mu.Lock()
	h.opened++
	h.mu.Unlock()
	return nil
}

// allowLinks lets a napplet open links without a prompt.
func allowLinks(t *testing.T, ci *Instance) {
	t.Helper()
	key := RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}
	setSessionRule(key, Rule{Decision: DecisionAllow})
	t.Cleanup(func() { clearSessionRule(key) })
}

// ─── the limiter ─────────────────────────────────────────────────

func TestNapLimiterDefaults(t *testing.T) {
	if napEnvelopeLimit.every != rate.Limit(200) || napEnvelopeLimit.burst != 400 {
		t.Errorf("envelope bucket %v", napEnvelopeLimit)
	}
	want := map[napLimitClass]napLimitSpec{
		limitPrompt:       {rate.Every(6 * time.Second), 5},
		limitLink:         {rate.Every(2 * time.Second), 5},
		limitIntent:       {rate.Every(time.Second), 10},
		limitColdLaunch:   {rate.Every(10 * time.Second), 3},
		limitUpload:       {rate.Every(6 * time.Second), 5},
		limitResource:     {rate.Every(time.Second), 100},
		limitIncOpen:      {rate.Every(time.Second), 10},
		limitIncEmit:      {rate.Limit(50), 100},
		limitPublish:      {rate.Every(time.Second), 10},
		limitNotify:       {rate.Every(3 * time.Second), 20},
		limitNotifyUrgent: {rate.Every(20 * time.Second), 3},
		limitOpenSettings: {rate.Every(2 * time.Second), 1},
	}
	if len(want) != int(limitCount)-1 {
		t.Fatalf("%d classes checked, %d declared", len(want), limitCount-1)
	}
	for class := limitNone + 1; class < limitCount; class++ {
		if got := napLimitSpecs[class]; got != want[class] {
			t.Errorf("class %d: %v, want %v", class, got, want[class])
		}
	}
	if napLimitSpecs[limitNone] != (napLimitSpec{}) {
		t.Errorf("limitNone has a bucket: %v", napLimitSpecs[limitNone])
	}
	// the resource burst must fit resource.info's advertised maxUrls
	if napLimitSpecs[limitResource].burst < resourceMaxURLs {
		t.Errorf("resource burst %d below maxUrls %d", napLimitSpecs[limitResource].burst, resourceMaxURLs)
	}
}

func TestNapLimiterFrozenClock(t *testing.T) {
	advance := freezeNapNow(t)
	tiny := napLimitSpec{rate.Every(time.Second), 3}
	var classes [limitCount]napLimitSpec
	for class := range classes {
		classes[class] = tiny
	}

	l := newNapLimiterWith(tiny, classes)
	for i := 1; i <= 3; i++ {
		if !l.allow(limitLink, 1) {
			t.Fatalf("token %d of a burst of 3 refused", i)
		}
	}
	if l.allow(limitLink, 1) {
		t.Fatal("a 4th token admitted at a frozen clock")
	}
	// buckets are independent
	if !l.allow(limitIntent, 1) {
		t.Fatal("another class was charged")
	}
	advance(time.Second)
	if !l.allow(limitLink, 1) {
		t.Fatal("one refill interval admitted nothing")
	}
	if l.allow(limitLink, 1) {
		t.Fatal("one refill interval admitted two")
	}

	if !newNapLimiterWith(tiny, classes).allow(limitLink, 3) {
		t.Fatal("n equal to the burst refused on a fresh bucket")
	}
	if newNapLimiterWith(tiny, classes).allow(limitLink, 4) {
		t.Fatal("n above the burst admitted")
	}
	if !l.allow(limitNone, 1000) {
		t.Fatal("limitNone refused")
	}
	var none *napLimiter
	if !none.allow(limitLink, 1000) || !none.allowEnvelope() {
		t.Fatal("a nil limiter refused")
	}

	for i := 1; i <= 3; i++ {
		if !l.allowEnvelope() {
			t.Fatalf("envelope %d of a burst of 3 refused", i)
		}
	}
	if l.allowEnvelope() {
		t.Fatal("a 4th envelope admitted at a frozen clock")
	}
	advance(time.Second)
	if !l.allowEnvelope() || l.allowEnvelope() {
		t.Fatal("the envelope bucket did not refill exactly one")
	}
}

func TestNapRouteLimitClasses(t *testing.T) {
	want := map[string]napLimitClass{
		"link.open":              limitLink,
		"intent.invoke":          limitIntent,
		"upload.upload":          limitUpload,
		"resource.bytes":         limitResource,
		"resource.bytesMany":     limitResource,
		"inc.channel.open":       limitIncOpen,
		"inc.emit":               limitIncEmit,
		"inc.channel.emit":       limitIncEmit,
		"inc.channel.broadcast":  limitIncEmit,
		"relay.publish":          limitPublish,
		"relay.publishEncrypted": limitPublish,
		"outbox.publish":         limitPublish,
		"common.follow":          limitPublish,
		"common.unfollow":        limitPublish,
		"common.react":           limitPublish,
		"common.report":          limitPublish,
	}
	for typ := range napGoldenRoutes {
		r := napRoutes[typ]
		if r == nil {
			t.Fatalf("%s not registered", typ)
		}
		if got := r.limit; got != want[typ] {
			t.Errorf("%s: limit class %d, want %d", typ, got, want[typ])
		}
	}
}

// ─── on the dispatch path ────────────────────────────────────────

// TestNapEnvelopeBucketRateLimits: over the envelope bucket, a request is
// answered rate-limited in its shape and a reply-less one is dropped (D-14).
func TestNapEnvelopeBucketRateLimits(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	withTestRoute(t, "test.sentinel", napRoute{h: func(c *napCall) { c.reply(nil) }, gate: openGate("test"), fail: failShape(failErr)})
	three := napLimitSpec{rate.Every(time.Hour), 3}

	ci, rec := openNapplet(t, "envelopes")
	withLimits(t, ci, newNapLimiterWith(three, napLimitSpecs))
	ready(t, ci, rec, 1)
	for _, id := range []string{"k1", "k2", "k3", "k4"} {
		post(t, ci, map[string]any{"type": "storage.keys", "id": id})
	}
	for _, id := range []string{"k1", "k2", "k3"} {
		if got := waitID(t, rec, "storage.keys.result", id); got["error"] != nil {
			t.Fatalf("%s within the bucket: %v", id, got)
		}
	}
	if got := waitID(t, rec, "storage.keys.result", "k4"); got["error"] != napErrRateLimited {
		t.Fatalf("over the bucket: %v", got)
	}

	// a reply-less type over the bucket is dropped: the 4th emit never
	// reaches the listener
	listener, lrec := openNapplet(t, "listener")
	ready(t, listener, lrec, 1)
	post(t, listener, map[string]any{"type": "inc.subscribe", "id": "s", "topic": "flood"})
	waitID(t, lrec, "inc.subscribe.result", "s")
	emitter, erec := openNapplet(t, "emitter")
	withLimits(t, emitter, newNapLimiterWith(three, napLimitSpecs))
	ready(t, emitter, erec, 1)
	for i := range 4 {
		post(t, emitter, map[string]any{"type": "inc.emit", "topic": "flood", "payload": i})
	}
	lrec.wait(t, "inc.event", 3)
	// the sentinel is over the bucket too, but still answered: everything
	// before it has been handled
	napSettled(t, emitter, erec)
	if got := waitID(t, erec, "test.sentinel.result", "sentinel"); got["error"] != napErrRateLimited {
		t.Fatalf("sentinel: %v", got)
	}
	if n := len(lrec.find("inc.event")); n != 3 {
		t.Fatalf("%d emits delivered, want 3", n)
	}
	if got := erec.find("inc.emit.result"); len(got) != 0 {
		t.Fatalf("a reply-less type was answered: %v", got)
	}
}

// TestNapEnvelopeBucketChargesRefusedAndDropped: the envelope bucket is
// charged before an envelope is parsed, so the ones that are refused
// (colliding keys, too large) or dropped (unknown type, bad id) use it up as
// well: a napplet cannot loop on them at an unlimited rate (WR-05).
func TestNapEnvelopeBucketChargesRefusedAndDropped(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	four := napLimitSpec{rate.Every(time.Hour), 4}

	ci, rec := openNapplet(t, "envelope-charges")
	withLimits(t, ci, newNapLimiterWith(four, napLimitSpecs))
	ready(t, ci, rec, 1)
	// four tokens: a collision and an oversized one (both answered), an
	// unknown type and a bad id (both dropped)
	post(t, ci, map[string]any{"type": "storage.keys", "id": "collide", "ID": "x"})
	post(t, ci, padded(t, map[string]any{"type": "storage.keys", "id": "big"}, napDefaultMaxRaw+1))
	post(t, ci, map[string]any{"type": "nope.unknown", "id": "u"})
	post(t, ci, map[string]any{"type": "storage.keys", "id": map[string]any{}})
	if got := waitID(t, rec, "storage.keys.result", "collide"); got["error"] != napErrInvalid {
		t.Fatalf("collision: %v", got)
	}
	if got := waitID(t, rec, "storage.keys.result", "big"); got["error"] != napErrTooLarge {
		t.Fatalf("too large: %v", got)
	}
	// the bucket is empty now: a well-formed request is rate-limited
	post(t, ci, map[string]any{"type": "storage.keys", "id": "after"})
	if got := waitID(t, rec, "storage.keys.result", "after"); got["error"] != napErrRateLimited {
		t.Fatalf("after four refused or dropped envelopes: %v", got)
	}
	// and a colliding one over the bucket is refused for the rate first
	post(t, ci, map[string]any{"type": "storage.keys", "id": "collide-over", "ID": "x"})
	if got := waitID(t, rec, "storage.keys.result", "collide-over"); got["error"] != napErrRateLimited {
		t.Fatalf("collision over the bucket: %v", got)
	}
}

// TestNapCategoryLimitRateLimits: a category bucket refuses in the route's
// shape, and only the window that used it up (D-14).
func TestNapCategoryLimitRateLimits(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	linkHost := &limitLinkHost{}
	host = linkHost

	a, rec := openNapplet(t, "links-a")
	withLimits(t, a, limitsWith(napEnvelopeLimit, map[napLimitClass]napLimitSpec{
		limitLink:     {rate.Every(time.Hour), 2},
		limitResource: {rate.Every(time.Hour), 1},
	}))
	allowLinks(t, a)
	ready(t, a, rec, 1)
	for _, id := range []string{"l1", "l2", "l3"} {
		post(t, a, map[string]any{"type": "link.open", "id": id, "url": "https://example.com/" + id})
	}
	for _, id := range []string{"l1", "l2"} {
		if got := waitID(t, rec, "link.open.result", id); got["status"] != "opened" {
			t.Fatalf("%s within the bucket: %v", id, got)
		}
	}
	if got := waitID(t, rec, "link.open.result", "l3"); got["status"] != "denied" || got["error"] != napErrRateLimited {
		t.Fatalf("over the link bucket: %v", got)
	}

	post(t, a, map[string]any{"type": "resource.bytes", "id": "r1", "url": "data:text/plain,one"})
	post(t, a, map[string]any{"type": "resource.bytes", "id": "r2", "url": "data:text/plain,two"})
	if got := waitID(t, rec, "resource.bytes.result", "r1"); got["blob"] == nil {
		t.Fatalf("resource within the bucket: %v", got)
	}
	if got := waitID(t, rec, "resource.bytes.error", "r2"); got["error"] != "quota-exceeded" {
		t.Fatalf("over the resource bucket: %v", got)
	}

	// another window of the same napplet has buckets of its own
	b, brec := openNapplet(t, "links-a")
	allowLinks(t, b)
	ready(t, b, brec, 1)
	post(t, b, map[string]any{"type": "link.open", "id": "b1", "url": "https://example.com/b"})
	if got := waitID(t, brec, "link.open.result", "b1"); got["status"] != "opened" {
		t.Fatalf("an independent window was limited: %v", got)
	}
	linkHost.mu.Lock()
	opened := linkHost.opened
	linkHost.mu.Unlock()
	if opened != 3 {
		t.Fatalf("%d links opened, want 3", opened)
	}
}

// TestNapFullQueueDoesNotBlockReader: a full dispatch queue answers
// rate-limited at once, so the window's reader, which also carries prompt
// answers, never blocks behind a flood (D-16).
func TestNapFullQueueDoesNotBlockReader(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "flood")
	entered := make(chan struct{})
	release := make(chan struct{})
	var parked, released sync.Once
	unpark := func() { released.Do(func() { close(release) }) }
	t.Cleanup(unpark)
	ci.nap.beforeHandler = func(c *napCall) {
		parked.Do(func() {
			close(entered)
			<-release
		})
	}
	ready(t, ci, rec, 1)

	rpc := func(i int, id string) {
		raw, _ := json.Marshal(map[string]any{"type": "storage.keys", "id": id})
		param, _ := json.Marshal(string(raw))
		HandleMessage(ci.instance, WireMsg{T: "rpc", ID: i, Method: "nap.msg", Params: string(param)})
	}
	rpc(0, "held")
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("rig: the handler never ran")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range napQueueSlots + 4 {
			rpc(i+1, "q"+strconv.Itoa(i))
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the reader blocked on a full queue")
	}
	// the overflow is answered right away, while the worker is still held
	limited := rec.find("storage.keys.result")
	if len(limited) != 4 {
		t.Fatalf("%d immediate answers, want 4: %v", len(limited), limited)
	}
	for _, got := range limited {
		if got["error"] != napErrRateLimited {
			t.Fatalf("overflow answer: %v", got)
		}
	}
	if n := len(ci.nap.queue); n != napQueueSlots {
		t.Fatalf("queue holds %d, want %d", n, napQueueSlots)
	}

	// a prompt answer for this window still goes through the same reader
	p := newPrompt("flood", "test prompt", "", "", nil)
	p.Instance = ci.instance
	enqueuePrompt(p)
	t.Cleanup(func() { AnswerPrompt(p.ID, Answer{}) })
	answered := make(chan struct{})
	go func() {
		HandleMessage(ci.instance, WireMsg{T: "promptAnswer", ID: p.ID, Params: `{"ok":true}`})
		close(answered)
	}()
	select {
	case <-answered:
	case <-time.After(time.Second):
		t.Fatal("the prompt answer was stuck behind the flood")
	}
	select {
	case a := <-p.resp:
		if !a.OK {
			t.Fatalf("prompt answer: %v", a)
		}
	default:
		t.Fatal("the prompt answer was not processed")
	}
	if n := len(ci.nap.queue); n != napQueueSlots {
		t.Fatalf("the queue drained early: %d", n)
	}

	// released, the worker handles everything that was queued
	unpark()
	rec.wait(t, "storage.keys.result", 4+1+napQueueSlots)
	ok := 0
	for _, got := range rec.find("storage.keys.result") {
		if got["error"] == nil {
			ok++
		}
	}
	if ok != 1+napQueueSlots {
		t.Fatalf("%d handled, want %d", ok, 1+napQueueSlots)
	}
}

// TestNapLimitsSurviveSessionRestart: a reload does not refill the buckets.
func TestNapLimitsSurviveSessionRestart(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	host = &limitLinkHost{}
	ci, rec := openNapplet(t, "restart")
	withLimits(t, ci, limitsWith(napEnvelopeLimit, map[napLimitClass]napLimitSpec{
		limitLink: {rate.Every(time.Hour), 1},
	}))
	allowLinks(t, ci)
	ready(t, ci, rec, 1)
	post(t, ci, map[string]any{"type": "link.open", "id": "first", "url": "https://example.com/1"})
	if got := waitID(t, rec, "link.open.result", "first"); got["status"] != "opened" {
		t.Fatalf("first link: %v", got)
	}

	ready(t, ci, rec, 2)
	post(t, ci, map[string]any{"type": "link.open", "id": "after", "url": "https://example.com/2"})
	if got := waitID(t, rec, "link.open.result", "after"); got["status"] != "denied" || got["error"] != napErrRateLimited {
		t.Fatalf("a restart refilled the bucket: %v", got)
	}
}

// ─── handler-side limits ─────────────────────────────────────────

// dataURLs is n distinct data: URLs, fetched without any network or prompt.
func dataURLs(n int) []string {
	urls := make([]string, n)
	for i := range urls {
		urls[i] = "data:text/plain,item" + strconv.Itoa(i)
	}
	return urls
}

// TestResourceChargedPerURL: resource.bytesMany costs one resource token per
// URL in total (the dispatcher's one plus the rest in the handler), so a
// full batch of resource.info's maxUrls fits a full bucket and the next one
// does not; a batch costing more than the burst never passes (D-14, D-19).
func TestResourceChargedPerURL(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)

	full, rec := openNapplet(t, "per-url")
	ready(t, full, rec, 1)
	post(t, full, map[string]any{"type": "resource.bytesMany", "id": "m1", "urls": dataURLs(resourceMaxURLs)})
	got := waitID(t, rec, "resource.bytesMany.result", "m1")
	items, _ := got["items"].([]any)
	if len(items) != resourceMaxURLs {
		t.Fatalf("a full batch on a full bucket: %v", got)
	}
	for _, item := range items {
		if item.(map[string]any)["ok"] != true {
			t.Fatalf("item failed: %v", item)
		}
	}
	post(t, full, map[string]any{"type": "resource.bytesMany", "id": "m2", "urls": dataURLs(1)})
	if got := waitID(t, rec, "resource.bytesMany.error", "m2"); got["error"] != "quota-exceeded" {
		t.Fatalf("a batch right after a full one: %v", got)
	}
	if got := rec.find("resource.bytesMany.result"); len(got) != 1 {
		t.Fatalf("%d batches answered, want 1", len(got))
	}

	ten := map[napLimitClass]napLimitSpec{limitResource: {rate.Every(time.Hour), 10}}

	// a batch as large as the burst passes on a full bucket
	fits, frec := openNapplet(t, "per-url-fits")
	withLimits(t, fits, limitsWith(napEnvelopeLimit, ten))
	ready(t, fits, frec, 1)
	post(t, fits, map[string]any{"type": "resource.bytesMany", "id": "ten", "urls": dataURLs(10)})
	if got := waitID(t, frec, "resource.bytesMany.result", "ten"); len(got["items"].([]any)) != 10 {
		t.Fatalf("10 URLs on a bucket of 10: %v", got)
	}

	// one more than the burst never does, and costs only the dispatcher's
	// token: the rest is all or nothing
	over, orec := openNapplet(t, "per-url-over")
	withLimits(t, over, limitsWith(napEnvelopeLimit, ten))
	ready(t, over, orec, 1)
	post(t, over, map[string]any{"type": "resource.bytesMany", "id": "eleven", "urls": dataURLs(11)})
	if got := waitID(t, orec, "resource.bytesMany.error", "eleven"); got["error"] != "quota-exceeded" {
		t.Fatalf("11 URLs on a bucket of 10: %v", got)
	}
	post(t, over, map[string]any{"type": "resource.bytesMany", "id": "nine", "urls": dataURLs(9)})
	if got := waitID(t, orec, "resource.bytesMany.result", "nine"); len(got["items"].([]any)) != 9 {
		t.Fatalf("9 URLs on the 9 tokens left: %v", got)
	}
	post(t, over, map[string]any{"type": "resource.bytes", "id": "empty", "url": "data:text/plain,x"})
	if got := waitID(t, orec, "resource.bytes.error", "empty"); got["error"] != "quota-exceeded" {
		t.Fatalf("resource.bytes on an empty bucket: %v", got)
	}
}

// TestResourceInFlightCap: a window has at most resourceMaxInFlight resource
// requests running; more are answered quota-exceeded in their own error
// shape before any fetch starts, and a finished request frees its slot
// (D-14, NAP-RESOURCE "10 in-flight").
func TestResourceInFlightCap(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	ci, rec := openNapplet(t, "in-flight")
	ready(t, ci, rec, 1)

	// requests that stay in flight, registered the way the handlers do it
	// (loopback https is blocked by netguard, so no live server holds them)
	dones := make([]func(), 0, resourceMaxInFlight)
	for i := range resourceMaxInFlight {
		call := &napCall{ci: ci, gen: ci.nap.gen, ctx: ci.nap.ctx, ID: json.RawMessage(`"held` + strconv.Itoa(i) + `"`)}
		_, done, ok := call.resourceTrack()
		if !ok {
			t.Fatalf("held request %d refused", i)
		}
		dones = append(dones, done)
	}
	t.Cleanup(func() {
		for _, done := range dones {
			done()
		}
	})

	post(t, ci, map[string]any{"type": "resource.bytes", "id": "b1", "url": "data:text/plain,one"})
	if got := waitID(t, rec, "resource.bytes.error", "b1"); got["error"] != "quota-exceeded" {
		t.Fatalf("resource.bytes past the cap: %v", got)
	}
	post(t, ci, map[string]any{"type": "resource.bytesMany", "id": "m1", "urls": dataURLs(2)})
	if got := waitID(t, rec, "resource.bytesMany.error", "m1"); got["error"] != "quota-exceeded" {
		t.Fatalf("resource.bytesMany past the cap: %v", got)
	}
	ci.nap.mu.Lock()
	inFlight := len(ci.nap.fetches)
	ci.nap.mu.Unlock()
	if inFlight != resourceMaxInFlight {
		t.Fatalf("%d in flight after refusals, want %d", inFlight, resourceMaxInFlight)
	}

	// one finishes: the next request runs
	dones[0]()
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "b2", "url": "data:text/plain,one"})
	if got := waitID(t, rec, "resource.bytes.result", "b2"); got["blob"] == nil {
		t.Fatalf("resource.bytes after a slot freed: %v", got)
	}
}

// TestIncChannelCap: only the opener is charged for a channel (D-14, I-3,
// WR-07). A window opens at most incMaxChannelsPerPeer channels toward one
// peer and incMaxChannels in all; past either, the open answers rate-limited
// and the peer hears nothing. The channels others opened toward a window
// never keep it from opening its own, nor others from opening toward it,
// until incMaxInboundChannels of them are open. Closing one frees its slot.
func TestIncChannelCap(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	opens := map[napLimitClass]napLimitSpec{limitIncOpen: {rate.Every(time.Hour), 4 * incMaxChannels}}
	open := func(d string) (*Instance, *recTransport) {
		ci, rec := openNapplet(t, d)
		withLimits(t, ci, limitsWith(napEnvelopeLimit, opens))
		ready(t, ci, rec, 1)
		return ci, rec
	}
	openChannel := func(ci *Instance, rec *recTransport, id, target string) map[string]any {
		t.Helper()
		post(t, ci, map[string]any{"type": "inc.channel.open", "id": id, "target": target})
		return waitID(t, rec, "inc.channel.open.result", id)
	}

	a, recA := open("cap-a")
	peers := incMaxChannels / incMaxChannelsPerPeer
	recPeers := make([]*recTransport, peers)
	for p := range peers {
		_, recPeers[p] = open("cap-p" + strconv.Itoa(p))
	}
	c, recC := open("cap-c")

	// toward one peer: incMaxChannelsPerPeer, then refused
	var first string
	for i := range incMaxChannelsPerPeer {
		got := openChannel(a, recA, "p0-"+strconv.Itoa(i), "cap-p0")
		channelID, _ := got["channelId"].(string)
		if channelID == "" {
			t.Fatalf("channel %d toward one peer: %v", i, got)
		}
		if i == 0 {
			first = channelID
		}
	}
	if got := openChannel(a, recA, "p0-over", "cap-p0"); got["error"] != napErrRateLimited || got["channelId"] != nil {
		t.Fatalf("past the per-peer cap: %v", got)
	}
	// the peer a holds channels with still opens its own, and others still
	// open toward it
	p0 := lookupPeer(t, "cap-p0")
	recP0 := recPeers[0]
	if got := openChannel(p0, recP0, "p0-own", "cap-c"); got["channelId"] == nil {
		t.Fatalf("a window others opened channels toward could not open its own: %v", got)
	}
	if got := openChannel(c, recC, "c-to-p0", "cap-p0"); got["channelId"] == nil {
		t.Fatalf("opening toward a window others hold channels with: %v", got)
	}

	// a fills its own cap across the other peers
	for p := 1; p < peers; p++ {
		for i := range incMaxChannelsPerPeer {
			id := "p" + strconv.Itoa(p) + "-" + strconv.Itoa(i)
			if got := openChannel(a, recA, id, "cap-p"+strconv.Itoa(p)); got["channelId"] == nil {
				t.Fatalf("%s within the cap: %v", id, got)
			}
		}
	}
	if got := openChannel(a, recA, "over", "cap-c"); got["error"] != napErrRateLimited || got["channelId"] != nil {
		t.Fatalf("the opener past its cap: %v", got)
	}
	// a peer hears of a channel before its opener gets the answer, so the
	// refusals above are final
	if n := len(recP0.find("inc.channel.opened")); n != incMaxChannelsPerPeer+1 {
		t.Fatalf("p0 heard of %d channels, want %d", n, incMaxChannelsPerPeer+1)
	}
	for _, got := range recC.find("inc.channel.opened") {
		if got["peer"] == incSender(a) {
			t.Fatalf("c heard of a refused channel: %v", got)
		}
	}

	// closing one frees a slot
	post(t, a, map[string]any{"type": "inc.channel.close", "channelId": first})
	recA.wait(t, "inc.channel.closed", 1)
	if got := openChannel(a, recA, "again", "cap-c"); got["channelId"] == nil {
		t.Fatalf("an open after a close: %v", got)
	}

	// the inbound cap: enough openers fill a window's inbound slots, and the
	// next opener is refused, while the window itself still opens its own
	target, recT := open("cap-target")
	for o := range incMaxInboundChannels / incMaxChannelsPerPeer {
		opener, rec := open("cap-o" + strconv.Itoa(o))
		for i := range incMaxChannelsPerPeer {
			id := "in-" + strconv.Itoa(i)
			if got := openChannel(opener, rec, id, "cap-target"); got["channelId"] == nil {
				t.Fatalf("opener %d channel %d within the inbound cap: %v", o, i, got)
			}
		}
	}
	late, recLate := open("cap-late")
	if got := openChannel(late, recLate, "late", "cap-target"); got["error"] != napErrRateLimited {
		t.Fatalf("past the inbound cap: %v", got)
	}
	if got := openChannel(target, recT, "target-own", "cap-late"); got["channelId"] == nil {
		t.Fatalf("a full inbound window could not open its own: %v", got)
	}
}

// lookupPeer is the open napplet window with that d tag.
func lookupPeer(t *testing.T, d string) *Instance {
	t.Helper()
	for _, ci := range liveNapplets() {
		if ci.napp.D == d {
			return ci
		}
	}
	t.Fatalf("no window for %s", d)
	return nil
}

// TestIncTopicAndNotifyChannelCaps: a window listens on at most
// incMaxTopics topics of at most incMaxTopicBytes each, and registers at most
// notifyMaxChannels notification channels; past that inc.subscribe answers
// in its error shape and notify.channel.register (reply-less) is dropped,
// while renewing one already held still works (WR-06).
func TestIncTopicAndNotifyChannelCaps(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "topic-caps")
	ready(t, ci, rec, 1)
	settle := func(id string) {
		t.Helper()
		post(t, ci, map[string]any{"type": "storage.keys", "id": id})
		waitID(t, rec, "storage.keys.result", id)
	}

	long := strings.Repeat("t", incMaxTopicBytes+1)
	post(t, ci, map[string]any{"type": "inc.subscribe", "id": "long", "topic": long})
	if got := waitID(t, rec, "inc.subscribe.result", "long"); got["error"] != napErrTooLarge {
		t.Fatalf("a topic over %d bytes: %v", incMaxTopicBytes, got)
	}
	for i := range incMaxTopics {
		id := "sub" + strconv.Itoa(i)
		topic := "topic-" + strconv.Itoa(i)
		if i == 0 {
			topic = strings.Repeat("t", incMaxTopicBytes)
		}
		post(t, ci, map[string]any{"type": "inc.subscribe", "id": id, "topic": topic})
		if got := waitID(t, rec, "inc.subscribe.result", id); got["error"] != nil {
			t.Fatalf("topic %d within the cap: %v", i, got)
		}
	}
	post(t, ci, map[string]any{"type": "inc.subscribe", "id": "over", "topic": "one-too-many"})
	if got := waitID(t, rec, "inc.subscribe.result", "over"); got["error"] != napErrRateLimited {
		t.Fatalf("a topic past the cap: %v", got)
	}
	post(t, ci, map[string]any{"type": "inc.subscribe", "id": "again", "topic": "topic-1"})
	if got := waitID(t, rec, "inc.subscribe.result", "again"); got["error"] != nil {
		t.Fatalf("re-subscribing a held topic: %v", got)
	}
	if _, ok := ci.handlerFor("one-too-many"); ok {
		t.Fatal("a refused topic was registered on the window")
	}

	for i := range notifyMaxChannels + 1 {
		post(t, ci, map[string]any{"type": "notify.channel.register", "channelId": "ch" + strconv.Itoa(i), "label": "Channel"})
	}
	post(t, ci, map[string]any{"type": "notify.channel.register", "channelId": "ch0", "label": "Renamed"})
	settle("after-channels")
	ci.nap.mu.Lock()
	n, renamed := len(ci.nap.notifyChannels), ci.nap.notifyChannels["ch0"].Label
	_, extra := ci.nap.notifyChannels["ch"+strconv.Itoa(notifyMaxChannels)]
	ci.nap.mu.Unlock()
	if n != notifyMaxChannels || extra {
		t.Fatalf("%d channels registered (the one past the cap: %v), want %d", n, extra, notifyMaxChannels)
	}
	if renamed != "Renamed" {
		t.Fatalf("re-registering a held channel: label %q", renamed)
	}
}

// holdUploads stores uploads in a window's session as if they were running.
func holdUploads(t *testing.T, ci *Instance, statuses ...string) []string {
	t.Helper()
	ids := make([]string, len(statuses))
	ci.nap.mu.Lock()
	defer ci.nap.mu.Unlock()
	for i, status := range statuses {
		ids[i] = "held-" + strconv.Itoa(i)
		ci.nap.uploads[ids[i]] = &napUploadStatus{OK: true, UploadID: ids[i], Status: status, Rail: "blossom"}
	}
	return ids
}

// TestUploadActiveCap: a window has at most uploadMaxActive uploads pending
// or uploading; another is answered rate-limited before any server lookup
// or prompt, and requests that raced past that check are caught again
// before their pending entry is stored (D-14, U-4).
func TestUploadActiveCap(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	ci, rec := openNapplet(t, "upload-cap")
	withLimits(t, ci, limitsWith(napEnvelopeLimit, map[napLimitClass]napLimitSpec{
		limitUpload: {rate.Every(time.Hour), 100},
	}))
	setupNapUploadTest(t, ci)
	ready(t, ci, rec, 1)

	var lookups atomic.Int32
	napUploadServers = func(context.Context, nostr.PubKey) []string {
		lookups.Add(1)
		return []string{"https://one.example"}
	}
	napUploadToServer = func(context.Context, string, []byte, string, string) (*blossom.BlobDescriptor, error) {
		return nil, errors.New("test server is down")
	}

	held := holdUploads(t, ci, "pending", "uploading", "pending", "uploading", "complete", "failed", "cancelled")
	post(t, ci, uploadEnvelope("over", []byte("one too many")))
	if got := waitID(t, rec, "upload.upload.result", "over"); got["error"] != napErrRateLimited || got["result"] != nil {
		t.Fatalf("upload past the cap: %v", got)
	}
	if n := lookups.Load(); n != 0 {
		t.Fatalf("%d server lookups for a refused upload", n)
	}
	if p := CurrentPrompt(); p != nil {
		t.Fatalf("a refused upload prompted: %v", p)
	}

	// one finishes: the next upload proceeds
	ci.nap.mu.Lock()
	ci.nap.uploads[held[0]].Status = "complete"
	ci.nap.mu.Unlock()
	post(t, ci, uploadEnvelope("after", []byte("room now")))
	got := waitID(t, rec, "upload.upload.result", "after")
	if result, _ := got["result"].(map[string]any); result == nil || result["status"] != "pending" {
		t.Fatalf("upload after a slot freed: %v", got)
	}
	if n := lookups.Load(); n != 1 {
		t.Fatalf("%d server lookups, want 1", n)
	}
	rec.wait(t, "upload.status.changed", 2) // uploading, then failed
}

// TestUploadActiveCapRace: requests that all passed the synchronous check
// while the server lookup was running are counted again when they store
// their pending entry, so no more than uploadMaxActive are ever active.
func TestUploadActiveCapRace(t *testing.T) {
	setupNapTest(t)
	freezeNapNow(t)
	ci, rec := openNapplet(t, "upload-race")
	withLimits(t, ci, limitsWith(napEnvelopeLimit, map[napLimitClass]napLimitSpec{
		limitUpload: {rate.Every(time.Hour), 100},
	}))
	setupNapUploadTest(t, ci)
	// the admitted upload stays pending on its prompt
	clearSessionRule(RuleKey{Napp: ci.napp.ID, Permission: PermUpload})
	ready(t, ci, rec, 1)

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	napUploadServers = func(context.Context, nostr.PubKey) []string {
		entered <- struct{}{}
		<-release
		return []string{"https://one.example"}
	}
	napUploadToServer = func(context.Context, string, []byte, string, string) (*blossom.BlobDescriptor, error) {
		t.Error("uploaded without approval")
		return nil, errors.New("not approved")
	}

	holdUploads(t, ci, "pending", "uploading", "pending")
	post(t, ci, uploadEnvelope("r1", []byte("first")))
	post(t, ci, uploadEnvelope("r2", []byte("second")))
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("both uploads should be past the synchronous check")
		}
	}
	close(release)

	pending, limited := 0, 0
	for _, id := range []string{"r1", "r2"} {
		got := waitID(t, rec, "upload.upload.result", id)
		switch {
		case got["error"] == napErrRateLimited:
			limited++
		case got["result"] != nil:
			pending++
		default:
			t.Fatalf("%s: %v", id, got)
		}
	}
	if pending != 1 || limited != 1 {
		t.Fatalf("%d admitted and %d refused, want 1 and 1", pending, limited)
	}
	ci.nap.mu.Lock()
	active := 0
	for _, u := range ci.nap.uploads {
		if u.Status == "pending" || u.Status == "uploading" {
			active++
		}
	}
	ci.nap.mu.Unlock()
	if active != uploadMaxActive {
		t.Fatalf("%d uploads active, want %d", active, uploadMaxActive)
	}
	// dismiss the admitted upload's prompt
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if p := CurrentPrompt(); p != nil {
			AnswerPrompt(p.ID, Answer{OK: false})
			break
		}
	}
	rec.wait(t, "upload.status.changed", 1)
}
