package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withTestRoute registers a route for the test's duration. Tests that use it
// must not run in parallel: napRoutes is package state.
func withTestRoute(t *testing.T, typ string, r napRoute) {
	t.Helper()
	if err := validateRoute(typ, r); err != nil {
		t.Fatalf("test route: %v", err)
	}
	if _, exists := napRoutes[typ]; exists {
		t.Fatalf("test route %s shadows a registered one", typ)
	}
	napRoutes[typ] = &r
	t.Cleanup(func() { delete(napRoutes, typ) })
}

// napGoldenRoutes is every NAP request type with its gate and failure kind.
// Adding, removing or regating a type has to be done here too, on purpose.
var napGoldenRoutes = map[string]string{
	"theme.get": "gate=open fail=default",

	"storage.get":    "gate=open fail=err",
	"storage.set":    "gate=open fail=err",
	"storage.remove": "gate=open fail=err",
	"storage.keys":   "gate=open fail=err",

	"link.open": "gate=perCall:open_link fail=link",

	"config.registerSchema": "gate=open fail=okFalseCode",
	"config.get":            "gate=open fail=schemaError",
	"config.subscribe":      "gate=open fail=none",
	"config.unsubscribe":    "gate=open fail=none",
	"config.openSettings":   "gate=open fail=none",

	"notify.send":               "gate=session:notify fail=err",
	"notify.permission.request": "gate=session:notify fail=granted",
	"notify.dismiss":            "gate=open fail=none",
	"notify.badge":              "gate=open fail=none",
	"notify.channel.register":   "gate=open fail=none",

	"common.encodeNip19": "gate=open fail=okFalse",
	"common.decodeNip19": "gate=open fail=okFalse",
	"common.getProfile":  "gate=open fail=okFalse",
	"common.follows":     "gate=open fail=okFalse",
	"common.follow":      "gate=perCall:publish fail=okFalse",
	"common.unfollow":    "gate=perCall:publish fail=okFalse",
	"common.react":       "gate=perCall:publish fail=okFalse",
	"common.report":      "gate=perCall:publish fail=okFalse",

	"relay.subscribe":        "gate=open fail=lifecycle",
	"relay.close":            "gate=open fail=none",
	"relay.query":            "gate=open fail=err",
	"relay.publish":          "gate=perCall:publish fail=okFalse",
	"relay.publishEncrypted": "gate=perCall:publish fail=okFalse",

	"outbox.getEvent":      "gate=open fail=err",
	"outbox.resolveRelays": "gate=open fail=err",
	"outbox.query":         "gate=open fail=err",
	"outbox.subscribe":     "gate=open fail=lifecycle",
	"outbox.close":         "gate=open fail=lifecycle",
	"outbox.publish":       "gate=perCall:publish fail=okFalse",

	"identity.getPublicKey": "gate=open fail=default",
	"identity.getRelays":    "gate=open fail=err",
	"identity.getProfile":   "gate=open fail=err",
	"identity.getFollows":   "gate=open fail=err",
	"identity.getMutes":     "gate=open fail=err",
	"identity.getBlocked":   "gate=open fail=err",
	"identity.getZaps":      "gate=open fail=err",
	"identity.getBadges":    "gate=open fail=err",
	"identity.getList":      "gate=open fail=err",

	"intent.invoke":    "gate=open fail=intent",
	"intent.available": "gate=open fail=err",
	"intent.handlers":  "gate=open fail=err",

	"inc.emit":              "gate=open fail=none",
	"inc.subscribe":         "gate=open fail=err",
	"inc.unsubscribe":       "gate=open fail=none",
	"inc.channel.emit":      "gate=open fail=none",
	"inc.channel.broadcast": "gate=open fail=none",
	"inc.channel.close":     "gate=open fail=none",
	"inc.channel.open":      "gate=open fail=err",
	"inc.channel.list":      "gate=open fail=default",

	"upload.info":   "gate=open fail=err",
	"upload.status": "gate=open fail=err",
	"upload.upload": "gate=perCall:upload fail=err",

	"media.session.create":  "gate=dynamic:media fail=err",
	"media.session.update":  "gate=open fail=none",
	"media.session.destroy": "gate=open fail=none",
	"media.state":           "gate=open fail=none",
	"media.capabilities":    "gate=open fail=none",
	"media.command":         "gate=open fail=none",

	"resource.info":      "gate=open fail=typedErr",
	"resource.bytes":     "gate=dynamic:fetch fail=typedErr",
	"resource.bytesMany": "gate=dynamic:fetch fail=typedErr",
	"resource.cancel":    "gate=open fail=none",
}

// TestNapRouteTableGolden pins the route table. It compares sorted, so the
// order the nap_*.go inits run in does not matter.
func TestNapRouteTableGolden(t *testing.T) {
	if len(napGoldenRoutes) != 68 {
		t.Fatalf("the golden table has %d types, want 68", len(napGoldenRoutes))
	}
	if len(napRoutes) == 0 {
		t.Fatal("no NAP routes are registered")
	}
	got := map[string]string{}
	for typ, r := range napRoutes {
		got[typ] = r.String()
	}
	if !maps.Equal(got, napGoldenRoutes) {
		var b strings.Builder
		for _, typ := range slices.Sorted(maps.Keys(got)) {
			fmt.Fprintf(&b, "\t%q: %q,\n", typ, got[typ])
		}
		for _, typ := range slices.Sorted(maps.Keys(napGoldenRoutes)) {
			if got[typ] != napGoldenRoutes[typ] {
				t.Errorf("%s: registered %q, golden %q", typ, got[typ], napGoldenRoutes[typ])
			}
		}
		for _, typ := range slices.Sorted(maps.Keys(got)) {
			if _, ok := napGoldenRoutes[typ]; !ok {
				t.Errorf("%s is registered but not in the golden table", typ)
			}
		}
		t.Errorf("the route table changed; the registered table is:\n%s", b.String())
	}
	// every declared route has its handler: a spec without one would never run
	for _, typ := range slices.Sorted(maps.Keys(napRouteSpecs)) {
		if r := napRoutes[typ]; r == nil || r.h == nil {
			t.Errorf("declared route %s has no registered handler", typ)
		}
	}
}

func TestNapRouteRegistrationPanics(t *testing.T) {
	h := func(*napCall) {}
	ok := napRoute{gate: openGate("fine"), fail: failShape(failErr)}
	specs := map[string]napRoute{"x.a": ok, "x.b": ok, "x.bad": {gate: openGate(""), fail: failShape(failErr)}}

	panics := func(name, want string, fn func()) {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("did not panic")
				}
				if !strings.Contains(fmt.Sprint(r), want) {
					t.Fatalf("panic %q does not mention %q", r, want)
				}
			}()
			fn()
		})
	}
	panics("duplicate type", "duplicate NAP handler for x.a", func() {
		dst := map[string]*napRoute{}
		registerNapRoutes(dst, specs, map[string]napHandler{"x.a": h})
		// a second registration collides; it never merges
		registerNapRoutes(dst, specs, map[string]napHandler{"x.a": h})
	})
	panics("handler without a declared route", "x.undeclared has no declared route", func() {
		registerNapRoutes(map[string]*napRoute{}, specs, map[string]napHandler{"x.undeclared": h})
	})
	panics("invalid declared route", "needs a reason", func() {
		registerNapRoutes(map[string]*napRoute{}, specs, map[string]napHandler{"x.bad": h})
	})
	panics("nil handler", "x.b is nil", func() {
		registerNapRoutes(map[string]*napRoute{}, specs, map[string]napHandler{"x.b": nil})
	})
	panics("empty registration", "no NAP handlers", func() {
		registerNapRoutes(map[string]*napRoute{}, specs, map[string]napHandler{})
	})

	dst := map[string]*napRoute{}
	registerNapRoutes(dst, specs, map[string]napHandler{"x.a": h, "x.b": h})
	if len(dst) != 2 || dst["x.a"].h == nil || dst["x.a"] == dst["x.b"] {
		t.Fatalf("valid registration: %v", dst)
	}

	invalid := map[string]napRoute{
		"unset gate":               {fail: failShape(failErr)},
		"open without reason":      {gate: openGate(""), fail: failShape(failErr)},
		"open with blank reason":   {gate: openGate("  "), fail: failShape(failErr)},
		"dynamic without reason":   {gate: dynamicGate("", PermFetch), fail: failShape(failErr)},
		"dynamic without perms":    {gate: dynamicGate("x"), fail: failShape(failErr)},
		"dynamic with empty perm":  {gate: dynamicGate("x", ""), fail: failShape(failErr)},
		"session without perm":     {gate: sessionGate(""), fail: failShape(failErr)},
		"per-call without perm":    {gate: perCallGate(""), fail: failShape(failErr)},
		"unset failure":            {gate: openGate("x")},
		"lifecycle without closed": {gate: openGate("x"), fail: failShape(failLifecycle)},
	}
	for name, r := range invalid {
		if validateRoute("x.y", r) == nil {
			t.Errorf("%s: validateRoute accepted it", name)
		}
	}
	for typ, r := range napRouteSpecs {
		if err := validateRoute(typ, r); err != nil {
			t.Errorf("declared route: %v", err)
		}
	}
}

// TestNapOpenReasonsNameTheirOwner: an Open gate is a promise that someone
// owns the real consent question; for the types whose consent moves later
// (D-03), the reason names who.
func TestNapOpenReasonsNameTheirOwner(t *testing.T) {
	owner := regexp.MustCompile(`Phase [0-9]|[A-Z]+-[0-9]+`)
	owned := func(typ string) bool {
		return strings.HasPrefix(typ, "storage.") || strings.HasPrefix(typ, "identity.") ||
			slices.Contains([]string{"intent.invoke", "inc.channel.open", "inc.emit", "config.openSettings"}, typ)
	}
	checked := 0
	for _, typ := range slices.Sorted(maps.Keys(napRoutes)) {
		g := napRoutes[typ].gate
		if g.kind != napGateOpen && g.kind != napGateDynamic {
			continue
		}
		if strings.TrimSpace(g.reason) == "" {
			t.Errorf("%s: %s gate without a reason", typ, g.kind)
		}
		if owned(typ) {
			checked++
			if !owner.MatchString(g.reason) {
				t.Errorf("%s: reason %q names no requirement or phase", typ, g.reason)
			}
		}
	}
	// 4 storage, 9 identity, and the 4 named ones
	if checked != 17 {
		t.Errorf("checked %d D-03 reasons, want 17", checked)
	}
}

// TestNapDispatchShortCircuitsStoredDenials: a stored deny rule or a session
// answer of no is answered by the dispatcher, in the route's denial shape,
// and the handler never runs (D-04). Dynamic routes always reach theirs.
func TestNapDispatchShortCircuitsStoredDenials(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "denied")
	ready(t, ci, rec, 1)

	var linkCalls, notifyCalls, dynamicCalls atomic.Int32
	withTestRoute(t, "test.link", napRoute{
		h:    func(c *napCall) { linkCalls.Add(1); c.reply(map[string]any{"status": "opened"}) },
		gate: perCallGate(PermOpenLink), fail: failShape(failLink),
	})
	withTestRoute(t, "test.notify", napRoute{
		h:    func(c *napCall) { notifyCalls.Add(1); c.reply(map[string]any{}) },
		gate: sessionGate(PermNotify),
		fail: failShape(failErr).withCodes(map[string]string{napErrDenied: "permission denied"}),
	})
	withTestRoute(t, "test.dynamic", napRoute{
		h:    func(c *napCall) { dynamicCalls.Add(1); c.reply(map[string]any{"ran": true}) },
		gate: dynamicGate("decides per payload", PermOpenLink), fail: failShape(failErr),
	})

	key := RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}
	setSessionRule(key, Rule{Decision: DecisionDeny})
	t.Cleanup(func() { clearSessionRule(key) })

	post(t, ci, map[string]any{"type": "test.link", "id": "l1"})
	if got := rec.wait(t, "test.link.result", 1); got["id"] != "l1" || got["status"] != "denied" || got["error"] != napErrDenied {
		t.Fatalf("denied link: %v", got)
	}

	ci.nap.mu.Lock()
	ci.nap.grants[PermNotify] = false
	ci.nap.mu.Unlock()
	post(t, ci, map[string]any{"type": "test.notify", "id": "n1"})
	if got := rec.wait(t, "test.notify.result", 1); got["id"] != "n1" || got["error"] != "permission denied" {
		t.Fatalf("denied session: %v", got)
	}

	// the same deny rule does not stop a Dynamic route: its handler decides
	post(t, ci, map[string]any{"type": "test.dynamic", "id": "d1"})
	if got := rec.wait(t, "test.dynamic.result", 1); got["ran"] != true {
		t.Fatalf("dynamic route: %v", got)
	}

	if linkCalls.Load() != 0 || notifyCalls.Load() != 0 {
		t.Fatalf("a denied handler ran: link %d, notify %d", linkCalls.Load(), notifyCalls.Load())
	}
	if dynamicCalls.Load() != 1 {
		t.Fatalf("the dynamic handler ran %d times", dynamicCalls.Load())
	}

	// with the denials gone, the same routes reach their handlers
	setSessionRule(key, Rule{Decision: DecisionAllow})
	ci.nap.mu.Lock()
	ci.nap.grants[PermNotify] = true
	ci.nap.mu.Unlock()
	post(t, ci, map[string]any{"type": "test.link", "id": "l2"})
	post(t, ci, map[string]any{"type": "test.notify", "id": "n2"})
	if got := rec.wait(t, "test.link.result", 2); got["status"] != "opened" {
		t.Fatalf("allowed link: %v", got)
	}
	rec.wait(t, "test.notify.result", 2)
	if linkCalls.Load() != 1 || notifyCalls.Load() != 1 {
		t.Fatalf("allowed handlers: link %d, notify %d", linkCalls.Load(), notifyCalls.Load())
	}
}

// settled waits until the worker has handled everything posted before it: a
// sentinel route answers, and the queue is in order.
func napSettled(t *testing.T, ci *Instance, rec *recTransport) {
	t.Helper()
	n := len(rec.find("test.sentinel.result")) + 1
	post(t, ci, map[string]any{"type": "test.sentinel", "id": "sentinel"})
	rec.wait(t, "test.sentinel.result", n)
}

// TestNapRepliesExactlyOnce: every request with an id gets one answer, in
// its route's shape, whatever the handler does (D-06).
func TestNapRepliesExactlyOnce(t *testing.T) {
	setupNapTest(t)
	open := openGate("test")
	withTestRoute(t, "test.sentinel", napRoute{h: func(c *napCall) { c.reply(nil) }, gate: open, fail: failShape(failErr)})
	ci, rec := openNapplet(t, "once")
	ready(t, ci, rec, 1)

	t.Run("sync handler forgets to answer", func(t *testing.T) {
		withTestRoute(t, "test.silent", napRoute{h: func(*napCall) {}, gate: open, fail: failShape(failOkFalse)})
		post(t, ci, map[string]any{"type": "test.silent", "id": "s1"})
		got := rec.wait(t, "test.silent.result", 1)
		if got["id"] != "s1" || got["ok"] != false || got["error"] != napErrInternal {
			t.Fatalf("auto-fail: %v", got)
		}
		napSettled(t, ci, rec)
		if n := len(rec.find("test.silent.result")); n != 1 {
			t.Fatalf("%d answers", n)
		}
	})

	t.Run("async closure forgets to answer", func(t *testing.T) {
		release := make(chan struct{})
		withTestRoute(t, "test.asyncSilent", napRoute{
			h:    func(c *napCall) { c.async(func(context.Context) { <-release }) },
			gate: open, fail: failShape(failTypedErr),
		})
		post(t, ci, map[string]any{"type": "test.asyncSilent", "id": "a1"})
		napSettled(t, ci, rec)
		time.Sleep(20 * time.Millisecond)
		// handed off: nothing is said while the closure still runs
		if got := rec.find("test.asyncSilent.error"); len(got) != 0 {
			t.Fatalf("answered before the closure returned: %v", got)
		}
		close(release)
		if got := rec.wait(t, "test.asyncSilent.error", 1); got["id"] != "a1" || got["error"] != napErrInternal {
			t.Fatalf("async auto-fail: %v", got)
		}
	})

	t.Run("second reply is dropped", func(t *testing.T) {
		withTestRoute(t, "test.twice", napRoute{
			h: func(c *napCall) {
				c.reply(map[string]any{"n": 1})
				c.reply(map[string]any{"n": 2})
				c.replyAs("test.twice.error", map[string]any{"error": "late"})
			},
			gate: open, fail: failShape(failErr),
		})
		post(t, ci, map[string]any{"type": "test.twice", "id": "t1"})
		napSettled(t, ci, rec)
		if got := rec.find("test.twice.result"); len(got) != 1 || got[0]["n"] != float64(1) {
			t.Fatalf("answers: %v", got)
		}
		if got := rec.find("test.twice.error"); len(got) != 0 {
			t.Fatalf("late typed answer delivered: %v", got)
		}
	})

	t.Run("lifecycle route whose payload does not decode", func(t *testing.T) {
		// the exact subId passes napEnqueue, the struct decode fails on the
		// relay; relay.* has no shim timeout, so this must still end (WR-02)
		post(t, ci, map[string]any{"type": "relay.subscribe", "subId": "bad-decode", "filters": []any{map[string]any{}}, "relay": 5})
		got := rec.wait(t, "relay.closed", 1)
		if got["subId"] != "bad-decode" || got["reason"] != "invalid: invalid-request" {
			t.Fatalf("undecodable subscribe: %v", got)
		}
		napSettled(t, ci, rec)
		if n := len(rec.find("relay.closed")); n != 1 {
			t.Fatalf("%d answers", n)
		}
	})

	t.Run("reply-less route gets nothing", func(t *testing.T) {
		withTestRoute(t, "test.fire", napRoute{h: func(*napCall) {}, gate: open, fail: failShape(failNone)})
		post(t, ci, map[string]any{"type": "test.fire", "id": "f1"})
		napSettled(t, ci, rec)
		for _, p := range rec.find("test.fire.result") {
			t.Fatalf("reply-less route answered: %v", p)
		}
	})

	t.Run("lifecycle route whose async exits silently gets nothing", func(t *testing.T) {
		done := make(chan struct{})
		withTestRoute(t, "test.sub", napRoute{
			h:    func(c *napCall) { c.async(func(context.Context) { close(done) }) },
			gate: open, fail: lifecycleShape("test.closed"),
		})
		post(t, ci, map[string]any{"type": "test.sub", "id": "l1", "subId": "sub"})
		<-done
		time.Sleep(20 * time.Millisecond)
		napSettled(t, ci, rec)
		if got := append(rec.find("test.closed"), rec.find("test.sub.result")...); len(got) != 0 {
			t.Fatalf("lifecycle route answered: %v", got)
		}
	})

	t.Run("reply then panic delivers only the reply", func(t *testing.T) {
		withTestRoute(t, "test.replyBoom", napRoute{
			h:    func(c *napCall) { c.reply(map[string]any{"ok": true}); panic("after") },
			gate: open, fail: failShape(failOkFalse),
		})
		post(t, ci, map[string]any{"type": "test.replyBoom", "id": "r1"})
		napSettled(t, ci, rec)
		if got := rec.find("test.replyBoom.result"); len(got) != 1 || got[0]["ok"] != true {
			t.Fatalf("answers: %v", got)
		}
	})

	t.Run("racing answers deliver exactly one", func(t *testing.T) {
		withTestRoute(t, "test.race", napRoute{
			h: func(c *napCall) {
				c.async(func(context.Context) {
					var wg sync.WaitGroup
					for i := range 8 {
						wg.Go(func() { c.reply(map[string]any{"n": i}) })
						wg.Go(func() { c.failWith(napErrInternal) })
					}
					wg.Wait()
					panic("and a panic on top")
				})
			},
			gate: open, fail: failShape(failErr),
		})
		post(t, ci, map[string]any{"type": "test.race", "id": "x1"})
		rec.wait(t, "test.race.result", 1)
		time.Sleep(30 * time.Millisecond)
		if got := rec.find("test.race.result"); len(got) != 1 {
			t.Fatalf("%d answers: %v", len(got), got)
		}
	})

	t.Run("relay.publish fails in its result (R-2)", func(t *testing.T) {
		post(t, ci, map[string]any{"type": "relay.publish", "id": "p1", "event": "not an event"})
		got := rec.wait(t, "relay.publish.result", 1)
		if got["id"] != "p1" || got["ok"] != false || got["error"] != napErrInvalid {
			t.Fatalf("relay.publish decode failure: %v", got)
		}
		if got := rec.find("relay.publish.error"); len(got) != 0 {
			t.Fatalf("relay.publish answered with .error: %v", got)
		}
	})

	t.Run("a panic in relay.publish's shape", func(t *testing.T) {
		spec := napRouteSpecs["relay.publish"]
		withTestRoute(t, "test.publishBoom", napRoute{h: func(*napCall) { panic("boom") }, gate: spec.gate, fail: spec.fail})
		post(t, ci, map[string]any{"type": "test.publishBoom", "id": "pb"})
		got := rec.wait(t, "test.publishBoom.result", 1)
		if got["id"] != "pb" || got["ok"] != false || got["error"] != napErrInternal {
			t.Fatalf("panic answer: %v", got)
		}
		if got := rec.find("test.publishBoom.error"); len(got) != 0 {
			t.Fatalf("answered with .error: %v", got)
		}
	})

	t.Run("cancelled resource.bytesMany is never answered", func(t *testing.T) {
		started := make(chan struct{}, 1)
		prev := resourceClient
		resourceClient = &http.Client{Transport: napRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			started <- struct{}{}
			<-r.Context().Done()
			return nil, r.Context().Err()
		})}
		t.Cleanup(func() { resourceClient = prev })
		ci.nap.mu.Lock()
		ci.nap.grants[PermFetch] = true
		ci.nap.mu.Unlock()

		post(t, ci, map[string]any{"type": "resource.bytesMany", "id": "many", "urls": []string{"https://8.8.8.8/blob"}})
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("the fetch never started")
		}
		post(t, ci, map[string]any{"type": "resource.cancel", "id": "many"})
		napSettled(t, ci, rec)
		time.Sleep(100 * time.Millisecond)
		if got := append(rec.find("resource.bytesMany.result"), rec.find("resource.bytesMany.error")...); len(got) != 0 {
			t.Fatalf("a cancelled bulk fetch was answered: %v", got)
		}
	})
}

type napRoundTripFunc func(*http.Request) (*http.Response, error)

func (f napRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestNapFailShapesSettleTheShim runs every route's failure through the rule
// the pristine shim settles that type by, restated per kind: an answer the
// shim drops or rejects as malformed would leave the napplet waiting.
func TestNapFailShapesSettleTheShim(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "shapes")
	ready(t, ci, rec, 1)
	ci.nap.mu.Lock()
	gen, ctx := ci.nap.gen, ci.nap.ctx
	ci.nap.mu.Unlock()

	str := func(v any) bool { _, ok := v.(string); return ok }
	for _, typ := range slices.Sorted(maps.Keys(napRouteSpecs)) {
		r := napRoutes[typ]
		raw, _ := json.Marshal(map[string]any{
			"type": typ, "id": "x", "subId": "s",
			"request": map[string]any{"archetype": "profile", "action": "open"},
		})
		c := &napCall{ci: ci, gen: gen, ctx: ctx, route: r, Type: typ, ID: json.RawMessage(`"x"`), raw: raw}
		before := len(rec.types())
		c.failWith(napErrInternal)
		if !c.answered.Load() {
			t.Errorf("%s: failWith did not claim the call", typ)
		}
		rec.mu.Lock()
		pushed := slices.Clone(rec.pushes[before:])
		rec.mu.Unlock()

		if r.fail.kind == failNone {
			if len(pushed) != 0 {
				t.Errorf("%s: reply-less route answered %v", typ, pushed)
			}
			continue
		}
		if len(pushed) != 1 {
			t.Errorf("%s: %d answers: %v", typ, len(pushed), pushed)
			continue
		}
		got := pushed[0]
		if r.fail.kind == failLifecycle {
			if got["type"] != r.fail.closed || got["subId"] != "s" || !str(got["reason"]) {
				t.Errorf("%s: lifecycle failure %v", typ, got)
			}
			continue
		}
		if got["id"] != "x" {
			t.Errorf("%s: answer without the request's id: %v", typ, got)
		}
		for k := range r.fail.fields {
			if _, ok := got[k]; !ok {
				t.Errorf("%s: answer lacks static field %s: %v", typ, k, got)
			}
		}
		switch r.fail.kind {
		case failErr:
			ok := got["type"] == typ+".result" && str(got["error"])
			if !ok {
				t.Errorf("%s: err failure %v", typ, got)
			}
		case failOkFalse:
			if got["type"] != typ+".result" || got["ok"] != false || !str(got["error"]) {
				t.Errorf("%s: okFalse failure %v", typ, got)
			}
		case failOkFalseCode:
			if got["type"] != typ+".result" || got["ok"] != false || !str(got["code"]) || !str(got["error"]) {
				t.Errorf("%s: okFalseCode failure %v", typ, got)
			}
		case failTypedErr:
			if got["type"] != typ+".error" || !str(got["error"]) {
				t.Errorf("%s: typed failure %v", typ, got)
			}
		case failLink:
			if got["type"] != typ+".result" || got["status"] != "denied" || !str(got["error"]) {
				t.Errorf("%s: link failure %v", typ, got)
			}
		case failIntent:
			res, _ := got["result"].(map[string]any)
			if got["type"] != typ+".result" || res == nil || res["ok"] != false || res["handled"] != false ||
				res["archetype"] != "profile" || res["action"] != "open" || !str(res["error"]) {
				t.Errorf("%s: intent failure %v", typ, got)
			}
		case failDefault:
			if got["type"] != typ+".result" {
				t.Errorf("%s: default failure %v", typ, got)
			}
			if _, has := got["error"]; has {
				t.Errorf("%s: a default failure carries an error: %v", typ, got)
			}
		case failSchemaError:
			if got["type"] != "config.schemaError" || !str(got["code"]) {
				t.Errorf("%s: schemaError failure %v", typ, got)
			}
		case failGranted:
			if got["type"] != "notify.permission.result" || got["granted"] != false {
				t.Errorf("%s: granted failure %v", typ, got)
			}
		default:
			t.Errorf("%s: unchecked failure kind %s", typ, r.fail.kind)
		}

		// the types whose shim handlers are the strictest
		switch typ {
		case "inc.channel.list":
			if _, ok := got["channels"].([]any); !ok {
				t.Errorf("inc.channel.list without a channels list: %v", got)
			}
		case "identity.getPublicKey":
			if !str(got["pubkey"]) {
				t.Errorf("identity.getPublicKey without a string pubkey: %v", got)
			}
		case "theme.get":
			theme, _ := got["theme"].(map[string]any)
			colors, _ := theme["colors"].(map[string]any)
			if !str(colors["background"]) || !str(colors["text"]) || !str(colors["primary"]) {
				t.Errorf("theme.get without its three colors: %v", got)
			}
		case "relay.publish", "relay.publishEncrypted":
			if got["type"] != typ+".result" {
				t.Errorf("%s fails outside its result: %v", typ, got)
			}
		}
	}

	// an intent request without an archetype or action still gets typed ones
	c := &napCall{ci: ci, gen: gen, ctx: ctx, route: napRoutes["intent.invoke"], Type: "intent.invoke",
		ID: json.RawMessage(`"y"`), raw: json.RawMessage(`{"type":"intent.invoke","id":"y","request":{"archetype":7,"action":""}}`)}
	c.failWith(napErrDenied)
	got := rec.wait(t, "intent.invoke.result", 2)
	res, _ := got["result"].(map[string]any)
	if res["archetype"] != "" || res["action"] != "open" || res["error"] != "user cancelled" {
		t.Fatalf("intent defaults and code map: %v", got)
	}

	// the static fields are copied per failure, never shared with the table
	c = &napCall{ci: ci, gen: gen, ctx: ctx, route: napRoutes["relay.query"], Type: "relay.query", ID: json.RawMessage(`"q"`)}
	c.failWith(napErrInternal)
	if events := napRoutes["relay.query"].fail.fields["events"].([]any); len(events) != 0 {
		t.Fatalf("relay.query's static events changed: %v", events)
	}
}

func TestSafeGoRecovers(t *testing.T) {
	setupNapTest(t)
	withTestRoute(t, "test.safe", napRoute{h: func(*napCall) {}, gate: openGate("test"), fail: failShape(failOkFalse)})
	ci, rec := openNapplet(t, "safe")
	ready(t, ci, rec, 1)
	ci.nap.mu.Lock()
	gen, ctx := ci.nap.gen, ci.nap.ctx
	ci.nap.mu.Unlock()

	c := &napCall{ci: ci, gen: gen, ctx: ctx, route: napRoutes["test.safe"], Type: "test.safe", ID: json.RawMessage(`"s1"`)}
	safeGo(c, "test", func() { panic("boom") })
	got := rec.wait(t, "test.safe.result", 1)
	if got["id"] != "s1" || got["ok"] != false || got["error"] != napErrInternal {
		t.Fatalf("safeGo failure: %v", got)
	}
	// a second panicking goroutine for the same call says nothing more
	safeGo(c, "test", func() { panic("again") })

	// without a call there is nothing to answer: it only logs
	done := make(chan struct{})
	safeGo(nil, "test", func() {
		defer close(done)
		panic("nobody to tell")
	})
	<-done
	time.Sleep(30 * time.Millisecond)
	if got := rec.find("test.safe.result"); len(got) != 1 {
		t.Fatalf("%d answers: %v", len(got), got)
	}
}
