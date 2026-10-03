package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nipb7/blossom"
	"fiatjaf.com/nostr/sdk"
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
	withSentinel(t)
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

	t.Run("publishing", func(t *testing.T) {
		withSignedInUser(t)
		key := RuleKey{Napp: ci.napp.ID, Permission: PermPublish}
		setSessionRule(key, Rule{Decision: DecisionDeny})
		t.Cleanup(func() { clearSessionRule(key) })
		note := map[string]any{"kind": 1, "content": "hi", "tags": []any{}}
		post(t, ci, map[string]any{"type": "relay.publish", "id": "p1", "event": note, "relay": "wss://8.8.8.8"})
		post(t, ci, map[string]any{"type": "relay.publishEncrypted", "id": "p2", "event": note, "recipient": nostr.Generate().Public().Hex()})
		post(t, ci, map[string]any{"type": "outbox.publish", "id": "p3", "event": note})
		post(t, ci, map[string]any{"type": "common.follow", "id": "p4", "pubkey": nostr.Generate().Public().Hex()})
		for _, want := range []struct{ typ, id, code string }{
			{"relay.publish.result", "p1", napErrDenied},
			{"relay.publishEncrypted.result", "p2", napErrDenied},
			{"outbox.publish.result", "p3", "publish denied"},
			{"common.follow.result", "p4", napErrDenied},
		} {
			if got := waitID(t, rec, want.typ, want.id); got["ok"] != false || got["error"] != want.code {
				t.Errorf("%s: %v", want.typ, got)
			}
		}
	})

	t.Run("upload.upload", func(t *testing.T) {
		setupNapUploadTest(t, ci)
		key := RuleKey{Napp: ci.napp.ID, Permission: PermUpload}
		setSessionRule(key, Rule{Decision: DecisionDeny})
		post(t, ci, uploadEnvelope("u-deny", []byte("no")))
		if got := waitID(t, rec, "upload.upload.result", "u-deny"); got["error"] != "policy denied" {
			t.Fatalf("denied upload: %v", got)
		}
	})

	t.Run("notify", func(t *testing.T) {
		nh := &notifyTestHost{permission: true}
		host = nh
		key := RuleKey{Napp: ci.napp.ID, Permission: PermNotify}
		setSessionRule(key, Rule{Decision: DecisionDeny})
		t.Cleanup(func() { clearSessionRule(key) })
		post(t, ci, map[string]any{"type": "notify.permission.request", "id": "np"})
		post(t, ci, map[string]any{"type": "notify.send", "id": "ns", "title": "hello"})
		if got := waitID(t, rec, "notify.permission.result", "np"); got["granted"] != false {
			t.Errorf("permission: %v", got)
		}
		if got := waitID(t, rec, "notify.send.result", "ns"); got["error"] != "permission denied" {
			t.Errorf("send: %v", got)
		}
		if len(nh.requests) != 0 {
			t.Errorf("notified: %v", nh.requests)
		}
	})

	napSettled(t, ci, rec)
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

// ─── publish, upload, notify ─────────────────────────────────────

// withSentinel registers the route napSettled posts.
func withSentinel(t *testing.T) {
	t.Helper()
	withTestRoute(t, "test.sentinel", napRoute{h: func(c *napCall) { c.reply(nil) }, gate: openGate("test"), fail: failShape(failErr)})
}

// withSignedInUser signs a fresh user in with a plain key for the test.
func withSignedInUser(t *testing.T) nostr.SecretKey {
	t.Helper()
	prevKeyer, prevPK := userKeyer, userPubkey
	sk := nostr.Generate()
	userKeyer, userPubkey = keyer.NewPlainKeySigner(sk), sk.Public()
	t.Cleanup(func() { userKeyer, userPubkey = prevKeyer, prevPK })
	return sk
}

// staticRelayLists answers every relay list lookup from memory, so a
// publish finds its targets without asking the network.
type staticRelayLists map[nostr.PubKey]sdk.GenericList[string, sdk.Relay]

func (s staticRelayLists) Get(k [32]byte) (sdk.GenericList[string, sdk.Relay], bool) {
	v, ok := s[nostr.PubKey(k)]
	return v, ok
}
func (staticRelayLists) Delete([32]byte)                                       {}
func (staticRelayLists) Set([32]byte, sdk.GenericList[string, sdk.Relay]) bool { return true }
func (staticRelayLists) SetWithTTL([32]byte, sdk.GenericList[string, sdk.Relay], time.Duration) bool {
	return true
}

// fakePublishing stands in for the relays: every publish lands, and the
// events it was given are recorded.
func fakePublishing(t *testing.T) *[]nostr.Event {
	t.Helper()
	var mu sync.Mutex
	var published []nostr.Event
	saved := napPublishSigned
	napPublishSigned = func(_ context.Context, evt nostr.Event, targets []string) map[string]any {
		mu.Lock()
		published = append(published, evt)
		mu.Unlock()
		relays := map[string]any{}
		for _, u := range targets {
			relays[u] = map[string]any{"ok": true}
		}
		return map[string]any{"relays": relays, "published": len(targets), "failed": 0}
	}
	t.Cleanup(func() { napPublishSigned = saved })
	return &published
}

// TestNapPublishGoesThroughItsSinks: an approved publish signs and publishes
// through the call's sinks, in order, with the encryption first when there
// is one.
func TestNapPublishGoesThroughItsSinks(t *testing.T) {
	setupNapTest(t)
	withSystem(t)
	sk := withSignedInUser(t)
	published := fakePublishing(t)
	sinks := recordSinks(t)
	ci, rec := openNapplet(t, "publisher")
	ready(t, ci, rec, 1)
	key := RuleKey{Napp: ci.napp.ID, Permission: PermPublish}
	setSessionRule(key, Rule{Decision: DecisionAllow})
	t.Cleanup(func() { clearSessionRule(key) })

	note := map[string]any{"kind": 1, "content": "hello", "tags": []any{}}
	post(t, ci, map[string]any{"type": "relay.publish", "id": "plain", "event": note, "relay": "wss://8.8.8.8"})
	if got := waitID(t, rec, "relay.publish.result", "plain"); got["ok"] != true {
		t.Fatalf("publish: %v", got)
	}
	if names := sinks.names(); !slices.Equal(names, []string{"sign", "publish"}) {
		t.Fatalf("publish sinks: %v", names)
	}

	peer := nostr.Generate()
	lists := staticRelayLists{}
	for _, pk := range []nostr.PubKey{sk.Public(), peer.Public()} {
		lists[pk] = sdk.GenericList[string, sdk.Relay]{PubKey: pk, Items: []sdk.Relay{{URL: "wss://8.8.4.4", Inbox: true, Outbox: true}}}
	}
	sys.RelayListCache = lists
	post(t, ci, map[string]any{"type": "relay.publishEncrypted", "id": "secret", "event": map[string]any{
		"kind": 4, "content": "for your eyes", "tags": []any{},
	}, "recipient": peer.Public().Hex(), "encryption": "nip04"})
	if got := waitID(t, rec, "relay.publishEncrypted.result", "secret"); got["ok"] != true {
		t.Fatalf("publishEncrypted: %v", got)
	}
	if names := sinks.names(); !slices.Equal(names, []string{"sign", "publish", "encrypt", "sign", "publish"}) {
		t.Fatalf("publishEncrypted sinks: %v", names)
	}
	for _, c := range sinks.all() {
		if !c.approved {
			t.Fatalf("an unapproved sink call: %v", sinks.all())
		}
	}
	if len(*published) != 2 || (*published)[1].Content == "for your eyes" || !(*published)[1].VerifySignature() {
		t.Fatalf("published: %v", *published)
	}
}

// TestNapUploadGoesThroughItsSinks: an approved upload signs its
// authorization and PUTs through the call's sinks; a denied one reaches
// neither.
func TestNapUploadGoesThroughItsSinks(t *testing.T) {
	setupNapTest(t)
	sinks := recordSinks(t)
	ci, rec := openNapplet(t, "upload-sinks")
	setupNapUploadTest(t, ci)
	ready(t, ci, rec, 1)

	data := []byte("through the gate")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	napUploadServers = func(context.Context, nostr.PubKey) []string { return []string{"https://one.example"} }
	napUploadToServer = func(_ context.Context, server string, got []byte, mimeType, _ string) (*blossom.BlobDescriptor, error) {
		return &blossom.BlobDescriptor{URL: server + "/" + hash, SHA256: hash, Size: len(got), Type: mimeType}, nil
	}
	post(t, ci, uploadEnvelope("up", data))
	rec.wait(t, "upload.upload.result", 1)
	if got := rec.wait(t, "upload.status.changed", 2)["status"].(map[string]any); got["status"] != "complete" {
		t.Fatalf("upload: %v", got)
	}
	if calls := sinks.all(); !slices.Equal(calls, []napSinkCall{{"uploadAuth", true}, {"uploadToServer", true}}) {
		t.Fatalf("upload sinks: %v", calls)
	}

	setSessionRule(RuleKey{Napp: ci.napp.ID, Permission: PermUpload}, Rule{Decision: DecisionDeny})
	post(t, ci, uploadEnvelope("down", data))
	if got := waitID(t, rec, "upload.upload.result", "down"); got["error"] != "policy denied" {
		t.Fatalf("denied upload: %v", got)
	}
	if n := len(sinks.all()); n != 2 {
		t.Fatalf("a denied upload reached sinks: %v", sinks.all())
	}
}

// TestNapNotifyGoesThroughItsSinks: a granted permission request asks the
// platform through its sink, and a send after it notifies through its sink;
// with a stored denial neither is reached.
func TestNapNotifyGoesThroughItsSinks(t *testing.T) {
	setupNapTest(t)
	sinks := recordSinks(t)
	nh := &notifyTestHost{permission: true}
	host = nh
	ci, rec := openNapplet(t, "notify-sinks")
	ready(t, ci, rec, 1)
	key := RuleKey{Napp: ci.napp.ID, Permission: PermNotify}
	setSessionRule(key, Rule{Decision: DecisionAllow})
	t.Cleanup(func() { clearSessionRule(key) })

	post(t, ci, map[string]any{"type": "notify.permission.request", "id": "ask"})
	if got := waitID(t, rec, "notify.permission.result", "ask"); got["granted"] != true {
		t.Fatalf("permission: %v", got)
	}
	post(t, ci, map[string]any{"type": "notify.send", "id": "send", "title": "hello"})
	if got := waitID(t, rec, "notify.send.result", "send"); got["notificationId"] == nil {
		t.Fatalf("send: %v", got)
	}
	if calls := sinks.all(); !slices.Equal(calls, []napSinkCall{{"requestNotifyPermission", true}, {"notify", true}}) {
		t.Fatalf("notify sinks: %v", calls)
	}

	// a fixed string for each validation failure, never Go error text
	post(t, ci, map[string]any{"type": "notify.send", "id": "icon", "title": "x", "icon": "https://example.com/i.png"})
	if got := waitID(t, rec, "notify.send.result", "icon"); got["error"] != "unsupported icon" {
		t.Fatalf("icon: %v", got)
	}

	// a new window with the rule turned to deny reaches neither sink
	setSessionRule(key, Rule{Decision: DecisionDeny})
	other, orec := openNapplet(t, "notify-sinks-denied")
	ready(t, other, orec, 1)
	otherKey := RuleKey{Napp: other.napp.ID, Permission: PermNotify}
	setSessionRule(otherKey, Rule{Decision: DecisionDeny})
	t.Cleanup(func() { clearSessionRule(otherKey) })
	post(t, other, map[string]any{"type": "notify.permission.request", "id": "ask2"})
	post(t, other, map[string]any{"type": "notify.send", "id": "send2", "title": "hello"})
	if got := waitID(t, orec, "notify.permission.result", "ask2"); got["granted"] != false {
		t.Fatalf("denied permission: %v", got)
	}
	if got := waitID(t, orec, "notify.send.result", "send2"); got["error"] != "permission denied" {
		t.Fatalf("denied send: %v", got)
	}
	if n := len(sinks.all()); n != 2 {
		t.Fatalf("a denied notify reached sinks: %v", sinks.all())
	}
}

// ─── resource, media ─────────────────────────────────────────────

// TestNapResourceAndMediaGoThroughTheirSinks: an https fetch needs the
// session's PermFetch and goes through the fetch sink; data: needs no sink;
// blossom: goes through fetchBlossom, the documented unprompted exception;
// shell-owned media plays through its sink after PermMedia, and napplet-owned
// sessions reach no sink at all.
func TestNapResourceAndMediaGoThroughTheirSinks(t *testing.T) {
	t.Run("resource", func(t *testing.T) {
		setupNapTest(t)
		sinks := recordSinks(t)
		png := []byte("\x89PNG\r\n\x1a\nnot really an image")
		sum := sha256.Sum256(png)
		hash := hex.EncodeToString(sum[:])
		prev := resourceClient
		resourceClient = &http.Client{Transport: napRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(png))), Request: r}, nil
		})}
		t.Cleanup(func() { resourceClient = prev })
		ci, rec := openNapplet(t, "resource-sinks")
		ready(t, ci, rec, 1)

		ci.nap.mu.Lock()
		ci.nap.grants[PermFetch] = true
		ci.nap.mu.Unlock()
		post(t, ci, map[string]any{"type": "resource.bytes", "id": "web", "url": "https://8.8.8.8/a.png"})
		if got := waitID(t, rec, "resource.bytes.result", "web"); got["mime"] != "image/png" {
			t.Fatalf("https fetch: %v", got)
		}
		if calls := sinks.all(); !slices.Equal(calls, []napSinkCall{{"fetch", true}}) {
			t.Fatalf("https sinks: %v", calls)
		}

		post(t, ci, map[string]any{"type": "resource.bytes", "id": "data", "url": "data:text/plain,hello"})
		if got := waitID(t, rec, "resource.bytes.result", "data"); got["mime"] == nil {
			t.Fatalf("data: %v", got)
		}
		if n := len(sinks.all()); n != 1 {
			t.Fatalf("a data: URL reached a sink: %v", sinks.all())
		}

		post(t, ci, map[string]any{"type": "resource.bytes", "id": "blob", "url": "blossom:sha256:" + hash, "servers": []string{"https://8.8.4.4"}})
		if got := waitID(t, rec, "resource.bytes.result", "blob"); got["mime"] != "image/png" {
			t.Fatalf("blossom: %v", got)
		}
		if names := sinks.names(); !slices.Equal(names, []string{"fetch", "fetchBlossom"}) {
			t.Fatalf("blossom sinks: %v", names)
		}

		// the session said no: blocked-by-policy, and no fetch
		ci.nap.mu.Lock()
		ci.nap.grants[PermFetch] = false
		ci.nap.mu.Unlock()
		post(t, ci, map[string]any{"type": "resource.bytes", "id": "refused", "url": "https://8.8.8.8/b.png"})
		if got := waitID(t, rec, "resource.bytes.error", "refused"); got["error"] != "blocked-by-policy" {
			t.Fatalf("refused fetch: %v", got)
		}
		if n := len(sinks.all()); n != 2 {
			t.Fatalf("a refused fetch reached a sink: %v", sinks.all())
		}
	})

	t.Run("media", func(t *testing.T) {
		ci, rec, mh := setupMediaTest(t, "media-sinks")
		sinks := recordSinks(t)

		post(t, ci, shellCreate("shell", "https://1.1.1.1/a.mp3"))
		if got := waitID(t, rec, "media.session.create.result", "shell"); got["sessionId"] == nil {
			t.Fatalf("shell session: %v", got)
		}
		if calls := sinks.all(); !slices.Equal(calls, []napSinkCall{{"playMedia", true}}) {
			t.Fatalf("media sinks: %v", calls)
		}
		mh.player(t, 0)

		post(t, ci, map[string]any{"type": "media.session.create", "id": "own", "owner": "napplet"})
		if got := waitID(t, rec, "media.session.create.result", "own"); got["owner"] != "napplet" {
			t.Fatalf("napplet session: %v", got)
		}
		post(t, ci, map[string]any{"type": "media.session.create", "id": "garbled", "owner": 7})
		if got := waitID(t, rec, "media.session.create.result", "garbled"); got["error"] != napErrInvalid {
			t.Fatalf("undecodable create: %v", got)
		}
		if n := len(sinks.all()); n != 1 {
			t.Fatalf("a napplet-owned session reached a sink: %v", sinks.all())
		}
	})
}
