package backend

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// ─── sink rig ────────────────────────────────────────────────────

// napSinkCall is one sink a call reached, and whether it was approved.
type napSinkCall struct {
	sink     string
	approved bool
}

// napSinkLog records every sink any call reaches while a test runs.
type napSinkLog struct {
	mu    sync.Mutex
	calls []napSinkCall
}

func (l *napSinkLog) all() []napSinkCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls)
}

// names is the sinks reached, in order.
func (l *napSinkLog) names() []string {
	var out []string
	for _, c := range l.all() {
		out = append(out, c.sink)
	}
	return out
}

func setNapSinkHook(h func(sink string, approved bool)) {
	napSinkHookMu.Lock()
	napSinkHook = h
	napSinkHookMu.Unlock()
}

// recordSinks installs the sink hook for the test.
func recordSinks(t *testing.T) *napSinkLog {
	t.Helper()
	l := &napSinkLog{}
	setNapSinkHook(func(sink string, approved bool) {
		l.mu.Lock()
		l.calls = append(l.calls, napSinkCall{sink, approved})
		l.mu.Unlock()
	})
	t.Cleanup(func() { setNapSinkHook(nil) })
	return l
}

// ─── link.open, the tracer ───────────────────────────────────────

// TestNapLinkOpenGoesThroughItsGate: an approved link.open opens exactly its
// URL, and only through the openLink sink, approved.
func TestNapLinkOpenGoesThroughItsGate(t *testing.T) {
	setupNapTest(t)
	sinks := recordSinks(t)
	linkHost := &napLinkTestHost{}
	host = linkHost
	ci, rec := openNapplet(t, "gated-link")
	ready(t, ci, rec, 1)
	key := RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}
	setSessionRule(key, Rule{Decision: DecisionAllow})
	t.Cleanup(func() { clearSessionRule(key) })

	post(t, ci, map[string]any{"type": "link.open", "id": "l1", "url": "https://example.com/a"})
	got := rec.wait(t, "link.open.result", 1)
	if got["id"] != "l1" || got["status"] != "opened" {
		t.Fatalf("opened: %v", got)
	}
	if !slices.Equal(linkHost.opened, []string{"https://example.com/a"}) {
		t.Fatalf("opened URLs: %v", linkHost.opened)
	}
	if calls := sinks.all(); !slices.Equal(calls, []napSinkCall{{"openLink", true}}) {
		t.Fatalf("sinks: %v", calls)
	}
}

// TestNapDeniedRoutesMakeNoSinkCalls: with a stored deny rule for its
// permission, a gated route answers its denial shape and reaches no sink
// (D-04).
func TestNapDeniedRoutesMakeNoSinkCalls(t *testing.T) {
	setupNapTest(t)
	sinks := recordSinks(t)
	linkHost := &napLinkTestHost{}
	host = linkHost
	ci, rec := openNapplet(t, "denied-sinks")
	ready(t, ci, rec, 1)

	t.Run("link.open", func(t *testing.T) {
		key := RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}
		setSessionRule(key, Rule{Decision: DecisionDeny})
		t.Cleanup(func() { clearSessionRule(key) })
		post(t, ci, map[string]any{"type": "link.open", "id": "deny", "url": "https://example.com"})
		got := rec.wait(t, "link.open.result", 1)
		if got["id"] != "deny" || got["status"] != "denied" || got["error"] != napErrDenied {
			t.Fatalf("denied: %v", got)
		}
		if len(linkHost.opened) != 0 {
			t.Fatalf("opened %v", linkHost.opened)
		}
	})

	if calls := sinks.all(); len(calls) != 0 {
		t.Fatalf("denied routes reached sinks: %v", calls)
	}
}

// TestNapSinkRefusesWithoutItsGate: a handler that forgets to ask gets its
// sink refused, answered in the route's denial shape, with no side effect.
func TestNapSinkRefusesWithoutItsGate(t *testing.T) {
	setupNapTest(t)
	sinks := recordSinks(t)
	linkHost := &napLinkTestHost{}
	host = linkHost
	withTestRoute(t, "test.forgetful", napRoute{
		h: func(c *napCall) {
			if err := c.openLink("https://example.com/forgot"); err == nil {
				c.reply(map[string]any{"status": "opened"})
			}
		},
		gate: perCallGate(PermOpenLink), fail: failShape(failLink),
	})
	ci, rec := openNapplet(t, "forgetful")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "test.forgetful", "id": "f1"})
	got := rec.wait(t, "test.forgetful.result", 1)
	if got["id"] != "f1" || got["status"] != "denied" || got["error"] != napErrDenied {
		t.Fatalf("refused sink: %v", got)
	}
	if len(linkHost.opened) != 0 {
		t.Fatalf("a link opened without its gate: %v", linkHost.opened)
	}
	if calls := sinks.all(); !slices.Equal(calls, []napSinkCall{{"openLink", false}}) {
		t.Fatalf("sinks: %v", calls)
	}
	if n := len(rec.find("test.forgetful.result")); n != 1 {
		t.Fatalf("%d answers", n)
	}
}

// TestNapGateRefusesUndeclaredPermission: a gate question the route did not
// declare is refused as a bug, without a prompt, and answered user-denied.
func TestNapGateRefusesUndeclaredPermission(t *testing.T) {
	setupNapTest(t)
	sinks := recordSinks(t)
	type asked struct {
		ok  bool
		err error
	}
	results := make(chan asked, 4)
	ask := func(perm Permission) napHandler {
		return func(c *napCall) {
			ok, err := c.approve(perm, "do something", "", "")
			results <- asked{ok, err}
			if ok {
				c.reply(map[string]any{"status": "opened"})
			}
		}
	}
	withTestRoute(t, "test.wrongperm", napRoute{h: ask(PermPublish), gate: perCallGate(PermOpenLink), fail: failShape(failLink)})
	withTestRoute(t, "test.opengate", napRoute{h: ask(PermOpenLink), gate: openGate("test"), fail: failShape(failErr)})
	withTestRoute(t, "test.wrongkind", napRoute{
		h: func(c *napCall) {
			ok, err := c.grant(PermOpenLink, "do something", "")
			results <- asked{ok, err}
		},
		gate: perCallGate(PermOpenLink), fail: failShape(failLink),
	})
	ci, rec := openNapplet(t, "undeclared")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "test.wrongperm", "id": "w1"})
	if got := rec.wait(t, "test.wrongperm.result", 1); got["status"] != "denied" || got["error"] != napErrDenied {
		t.Fatalf("wrong permission: %v", got)
	}
	post(t, ci, map[string]any{"type": "test.opengate", "id": "o1"})
	if got := rec.wait(t, "test.opengate.result", 1); got["error"] != napErrDenied {
		t.Fatalf("open gate asking: %v", got)
	}
	// a session question on a per-call route is undeclared too
	post(t, ci, map[string]any{"type": "test.wrongkind", "id": "k1"})
	if got := rec.wait(t, "test.wrongkind.result", 1); got["status"] != "denied" || got["error"] != napErrDenied {
		t.Fatalf("wrong question kind: %v", got)
	}
	for range 3 {
		if r := <-results; r.ok || r.err != nil {
			t.Fatalf("undeclared question answered %v", r)
		}
	}
	if p := CurrentPrompt(); p != nil {
		t.Fatalf("an undeclared question prompted: %+v", p)
	}
	if calls := sinks.all(); len(calls) != 0 {
		t.Fatalf("sinks: %v", calls)
	}
}

// TestNapStorageRepliesUseTheVocabulary: storage failures answer codes,
// never prose or Go error text (D-07); over quota keeps NAP-STORAGE's own.
func TestNapStorageRepliesUseTheVocabulary(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "storage-codes")
	ready(t, ci, rec, 1)

	cases := []struct {
		env  map[string]any
		want string
	}{
		{map[string]any{"type": "storage.get", "id": "undecodable", "key": 7}, napErrInvalid},
		{map[string]any{"type": "storage.get", "id": "scope", "key": "a", "scope": "galaxy"}, napErrInvalid},
		{map[string]any{"type": "storage.get", "id": "nokey"}, napErrInvalid},
		{map[string]any{"type": "storage.set", "id": "novalue", "key": "a"}, napErrInvalid},
		{map[string]any{"type": "storage.remove", "id": "rmnokey"}, napErrInvalid},
	}
	for _, tc := range cases {
		post(t, ci, tc.env)
		typ := tc.env["type"].(string) + ".result"
		if got := waitID(t, rec, typ, tc.env["id"].(string)); got["error"] != tc.want {
			t.Errorf("%s: %v", tc.env["id"], got)
		}
	}

	big := make([]byte, nappletStorageQuota)
	for i := range big {
		big[i] = 'x'
	}
	post(t, ci, map[string]any{"type": "storage.set", "id": "quota", "key": "b", "value": string(big)})
	if got := waitID(t, rec, "storage.set.result", "quota"); got["error"] != "quota exceeded" {
		t.Errorf("over quota: %v", got)
	}

	// a write the disk refuses is the launcher's problem: internal-error,
	// and the path in the Go error stays in the log
	saved := dataDir
	blocked := filepath.Join(saved, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir = blocked
	t.Cleanup(func() { dataDir = saved })
	post(t, ci, map[string]any{"type": "storage.set", "id": "disk", "key": "c", "value": "v"})
	if got := waitID(t, rec, "storage.set.result", "disk"); got["error"] != napErrInternal {
		t.Errorf("persistence failure: %v", got)
	}
}
