package webview

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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
const refusals = posted.filter(p => p.type === "storage.keys.result" && p.ok === false)

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
  refusals: f.contentWindow.posted.filter(p => p.ok === false).length,
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
