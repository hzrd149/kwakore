package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nipb7/blossom"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"verdana/backend/napconfig"
	"verdana/backend/webview"
)

// ─── test rig ────────────────────────────────────────────────────

// recTransport stands in for a napplet window: it keeps every NAP push the
// backend evals into the host page.
type recTransport struct {
	mu      sync.Mutex
	pushes  []map[string]any
	gens    []int // the session each push named, one per eval
	notify  chan struct{}
	focused int
}

func newRecTransport() *recTransport { return &recTransport{notify: make(chan struct{}, 1024)} }

const pushPrefix = "window.__nap_push && window.__nap_push("

func (r *recTransport) Send(m WireMsg) {
	if m.T != "eval" || !strings.HasPrefix(m.Code, pushPrefix) {
		return
	}
	// __nap_push(<session gen>, "<json>")
	genStr, arg, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(m.Code, pushPrefix), ")"), ", ")
	gen, err := strconv.Atoi(genStr)
	if !ok || err != nil {
		panic("push without a session gen: " + m.Code)
	}
	var js string
	if err := json.Unmarshal([]byte(arg), &js); err != nil {
		panic(err)
	}
	var one map[string]any
	var many []map[string]any
	r.mu.Lock()
	r.gens = append(r.gens, gen)
	if json.Unmarshal([]byte(js), &many) == nil {
		r.pushes = append(r.pushes, many...)
	} else if json.Unmarshal([]byte(js), &one) == nil {
		r.pushes = append(r.pushes, one)
	}
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (r *recTransport) Close() {}
func (r *recTransport) Focus() {
	r.mu.Lock()
	r.focused++
	r.mu.Unlock()
}

// find returns the pushes of a type.
func (r *recTransport) find(typ string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []map[string]any{}
	for _, p := range r.pushes {
		if p["type"] == typ {
			out = append(out, p)
		}
	}
	return out
}

func (r *recTransport) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, p := range r.pushes {
		out = append(out, p["type"].(string))
	}
	return out
}

// wait blocks until a push of the type shows up (the n-th one).
func (r *recTransport) wait(t *testing.T, typ string, n int) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if got := r.find(typ); len(got) >= n {
			return got[n-1]
		}
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("no %s (#%d) pushed; got %v", typ, n, r.types())
		}
	}
}

func setupNapTest(t *testing.T) {
	t.Helper()
	// registered first, so it runs last: installs, uninstalls and settings
	// changes leave shortcut and intent syncs running, which read host and
	// dataDir, and the next test must not swap those under them
	t.Cleanup(backgroundSyncs.Wait)
	backgroundSyncs.Wait()
	dataDir = t.TempDir()
	host = noopHost{}
	napconfig.Init(filepath.Join(dataDir, "config"), zerolog.Nop())
	t.Cleanup(func() {
		storagesMu.Lock()
		storages = make(map[string]*nappStorage)
		storagesMu.Unlock()
		napconfig.Init(filepath.Join(dataDir, "config"), zerolog.Nop())
	})
}

// openNapplet registers a napplet window with a recording transport.
func openNapplet(t *testing.T, d string) (*Instance, *recTransport) {
	t.Helper()
	n := Napp{ID: "napplet~0123456789abcdef~" + d, D: d, Name: d, Format: FormatNapplet, Kind: KindNapplet}
	ci := &Instance{
		instance:   d + "-" + randomID()[:6],
		napp:       n,
		subs:       map[int]context.CancelFunc{},
		actions:    map[string]int{},
		changed:    make(chan struct{}),
		dispatches: map[int]chan WireMsg{},
		gone:       make(chan struct{}),
		nap:        newNapSession(),
	}
	registerInstance(ci)
	rec := newRecTransport()
	ci.attach(rec)
	t.Cleanup(func() { WindowClosed(ci.instance) })
	return ci, rec
}

// post is the napplet posting an envelope (through the host page's nap.msg).
func post(t *testing.T, ci *Instance, env map[string]any) {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	param, _ := json.Marshal(string(raw))
	if _, err := napRPC(ci, "nap.msg", string(param)); err != nil {
		t.Fatal(err)
	}
}

// ready is the host page starting a session (nap.start), as it does before it
// creates the napplet's frame. rec and n are unused: there is no handshake to
// wait for any more, and the signature stays for the many callers.
func ready(t *testing.T, ci *Instance, rec *recTransport, n int) {
	t.Helper()
	if _, err := napRPC(ci, "nap.start", ""); err != nil {
		t.Fatal(err)
	}
}

// loaded is the host page reporting the frame's load event (nap.loaded).
func loaded(t *testing.T, ci *Instance) {
	t.Helper()
	if _, err := napRPC(ci, "nap.loaded", ""); err != nil {
		t.Fatal(err)
	}
}

// ─── srcdoc ──────────────────────────────────────────────────────

func TestBuildSrcdoc(t *testing.T) {
	napplet := `<!doctype html><html lang="en"><head><title>x</title></head><body><header>h</header><script>napplet()</script></body></html>`
	doc, err := buildSrcdoc([]byte(napplet), []string{"relay", "storage"})
	if err != nil {
		t.Fatal(err)
	}
	// the launcher's preamble comes first, whatever the napplet wrote
	if !strings.HasPrefix(doc, `<!doctype html><html><head><meta http-equiv="Content-Security-Policy"`) {
		t.Fatalf("preamble not first: %.120s", doc)
	}
	// CSP, then one function-scoped script: the pristine prelude and its
	// activation, so nothing but window.napplet is left in the frame (SHIM-04)
	csp := strings.Index(doc, `http-equiv="Content-Security-Policy"`)
	scope := strings.Index(doc, "<script>(function(){")
	shim := strings.Index(doc, webview.ShimPrelude())
	activate := strings.Index(doc, `NappletShimPrelude.install({"domains":["relay","storage"]})`)
	closeScope := strings.Index(doc, "})()</script>")
	end := strings.Index(doc, "</head>")
	if !(csp >= 0 && csp < scope && scope < shim && shim < activate && activate < closeScope && closeScope < end) {
		t.Fatalf("preamble out of order: csp=%d scope=%d shim=%d activate=%d close=%d end=%d", csp, scope, shim, activate, closeScope, end)
	}
	// no handshake (presence detection) and no global install call
	rest := strings.Replace(doc, webview.ShimPrelude(), "", 1)
	for _, gone := range []string{"shell.ready", "globalThis.NappletShimPrelude"} {
		if strings.Contains(rest, gone) {
			t.Errorf("srcdoc still carries %q outside the prelude", gone)
		}
	}
	// the napplet's bytes follow, untouched
	if !strings.HasSuffix(doc, napplet) {
		t.Error("napplet bytes changed")
	}
	if !strings.Contains(doc, "connect-src 'none'") || strings.Contains(doc, "'unsafe-eval'") {
		t.Error("wrong CSP")
	}

	// nothing the napplet writes can get ahead of the preamble or into it:
	// a commented or scripted "<head>" is just more napplet bytes after it
	for _, tricky := range []string{
		`<!-- <head> --><html><head><script>fetch("https://x")</script>`,
		`<script>"<head>"; fetch("https://x")</script><head></head>`,
		`<!DOCTYPE html>` + "\n<p>hi</p>",
		`<p>hi</p>`,
		"\uFEFF<!doctype html><head></head>",
	} {
		doc, err := buildSrcdoc([]byte(tricky), nil)
		if err != nil {
			t.Fatal(err)
		}
		pre := doc[:strings.Index(doc, "</head>")+len("</head>")]
		if !strings.HasPrefix(doc, "<!doctype html><html><head><meta http-equiv") || strings.Contains(pre, "fetch") ||
			!strings.HasSuffix(doc, strings.TrimPrefix(tricky, "\uFEFF")) {
			t.Errorf("%q: %.160s", tricky, doc)
		}
	}
	if _, err := buildSrcdoc([]byte{0xff, 0xfe, 'x'}, nil); err == nil {
		t.Error("invalid UTF-8 accepted")
	}
}

// ─── session ─────────────────────────────────────────────────────

func TestNapSessionStartsFromHostPage(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "alpha")

	// nothing is answered before the host page starts the session
	post(t, ci, map[string]any{"type": "storage.keys", "id": "early"})
	time.Sleep(50 * time.Millisecond)
	if got := rec.types(); len(got) != 0 {
		t.Fatalf("pushed before nap.start: %v", got)
	}
	ready(t, ci, rec, 1)
	post(t, ci, map[string]any{"type": "storage.keys", "id": "k1"})
	res := rec.wait(t, "storage.keys.result", 1)
	if res["id"] != "k1" {
		t.Fatalf("answered the pre-start envelope: %v", res)
	}

	// unknown types: silence, and the session keeps working
	post(t, ci, map[string]any{"type": "nope.whatever", "id": "x"})
	post(t, ci, map[string]any{"type": "storage.keys", "id": "k2"})
	rec.wait(t, "storage.keys.result", 2)
	for _, typ := range rec.types() {
		if strings.HasPrefix(typ, "nope") {
			t.Fatalf("unknown type was answered: %v", rec.types())
		}
	}
	// presence detection: the napplet learns its domains from window.napplet,
	// never from a handshake
	if got := rec.find("shell.init"); len(got) != 0 {
		t.Fatalf("shell.init pushed: %v", got)
	}
	if got := rec.find("storage.keys.result"); len(got) != 2 || got[0]["id"] != "k1" || got[1]["id"] != "k2" {
		t.Fatalf("storage.keys answers: %v", got)
	}
}

func TestNapTheme(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "themed")
	ready(t, ci, rec, 1)

	themeMu.Lock()
	oldName, oldVars := themeName, themeVars
	themeName = "dark"
	themeVars = `{"surface":"#010203","text":"#f1f2f3","accent":"#456789"}`
	themeMu.Unlock()
	t.Cleanup(func() {
		themeMu.Lock()
		themeName, themeVars = oldName, oldVars
		themeMu.Unlock()
	})

	post(t, ci, map[string]any{"type": "theme.get", "id": "theme-1"})
	result := rec.wait(t, "theme.get.result", 1)
	if result["id"] != "theme-1" {
		t.Fatalf("correlation id = %v", result["id"])
	}
	colors := result["theme"].(map[string]any)["colors"].(map[string]any)
	if colors["background"] != "#010203" || colors["text"] != "#f1f2f3" || colors["primary"] != "#456789" {
		t.Fatalf("theme colors = %v", colors)
	}

	broadcastNappletTheme()
	changed := rec.wait(t, "theme.changed", 1)
	if _, exists := changed["id"]; exists {
		t.Fatalf("theme.changed unexpectedly has id: %v", changed)
	}
	if changed["theme"] == nil {
		t.Fatalf("theme.changed has no theme: %v", changed)
	}
}

// A handler that panics still answers: the shim has no deadline of its own,
// so an unanswered request would leave the napplet waiting for good.
func TestNapPanickingHandlerStillReplies(t *testing.T) {
	setupNapTest(t)
	withTestRoute(t, "test.boom", napRoute{
		h:    func(*napCall) { panic("boom") },
		gate: openGate("test"), fail: failShape(failOkFalse),
	})
	withTestRoute(t, "test.asyncBoom", napRoute{
		h:    func(c *napCall) { c.async(func(context.Context) { panic("boom") }) },
		gate: openGate("test"), fail: failShape(failOkFalse),
	})
	ci, rec := openNapplet(t, "boom")
	ready(t, ci, rec, 1)

	// both answer in the route's own failure shape, with the generic code
	post(t, ci, map[string]any{"type": "test.boom", "id": "b1"})
	if got := rec.wait(t, "test.boom.result", 1); got["id"] != "b1" || got["ok"] != false || got["error"] != napErrInternal {
		t.Fatalf("sync panic: %v", got)
	}
	post(t, ci, map[string]any{"type": "test.asyncBoom", "id": "b2"})
	if got := rec.wait(t, "test.asyncBoom.result", 1); got["id"] != "b2" || got["ok"] != false || got["error"] != napErrInternal {
		t.Fatalf("async panic: %v", got)
	}
}

// ─── subscriptions ───────────────────────────────────────────────

// subEntry is the session's subscription entry under key, or nil.
func subEntry(ci *Instance, key string) *napSub {
	ci.nap.mu.Lock()
	defer ci.nap.mu.Unlock()
	return ci.nap.subs[key]
}

// resubscribeCase is one subscription domain (relay or outbox) for
// testResubscribeKeepsLiveEntry.
type resubscribeCase struct {
	subscribe, close, closed string // envelope types
	key                      func(subID string) string
	refused                  string // the closed reason past napMaxSubs
}

// testResubscribeKeepsLiveEntry runs subscribe x, close x, subscribe x in one
// session, holding the first pump's exit back until the second subscription
// is live: the order a napplet posting raw envelopes can force. The first
// pump's cleanup must leave the second entry alone, so close still reaches it
// and it still counts toward napMaxSubs.
func testResubscribeKeepsLiveEntry(t *testing.T, tc resubscribeCase) {
	setupNapTest(t)
	withSystem(t)
	ci, rec := openNapplet(t, "resub")
	hold := make(chan struct{})
	release := sync.OnceFunc(func() { close(hold) })
	t.Cleanup(release)
	started := make(chan string, napMaxSubs+2)
	var pumps atomic.Int32
	ci.nap.pumpHook = func(ctx context.Context, subID string) {
		n := pumps.Add(1)
		started <- subID
		<-ctx.Done()
		if n == 1 {
			<-hold
		}
	}
	ready(t, ci, rec, 1)

	subscribe := func(id string) {
		post(t, ci, map[string]any{"type": tc.subscribe, "id": "s-" + id, "subId": id,
			"filters": []any{map[string]any{"kinds": []int{1}}}})
	}
	awaitStart := func(want string) {
		t.Helper()
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("pump started for %q, want %q", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no pump started for %q", want)
		}
	}
	awaitDone := func(sub *napSub, what string) {
		t.Helper()
		select {
		case <-sub.done:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s never stopped", what)
		}
	}
	key := tc.key("x")

	subscribe("x")
	awaitStart("x")
	first := subEntry(ci, key)
	post(t, ci, map[string]any{"type": tc.close, "id": "c1", "subId": "x"})
	subscribe("x")
	awaitStart("x")
	second := subEntry(ci, key)
	if first == nil || second == nil || second == first {
		t.Fatalf("entries: first %p, second %p", first, second)
	}

	// the closed pump ends only now, after the re-subscription took the id
	release()
	awaitDone(first, "the closed subscription")
	if got := subEntry(ci, key); got != second {
		t.Fatalf("the closed pump's cleanup dropped the live re-subscription: entry %p, want %p", got, second)
	}

	// the live one still counts: napMaxSubs-1 more fit, the next is refused
	for i := 1; i < napMaxSubs; i++ {
		id := "more-" + strconv.Itoa(i)
		subscribe(id)
		awaitStart(id)
	}
	n := len(rec.find(tc.closed)) + 1
	subscribe("over")
	if got := rec.wait(t, tc.closed, n); got["subId"] != "over" || got["reason"] != tc.refused {
		t.Fatalf("subscription past the cap: %v", got)
	}
	if len(started) != 0 {
		t.Fatal("a pump started past the cap")
	}

	// close still reaches it, and frees its slot
	post(t, ci, map[string]any{"type": tc.close, "id": "c2", "subId": "x"})
	awaitDone(second, "the re-subscription after close")
	if got := subEntry(ci, key); got != nil {
		t.Fatalf("closed subscription still tracked: %p", got)
	}
	subscribe("over")
	awaitStart("over")
}

// A closed relay subscription whose pump ends late must not untrack a
// re-subscription with the same subId (CR-03).
func TestNapRelayResubscribeKeepsLiveEntry(t *testing.T) {
	testResubscribeKeepsLiveEntry(t, resubscribeCase{
		subscribe: "relay.subscribe", close: "relay.close", closed: "relay.closed",
		key:     func(id string) string { return id },
		refused: "error: too many subscriptions",
	})
}

// ─── uploads ────────────────────────────────────────────────────

func setupNapUploadTest(t *testing.T, ci *Instance) {
	t.Helper()
	previousKeyer, previousPubkey := userKeyer, userPubkey
	previousServers, previousUpload, previousAuth := napUploadServers, napUploadToServer, napUploadAuth
	userSK := nostr.Generate()
	userKeyer, userPubkey = keyer.NewPlainKeySigner(userSK), userSK.Public()
	setSessionRule(RuleKey{Napp: ci.napp.ID, Permission: PermUpload}, Rule{Decision: DecisionAllow})
	t.Cleanup(func() {
		userKeyer, userPubkey = previousKeyer, previousPubkey
		napUploadServers, napUploadToServer, napUploadAuth = previousServers, previousUpload, previousAuth
		clearSessionRule(RuleKey{Napp: ci.napp.ID, Permission: PermUpload})
	})
}

func uploadEnvelope(id string, data []byte) map[string]any {
	return map[string]any{
		"type": "upload.upload", "id": id,
		"request": map[string]any{
			"rail": "blossom", "filename": "hello.txt", "caption": "a greeting",
			"data": map[string]any{"__blob": map[string]any{
				"b64": base64.StdEncoding.EncodeToString(data), "mime": "text/plain",
			}},
		},
	}
}

func TestNapUploadBlossomReplicatesAndReportsStatus(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "uploader")
	setupNapUploadTest(t, ci)
	ready(t, ci, rec, 1)

	data := []byte("hello blossom")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	napUploadServers = func(context.Context, nostr.PubKey) []string {
		return []string{"https://one.example/", "bad", "https://two.example"}
	}
	var attempted []string
	napUploadToServer = func(_ context.Context, server string, got []byte, mimeType string, _ string) (*blossom.BlobDescriptor, error) {
		attempted = append(attempted, server)
		if !bytes.Equal(got, data) || mimeType != "text/plain" {
			t.Fatalf("upload bytes/mime: %q %q", got, mimeType)
		}
		return &blossom.BlobDescriptor{
			URL: server + "/" + hash + ".txt", SHA256: hash, Size: len(data), Type: mimeType,
		}, nil
	}

	post(t, ci, uploadEnvelope("u1", data))
	started := rec.wait(t, "upload.upload.result", 1)
	result := started["result"].(map[string]any)
	if result["status"] != "pending" || result["rail"] != "blossom" {
		t.Fatalf("initial result: %v", result)
	}
	if got := rec.wait(t, "upload.status.changed", 1)["status"].(map[string]any); got["status"] != "uploading" {
		t.Fatalf("approved status: %v", got)
	}
	complete := rec.wait(t, "upload.status.changed", 2)["status"].(map[string]any)
	if complete["status"] != "complete" || complete["sha256"] != hash || complete["url"] != "https://one.example/"+hash+".txt" {
		t.Fatalf("complete status: %v", complete)
	}
	if !slices.Equal(attempted, []string{"https://one.example", "https://two.example"}) {
		t.Fatalf("server order: %v", attempted)
	}
	fallbacks := complete["fallbackUrls"].([]any)
	if len(fallbacks) != 1 || fallbacks[0] != "https://two.example/"+hash+".txt" {
		t.Fatalf("fallbacks: %v", fallbacks)
	}
	tags := complete["nip94"].([]any)
	for _, want := range []any{"url", "m", "x", "size", "fallback", "alt"} {
		if !slices.ContainsFunc(tags, func(tag any) bool { return tag.([]any)[0] == want }) {
			t.Errorf("NIP-94 lacks %s: %v", want, tags)
		}
	}

	post(t, ci, map[string]any{"type": "upload.status", "id": "s1", "uploadId": result["uploadId"]})
	if got := rec.wait(t, "upload.status.result", 1)["status"].(map[string]any); got["status"] != "complete" {
		t.Fatalf("status lookup: %v", got)
	}
}

func TestNapUploadRejectsBadRequestsAndUnverifiedResults(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "upload-errors")
	setupNapUploadTest(t, ci)
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "upload.upload", "id": "rail", "request": map[string]any{"rail": "nip96"}})
	if got := rec.wait(t, "upload.upload.result", 1); got["error"] != "unsupported rail" {
		t.Fatalf("unsupported rail: %v", got)
	}
	post(t, ci, map[string]any{"type": "upload.upload", "id": "data", "request": map[string]any{
		"data": map[string]any{"__blob": map[string]any{"b64": "%%%"}},
	}})
	if got := rec.wait(t, "upload.upload.result", 2); got["error"] != "invalid upload data" {
		t.Fatalf("invalid data: %v", got)
	}

	data := []byte("exact bytes")
	napUploadServers = func(context.Context, nostr.PubKey) []string { return []string{"https://bad.example"} }
	napUploadToServer = func(_ context.Context, server string, got []byte, mimeType string, _ string) (*blossom.BlobDescriptor, error) {
		return &blossom.BlobDescriptor{URL: server + "/wrong", SHA256: strings.Repeat("0", 64), Size: len(got)}, nil
	}
	post(t, ci, uploadEnvelope("bad-descriptor", data))
	rec.wait(t, "upload.upload.result", 3)
	failed := rec.wait(t, "upload.status.changed", 2)["status"].(map[string]any)
	if failed["status"] != "failed" || failed["error"] != "upload failed" {
		t.Fatalf("unverified descriptor accepted: %v", failed)
	}
}

// The shim times upload.upload out after 30s, so the result must not wait on
// the approval prompt: an upload the user approved late was already reported
// as failed to the napplet.
func TestNapUploadAnswersBeforeApproval(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "upload-ask")
	setupNapUploadTest(t, ci)
	clearSessionRule(RuleKey{Napp: ci.napp.ID, Permission: PermUpload})
	ready(t, ci, rec, 1)

	napUploadServers = func(context.Context, nostr.PubKey) []string { return []string{"https://one.example"} }
	napUploadToServer = func(context.Context, string, []byte, string, string) (*blossom.BlobDescriptor, error) {
		t.Error("uploaded without approval")
		return nil, errors.New("not approved")
	}
	post(t, ci, uploadEnvelope("ask", []byte("hold on")))
	result := rec.wait(t, "upload.upload.result", 1)["result"].(map[string]any)
	if result["status"] != "pending" || result["uploadId"] == "" {
		t.Fatalf("result before approval: %v", result)
	}
	// the result goes out before the prompt is queued
	var p *Prompt
	for deadline := time.Now().Add(2 * time.Second); p == nil && time.Now().Before(deadline); {
		if p = CurrentPrompt(); p == nil {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if p == nil {
		t.Fatal("no approval prompt")
	}
	AnswerPrompt(p.ID, Answer{OK: false})
	cancelled := rec.wait(t, "upload.status.changed", 1)["status"].(map[string]any)
	if cancelled["status"] != "cancelled" || cancelled["uploadId"] != result["uploadId"] {
		t.Fatalf("denied status: %v", cancelled)
	}
}

// One signature goes to every server, and the signer gets no deadline: a
// remote signer may wait on the user for minutes.
func TestNapUploadSignsOnceWithoutDeadline(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "upload-sign")
	setupNapUploadTest(t, ci)
	ready(t, ci, rec, 1)

	data := []byte("signed once")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPut || r.URL.Path != "/upload" || !bytes.Equal(body, data) {
			t.Errorf("request: %s %s %q", r.Method, r.URL.Path, body)
		}
		auths = append(auths, r.Header.Get("Authorization"))
		json.NewEncoder(w).Encode(blossom.BlobDescriptor{
			URL: "http://" + r.Host + "/" + hash, SHA256: hash, Size: len(data), Type: "text/plain",
		})
	}))
	defer srv.Close()
	other := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	napUploadServers = func(context.Context, nostr.PubKey) []string { return []string{srv.URL, other} }
	signs := 0
	signer := userKeyer
	userKeyer = deadlineCheckingSigner{t: t, Keyer: signer, signs: &signs}

	post(t, ci, uploadEnvelope("signed", data))
	rec.wait(t, "upload.upload.result", 1)
	complete := rec.wait(t, "upload.status.changed", 2)["status"].(map[string]any)
	if complete["status"] != "complete" || len(complete["fallbackUrls"].([]any)) != 1 {
		t.Fatalf("status: %v", complete)
	}
	if signs != 1 || len(auths) != 2 || auths[0] != auths[1] || !strings.HasPrefix(auths[0], "Nostr ") {
		t.Fatalf("signed %d times, sent %v", signs, auths)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auths[0], "Nostr "))
	if err != nil {
		t.Fatal(err)
	}
	var evt nostr.Event
	if err := json.Unmarshal(raw, &evt); err != nil || evt.Kind != 24242 || !evt.VerifySignature() ||
		evt.Tags.Find("x")[1] != hash || evt.Tags.Find("t")[1] != "upload" {
		t.Fatalf("authorization event: %v %v", evt, err)
	}
}

func TestNapUploadReportsSigningFailure(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "upload-unsigned")
	setupNapUploadTest(t, ci)
	ready(t, ci, rec, 1)

	napUploadServers = func(context.Context, nostr.PubKey) []string { return []string{"https://one.example"} }
	napUploadAuth = func(context.Context, nostr.Keyer, string) (string, error) {
		return "", errors.New("bunker offline")
	}
	napUploadToServer = func(context.Context, string, []byte, string, string) (*blossom.BlobDescriptor, error) {
		t.Error("uploaded with no authorization")
		return nil, errors.New("unsigned")
	}
	post(t, ci, uploadEnvelope("unsigned", []byte("x")))
	rec.wait(t, "upload.upload.result", 1)
	failed := rec.wait(t, "upload.status.changed", 2)["status"].(map[string]any)
	if failed["status"] != "failed" || failed["error"] != "signing failed" {
		t.Fatalf("status: %v", failed)
	}
}

type deadlineCheckingSigner struct {
	nostr.Keyer
	t     *testing.T
	signs *int
}

func (s deadlineCheckingSigner) SignEvent(ctx context.Context, evt *nostr.Event) error {
	if _, ok := ctx.Deadline(); ok {
		s.t.Error("signing has a deadline")
	}
	*s.signs++
	return s.Keyer.SignEvent(ctx, evt)
}

func TestNapUploadIsCancelledOnReload(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "upload-reload")
	setupNapUploadTest(t, ci)
	ready(t, ci, rec, 1)

	napUploadServers = func(context.Context, nostr.PubKey) []string { return []string{"https://slow.example"} }
	started, cancelled := make(chan struct{}), make(chan struct{})
	napUploadToServer = func(ctx context.Context, _ string, _ []byte, _ string, _ string) (*blossom.BlobDescriptor, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}
	post(t, ci, uploadEnvelope("slow", []byte("wait")))
	rec.wait(t, "upload.upload.result", 1)
	<-started
	if _, err := napRPC(ci, "nap.reset", ""); err != nil {
		t.Fatal(err)
	}
	ready(t, ci, rec, 2)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("reload did not cancel the upload")
	}
}

func TestNapStorage(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "store")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "storage.set", "id": "1", "key": "a", "value": "shared"})
	post(t, ci, map[string]any{"type": "storage.set", "id": "2", "key": "a", "value": "mine", "scope": "instance"})
	post(t, ci, map[string]any{"type": "storage.get", "id": "3", "key": "a"})
	post(t, ci, map[string]any{"type": "storage.get", "id": "4", "key": "a", "scope": "instance"})
	post(t, ci, map[string]any{"type": "storage.get", "id": "5", "key": "missing"})
	post(t, ci, map[string]any{"type": "storage.keys", "id": "6"})
	rec.wait(t, "storage.keys.result", 1)

	gets := rec.find("storage.get.result")
	if len(gets) != 3 {
		t.Fatalf("gets: %v", gets)
	}
	if gets[0]["id"] != "3" || gets[0]["value"] != "shared" {
		t.Errorf("shared get: %v", gets[0])
	}
	if gets[1]["value"] != "mine" {
		t.Errorf("instance get: %v", gets[1])
	}
	if v, present := gets[2]["value"]; !present || v != nil {
		t.Errorf("missing key must be an explicit null: %v", gets[2])
	}
	if keys := rec.find("storage.keys.result")[0]["keys"]; len(keys.([]any)) != 1 {
		t.Errorf("keys: %v", keys)
	}

	// quota
	post(t, ci, map[string]any{"type": "storage.set", "id": "big", "key": "b", "value": strings.Repeat("x", nappletStorageQuota)})
	res := rec.wait(t, "storage.set.result", 3)
	if res["id"] != "big" || res["error"] == nil {
		t.Errorf("quota not enforced: %v", res)
	}

	// shared storage outlives the window: another window of the same napplet sees it
	if v, ok := storageGet(ci.napp.ID, "a"); !ok || v != "shared" {
		t.Errorf("not persisted under the napplet id: %q %v", v, ok)
	}
}

func TestNapReloadIsANewSession(t *testing.T) {
	setupNapTest(t)
	a, recA := openNapplet(t, "reloader")
	b, recB := openNapplet(t, "listener")
	ready(t, a, recA, 1)
	ready(t, b, recB, 1)

	post(t, b, map[string]any{"type": "inc.subscribe", "id": "s", "topic": "t"})
	recB.wait(t, "inc.subscribe.result", 1)

	// b reloads: nap.reset drops its old session before the new document is ready.
	if _, err := napRPC(b, "nap.reset", ""); err != nil {
		t.Fatal(err)
	}
	ready(t, b, recB, 2)
	post(t, a, map[string]any{"type": "inc.emit", "topic": "t", "payload": 1})
	post(t, a, map[string]any{"type": "storage.keys", "id": "sync"})
	recA.wait(t, "storage.keys.result", 1)
	time.Sleep(50 * time.Millisecond)
	if got := recB.find("inc.event"); len(got) != 0 {
		t.Fatalf("subscription survived a reload: %v", got)
	}
}

func TestNapFrameShellReadyIsIgnored(t *testing.T) {
	setupNapTest(t)

	t.Run("before nap.start", func(t *testing.T) {
		ci, rec := openNapplet(t, "forged-start")
		ci.nap.mu.Lock()
		gen0 := ci.nap.gen
		ci.nap.mu.Unlock()

		// the frame cannot start a session: shell.ready is an unknown type
		post(t, ci, map[string]any{"type": "shell.ready"})
		post(t, ci, map[string]any{"type": "storage.keys", "id": "pre"})
		time.Sleep(50 * time.Millisecond)
		ci.nap.mu.Lock()
		established := ci.nap.established
		ci.nap.mu.Unlock()
		if established {
			t.Fatal("a frame-sent shell.ready started the session")
		}
		if got := rec.types(); len(got) != 0 {
			t.Fatalf("pushed before nap.start: %v", got)
		}

		ready(t, ci, rec, 1)
		post(t, ci, map[string]any{"type": "storage.keys", "id": "post"})
		rec.wait(t, "storage.keys.result", 1)
		time.Sleep(50 * time.Millisecond)
		if got := rec.find("storage.keys.result"); len(got) != 1 || got[0]["id"] != "post" {
			t.Fatalf("storage.keys answers: %v", got)
		}
		ci.nap.mu.Lock()
		gen := ci.nap.gen
		ci.nap.mu.Unlock()
		if gen != gen0+1 {
			t.Fatalf("gen = %d, want %d (advanced by nap.start only)", gen, gen0+1)
		}
	})

	t.Run("after nap.start", func(t *testing.T) {
		ci, rec := openNapplet(t, "forged-restart")
		ready(t, ci, rec, 1)

		post(t, ci, map[string]any{"type": "inc.subscribe", "id": "topic", "topic": "keep"})
		rec.wait(t, "inc.subscribe.result", 1)

		ci.nap.mu.Lock()
		gen := ci.nap.gen
		ctx := ci.nap.ctx
		ci.nap.grants[PermFetch] = true
		ci.nap.mu.Unlock()

		post(t, ci, map[string]any{"type": "shell.ready"})
		// confirms the shell.ready has been dispatched without depending on an
		// output that an ignored message must not produce
		post(t, ci, map[string]any{"type": "storage.keys", "id": "sync"})
		rec.wait(t, "storage.keys.result", 1)

		for _, typ := range []string{"shell.init", "shell.ready.result"} {
			if got := rec.find(typ); len(got) != 0 {
				t.Fatalf("%s pushed: %v", typ, got)
			}
		}
		ci.nap.mu.Lock()
		defer ci.nap.mu.Unlock()
		if ci.nap.gen != gen || ci.nap.ctx != ctx || !ci.nap.established {
			t.Fatalf("frame shell.ready changed the session: gen=%d (want %d), established=%v", ci.nap.gen, gen, ci.nap.established)
		}
		if !ci.nap.topics["keep"] || !ci.nap.grants[PermFetch] {
			t.Fatalf("frame shell.ready cleared session state: topics=%v grants=%v", ci.nap.topics, ci.nap.grants)
		}
	})
}

func TestNapStartDropsEnvelopesFromThePreviousDocument(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "restarted")
	ready(t, ci, rec, 1)

	ci.nap.mu.Lock()
	oldGen, oldCtx := ci.nap.gen, ci.nap.ctx
	ci.nap.mu.Unlock()
	ready(t, ci, rec, 2)

	// an envelope the outgoing document queued before the restart
	ci.napDispatch(&napCall{
		ci: ci, gen: oldGen, ctx: oldCtx, Type: "storage.keys",
		ID: json.RawMessage(`"old"`), raw: json.RawMessage(`{"type":"storage.keys","id":"old"}`),
	})
	post(t, ci, map[string]any{"type": "storage.keys", "id": "new"})
	rec.wait(t, "storage.keys.result", 1)
	time.Sleep(50 * time.Millisecond)
	if got := rec.find("storage.keys.result"); len(got) != 1 || got[0]["id"] != "new" {
		t.Fatalf("storage.keys answers: %v", got)
	}
	if oldCtx.Err() == nil {
		t.Error("the previous session's context survived nap.start")
	}
}

func TestNapStartTearsDownThePreviousSession(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "torn-down")
	ready(t, ci, rec, 1)
	subscribeTopic(t, ci, rec, "t", 1)
	ci.nap.mu.Lock()
	ci.nap.grants[PermFetch] = true
	ci.nap.mu.Unlock()

	ready(t, ci, rec, 2)

	if _, ok := ci.handlerFor("t"); ok {
		t.Error("the previous session's inc topic still routes to this window")
	}
	ci.nap.mu.Lock()
	defer ci.nap.mu.Unlock()
	if len(ci.nap.topics) != 0 || len(ci.nap.grants) != 0 {
		t.Fatalf("session state survived nap.start: topics=%v grants=%v", ci.nap.topics, ci.nap.grants)
	}
	if !ci.nap.established {
		t.Fatal("nap.start left the session unestablished")
	}
}

func TestNapLoadedPushesControlsOnEveryLoad(t *testing.T) {
	setupNapTest(t)
	host = &notifyTestHost{}
	ci, rec := openNapplet(t, "controls")

	// a load before any session pushes nothing
	loaded(t, ci)
	if got := rec.find("notify.controls"); len(got) != 0 {
		t.Fatalf("controls pushed before nap.start: %v", got)
	}

	ready(t, ci, rec, 1)
	loaded(t, ci)
	if got := rec.find("notify.controls"); len(got) != 1 {
		t.Fatalf("controls after the first load: %v", got)
	}
	if c, _ := rec.find("notify.controls")[0]["controls"].([]any); len(c) != 1 || c[0] != "system" {
		t.Fatalf("controls = %v", rec.find("notify.controls")[0])
	}

	// the napplet reloading its own frame keeps the session (NIP-5D-reload,
	// Phase 4), but the new document registered onControls again and gets
	// the push too (WR-05)
	loaded(t, ci)
	if got := rec.find("notify.controls"); len(got) != 2 {
		t.Fatalf("controls after a self-reload in the same session: %v", got)
	}

	ready(t, ci, rec, 2)
	loaded(t, ci)
	if got := rec.find("notify.controls"); len(got) != 3 {
		t.Fatalf("controls after the second session: %v", got)
	}
}

// WR-01: nap.start answers with the session it opened, and every push names
// the session it was made for, so the host page can drop one that passed
// napPushGen's check but landed after the next nap.start.
func TestNapPushesNameTheirSession(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "gen-tagged")
	for i := 1; i <= 2; i++ {
		res, err := napRPC(ci, "nap.start", "")
		if err != nil {
			t.Fatal(err)
		}
		ci.nap.mu.Lock()
		want := ci.nap.gen
		ci.nap.mu.Unlock()
		if got, _ := res.(map[string]any)["gen"].(int); got != want {
			t.Fatalf("nap.start #%d answered %v, want gen %d", i, res, want)
		}
		ci.napPush(map[string]any{"type": "probe"})
		rec.wait(t, "probe", i)
		rec.mu.Lock()
		last := rec.gens[len(rec.gens)-1]
		rec.mu.Unlock()
		if last != want {
			t.Errorf("push after nap.start #%d named session %d, want %d", i, last, want)
		}
	}
}

// ─── inc + intents ───────────────────────────────────────────────

func TestNapInc(t *testing.T) {
	setupNapTest(t)
	a, recA := openNapplet(t, "sender")
	b, recB := openNapplet(t, "receiver")
	ready(t, a, recA, 1)
	ready(t, b, recB, 1)

	post(t, a, map[string]any{"type": "inc.subscribe", "id": "sa", "topic": "chat"})
	post(t, b, map[string]any{"type": "inc.subscribe", "id": "sb", "topic": "chat"})
	recA.wait(t, "inc.subscribe.result", 1)
	recB.wait(t, "inc.subscribe.result", 1)

	post(t, a, map[string]any{"type": "inc.emit", "topic": "chat", "payload": map[string]any{"msg": "hi"}, "sender": "forged"})
	ev := recB.wait(t, "inc.event", 1)
	if ev["topic"] != "chat" || ev["sender"] != "sender" || ev["payload"].(map[string]any)["msg"] != "hi" {
		t.Errorf("event: %v", ev)
	}
	time.Sleep(50 * time.Millisecond)
	if got := recA.find("inc.event"); len(got) != 0 {
		t.Errorf("echoed to the sender: %v", got)
	}

	// channels: the target hears about it before the opener gets its id
	post(t, a, map[string]any{"type": "inc.channel.open", "id": "c1", "target": "receiver"})
	res := recA.wait(t, "inc.channel.open.result", 1)
	opened := recB.wait(t, "inc.channel.opened", 1)
	id, _ := res["channelId"].(string)
	if id == "" || opened["channelId"] != id || opened["peer"] != "sender" || res["peer"] != "receiver" {
		t.Fatalf("open: %v / %v", res, opened)
	}
	post(t, b, map[string]any{"type": "inc.channel.emit", "channelId": id, "payload": "pong"})
	if got := recA.wait(t, "inc.channel.event", 1); got["payload"] != "pong" || got["sender"] != "receiver" {
		t.Errorf("channel event: %v", got)
	}
	post(t, a, map[string]any{"type": "inc.channel.list", "id": "l"})
	if list := recA.wait(t, "inc.channel.list.result", 1)["channels"].([]any); len(list) != 1 {
		t.Errorf("list: %v", list)
	}

	// a closing window closes its channels, and the peer is told
	WindowClosed(b.instance)
	if got := recA.wait(t, "inc.channel.closed", 1); got["channelId"] != id {
		t.Errorf("closed: %v", got)
	}

	// no such target
	post(t, a, map[string]any{"type": "inc.channel.open", "id": "c2", "target": "nobody"})
	if got := recA.wait(t, "inc.channel.open.result", 2); got["error"] == nil || got["channelId"] != nil {
		t.Errorf("open to nobody: %v", got)
	}
}

func TestNapChannelCloseNotifiesBothEndpointsOnce(t *testing.T) {
	setupNapTest(t)
	a, recA := openNapplet(t, "closer")
	b, recB := openNapplet(t, "peer")
	ready(t, a, recA, 1)
	ready(t, b, recB, 1)

	post(t, a, map[string]any{"type": "inc.channel.open", "id": "open", "target": "peer"})
	opened := recA.wait(t, "inc.channel.open.result", 1)
	id, _ := opened["channelId"].(string)
	if id == "" {
		t.Fatalf("channel was not opened: %v", opened)
	}
	recB.wait(t, "inc.channel.opened", 1)

	post(t, a, map[string]any{"type": "inc.channel.close", "channelId": id})
	for name, rec := range map[string]*recTransport{"closer": recA, "peer": recB} {
		closed := rec.wait(t, "inc.channel.closed", 1)
		if closed["channelId"] != id || closed["reason"] != "closed by peer" {
			t.Errorf("%s terminal notification: %v", name, closed)
		}
	}

	// Once removed, a repeated close must not deliver another terminal event.
	post(t, a, map[string]any{"type": "inc.channel.close", "channelId": id})
	post(t, a, map[string]any{"type": "storage.keys", "id": "after-close"})
	recA.wait(t, "storage.keys.result", 1)
	if got := recA.find("inc.channel.closed"); len(got) != 1 {
		t.Errorf("closer received %d terminal notifications: %v", len(got), got)
	}
	if got := recB.find("inc.channel.closed"); len(got) != 1 {
		t.Errorf("peer received %d terminal notifications: %v", len(got), got)
	}
}

// A napplet can post any envelope it likes, ids and extra fields included:
// what keeps it in its lane is that the answer goes back to the window that
// asked and nowhere else. A windowId naming another window changes nothing.
func TestNapReplyIsBoundToTheSendingWindow(t *testing.T) {
	setupNapTest(t)
	a, recA := openNapplet(t, "asker")
	b, recB := openNapplet(t, "bystander")
	ready(t, a, recA, 1)
	ready(t, b, recB, 1)

	post(t, a, map[string]any{"type": "identity.getPublicKey", "id": "replay-id", "windowId": b.instance})
	if got := recA.wait(t, "identity.getPublicKey.result", 1); got["id"] != "replay-id" {
		t.Errorf("reply: %v", got)
	}

	post(t, b, map[string]any{"type": "storage.keys", "id": "sync"})
	recB.wait(t, "storage.keys.result", 1)
	if got := recB.find("identity.getPublicKey.result"); len(got) != 0 {
		t.Errorf("another window's reply reached the bystander: %v", got)
	}
	if got := recA.find("identity.getPublicKey.result"); len(got) != 1 {
		t.Errorf("asker got %d replies: %v", len(got), got)
	}
}

// A channel id is not a capability: a window that is not one of its two ends
// can neither speak on it nor close it, however it learned the id.
func TestNapIncChannelOpsRequireMembership(t *testing.T) {
	setupNapTest(t)
	a, recA := openNapplet(t, "end-a")
	b, recB := openNapplet(t, "end-b")
	c, recC := openNapplet(t, "outsider")
	ready(t, a, recA, 1)
	ready(t, b, recB, 1)
	ready(t, c, recC, 1)

	post(t, a, map[string]any{"type": "inc.channel.open", "id": "open", "target": "end-b"})
	id, _ := recA.wait(t, "inc.channel.open.result", 1)["channelId"].(string)
	if id == "" {
		t.Fatal("channel was not opened")
	}
	recB.wait(t, "inc.channel.opened", 1)

	post(t, c, map[string]any{"type": "inc.channel.emit", "channelId": id, "payload": "intruder"})
	post(t, c, map[string]any{"type": "inc.channel.close", "channelId": id})
	post(t, c, map[string]any{"type": "storage.keys", "id": "sync"})
	recC.wait(t, "storage.keys.result", 1)

	// the channel still works, and the first thing b hears on it is a
	post(t, a, map[string]any{"type": "inc.channel.emit", "channelId": id, "payload": "ping"})
	if got := recB.wait(t, "inc.channel.event", 1); got["payload"] != "ping" || got["sender"] != "end-a" {
		t.Errorf("first event on the channel: %v", got)
	}
	for name, rec := range map[string]*recTransport{"end-a": recA, "end-b": recB} {
		if got := rec.find("inc.channel.closed"); len(got) != 0 {
			t.Errorf("%s saw the outsider close the channel: %v", name, got)
		}
	}
	if got := recA.find("inc.channel.event"); len(got) != 0 {
		t.Errorf("outsider spoke on the channel: %v", got)
	}
}

// subscribeTopic is the napplet's shim calling napplet.inc.on(topic): it
// posts inc.subscribe and the launcher acknowledges it (the n-th ack).
func subscribeTopic(t *testing.T, ci *Instance, rec *recTransport, topic string, n int) {
	t.Helper()
	post(t, ci, map[string]any{"type": "inc.subscribe", "id": "sub-" + topic, "topic": topic})
	rec.wait(t, "inc.subscribe.result", n)
}

func TestIntentDeliveryToNapplet(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "handler")
	ready(t, ci, rec, 1)
	before := len(rec.types())

	done := make(chan error, 1)
	go func() {
		_, err := dispatchToNapplet(context.Background(), ci,
			&actionRequest{name: "napplet:profile/open", sender: "caller"}, json.RawMessage(`{"pubkey":"abc"}`))
		done <- err
	}()

	// a started napplet is not yet a handler: delivery waits for its own
	// inc.subscribe on the convention topic (NAP-INTENT: only once ready)
	time.Sleep(50 * time.Millisecond)
	if got := rec.types(); len(got) != before {
		t.Fatalf("pushed before the napplet subscribed: %v", got[before:])
	}
	subscribeTopic(t, ci, rec, "napplet:profile/open", 1)
	ev := rec.wait(t, "inc.event", 1)
	if ev["topic"] != "napplet:profile/open" || ev["sender"] != "caller" ||
		ev["payload"].(map[string]any)["pubkey"] != "abc" {
		t.Errorf("intent event: %v", ev)
	}
	if err := <-done; err != nil {
		t.Errorf("dispatch: %v", err)
	}
	if got := rec.find("inc.event"); len(got) != 1 {
		t.Errorf("inc.event pushes = %d, want 1: %v", len(got), got)
	}
	// the pristine shim drops intent.deliver; it must never be sent
	if got := rec.find("intent.deliver"); len(got) != 0 {
		t.Errorf("intent.deliver pushed: %v", got)
	}
}

// WR-02: readiness is judged against the session the event is pushed to. The
// stale registration below stands in for the interleaving where the wait saw
// the old document's subscription and nap.start replaced the session before
// the push: the intent must wait for the new document to subscribe, and reach
// it in its own session, instead of landing in a document not listening yet.
func TestIntentDeliveryWaitsForTheReceivingSession(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "handler")
	ready(t, ci, rec, 1)
	subscribeTopic(t, ci, rec, "napplet:profile/open", 1)

	// a new session whose window still lists the old subscription
	ci.nap.mu.Lock()
	ci.nap.resetLocked()
	ci.nap.established = true
	gen := ci.nap.gen
	ci.nap.mu.Unlock()
	if _, ok := ci.handlerFor("napplet:profile/open"); !ok {
		t.Fatal("rig: the stale registration is gone")
	}

	done := make(chan error, 1)
	go func() {
		_, err := dispatchToNapplet(context.Background(), ci,
			&actionRequest{name: "napplet:profile/open", sender: "caller"}, json.RawMessage(`{"pubkey":"abc"}`))
		done <- err
	}()
	// an ordinary subscription is the sync point: it wakes the waiting
	// dispatch, which must still find the convention topic unsubscribed
	subscribeTopic(t, ci, rec, "chat", 2)
	select {
	case err := <-done:
		t.Fatalf("dispatch finished before the new document subscribed: %v (pushes %v)", err, rec.types())
	case <-time.After(20 * time.Millisecond):
	}
	if got := rec.find("inc.event"); len(got) != 0 {
		t.Fatalf("pushed into a session that is not listening: %v", got)
	}

	subscribeTopic(t, ci, rec, "napplet:profile/open", 3)
	ev := rec.wait(t, "inc.event", 1)
	if ev["topic"] != "napplet:profile/open" || ev["sender"] != "caller" {
		t.Errorf("intent event: %v", ev)
	}
	if err := <-done; err != nil {
		t.Errorf("dispatch: %v", err)
	}
	rec.mu.Lock()
	last := rec.gens[len(rec.gens)-1]
	rec.mu.Unlock()
	if last != gen {
		t.Errorf("intent pushed for session %d, want the receiving session %d", last, gen)
	}
}

// nap.start runs on the host page's rpc goroutine, not on the session worker,
// so it can land while the worker is inside a handler of the outgoing
// document. That handler must not write into the new session: a stale
// inc.subscribe on a convention topic would make the new document look ready
// for an intent it never subscribed to.
func TestNapStartWaitsOutAnInFlightHandler(t *testing.T) {
	setupNapTest(t)
	saved := intentHandlerWait
	intentHandlerWait = 100 * time.Millisecond
	t.Cleanup(func() { intentHandlerWait = saved })

	ci, rec := openNapplet(t, "handler")
	entered := make(chan struct{})
	release := make(chan struct{})
	var parked, released sync.Once
	unpark := func() { released.Do(func() { close(release) }) }
	// on a failure too: the window's own cleanup waits out the handler
	t.Cleanup(unpark)
	// set before the first envelope starts the worker
	ci.nap.beforeHandler = func(c *napCall) {
		if c.Type != "inc.subscribe" {
			return
		}
		parked.Do(func() {
			close(entered)
			<-release
		})
	}
	ready(t, ci, rec, 1)

	// the outgoing document subscribes on its way out; its handler is past
	// the gen check when the host page starts the next session
	post(t, ci, map[string]any{"type": "inc.subscribe", "id": "old", "topic": "napplet:profile/open"})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("rig: the handler never ran")
	}
	started := make(chan int, 1)
	go func() {
		gen, err := ci.napStart()
		if err != nil {
			t.Error(err)
		}
		started <- gen
	}()
	// nap.start parks on the dispatch lock: once a writer is waiting, a new
	// reader cannot get in
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-started:
			t.Fatal("nap.start opened a session while a handler of the old one was running")
		case <-deadline:
			t.Fatal("nap.start never reached the dispatch lock")
		default:
		}
		if !ci.nap.dispatchMu.TryRLock() {
			break
		}
		ci.nap.dispatchMu.RUnlock()
		time.Sleep(time.Millisecond)
	}
	unpark()
	newGen := <-started

	// a sync point in the new session: the worker is past the old handler
	subscribeTopic(t, ci, rec, "chat", 2)

	ci.nap.mu.Lock()
	gen := ci.nap.gen
	topics := make([]string, 0, len(ci.nap.topics))
	for topic := range ci.nap.topics {
		topics = append(topics, topic)
	}
	ci.nap.mu.Unlock()
	if gen != newGen {
		t.Fatalf("rig: session %d, want %d", gen, newGen)
	}
	if !slices.Equal(topics, []string{"chat"}) {
		t.Errorf("new session's topics = %v, want only its own [chat]", topics)
	}
	if _, ok := ci.handlerFor("napplet:profile/open"); ok {
		t.Error("the old document's subscription is still registered as an action")
	}

	// and the stale subscription does not count as the new document being
	// ready: delivery waits for a subscription that never comes
	_, err := dispatchToNapplet(context.Background(), ci,
		&actionRequest{name: "napplet:profile/open", sender: "caller"}, json.RawMessage(`{"pubkey":"abc"}`))
	if !errors.Is(err, errNoHandler) {
		t.Fatalf("dispatch on a stale subscription: err = %v, want errNoHandler", err)
	}
	if got := rec.find("inc.event"); len(got) != 0 {
		t.Errorf("intent pushed into a session that is not listening: %v", got)
	}
}

func TestIntentDeliveryTimesOutWithoutSubscriber(t *testing.T) {
	setupNapTest(t)
	saved := intentHandlerWait
	intentHandlerWait = 100 * time.Millisecond
	t.Cleanup(func() { intentHandlerWait = saved })

	ci, rec := openNapplet(t, "deaf")
	ready(t, ci, rec, 1)
	_, err := dispatchToNapplet(context.Background(), ci,
		&actionRequest{name: "napplet:profile/open", sender: "caller"}, json.RawMessage(`{"pubkey":"abc"}`))
	if !errors.Is(err, errNoHandler) {
		t.Fatalf("dispatch to a napplet that never listens: err = %v, want errNoHandler", err)
	}
	if got := rec.find("inc.event"); len(got) != 0 {
		t.Errorf("delivered without a subscriber: %v", got)
	}
}

func TestIntentDeliveryReachesOnlyTheHandler(t *testing.T) {
	setupNapTest(t)
	target, recTarget := openNapplet(t, "resolved-handler")
	other, recOther := openNapplet(t, "other-listener")
	for _, w := range []struct {
		ci  *Instance
		rec *recTransport
	}{{target, recTarget}, {other, recOther}} {
		ready(t, w.ci, w.rec, 1)
		subscribeTopic(t, w.ci, w.rec, "napplet:profile/open", 1)
	}

	_, err := dispatchToNapplet(context.Background(), target,
		&actionRequest{name: "napplet:profile/open", sender: "caller"}, json.RawMessage(`{"pubkey":"abc"}`))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	ev := recTarget.wait(t, "inc.event", 1)
	if ev["topic"] != "napplet:profile/open" || ev["sender"] != "caller" {
		t.Errorf("intent event: %v", ev)
	}
	// the payload belongs to the resolved handler alone, never to every
	// listener of the convention topic
	time.Sleep(50 * time.Millisecond)
	if got := recTarget.find("inc.event"); len(got) != 1 {
		t.Errorf("handler inc.event pushes = %d, want 1: %v", len(got), got)
	}
	if got := recOther.find("inc.event"); len(got) != 0 {
		t.Errorf("another listener received the intent: %v", got)
	}
}

// A napplet's inc.emit on an intent convention topic is an ordinary NAP-INC
// broadcast: every listener on the exact topic gets it, stamped with the
// emitter's own sender. Only the launcher's sender is reserved: no emitter is
// ever delivered as "launcher", even one whose d tag is that word (conflict
// A23, CR-02).
func TestIncEmitBroadcastsOnIntentConventionTopic(t *testing.T) {
	setupNapTest(t)
	const topic = "napplet:profile/open"
	peer, recPeer := openNapplet(t, "peer")
	impostor, recImpostor := openNapplet(t, launcherSender)
	handler, rec := openNapplet(t, "profile-handler")
	other, recOther := openNapplet(t, "other-listener")
	for _, w := range []struct {
		ci  *Instance
		rec *recTransport
	}{{peer, recPeer}, {impostor, recImpostor}, {handler, rec}, {other, recOther}} {
		ready(t, w.ci, w.rec, 1)
	}
	subscribeTopic(t, handler, rec, topic, 1)
	subscribeTopic(t, other, recOther, topic, 1)
	// the emitter listens too: NAP-INC routes to the other subscribers
	subscribeTopic(t, peer, recPeer, topic, 1)

	post(t, peer, map[string]any{"type": "inc.emit", "topic": topic, "payload": map[string]any{"pubkey": "abc"}})
	post(t, impostor, map[string]any{"type": "inc.emit", "topic": topic, "payload": map[string]any{"pubkey": "def"}})

	for _, l := range []struct {
		name string
		rec  *recTransport
	}{{"handler", rec}, {"other listener", recOther}} {
		got := map[string]string{}
		for i := 1; i <= 2; i++ {
			ev := l.rec.wait(t, "inc.event", i)
			if ev["topic"] != topic {
				t.Errorf("%s: event on %v, want %s", l.name, ev["topic"], topic)
			}
			sender, _ := ev["sender"].(string)
			if sender == launcherSender {
				t.Errorf("%s: a napplet broadcast was delivered as the launcher: %v", l.name, ev)
			}
			payload, _ := ev["payload"].(map[string]any)
			pk, _ := payload["pubkey"].(string)
			got[pk] = sender
		}
		if got["abc"] != incSender(peer) || got["def"] != incSender(impostor) {
			t.Errorf("%s: senders by payload = %v, want abc=%q def=%q", l.name, got, incSender(peer), incSender(impostor))
		}
	}
	if incSender(impostor) != impostor.napp.Address() {
		t.Errorf("incSender(d=%q) = %q, want its address", launcherSender, incSender(impostor))
	}

	// the impostor's broadcast reaches the emitter that listens; its own does not
	ev := recPeer.wait(t, "inc.event", 1)
	if ev["sender"] != incSender(impostor) {
		t.Errorf("peer received %v, want only the impostor's broadcast", ev)
	}
	// sync point: the peer's queue is drained once this is answered
	subscribeTopic(t, peer, recPeer, "chat", 2)
	if got := recPeer.find("inc.event"); len(got) != 1 {
		t.Errorf("the emitter heard its own broadcast: %v", got)
	}

	// listening on the convention topic is still the handler's readiness
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, ok := handler.waitForHandler(ctx, topic); !ok {
		t.Error("the handler's subscription no longer registers the intent")
	}
}

// installProfileHandler makes handler the installed, default handler of the
// profile archetype for the test, and returns the default's rule key.
func installProfileHandler(t *testing.T, handler *Instance) RuleKey {
	t.Helper()
	handler.napp.Conventions = []NappletConvention{{ID: "napplet:profile/open"}}
	handler.napp.Actions = []string{"napplet:profile/open"}
	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	state.InstalledNapps[handler.napp.ID] = handler.napp
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		delete(state.InstalledNapps, handler.napp.ID)
		stateMu.Unlock()
	})
	key := intentDefaultKey("profile")
	setSessionRule(key, Rule{Decision: DecisionAllow, Target: handler.napp.ID})
	t.Cleanup(func() { clearSessionRule(key) })
	return key
}

// launcherSender names only the launcher: a napplet whose author picked the d
// tag "launcher" is named by its address, both on its own inc emits and on
// the intents it invokes (CR-02).
func TestLauncherSenderCannotBeForged(t *testing.T) {
	setupNapTest(t)
	impostor, recImpostor := openNapplet(t, launcherSender)
	if got := incSender(impostor); got == launcherSender || got != impostor.napp.Address() {
		t.Fatalf("incSender(d=%q) = %q, want its address %q", launcherSender, got, impostor.napp.Address())
	}
	// the d tag itself is never rewritten (CRIT-01, W-1)
	if impostor.napp.D != launcherSender {
		t.Fatalf("d tag changed to %q", impostor.napp.D)
	}

	handler, rec := openNapplet(t, "profile-handler")
	key := installProfileHandler(t, handler)

	ready(t, impostor, recImpostor, 1)
	ready(t, handler, rec, 1)
	subscribeTopic(t, handler, rec, "chat", 1)
	post(t, impostor, map[string]any{"type": "inc.emit", "topic": "chat", "payload": "hi"})
	if ev := rec.wait(t, "inc.event", 1); ev["sender"] != impostor.napp.Address() {
		t.Errorf("inc emit sender = %v, want the impostor's address", ev["sender"])
	}

	done := make(chan error, 1)
	go func() {
		_, err := runNappAction(context.Background(), impostor, "napplet:profile/open",
			json.RawMessage(`{"pubkey":"abc"}`), actionOptions{DefaultKey: key})
		done <- err
	}()
	subscribeTopic(t, handler, rec, "napplet:profile/open", 2)
	delivery := rec.wait(t, "inc.event", 2)
	if delivery["topic"] != "napplet:profile/open" || delivery["sender"] != impostor.napp.Address() {
		t.Errorf("intent from d=%q delivered as %v", launcherSender, delivery)
	}
	if err := <-done; err != nil {
		t.Errorf("dispatch: %v", err)
	}
}

// WR-03: an intent from a root napplet (kind 15129, no d tag) names its
// caller by address, as its own inc emits do, never with an empty sender.
func TestIntentFromRootNappletNamesItsAddress(t *testing.T) {
	setupNapTest(t)
	root, recRoot := openNapplet(t, "")
	root.napp.Kind = KindRootNapplet
	if root.napp.D != "" || !strings.HasPrefix(root.napp.Address(), "15129:") {
		t.Fatalf("rig: not a root napplet: %q", root.napp.Address())
	}
	handler, rec := openNapplet(t, "profile-handler")
	key := installProfileHandler(t, handler)
	ready(t, root, recRoot, 1)
	ready(t, handler, rec, 1)

	done := make(chan error, 1)
	go func() {
		_, err := runNappAction(context.Background(), root, "napplet:profile/open",
			json.RawMessage(`{"pubkey":"abc"}`), actionOptions{DefaultKey: key})
		done <- err
	}()
	subscribeTopic(t, handler, rec, "napplet:profile/open", 1)
	delivery := rec.wait(t, "inc.event", 1)
	if delivery["sender"] != root.napp.Address() || delivery["sender"] != incSender(root) {
		t.Errorf("intent from a root napplet delivered with sender %q, want its address %q",
			delivery["sender"], root.napp.Address())
	}
	if err := <-done; err != nil {
		t.Errorf("dispatch: %v", err)
	}
}

func TestConventionPartsAcceptsOnlyStableIdentity(t *testing.T) {
	archetype, action, ok := conventionParts("napplet:profile/open")
	if !ok || archetype != "profile" || action != "open" {
		t.Fatalf("parts = %q %q %v", archetype, action, ok)
	}
	for _, bad := range []string{"napplet:p/open#f", "napplet:p/open?a=1", "https://x/y", "napplet:P/open", "napplet:p/a/b"} {
		if _, _, ok := conventionParts(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

type intentDiscoveryHost struct {
	noopHost
	opened chan string
}

func (h *intentDiscoveryHost) OpenDiscovery(archetype string) { h.opened <- archetype }

func TestNapIntentWithoutHandlerOpensDiscovery(t *testing.T) {
	setupNapTest(t)
	caller, rec := openNapplet(t, "intent-caller")
	ready(t, caller, rec, 1)

	oldHost := host
	discovery := &intentDiscoveryHost{opened: make(chan string, 1)}
	host = discovery
	t.Cleanup(func() { host = oldHost })

	post(t, caller, map[string]any{"type": "intent.invoke", "id": "missing", "request": map[string]any{
		"archetype": "emoji-list", "payload": map[string]any{"seed": []string{"wave"}},
	}})
	result := rec.wait(t, "intent.invoke.result", 1)["result"].(map[string]any)
	if result["ok"] != false || result["handled"] != false || result["archetype"] != "emoji-list" ||
		result["action"] != "open" || result["error"] != "no handler" {
		t.Fatalf("missing handler result: %v", result)
	}
	select {
	case archetype := <-discovery.opened:
		if archetype != "emoji-list" {
			t.Fatalf("opened discovery for %q", archetype)
		}
	case <-time.After(time.Second):
		t.Fatal("missing handler did not open discovery")
	}
}

func TestNapIntentChangedReportsLastHandlerRemoval(t *testing.T) {
	setupNapTest(t)
	caller, rec := openNapplet(t, "intent-watcher")
	ready(t, caller, rec, 1)
	handler := Napp{
		ID: "napplet~0123456789abcdef~profile-handler", D: "profile-handler",
		Name: "Profile Handler", Format: FormatNapplet,
		Conventions: []NappletConvention{{ID: "napplet:profile/open"}},
	}

	intentChangedMu.Lock()
	intentLastArchetypes = make(map[string]struct{})
	intentChangedMu.Unlock()
	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	state.InstalledNapps[handler.ID] = handler
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		delete(state.InstalledNapps, handler.ID)
		stateMu.Unlock()
	})

	broadcastIntentChanges()
	first := rec.wait(t, "intent.changed", 1)["availability"].(map[string]any)
	if first["archetype"] != "profile" || first["available"] != true {
		t.Fatalf("handler addition: %v", first)
	}

	stateMu.Lock()
	delete(state.InstalledNapps, handler.ID)
	stateMu.Unlock()
	broadcastIntentChanges()
	removed := rec.wait(t, "intent.changed", 2)["availability"].(map[string]any)
	if removed["archetype"] != "profile" || removed["available"] != false ||
		len(removed["candidates"].([]any)) != 0 {
		t.Fatalf("last handler removal: %v", removed)
	}
}

func TestNapIntentAcceptanceSurvivesSourceLifecycle(t *testing.T) {
	setupNapTest(t)
	caller, recCaller := openNapplet(t, "intent-caller")
	handler, recHandler := openNapplet(t, "profile-handler")
	handler.napp.Conventions = []NappletConvention{{ID: "napplet:profile/open"}}
	handler.napp.Actions = []string{"napplet:profile/open"}

	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	state.InstalledNapps[handler.napp.ID] = handler.napp
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		delete(state.InstalledNapps, handler.napp.ID)
		stateMu.Unlock()
	})
	key := intentDefaultKey("profile")
	setSessionRule(key, Rule{Decision: DecisionAllow, Target: handler.napp.ID})
	t.Cleanup(func() { clearSessionRule(key) })

	ready(t, caller, recCaller, 1)
	post(t, caller, map[string]any{"type": "intent.available", "id": "available", "archetype": "profile"})
	availability := recCaller.wait(t, "intent.available.result", 1)["availability"].(map[string]any)
	candidates := availability["candidates"].([]any)
	if availability["hasDefault"] != true || candidates[0].(map[string]any)["isDefault"] != true {
		t.Fatalf("default not reflected by availability: %v", availability)
	}
	contract := candidates[0].(map[string]any)["contracts"].([]any)[0].(map[string]any)
	if contract["convention"] != "napplet:profile/open" {
		t.Fatalf("availability contracts: %v", availability)
	}

	post(t, caller, map[string]any{"type": "intent.invoke", "id": "mismatch", "request": map[string]any{
		"archetype": "profile", "action": "open", "convention": "napplet:note/open",
	}})
	rejected := recCaller.wait(t, "intent.invoke.result", 1)["result"].(map[string]any)
	if rejected["ok"] != false || rejected["error"] != "invalid convention" {
		t.Fatalf("mismatched convention accepted: %v", rejected)
	}

	post(t, caller, map[string]any{"type": "intent.invoke", "id": "invoke", "request": map[string]any{
		"archetype": "profile",
		"payload":   map[string]any{"pubkey": "abc"},
		"behavior":  map[string]any{"focus": true},
	}})
	result := recCaller.wait(t, "intent.invoke.result", 2)["result"].(map[string]any)
	if result["ok"] != true || result["handler"] != handler.napp.D || result["handled"] != true ||
		result["windowId"] != handler.instance || result["action"] != "open" ||
		result["convention"] != "napplet:profile/open" {
		t.Fatalf("acceptance result: %v", result)
	}
	recHandler.mu.Lock()
	focused := recHandler.focused
	recHandler.mu.Unlock()
	if focused != 1 {
		t.Fatalf("focus requests = %d, want 1", focused)
	}

	// End the source session before the target becomes ready. Accepted delivery
	// belongs to the runtime and must still reach the target afterward.
	caller.napReset()
	ready(t, handler, recHandler, 1)
	subscribeTopic(t, handler, recHandler, "napplet:profile/open", 1)
	delivery := recHandler.wait(t, "inc.event", 1)
	if delivery["sender"] != caller.napp.D || delivery["topic"] != "napplet:profile/open" ||
		delivery["payload"].(map[string]any)["pubkey"] != "abc" {
		t.Errorf("delivery: %v", delivery)
	}
}

func TestOpenUserProfileDeliversToHandler(t *testing.T) {
	setupNapTest(t)
	sk := nostr.Generate()
	asUser(t, sk)
	handler, rec := openNapplet(t, "profile-handler")
	handler.napp.Conventions = []NappletConvention{{ID: "napplet:profile/open"}}
	handler.napp.Actions = []string{"napplet:profile/open"}

	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	state.InstalledNapps[handler.napp.ID] = handler.napp
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		delete(state.InstalledNapps, handler.napp.ID)
		stateMu.Unlock()
	})
	key := intentDefaultKey("profile")
	setSessionRule(key, Rule{Decision: DecisionAllow, Target: handler.napp.ID})
	t.Cleanup(func() { clearSessionRule(key) })

	ready(t, handler, rec, 1)
	done := make(chan error, 1)
	go func() { done <- OpenUserProfile(context.Background()) }()
	subscribeTopic(t, handler, rec, "napplet:profile/open", 1)
	delivery := rec.wait(t, "inc.event", 1)
	if delivery["sender"] != "launcher" || delivery["topic"] != "napplet:profile/open" ||
		delivery["payload"].(map[string]any)["pubkey"] != sk.Public().Hex() {
		t.Errorf("delivery: %v", delivery)
	}
	if err := <-done; err != nil {
		t.Errorf("OpenUserProfile: %v", err)
	}
}

func TestOpenUserProfileWithoutHandlerOpensDiscovery(t *testing.T) {
	setupNapTest(t)
	asUser(t, nostr.Generate())
	discovery := &intentDiscoveryHost{opened: make(chan string, 1)}
	host = discovery

	if err := OpenUserProfile(context.Background()); err != nil {
		t.Fatalf("OpenUserProfile: %v", err)
	}
	select {
	case archetype := <-discovery.opened:
		if archetype != "profile" {
			t.Fatalf("opened discovery for %q", archetype)
		}
	default:
		t.Fatal("no handler did not open discovery")
	}
}

func TestOpenUserProfileLoggedOut(t *testing.T) {
	setupNapTest(t)
	if err := OpenUserProfile(context.Background()); err == nil {
		t.Fatal("opened a profile with no one logged in")
	}
}

// ─── link ────────────────────────────────────────────────────────

func TestNapLinkRejectsSchemes(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "linker")
	ready(t, ci, rec, 1)
	for i, u := range []string{"javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "blob:null/x"} {
		post(t, ci, map[string]any{"type": "link.open", "id": u, "url": u})
		got := rec.wait(t, "link.open.result", i+1)
		if got["id"] != u || got["status"] != "denied" || got["error"] != "unsupported-scheme" {
			t.Errorf("%s: %v", u, got)
		}
	}
}

type napLinkTestHost struct {
	noopHost
	opened []string
	err    error
}

func (h *napLinkTestHost) OpenLink(url string) error {
	h.opened = append(h.opened, url)
	return h.err
}

func TestNapLinkResultsAndLabelSanitizing(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "link-results")
	// more link.opens than the default link bucket holds: this test is about
	// result shapes, not rate limits (TestNapCategoryLimitRateLimits)
	withLimits(t, ci, limitsWith(napEnvelopeLimit, map[napLimitClass]napLimitSpec{
		limitLink: {rate.Every(time.Second), 100},
	}))
	ready(t, ci, rec, 1)
	key := RuleKey{Napp: ci.napp.ID, Permission: PermOpenLink}
	t.Cleanup(func() { clearSessionRule(key) })

	invalid := []struct {
		id, url, code string
	}{
		{"missing", "", "invalid-url"},
		{"relative", "/somewhere", "invalid-url"},
		{"hostless", "https:///somewhere", "invalid-url"},
	}
	for i, tc := range invalid {
		post(t, ci, map[string]any{"type": "link.open", "id": tc.id, "url": tc.url})
		got := rec.wait(t, "link.open.result", i+1)
		if got["id"] != tc.id || got["status"] != "denied" || got["error"] != tc.code {
			t.Errorf("%s: %v", tc.id, got)
		}
	}

	setSessionRule(key, Rule{Decision: DecisionDeny})
	post(t, ci, map[string]any{"type": "link.open", "id": "deny", "url": "https://example.com"})
	denied := rec.wait(t, "link.open.result", len(invalid)+1)
	if denied["status"] != "denied" || denied["error"] != "user-denied" {
		t.Fatalf("user denial: %v", denied)
	}

	linkHost := &napLinkTestHost{}
	host = linkHost
	setSessionRule(key, Rule{Decision: DecisionAllow})
	post(t, ci, map[string]any{
		"type": "link.open", "id": "open", "url": "https://example.com/read",
		"options": map[string]any{"label": "Read this\nmisleading page"},
	})
	opened := rec.wait(t, "link.open.result", len(invalid)+2)
	if opened["id"] != "open" || opened["status"] != "opened" || opened["error"] != nil {
		t.Fatalf("opened result: %v", opened)
	}
	if !slices.Equal(linkHost.opened, []string{"https://example.com/read"}) {
		t.Fatalf("opened URLs: %v", linkHost.opened)
	}

	linkHost.err = errors.New("platform details must not cross the wire")
	post(t, ci, map[string]any{"type": "link.open", "id": "failure", "url": "https://example.com/fail"})
	failed := rec.wait(t, "link.open.result", len(invalid)+3)
	if failed["status"] != "denied" || failed["error"] != "blocked-by-policy" {
		t.Fatalf("platform failure: %v", failed)
	}
}

func TestNapLinkLabel(t *testing.T) {
	if got := napLinkLabel("  Read\nthis\t\u202epage  "); got != "Read this page" {
		t.Fatalf("sanitized label = %q", got)
	}
	long := strings.Repeat("界", maxNapLinkLabelRunes+1)
	if got := napLinkLabel(long); len([]rune(got)) != maxNapLinkLabelRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated label has %d runes and suffix %q", len([]rune(got)), got[len(got)-3:])
	}
}

// ─── network guard + resources ───────────────────────────────────

func TestResourceFetchBlocksLoopback(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer srv.Close()
	_, err := httpsResource(context.Background(), srv.URL)
	var re *resourceErr
	if err == nil || !errorsAs(err, &re) || re.code != "blocked-by-policy" {
		t.Fatalf("loopback fetch: %v", err)
	}
	if _, err := napExplicitRelay(context.Background(), "ws://127.0.0.1:7777"); err == nil {
		t.Error("loopback relay allowed")
	}
	if _, err := napExplicitRelay(context.Background(), "https://relay.example.com"); err == nil {
		t.Error("non-websocket relay allowed")
	}
}

func TestNapResourceTrackKeepsRequestIDOwnership(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "resource-tracker")
	ready(t, ci, rec, 1)
	call := func() *napCall {
		return &napCall{ci: ci, gen: ci.nap.gen, ctx: ci.nap.ctx, ID: json.RawMessage(`"request"`)}
	}

	first := call()
	firstCtx, firstDone, ok := first.resourceTrack()
	if !ok {
		t.Fatal("first request was rejected")
	}
	duplicate := call()
	duplicate.Type = "resource.bytes"
	duplicate.raw = json.RawMessage(`{"url":"data:text/plain,duplicate"}`)
	napResourceBytes(duplicate)
	if got := rec.wait(t, "resource.bytes.error", 1); got["error"] != "duplicate-request" {
		t.Fatalf("duplicate request was not rejected: %v", got)
	}

	// Cancellation releases A's id immediately, so B may reuse it before A's
	// deferred completion runs. A must not delete B's registration.
	napResourceCancel(call())
	select {
	case <-firstCtx.Done():
	default:
		t.Fatal("live request was not cancelled")
	}

	secondCtx, secondDone, ok := call().resourceTrack()
	if !ok {
		t.Fatal("request id was not reusable after cancellation")
	}
	// A's first deferred completion is stale and must not remove B.
	firstDone()
	ci.nap.mu.Lock()
	_, tracked := ci.nap.fetches[`"request"`]
	ci.nap.mu.Unlock()
	if !tracked {
		t.Fatal("old completion removed the newer request registration")
	}
	napResourceCancel(call())
	select {
	case <-secondCtx.Done():
	default:
		t.Fatal("new request was not cancelled")
	}
	secondDone()
	ci.nap.mu.Lock()
	_, tracked = ci.nap.fetches[`"request"`]
	ci.nap.mu.Unlock()
	if tracked {
		t.Fatal("completed request remained registered")
	}
}

func TestNapResourceInfoReportsCurrentBulkLimits(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "resource-info")
	ready(t, ci, rec, 1)
	c := &napCall{ci: ci, gen: ci.nap.gen, ctx: ci.nap.ctx, Type: "resource.info", ID: json.RawMessage(`"info"`)}
	napResourceInfo(c)
	got := rec.wait(t, "resource.info.result", 1)
	info, ok := got["info"].(map[string]any)
	if !ok || info["maxUrls"] != float64(resourceMaxURLs) || info["maxServers"] != float64(resourceMaxServers) {
		t.Fatalf("resource info limits: %#v", got["info"])
	}
}

func TestNapResourceBytesManyUsesTooLargeForBulkCap(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "resource-many-cap")
	ready(t, ci, rec, 1)
	requests := make([]map[string]any, resourceMaxURLs+1)
	for i := range requests {
		requests[i] = map[string]any{"url": "data:text/plain,ok"}
	}
	raw, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		t.Fatal(err)
	}
	c := &napCall{ci: ci, gen: ci.nap.gen, ctx: ci.nap.ctx, Type: "resource.bytesMany", ID: json.RawMessage(`"many"`), raw: raw}
	napResourceBytesMany(c)
	if got := rec.wait(t, "resource.bytesMany.error", 1); got["error"] != "too-large" {
		t.Fatalf("bulk cap error: %#v", got)
	}
}

func TestBlossomResourceCapsServerHints(t *testing.T) {
	hints := make([]string, resourceMaxServers+1)
	_, err := fetchBlossomResource(context.Background(), nil, "sha256:"+strings.Repeat("0", 64), hints)
	var re *resourceErr
	if !errors.As(err, &re) || re.code != "too-large" {
		t.Fatalf("server hint cap: %v", err)
	}
}

func TestSniffResource(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if m, err := sniffResource(buf.Bytes(), "text/html"); err != nil || m != "image/png" {
		t.Errorf("png: %q %v", m, err)
	}
	for name, body := range map[string]string{
		"svg":  `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`,
		"svg2": `<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`,
		"html": `<!doctype html><script>steal()</script>`,
	} {
		if m, err := sniffResource([]byte(body), "image/png"); err == nil {
			t.Errorf("%s served as %q", name, m)
		}
	}
	if m, err := sniffResource([]byte(`{"a":1}`), "application/json; charset=utf-8"); err != nil || m != "application/json" {
		t.Errorf("json: %q %v", m, err)
	}
	if r, err := decodeDataURL("data:image/png;base64," + b64(buf.Bytes())); err != nil || r.mime != "image/png" {
		t.Errorf("data url: %v", err)
	}
}

func errorsAs(err error, target any) bool { return errors.As(err, target) }

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestNappletDocumentChecksTheHash(t *testing.T) {
	setupNapTest(t)
	html := []byte("<!doctype html><p>napplet</p>")
	sum := sha256.Sum256(html)
	n := Napp{ID: "napplet~0123456789abcdef~doc", D: "doc", Format: FormatNapplet,
		Paths: []NappPath{{Path: "/index.html", Sha256: hex.EncodeToString(sum[:])}}}
	base, err := nappBaseDir(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "index.html")
	if err := os.WriteFile(path, html, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := nappletDocument(n); err != nil || !bytes.Equal(got, html) {
		t.Fatalf("verified document refused: %v", err)
	}
	// changed on disk after install: never runs
	if err := os.WriteFile(path, append(html, '!'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := nappletDocument(n); err == nil {
		t.Fatal("tampered document accepted")
	}
}

func TestNapCommonNip19(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "nip19")
	ready(t, ci, rec, 1)
	pk := nostr.Generate().Public()

	post(t, ci, map[string]any{"type": "common.encodeNip19", "id": "e1", "input": map[string]any{"type": "npub", "hex": pk.Hex()}})
	enc := rec.wait(t, "common.encodeNip19.result", 1)
	if enc["ok"] != true || enc["value"] != nip19.EncodeNpub(pk) {
		t.Fatalf("encode: %v", enc)
	}
	post(t, ci, map[string]any{"type": "common.encodeNip19", "id": "e2", "input": map[string]any{
		"type": "naddr", "pubkey": pk.Hex(), "kind": 30023, "identifier": "post", "relays": []string{"wss://r.example.com"}}})
	naddr := rec.wait(t, "common.encodeNip19.result", 2)["value"].(string)

	post(t, ci, map[string]any{"type": "common.decodeNip19", "id": "d1", "value": "nostr:" + naddr})
	dec := rec.wait(t, "common.decodeNip19.result", 1)
	if dec["ok"] != true || dec["nip19Type"] != "naddr" || dec["pubkey"] != pk.Hex() ||
		dec["identifier"] != "post" || dec["kind"] != float64(30023) {
		t.Errorf("decode naddr: %v", dec)
	}

	post(t, ci, map[string]any{"type": "common.encodeNip19", "id": "e3", "input": map[string]any{"type": "nrelay", "relay": "wss://r.example.com"}})
	nrelay := rec.wait(t, "common.encodeNip19.result", 3)["value"].(string)
	post(t, ci, map[string]any{"type": "common.decodeNip19", "id": "d2", "value": nrelay})
	if got := rec.wait(t, "common.decodeNip19.result", 2); got["relay"] != "wss://r.example.com" {
		t.Errorf("nrelay round trip: %v", got)
	}

	// never secret keys
	post(t, ci, map[string]any{"type": "common.decodeNip19", "id": "d3", "value": nip19.EncodeNsec(nostr.Generate())})
	if got := rec.wait(t, "common.decodeNip19.result", 3); got["ok"] != false || got["pubkey"] != nil {
		t.Errorf("nsec decoded: %v", got)
	}
	post(t, ci, map[string]any{"type": "common.encodeNip19", "id": "e4", "input": map[string]any{"type": "nsec", "hex": pk.Hex()}})
	if got := rec.wait(t, "common.encodeNip19.result", 4); got["ok"] != false {
		t.Errorf("nsec encoded: %v", got)
	}
}
