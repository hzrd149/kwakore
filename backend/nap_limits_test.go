package backend

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

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
