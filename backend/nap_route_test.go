package backend

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
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
