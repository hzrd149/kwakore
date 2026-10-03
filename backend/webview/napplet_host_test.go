package webview

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// These tests run napplet-host.js, byte for byte as embedded, in node against
// a fake browser: the host page's ordering and replacement guarantees (D-06,
// D-07) are only as good as this script, and no Go test reaches it otherwise.

// needNode returns node's path, or skips the test when there is none. CI sets
// VERDANA_REQUIRE_NODE=1, which turns a missing node into a failure: these
// tests guard the napplet sandbox and must never skip silently there.
func needNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("VERDANA_REQUIRE_NODE") == "1" {
			t.Fatal("node is required (VERDANA_REQUIRE_NODE=1) but not on PATH")
		}
		t.Skip("node not on PATH")
	}
	return node
}

// hostHarness fakes just enough of a browser for the host page. Every rpc and
// every frame append and removal goes to one ordered log. hold(method) keeps
// that method's calls pending until release(method) settles the oldest one.
const hostHarness = `
const log = []
const rpcs = []
const errors = []
const handlers = {}
const holding = new Set()
const held = {}
const frames = []
const appended = []
let bodyText = ""
const listeners = {}

console.error = (...args) => { errors.push(args.map(String).join(" ")) }

globalThis.window = globalThis
window.top = window
window.addEventListener = (type, fn) => { (listeners[type] = listeners[type] || []).push(fn) }
window.__verdanaNappletRPC = (method, params) => {
  log.push(method)
  rpcs.push({ method, params })
  if (holding.has(method)) {
    return new Promise((resolve, reject) => { (held[method] = held[method] || []).push({ resolve, reject, params }) })
  }
  const h = handlers[method]
  return Promise.resolve(JSON.stringify(h ? h(params) : null))
}
const hold = method => { holding.add(method) }
const unhold = method => { holding.delete(method) }
// release settles the oldest held call with value, or with what the method's
// handler answers when no value is given
const release = (method, value) => {
  const w = (held[method] || []).shift()
  if (!w) throw new Error("nothing held for " + method)
  const h = handlers[method]
  w.resolve(JSON.stringify(value !== undefined ? value : h ? h(w.params) : null))
}
// Go's napStart: every nap.start opens the next session
let gen = 0
handlers["nap.start"] = () => ({ gen: ++gen })

const makeFrame = () => {
  const f = {
    n: frames.length,
    attrs: {},
    style: {},
    listeners: {},
    removed: false,
    srcdoc: undefined,
    setAttribute(k, v) { this.attrs[k] = String(v) },
    addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn) },
    remove() { this.removed = true; log.push("remove#" + this.n) },
    contentWindow: { posted: [], postMessage(msg) { this.posted.push(msg) } },
  }
  frames.push(f)
  return f
}
globalThis.document = {
  readyState: "complete",
  body: {
    appendChild(el) { appended.push(el); log.push("append#" + el.n) },
    set textContent(v) { bodyText = v },
    get textContent() { return bodyText },
  },
  documentElement: { style: { setProperty() {} } },
  createElement(tag) {
    if (tag !== "iframe") throw new Error("unexpected element " + tag)
    return makeFrame()
  },
  addEventListener() {},
}

const flush = async (n = 3) => { for (let i = 0; i < n; i++) await new Promise(r => setTimeout(r, 0)) }
const fireMessage = (source, data) => { for (const fn of listeners.message || []) fn({ source, data }) }
const fireLoad = f => { for (const fn of f.listeners.load || []) fn({}) }
const count = method => rpcs.filter(r => r.method === method).length
const live = () => appended.filter(f => !f.removed)
`

// runHost runs setup, then the embedded host page, then steps (the body of an
// async function whose return value is printed as JSON), and decodes it.
func runHost(t *testing.T, setup, steps string, out any) {
	t.Helper()
	node := needNode(t)
	program := hostHarness + "\n" + setup + "\n" + nappletHostJS + "\n" +
		";(async () => {\n" + steps + "\n})().then(r => { process.stdout.write(JSON.stringify(r)) }, " +
		"err => { process.stderr.write(String(err && err.stack || err)); process.exitCode = 1 })\n"
	cmd := exec.Command(node, "-")
	cmd.Stdin = strings.NewReader(program)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("harness output %q: %v", raw, err)
	}
}

func indexOf(log []string, entry string, from int) int {
	for i := from; i < len(log); i++ {
		if log[i] == entry {
			return i
		}
	}
	return -1
}

// D-06/D-07: the session is started by this trusted page, before the napplet's
// frame exists, and only that frame's posts are forwarded.
func TestNappletHostStartsSessionBeforeFrame(t *testing.T) {
	var got struct {
		LogBeforeStart   []string          `json:"logBeforeStart"`
		AppendedEarly    int               `json:"appendedEarly"`
		Appended         int               `json:"appended"`
		Attrs            map[string]string `json:"attrs"`
		Srcdoc           string            `json:"srcdoc"`
		LoadedAfterLoad  int               `json:"loadedAfterLoad"`
		MsgParams        []string          `json:"msgParams"`
		MsgAfterStranger int               `json:"msgAfterStranger"`
	}
	runHost(t, `
handlers["nap.boot"] = () => ({ srcdoc: "<p>napplet</p>", title: "probe" })
hold("nap.start")
`, `
await flush()
const logBeforeStart = log.slice()
const appendedEarly = appended.length
release("nap.start")
await flush()
const f = appended[0]
fireLoad(f)
await flush()
const loadedAfterLoad = count("nap.loaded")
fireMessage(f.contentWindow, { type: "storage.keys", id: "k1" })
await flush()
fireMessage({ postMessage() {} }, { type: "storage.keys", id: "stranger" })
fireMessage(null, { type: "storage.keys", id: "nobody" })
await flush()
return {
  logBeforeStart, appendedEarly, appended: appended.length,
  attrs: f.attrs, srcdoc: f.srcdoc, loadedAfterLoad,
  msgParams: rpcs.filter(r => r.method === "nap.msg").map(r => r.params),
  msgAfterStranger: count("nap.msg"),
}
`, &got)

	if !slices.Equal(got.LogBeforeStart, []string{"nap.boot", "nap.start"}) {
		t.Errorf("rpc log before nap.start resolved = %v, want [nap.boot nap.start]", got.LogBeforeStart)
	}
	if got.AppendedEarly != 0 {
		t.Errorf("%d iframe(s) appended before nap.start resolved", got.AppendedEarly)
	}
	if got.Appended != 1 {
		t.Fatalf("appended %d iframes, want 1", got.Appended)
	}
	if got.Attrs["sandbox"] != "allow-scripts" {
		t.Errorf("sandbox = %q, want exactly allow-scripts", got.Attrs["sandbox"])
	}
	if got.Attrs["referrerpolicy"] != "no-referrer" {
		t.Errorf("referrerpolicy = %q", got.Attrs["referrerpolicy"])
	}
	if got.Srcdoc != "<p>napplet</p>" {
		t.Errorf("srcdoc = %q, want the one nap.boot returned", got.Srcdoc)
	}
	if got.LoadedAfterLoad != 1 {
		t.Errorf("load sent nap.loaded %d times, want 1", got.LoadedAfterLoad)
	}
	if len(got.MsgParams) != 1 || got.MsgAfterStranger != 1 {
		t.Fatalf("nap.msg calls = %v (after strangers: %d), want only the frame's one", got.MsgParams, got.MsgAfterStranger)
	}
	// rpc params are JSON, and the envelope travels as a JSON string inside
	var inner string
	if err := json.Unmarshal([]byte(got.MsgParams[0]), &inner); err != nil {
		t.Fatalf("nap.msg params %q: %v", got.MsgParams[0], err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(inner), &env); err != nil {
		t.Fatalf("nap.msg envelope %q: %v", inner, err)
	}
	if env["type"] != "storage.keys" || env["id"] != "k1" {
		t.Errorf("forwarded envelope = %v", env)
	}
}

// D-07: every session gets a fresh frame, the old one goes before the new
// session starts, and nothing the old document does reaches Go any more.
func TestNappletHostReplacesFrameOnReload(t *testing.T) {
	var got struct {
		Log            []string `json:"log"`
		MsgFromOld     int      `json:"msgFromOld"`
		LoadedFromOld  int      `json:"loadedFromOld"`
		LiveAfterBoots int      `json:"liveAfterBoots"`
		OverlapStarts  int      `json:"overlapStarts"`
		LiveAfterStart int      `json:"liveAfterStart"`
		Srcdocs        []string `json:"srcdocs"`
	}
	runHost(t, `
let boots = 0
handlers["nap.boot"] = () => ({ srcdoc: "doc" + (++boots), title: "probe" })
`, `
await flush()
const old = appended[0]
fireLoad(old)
await flush()
const loadedBefore = count("nap.loaded")
window.__nap_reload()
await flush()
const reloadLog = log.slice()

fireMessage(old.contentWindow, { type: "storage.keys", id: "old" })
fireLoad(old)
await flush()
const msgFromOld = count("nap.msg")
const loadedFromOld = count("nap.loaded") - loadedBefore

// two boots whose nap.boot answers are both still out
const startsBefore = count("nap.start")
hold("nap.boot")
window.__nap_reload()
window.__nap_reload()
await flush()
unhold("nap.boot")
release("nap.boot", { srcdoc: "overlap-a", title: "a" })
release("nap.boot", { srcdoc: "overlap-b", title: "b" })
await flush()
const liveAfterBoots = live().length
const overlapStarts = count("nap.start") - startsBefore

// a boot that is already waiting on nap.start when the next one begins
hold("nap.start")
window.__nap_reload()
await flush()
window.__nap_reload()
await flush()
release("nap.start")
await flush()
release("nap.start")
await flush()
const liveAfterStart = live().length

return { log: reloadLog, msgFromOld, loadedFromOld, liveAfterBoots, overlapStarts, liveAfterStart, srcdocs: live().map(f => f.srcdoc) }
`, &got)

	firstStart := indexOf(got.Log, "nap.start", 0)
	remove := indexOf(got.Log, "remove#0", 0)
	secondStart := indexOf(got.Log, "nap.start", firstStart+1)
	secondAppend := indexOf(got.Log, "append#1", 0)
	if firstStart < 0 || remove < 0 || secondStart < 0 || secondAppend < 0 ||
		!(remove < secondStart && secondStart < secondAppend) {
		t.Fatalf("reload order = %v, want the old frame removed before the second nap.start and the new frame appended after it", got.Log)
	}
	if got.MsgFromOld != 0 {
		t.Errorf("the replaced frame's post reached Go (%d nap.msg)", got.MsgFromOld)
	}
	if got.LoadedFromOld != 0 {
		t.Errorf("the replaced frame's load sent nap.loaded %d times", got.LoadedFromOld)
	}
	if got.LiveAfterBoots != 1 {
		t.Errorf("overlapping boots left %d live iframes, want 1", got.LiveAfterBoots)
	}
	// the overtaken boot gives up before it starts a session of its own
	if got.OverlapStarts != 1 {
		t.Errorf("overlapping boots sent %d nap.start rpcs, want 1", got.OverlapStarts)
	}
	if got.LiveAfterStart != 1 {
		t.Errorf("a boot overtaken during nap.start left %d live iframes, want 1", got.LiveAfterStart)
	}
	if len(got.Srcdocs) != 1 || got.Srcdocs[0] == "overlap-a" {
		t.Errorf("live frame documents = %v, want the newest boot's only", got.Srcdocs)
	}
}

var maxPendingRE = regexp.MustCompile(`const MAX_PENDING = (\d+)`)

// D-07 queue bound: a napplet that floods the host page gets terminal
// refusals past MAX_PENDING instead of an ever-growing lane.
func TestNappletHostBoundsPendingEnvelopes(t *testing.T) {
	m := maxPendingRE.FindStringSubmatch(nappletHostJS)
	if m == nil {
		t.Fatal("napplet-host.js declares no MAX_PENDING")
	}
	maxPending, err := strconv.Atoi(m[1])
	if err != nil || maxPending <= 0 {
		t.Fatalf("MAX_PENDING = %q", m[1])
	}
	const extra = 10

	var got struct {
		InFlight   int              `json:"inFlight"`
		Refusals   []map[string]any `json:"refusals"`
		Other      int              `json:"other"`
		Drained    int              `json:"drained"`
		AfterDrain int              `json:"afterDrain"`
	}
	runHost(t, `
handlers["nap.boot"] = () => ({ srcdoc: "<p>flood</p>", title: "flood" })
const TOTAL = `+strconv.Itoa(maxPending+extra)+`
`, `
await flush()
const f = appended[0]
hold("nap.msg")
for (let i = 0; i < TOTAL; i++) fireMessage(f.contentWindow, { type: "storage.keys", id: "e" + i })
await flush()
const inFlight = count("nap.msg")
const posted = f.contentWindow.posted.slice()
// refused in storage.keys' own failure shape: an error, no ok key
const refusals = posted.filter(p => p.type === "storage.keys.result" && p.error === "rate-limited" && !("ok" in p))

// the lane drains once Go answers, and takes envelopes again
unhold("nap.msg")
release("nap.msg")
await flush()
const drained = count("nap.msg")
fireMessage(f.contentWindow, { type: "storage.keys", id: "after" })
await flush()
return { inFlight, refusals, other: posted.length - refusals.length, drained, afterDrain: count("nap.msg") }
`, &got)

	if got.InFlight != 1 {
		t.Errorf("%d nap.msg rpcs in flight, want 1 (one ordered lane)", got.InFlight)
	}
	if len(got.Refusals) != extra || got.Other != 0 {
		t.Fatalf("refusals = %d (other posts %d), want exactly %d", len(got.Refusals), got.Other, extra)
	}
	for i, r := range got.Refusals {
		if want := "e" + strconv.Itoa(maxPending+i); r["id"] != want {
			t.Errorf("refusal %d answers id %v, want %s", i, r["id"], want)
		}
	}
	if got.Drained != maxPending {
		t.Errorf("after draining %d nap.msg rpcs, want %d (every queued envelope, none of the refused)", got.Drained, maxPending)
	}
	if got.AfterDrain != maxPending+1 {
		t.Errorf("the drained lane did not take a new envelope: %d nap.msg", got.AfterDrain)
	}
}

// WR-01: Go checks a push's session before it sends, but the send can still
// land after nap.start answered for the next session. Each push names its
// session, and the host page delivers only those for the current frame's.
func TestNappletHostDropsPushesForOtherSessions(t *testing.T) {
	var got struct {
		First        []string `json:"first"`
		Second       []string `json:"second"`
		BootText     string   `json:"bootText"`
		NoGenAppends int      `json:"noGenAppends"`
	}
	runHost(t, `
handlers["nap.boot"] = () => ({ srcdoc: "<p>napplet</p>", title: "probe" })
`, `
await flush()
const f0 = appended[0]
window.__nap_push(1, JSON.stringify({ type: "for-1" }))
window.__nap_push(2, JSON.stringify({ type: "not-yet" }))

// a reload: the session-1 push in flight lands while nap.start is out, and
// again once the new frame is up
hold("nap.start")
window.__nap_reload()
await flush()
window.__nap_push(1, JSON.stringify({ type: "stale-while-starting" }))
unhold("nap.start")
release("nap.start")
await flush()
const f1 = appended[1]
window.__nap_push(1, JSON.stringify({ type: "stale-after-start" }))
window.__nap_push("2", JSON.stringify({ type: "string-gen" }))
window.__nap_push(2, JSON.stringify({ type: "for-2" }))

// a nap.start answer without a session never gets a frame
handlers["nap.start"] = () => null
const before = appended.length
window.__nap_reload()
await flush()

return {
  first: f0.contentWindow.posted.map(p => p.type),
  second: f1.contentWindow.posted.map(p => p.type),
  bootText: document.body.textContent,
  noGenAppends: appended.length - before,
}
`, &got)

	if !slices.Equal(got.First, []string{"for-1"}) {
		t.Errorf("first frame got %v, want only its own session's push", got.First)
	}
	if !slices.Equal(got.Second, []string{"for-2"}) {
		t.Errorf("replacement frame got %v, want only session 2's push", got.Second)
	}
	if got.NoGenAppends != 0 || !strings.Contains(got.BootText, "could not be started") {
		t.Errorf("nap.start without a gen: %d frame(s) appended, body %q", got.NoGenAppends, got.BootText)
	}
}

// WR-04: the pending bound limits the napplet, not this page. A boot or a
// load while the napplet has MAX_PENDING envelopes in flight still starts the
// session and reports the load, after the queued envelopes, in order.
func TestNappletHostLifecycleBypassesPendingBound(t *testing.T) {
	m := maxPendingRE.FindStringSubmatch(nappletHostJS)
	if m == nil {
		t.Fatal("napplet-host.js declares no MAX_PENDING")
	}
	var got struct {
		BootText  string   `json:"bootText"`
		Loaded    int      `json:"loaded"`
		Starts    int      `json:"starts"`
		Appended  int      `json:"appended"`
		Refusals  int      `json:"refusals"`
		LaneOrder []string `json:"laneOrder"`
	}
	runHost(t, `
handlers["nap.boot"] = () => ({ srcdoc: "<p>flood</p>", title: "flood" })
const TOTAL = `+m[1]+`
`, `
await flush()
const f = appended[0]
hold("nap.msg")
for (let i = 0; i < TOTAL; i++) fireMessage(f.contentWindow, { type: "storage.keys", id: "e" + i })
await flush()
fireLoad(f)
window.__nap_reload()
await flush()
const bootText = document.body.textContent
unhold("nap.msg")
release("nap.msg")
await flush(10)
const lane = log.filter(e => e === "nap.loaded" || e === "nap.start" || e === "nap.msg")
return {
  bootText, loaded: count("nap.loaded"), starts: count("nap.start"), appended: appended.length,
  refusals: f.contentWindow.posted.filter(p => typeof p.error === "string").length,
  laneOrder: lane.slice(-3),
}
`, &got)

	if got.BootText != "" {
		t.Errorf("a boot behind a full lane failed: %q", got.BootText)
	}
	if got.Starts != 2 || got.Appended != 2 {
		t.Errorf("nap.start sent %d times, %d frames appended; want 2 and 2", got.Starts, got.Appended)
	}
	if got.Loaded != 1 {
		t.Errorf("nap.loaded sent %d times behind a full lane, want 1", got.Loaded)
	}
	if got.Refusals != 0 {
		t.Errorf("%d of the napplet's own envelopes were refused, want none (exactly MAX_PENDING)", got.Refusals)
	}
	// the trusted calls keep their place: after every queued envelope
	if !slices.Equal(got.LaneOrder, []string{"nap.msg", "nap.loaded", "nap.start"}) {
		t.Errorf("lane tail = %v, want the queued envelopes, then nap.loaded, then nap.start", got.LaneOrder)
	}
}

// ─── refusals ───────────────────────────────────────────────────────

// loadFailFixture is ../testdata/nap-fail-envelopes.json, the failure
// envelopes Go's failWith and the host page's refuse must both build
// (TestGoFailWithMatchesSharedFixture is the Go side).
func loadFailFixture(t *testing.T) (raw []byte, cases []struct {
	Name   string          `json:"name"`
	Code   string          `json:"code"`
	Expect json.RawMessage `json:"expect"`
}) {
	t.Helper()
	raw, err := os.ReadFile("../testdata/nap-fail-envelopes.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []struct {
			Name   string          `json:"name"`
			Code   string          `json:"code"`
			Expect json.RawMessage `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("nap-fail-envelopes.json has no cases")
	}
	return raw, f.Cases
}

// failShapesJSON is the FAIL_SHAPES table between its markers, as shipped.
func failShapesJSON(t *testing.T) string {
	t.Helper()
	_, rest, ok := strings.Cut(nappletHostJS, "/* nap-fail-shapes:begin */")
	body, _, ok2 := strings.Cut(rest, "/* nap-fail-shapes:end */")
	if !ok || !ok2 {
		t.Fatal("napplet-host.js has no FAIL_SHAPES markers")
	}
	return body
}

// failModes drives a request into each of the host page's own failures:
// internal-error (nap.msg fails), too-large (past every envelope bound),
// rate-limited (MAX_PENDING envelopes already in the lane) and
// invalid-request (an envelope that cannot be encoded). Each returns what
// the frame was posted for that one request.
const failModes = `
const failOnce = async (f, code, request) => {
  const before = f.contentWindow.posted.length
  const msgsBefore = count("nap.msg")
  // a shallow copy, not a JSON round trip: ids such as Infinity must reach
  // the host page as they are
  const data = Object.assign({}, request)
  let reached
  if (code === "internal-error") {
    handlers["nap.msg"] = () => ({ __bridge_error: "boom" })
    fireMessage(f.contentWindow, data)
    await flush()
    reached = count("nap.msg") - msgsBefore
  } else if (code === "too-large") {
    handlers["nap.msg"] = () => null
    // one past the bound for this type: upload.upload gets 24 MiB, the rest 1 MiB
    data.pad = "x".repeat((data.type === "upload.upload" ? 24 : 1) * 1024 * 1024 + 1)
    fireMessage(f.contentWindow, data)
    await flush()
    reached = count("nap.msg") - msgsBefore
  } else if (code === "invalid-request") {
    handlers["nap.msg"] = () => null
    data.self = data
    fireMessage(f.contentWindow, data)
    await flush()
    reached = count("nap.msg") - msgsBefore
  } else if (code === "rate-limited") {
    handlers["nap.msg"] = () => null
    hold("nap.msg")
    // reply-less fillers: when they drain they post nothing
    for (let i = 0; i < MAX_PENDING; i++) fireMessage(f.contentWindow, { type: "relay.close", subId: "fill" + i })
    await flush()
    fireMessage(f.contentWindow, data)
    await flush()
    const posted = f.contentWindow.posted.slice(before)
    unhold("nap.msg")
    release("nap.msg")
    await flush(10)
    // only the first filler was in flight: the request itself never reached Go
    reached = count("nap.msg") - msgsBefore - MAX_PENDING
    return { posted, reached, drained: f.contentWindow.posted.length - before - posted.length }
  } else {
    throw new Error("no way to make the host page fail with " + code)
  }
  return { posted: f.contentWindow.posted.slice(before), reached, drained: 0 }
}
`

func maxPending(t *testing.T) string {
	t.Helper()
	m := maxPendingRE.FindStringSubmatch(nappletHostJS)
	if m == nil {
		t.Fatal("napplet-host.js declares no MAX_PENDING")
	}
	return m[1]
}

// DISP-02: for every case of the shared fixture the host page posts exactly
// the envelope Go's failWith sends for it (or nothing when Go sends nothing),
// whichever of its own failures refused the request.
func TestNappletHostRefusalsMatchSharedFixture(t *testing.T) {
	raw, cases := loadFailFixture(t)
	var got []struct {
		Posted  []json.RawMessage `json:"posted"`
		Reached int               `json:"reached"`
		Drained int               `json:"drained"`
	}
	runHost(t, `
handlers["nap.boot"] = () => ({ srcdoc: "<p>refusals</p>", title: "refusals" })
const FIXTURE = `+string(raw)+`
const MAX_PENDING = `+maxPending(t)+`
`+failModes, `
await flush()
const f = appended[0]
const out = []
for (const c of FIXTURE.cases) out.push(await failOnce(f, c.code, c.request))
return out
`, &got)

	if len(got) != len(cases) {
		t.Fatalf("harness ran %d cases, fixture has %d", len(got), len(cases))
	}
	for i, tc := range cases {
		r := got[i]
		// the request must have failed in the host page, never in Go
		wantReached := 0
		if tc.Code == "internal-error" {
			wantReached = 1
		}
		if r.Reached != wantReached {
			t.Errorf("%s: %d nap.msg rpcs for the request, want %d", tc.Name, r.Reached, wantReached)
		}
		if r.Drained != 0 {
			t.Errorf("%s: the drained fillers posted %d messages", tc.Name, r.Drained)
		}
		var want any
		if err := json.Unmarshal(tc.Expect, &want); err != nil {
			t.Fatalf("%s: expect: %v", tc.Name, err)
		}
		if want == nil {
			if len(r.Posted) != 0 {
				t.Errorf("%s: host page posted %s, want nothing", tc.Name, r.Posted)
			}
			continue
		}
		if len(r.Posted) != 1 {
			t.Errorf("%s: host page posted %d messages %s, want exactly one", tc.Name, len(r.Posted), r.Posted)
			continue
		}
		var posted any
		if err := json.Unmarshal(r.Posted[0], &posted); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(posted, want) {
			t.Errorf("%s:\n got  %s\n want %s", tc.Name, r.Posted[0], tc.Expect)
		}
	}
}

// NIP-5D, A16, D-11: whatever fails, the host page never answers an unknown
// type, a reply-less type (resource.cancel's id names another request), an
// id Go would not echo, or a subscription without a valid subId.
func TestNappletHostRefusesNothingItMustNotAnswer(t *testing.T) {
	var got []struct {
		Label   string            `json:"label"`
		Posted  []json.RawMessage `json:"posted"`
		Reached int               `json:"reached"`
	}
	runHost(t, `
handlers["nap.boot"] = () => ({ srcdoc: "<p>silent</p>", title: "silent" })
const SHAPES = `+failShapesJSON(t)+`
const MAX_PENDING = `+maxPending(t)+`
`+failModes, `
await flush()
const f = appended[0]
const requests = []
// unknown types, including ones that name an Object.prototype member
for (const type of ["nope.unknown", "__proto__", "constructor", "toString", "hasOwnProperty", "relay.subscribe.result", "shell.ready", ""]) {
  requests.push({ type, id: "u", subId: "s" })
}
// every reply-less type, even carrying an id and a subId
for (const type of Object.keys(SHAPES)) {
  if (SHAPES[type].kind === "none") requests.push({ type, id: "n", subId: "s" })
}
// ids Go would not echo, on a type that answers
for (const id of [{}, [], true, null, "a".repeat(129), "\u00e9".repeat(65), Infinity, NaN]) {
  requests.push({ type: "storage.get", id, key: "k" })
}
requests.push({ type: "storage.get", key: "k" })
// subscriptions without a valid subId, or with a bad id
for (const type of Object.keys(SHAPES)) {
  if (SHAPES[type].kind !== "lifecycle") continue
  for (const subId of [undefined, "", 5, null, "a".repeat(129)]) requests.push({ type, subId })
  requests.push({ type, id: {}, subId: "s" })
}
const out = []
for (const code of ["internal-error", "too-large", "rate-limited"]) {
  for (const request of requests) {
    const r = await failOnce(f, code, request)
    out.push({ label: code + " " + JSON.stringify(request).slice(0, 80), posted: r.posted, reached: r.reached })
  }
}
return out
`, &got)

	if len(got) < 3*20 {
		t.Fatalf("only %d probes ran", len(got))
	}
	for _, r := range got {
		if len(r.Posted) != 0 {
			t.Errorf("%s: host page answered %s", r.Label, r.Posted)
		}
		if strings.HasPrefix(r.Label, "internal-error ") && r.Reached != 1 {
			t.Errorf("%s: %d nap.msg rpcs, want the request to have reached (and failed in) the rpc", r.Label, r.Reached)
		}
	}
}

// WR-04: the host page measures an envelope the way Go and the wire do, not
// in UTF-16 units. Multi-byte text that fits 1 MiB of UTF-16 but not 1 MiB of
// UTF-8 is refused too-large, as Go would refuse it; an upload whose escapes
// would push its wire line past Go's cap is refused "file too large" before
// it reaches Go (where it would close the window); and an envelope within
// both bounds still reaches Go.
func TestNappletHostMeasuresWireBytes(t *testing.T) {
	var got []struct {
		Posted  []json.RawMessage `json:"posted"`
		Reached int               `json:"reached"`
	}
	runHost(t, `
handlers["nap.boot"] = () => ({ srcdoc: "<p>sizes</p>", title: "sizes" })
handlers["nap.msg"] = () => null
`, `
await flush()
const f = appended[0]
const MiB = 1024 * 1024
const probe = async data => {
  const before = f.contentWindow.posted.length
  const msgsBefore = count("nap.msg")
  fireMessage(f.contentWindow, data)
  await flush()
  return { posted: f.contentWindow.posted.slice(before), reached: count("nap.msg") - msgsBefore }
}
return [
  // 600 Ki UTF-16 units, 1.2 MB of UTF-8
  await probe({ type: "storage.get", id: "utf8", key: "k", pad: "\u00e9".repeat(600 * 1024) }),
  // 20 MiB of UTF-16, but 40 MiB once the child escapes every "<"
  await probe({ type: "upload.upload", id: "escapes", pad: "<".repeat(4 * MiB) + "x".repeat(16 * MiB) }),
  // within both bounds
  await probe({ type: "storage.get", id: "fits", key: "k", pad: "x".repeat(900 * 1024) }),
]
`, &got)

	if len(got) != 3 {
		t.Fatalf("%d probes ran", len(got))
	}
	want := []string{
		`{"type":"storage.get.result","error":"too-large","id":"utf8"}`,
		`{"type":"upload.upload.result","error":"file too large","id":"escapes"}`,
	}
	for i, w := range want {
		if got[i].Reached != 0 {
			t.Errorf("probe %d reached Go %d times, want refused by the page", i, got[i].Reached)
		}
		if len(got[i].Posted) != 1 {
			t.Errorf("probe %d posted %s, want one refusal", i, got[i].Posted)
			continue
		}
		var a, b any
		_ = json.Unmarshal(got[i].Posted[0], &a)
		_ = json.Unmarshal([]byte(w), &b)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("probe %d:\n got  %s\n want %s", i, got[i].Posted[0], w)
		}
	}
	if got[2].Reached != 1 || len(got[2].Posted) != 0 {
		t.Errorf("an envelope within the bounds: reached %d, posted %s", got[2].Reached, got[2].Posted)
	}
}
