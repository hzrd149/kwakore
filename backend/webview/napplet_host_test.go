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

// SBOX-01, D-01, D-02, D-21: a frame's second load means its document was
// replaced. The frame goes, the old session ends through nap.reset on the
// lane, and a fresh frame boots with a fresh nap.boot answer and session.
func TestNappletHostRebuildsReplacedFrame(t *testing.T) {
	var got struct {
		Log            []string `json:"log"`
		LoadedFirst    int      `json:"loadedFirst"`
		LoadedAfter    int      `json:"loadedAfter"`
		Srcdoc1        string   `json:"srcdoc1"`
		MsgFromOld     int      `json:"msgFromOld"`
		OldPosted      []string `json:"oldPosted"`
		NewPosted      []string `json:"newPosted"`
		LoadedSecond   int      `json:"loadedSecond"`
		Resets         int      `json:"resets"`
		Boots          int      `json:"boots"`
		Frame1Removed  bool     `json:"frame1Removed"`
		Srcdoc2        string   `json:"srcdoc2"`
		StaleChanged   bool     `json:"staleChanged"`
		DoubleBoots    int      `json:"doubleBoots"`
		DoubleResets   int      `json:"doubleResets"`
		DoubleAppended int      `json:"doubleAppended"`
		Live           int      `json:"live"`
	}
	runHost(t, `
let boots = 0
handlers["nap.boot"] = () => ({ srcdoc: "doc" + (++boots), title: "probe" })
`, `
await flush()
const f0 = appended[0]
fireLoad(f0)
await flush()
const loadedFirst = count("nap.loaded")
const mark = log.length
// the napplet reloads itself: a second load of the same frame
fireLoad(f0)
await flush()
const replaceLog = log.slice(mark)
const loadedAfter = count("nap.loaded")
const f1 = appended[1]
if (!f1) return { log: replaceLog, loadedFirst, loadedAfter }

// the replaced document keeps posting, and a push for its session lands late
fireMessage(f0.contentWindow, { type: "storage.keys", id: "old" })
window.__nap_push(1, JSON.stringify({ type: "for-old-session" }))
window.__nap_push(2, JSON.stringify({ type: "for-new-session" }))
await flush()
const msgFromOld = count("nap.msg")

// the new frame's first load is its boot, its second replaces it again
fireLoad(f1)
await flush()
const loadedSecond = count("nap.loaded")
fireLoad(f1)
await flush()
const f2 = appended[2]
if (!f2) return { log: replaceLog, loadedFirst, loadedAfter, srcdoc1: f1.srcdoc, loadedSecond }

// a late load of a frame that is no longer current changes nothing
const before = { resets: count("nap.reset"), boots: count("nap.boot"), log: log.length }
fireLoad(f0)
fireLoad(f1)
await flush()
const staleChanged = count("nap.reset") !== before.resets || count("nap.boot") !== before.boots || log.length !== before.log

// a replacement whose late load fires right behind it: one rebuild only
fireLoad(f2)
await flush()
const dBoots = count("nap.boot"), dResets = count("nap.reset"), dAppended = appended.length
fireLoad(f2)
fireLoad(f2)
await flush()

return {
  log: replaceLog, loadedFirst, loadedAfter, srcdoc1: f1 && f1.srcdoc, msgFromOld,
  oldPosted: f0.contentWindow.posted.map(p => p.type),
  newPosted: f1.contentWindow.posted.map(p => p.type),
  loadedSecond, resets: before.resets, boots: before.boots,
  frame1Removed: f1.removed, srcdoc2: f2 && f2.srcdoc, staleChanged,
  doubleBoots: count("nap.boot") - dBoots, doubleResets: count("nap.reset") - dResets,
  doubleAppended: appended.length - dAppended, live: live().length,
}
`, &got)

	if got.LoadedFirst != 1 {
		t.Errorf("the first load sent nap.loaded %d times, want 1", got.LoadedFirst)
	}
	if got.LoadedAfter != 1 {
		t.Errorf("a replaced document's load sent nap.loaded (%d in total, want 1)", got.LoadedAfter)
	}
	remove := indexOf(got.Log, "remove#0", 0)
	reset := indexOf(got.Log, "nap.reset", 0)
	boot := indexOf(got.Log, "nap.boot", 0)
	start := indexOf(got.Log, "nap.start", 0)
	appendNew := indexOf(got.Log, "append#1", 0)
	if remove != 0 || reset < 0 || boot < 0 || start < 0 || appendNew < 0 ||
		!(remove < reset && reset < boot && boot < start && start < appendNew) {
		t.Fatalf("replacement order = %v, want remove#0, nap.reset, nap.boot, nap.start, append#1", got.Log)
	}
	if got.Srcdoc1 != "doc2" {
		t.Errorf("rebuilt frame srcdoc = %q, want the second nap.boot answer", got.Srcdoc1)
	}
	if got.MsgFromOld != 0 {
		t.Errorf("the replaced document's post reached Go (%d nap.msg)", got.MsgFromOld)
	}
	if len(got.OldPosted) != 0 {
		t.Errorf("the replaced frame got %v", got.OldPosted)
	}
	if !slices.Equal(got.NewPosted, []string{"for-new-session"}) {
		t.Errorf("the rebuilt frame got %v, want only its own session's push", got.NewPosted)
	}
	if got.LoadedSecond != 2 {
		t.Errorf("the rebuilt frame's first load: nap.loaded %d in total, want 2", got.LoadedSecond)
	}
	if got.Resets != 2 || got.Boots != 3 || !got.Frame1Removed || got.Srcdoc2 != "doc3" {
		t.Errorf("second replacement: resets %d, boots %d, frame1 removed %v, srcdoc %q; want 2, 3, true, doc3",
			got.Resets, got.Boots, got.Frame1Removed, got.Srcdoc2)
	}
	if got.StaleChanged {
		t.Error("a load of a frame that is no longer current did something")
	}
	if got.DoubleBoots != 1 || got.DoubleResets != 1 || got.DoubleAppended != 1 {
		t.Errorf("one replacement with a late load behind it: %d nap.boot, %d nap.reset, %d appended; want 1 each",
			got.DoubleBoots, got.DoubleResets, got.DoubleAppended)
	}
	if got.Live != 1 {
		t.Errorf("%d live frames, want 1", got.Live)
	}
}

// markerSetup defines MARKER, the document-start marker's type, from the Go
// constant the preamble posts, so the harness never retypes the literal.
func markerSetup(extra string) string {
	return "const MARKER = " + strconv.Quote(DocumentMarker) + "\n" + extra
}

// SBOX-01, D-18 (RESEARCH C2): every document built from the srcdoc posts the
// document-start marker before any of its scripts run. The first one is the
// boot; a second one from the same frame means its document was replaced, and
// the frame is dropped right then, before the replacing document's load and
// before any envelope it posts behind the marker can reach the old session.
func TestNappletHostMarkerReplacesFrame(t *testing.T) {
	var got struct {
		RebuiltEarly bool     `json:"rebuiltEarly"`
		Loaded       int      `json:"loaded"`
		Log          []string `json:"log"`
		MsgEarly     int      `json:"msgEarly"`
		Boots        int      `json:"boots"`
		Resets       int      `json:"resets"`
		Appended     int      `json:"appended"`
		Live         int      `json:"live"`
		Srcdoc1      string   `json:"srcdoc1"`
		MsgTotal     int      `json:"msgTotal"`
		MsgParams    []string `json:"msgParams"`
		Posted0      int      `json:"posted0"`
		Errors       []string `json:"errors"`
	}
	runHost(t, markerSetup(`
let boots = 0
handlers["nap.boot"] = () => ({ srcdoc: "doc" + (++boots), title: "probe" })
`), `
await flush()
const f0 = appended[0]
// the first document announces itself before its load: that is the boot
fireMessage(f0.contentWindow, { type: MARKER })
await flush()
const rebuiltEarly = count("nap.boot") !== 1 || count("nap.reset") !== 0 || f0.removed
fireLoad(f0)
await flush()
const loaded = count("nap.loaded")

// the napplet reloads itself and holds back its load event: the reloaded
// document's marker comes first, its first envelope right behind it
const mark = log.length
fireMessage(f0.contentWindow, { type: MARKER })
fireMessage(f0.contentWindow, { type: "storage.keys", id: "early" })
await flush(6)
const msgEarly = count("nap.msg")
// the reloaded document's load finally fires: it changes nothing more
fireLoad(f0)
await flush(6)
const replaceLog = log.slice(mark)

// the rebuilt frame works: its own marker is its boot, its envelopes go to Go
const f1 = appended[1]
if (f1) {
  fireMessage(f1.contentWindow, { type: MARKER })
  fireLoad(f1)
  await flush()
  fireMessage(f1.contentWindow, { type: "storage.keys", id: "new" })
  await flush()
}
return {
  rebuiltEarly, loaded, log: replaceLog, msgEarly,
  boots: count("nap.boot"), resets: count("nap.reset"), appended: appended.length, live: live().length,
  srcdoc1: f1 && f1.srcdoc, msgTotal: count("nap.msg"),
  msgParams: rpcs.filter(r => r.method === "nap.msg").map(r => r.params),
  posted0: f0.contentWindow.posted.length,
  errors,
}
`, &got)

	if got.RebuiltEarly {
		t.Error("the first document's marker, before its load, rebuilt the frame")
	}
	if got.Loaded != 1 {
		t.Errorf("the first load sent nap.loaded %d times, want 1", got.Loaded)
	}
	remove := indexOf(got.Log, "remove#0", 0)
	reset := indexOf(got.Log, "nap.reset", 0)
	boot := indexOf(got.Log, "nap.boot", 0)
	start := indexOf(got.Log, "nap.start", 0)
	appendNew := indexOf(got.Log, "append#1", 0)
	if remove != 0 || reset < 0 || boot < 0 || start < 0 || appendNew < 0 ||
		!(remove < reset && reset < boot && boot < start && start < appendNew) {
		t.Fatalf("replacement order = %v, want remove#0, nap.reset, nap.boot, nap.start, append#1", got.Log)
	}
	if indexOf(got.Log, "nap.msg", 0) >= 0 || got.MsgEarly != 0 {
		t.Errorf("the reloaded document's envelope behind its marker reached Go (log %v)", got.Log)
	}
	// one replacement, reported by its marker and then by its load: one rebuild
	if got.Boots != 2 || got.Resets != 1 || got.Appended != 2 || got.Live != 1 {
		t.Errorf("nap.boot %d, nap.reset %d, frames %d, live %d; want 2, 1, 2, 1",
			got.Boots, got.Resets, got.Appended, got.Live)
	}
	if got.Srcdoc1 != "doc2" {
		t.Errorf("rebuilt frame srcdoc = %q, want the second nap.boot answer", got.Srcdoc1)
	}
	// a marker is never forwarded: the only nap.msg is the rebuilt frame's own
	if got.MsgTotal != 1 || len(got.MsgParams) != 1 || !strings.Contains(got.MsgParams[0], `\"new\"`) {
		t.Errorf("nap.msg params = %v, want only the rebuilt frame's envelope", got.MsgParams)
	}
	for _, p := range got.MsgParams {
		if strings.Contains(p, DocumentMarker) {
			t.Errorf("a marker reached Go: %s", p)
		}
	}
	// and never answered: no refusal or reply went back into either frame
	if got.Posted0 != 0 {
		t.Errorf("the replaced frame was posted %d messages", got.Posted0)
	}
	if len(got.Errors) != 0 {
		t.Errorf("host page errors: %v", got.Errors)
	}
}

// D-18 edges: the orderings the spike saw and the messages a napplet or a
// stranger can forge. Markers count only from the current frame, only when
// their type is the marker string itself, and never produce a post anywhere.
func TestNappletHostMarkerEdgeCases(t *testing.T) {
	t.Run("a first marker after the first load rebuilds nothing", func(t *testing.T) {
		var got struct {
			Boots, Resets, Appended, Loaded, Msgs, Posted int
		}
		runHost(t, markerSetup(`handlers["nap.boot"] = () => ({ srcdoc: "doc", title: "probe" })`), `
await flush()
const f0 = appended[0]
fireLoad(f0)
await flush()
fireMessage(f0.contentWindow, { type: MARKER })
await flush(6)
return {
  Boots: count("nap.boot"), Resets: count("nap.reset"), Appended: appended.length,
  Loaded: count("nap.loaded"), Msgs: count("nap.msg"), Posted: f0.contentWindow.posted.length,
}
`, &got)
		if got.Boots != 1 || got.Resets != 0 || got.Appended != 1 || got.Loaded != 1 || got.Msgs != 0 || got.Posted != 0 {
			t.Errorf("load then marker: nap.boot %d, nap.reset %d, frames %d, nap.loaded %d, nap.msg %d, posted %d; want 1, 0, 1, 1, 0, 0",
				got.Boots, got.Resets, got.Appended, got.Loaded, got.Msgs, got.Posted)
		}
	})

	t.Run("forged and foreign markers are not counted", func(t *testing.T) {
		var got struct {
			Boots, Resets, Msgs, Posted int
			Errors                      []string
			Replaced                    bool
		}
		runHost(t, markerSetup(`handlers["nap.boot"] = () => ({ srcdoc: "doc", title: "probe" })`), `
await flush()
const f0 = appended[0]
fireMessage(f0.contentWindow, { type: MARKER })
fireLoad(f0)
await flush()
// a stranger and a sourceless message, however many, are not the frame
const stranger = { posted: [], postMessage(m) { this.posted.push(m) } }
for (let i = 0; i < 3; i++) {
  fireMessage(stranger, { type: MARKER })
  fireMessage(null, { type: MARKER })
}
// the frame's own messages whose type only looks like the marker
fireMessage(f0.contentWindow, { type: 5 })
fireMessage(f0.contentWindow, { type: new String(MARKER) })
fireMessage(f0.contentWindow, { type: MARKER + " " })
fireMessage(f0.contentWindow, { type: [MARKER] })
fireMessage(f0.contentWindow, MARKER)
fireMessage(f0.contentWindow, null)
await flush(6)
const before = { boots: count("nap.boot"), resets: count("nap.reset") }
// the real second marker still replaces: none of the above was counted as
// the first, and the frame is still current
fireMessage(f0.contentWindow, { type: MARKER })
await flush(6)
return {
  Boots: before.boots, Resets: before.resets,
  Msgs: rpcs.filter(r => r.method === "nap.msg" && r.params.includes("__verdana")).length,
  Posted: f0.contentWindow.posted.length + stranger.posted.length,
  Errors: errors, Replaced: f0.removed && count("nap.reset") === 1 && count("nap.boot") === 2,
}
`, &got)
		if got.Boots != 1 || got.Resets != 0 {
			t.Errorf("forged or foreign markers rebuilt the frame: nap.boot %d, nap.reset %d; want 1, 0", got.Boots, got.Resets)
		}
		// the near-miss string type is an ordinary envelope Go refuses; the
		// marker itself and the non-string types never become a nap.msg
		if got.Msgs != 1 {
			t.Errorf("%d nap.msg carry the marker name, want 1 (the %q near miss only)", got.Msgs, DocumentMarker+" ")
		}
		if !got.Replaced {
			t.Error("the frame's real second marker did not replace it")
		}
		if got.Posted != 0 || len(got.Errors) != 0 {
			t.Errorf("posts into the frame or the stranger %d, host page errors %v; want none", got.Posted, got.Errors)
		}
	})

	t.Run("a marker is never answered", func(t *testing.T) {
		var got struct{ Posted, Msgs int }
		runHost(t, markerSetup(`handlers["nap.boot"] = () => ({ srcdoc: "doc", title: "probe" })`), `
await flush()
const f0 = appended[0]
fireMessage(f0.contentWindow, { type: MARKER, id: "m1" })
fireLoad(f0)
await flush(6)
return { Posted: f0.contentWindow.posted.length, Msgs: count("nap.msg") }
`, &got)
		if got.Posted != 0 || got.Msgs != 0 {
			t.Errorf("a marker carrying an id: %d posts into the frame, %d nap.msg; want 0, 0", got.Posted, got.Msgs)
		}
	})

	t.Run("an extra marker in a live document counts toward the reload cap", func(t *testing.T) {
		var got struct {
			Boots, Resets, Appended, Live int
			Body                          string
		}
		runHost(t, markerSetup(reloadLoopSetup), `
await flush()
// each document announces itself and loads; then the napplet posts a second
// marker on its own, with no reload: alternately by marker and by load, the
// four quick replacements share one cap
for (let i = 0; i < 4; i++) {
  const f = appended[i]
  now += 500
  fireMessage(f.contentWindow, { type: MARKER })
  fireLoad(f)
  await flush()
  if (i % 2 === 0) fireMessage(f.contentWindow, { type: MARKER })
  else fireLoad(f)
  await flush(6)
}
return { Boots: count("nap.boot"), Resets: count("nap.reset"), Appended: appended.length, Live: live().length, Body: document.body.textContent }
`, &got)
		if got.Boots != 4 || got.Resets != 4 || got.Appended != 4 || got.Live != 0 {
			t.Errorf("nap.boot %d, nap.reset %d, frames %d, live %d; want 4, 4, 4, 0", got.Boots, got.Resets, got.Appended, got.Live)
		}
		if !strings.Contains(got.Body, "keeps reloading itself") {
			t.Errorf("window text after the halt = %q", got.Body)
		}
	})
}

// SBOX-01, D-02: a refusal the host page builds belongs to the frame that sent
// the envelope. One whose frame was replaced while the envelope was out is
// dropped: it never reaches the replaced document or the one rebuilt after it.
func TestNappletHostDropsRefusalsForReplacedFrames(t *testing.T) {
	var got struct {
		Rebuilt   bool     `json:"rebuilt"`
		OldPosted []string `json:"oldPosted"`
		NewPosted []string `json:"newPosted"`
		Own       []string `json:"own"`
	}
	runHost(t, `
let boots = 0
handlers["nap.boot"] = () => ({ srcdoc: "doc" + (++boots), title: "probe" })
`, `
await flush()
const f0 = appended[0]
fireLoad(f0)
await flush()
hold("nap.msg")
fireMessage(f0.contentWindow, { type: "storage.keys", id: "old" })
await flush()
// the document is replaced while its envelope is out, and then Go fails it
fireLoad(f0)
await flush()
unhold("nap.msg")
release("nap.msg", { __bridge_error: "boom" })
await flush(10)
const f1 = appended[1]
if (!f1) return { rebuilt: false }
fireLoad(f1)
await flush()
const newPosted = f1.contentWindow.posted.map(p => p.type + ":" + p.id)

// the rebuilt frame's own failure still reaches it
handlers["nap.msg"] = () => ({ __bridge_error: "boom" })
fireMessage(f1.contentWindow, { type: "storage.keys", id: "new" })
await flush()
return {
  rebuilt: true,
  oldPosted: f0.contentWindow.posted.map(p => p.type + ":" + p.id),
  newPosted,
  own: f1.contentWindow.posted.map(p => p.type + ":" + p.id + ":" + p.error),
}
`, &got)

	if !got.Rebuilt {
		t.Fatal("the replaced frame was not rebuilt")
	}
	if len(got.OldPosted) != 0 {
		t.Errorf("the replaced frame got %v, want nothing", got.OldPosted)
	}
	if len(got.NewPosted) != 0 {
		t.Errorf("the rebuilt frame got %v, the refusal of the document it replaced", got.NewPosted)
	}
	if !slices.Equal(got.Own, []string{"storage.keys.result:new:internal-error"}) {
		t.Errorf("the rebuilt frame's own refusal: %v", got.Own)
	}
}

// reloadLoopSetup stubs the monotonic clock the loop cap reads
// (performance.now) and numbers every nap.boot answer; the wall clock is
// stubbed to jump backwards on every read, which must change nothing.
// replaceAt(f, ms) advances the clock and gives f its first load (when it
// has none yet) and then a second one: a replaced document.
const reloadLoopSetup = `
let now = 0
Object.defineProperty(globalThis, "performance", { value: { now: () => now }, configurable: true, writable: true })
let wall = 1e12
Date.now = () => (wall -= 60 * 60 * 1000)
let boots = 0
handlers["nap.boot"] = () => ({ srcdoc: "doc" + (++boots), title: "probe" })
const loadedOnce = new Set()
const replaceAt = async (f, ms) => {
  now += ms
  if (!loadedOnce.has(f)) { loadedOnce.add(f); fireLoad(f) }
  await flush()
  fireLoad(f)
  await flush(6)
}
`

// D-03: a napplet that keeps replacing its own document is rebuilt at most
// REBUILD_LIMIT times within REBUILD_WINDOW_MS; the next replacement still
// ends its session but boots nothing and says why in the window. Only the
// launcher's dev reload starts it again.
func TestNappletHostStopsReloadLoop(t *testing.T) {
	t.Run("halts on the fourth quick replacement", func(t *testing.T) {
		var got struct {
			Boots, Starts, Resets, Appended, Live int
			Body                                  string
			MsgAfter, ResetsAfter, BootsAfter     int
		}
		runHost(t, reloadLoopSetup, `
await flush()
for (let i = 0; i < 4; i++) await replaceAt(appended[i], 500)
const last = appended[appended.length - 1]
const out = {
  Boots: count("nap.boot"), Starts: count("nap.start"), Resets: count("nap.reset"),
  Appended: appended.length, Live: live().length, Body: document.body.textContent,
}
// the halted frame is gone: its posts and loads reach nothing
fireMessage(last.contentWindow, { type: "storage.keys", id: "after" })
fireLoad(last)
fireLoad(last)
await flush(6)
return Object.assign(out, { MsgAfter: count("nap.msg"), ResetsAfter: count("nap.reset"), BootsAfter: count("nap.boot") })
`, &got)

		// the initial boot plus three rebuilds; the fourth replacement only resets
		if got.Boots != 4 || got.Starts != 4 || got.Appended != 4 {
			t.Errorf("nap.boot %d, nap.start %d, frames %d; want 4, 4, 4", got.Boots, got.Starts, got.Appended)
		}
		if got.Resets != 4 {
			t.Errorf("nap.reset sent %d times, want 4 (the halted replacement still ends its session)", got.Resets)
		}
		if got.Live != 0 {
			t.Errorf("%d live frames after the halt, want 0", got.Live)
		}
		if !strings.Contains(got.Body, "keeps reloading itself") {
			t.Errorf("window text after the halt = %q", got.Body)
		}
		if got.MsgAfter != 0 || got.ResetsAfter != 4 || got.BootsAfter != 4 {
			t.Errorf("after the halt: nap.msg %d, nap.reset %d, nap.boot %d; want 0, 4, 4", got.MsgAfter, got.ResetsAfter, got.BootsAfter)
		}
	})

	t.Run("spaced replacements never halt", func(t *testing.T) {
		var got struct {
			Boots, Resets, Appended, Live int
			Body                          string
		}
		runHost(t, reloadLoopSetup, `
await flush()
for (let i = 0; i < 8; i++) await replaceAt(appended[i], 4000)
return { Boots: count("nap.boot"), Resets: count("nap.reset"), Appended: appended.length, Live: live().length, Body: document.body.textContent }
`, &got)
		if got.Boots != 9 || got.Resets != 8 || got.Appended != 9 || got.Live != 1 || got.Body != "" {
			t.Errorf("eight replacements 4 s apart: nap.boot %d, nap.reset %d, frames %d, live %d, body %q; want 9, 8, 9, 1, empty",
				got.Boots, got.Resets, got.Appended, got.Live, got.Body)
		}
	})

	t.Run("a dev reload clears the halt and the history", func(t *testing.T) {
		var got struct {
			Halted                     bool
			BootsReload, AppendedAfter int
			Boots, Appended, Live      int
			Body                       string
		}
		runHost(t, reloadLoopSetup, `
await flush()
for (let i = 0; i < 4; i++) await replaceAt(appended[i], 500)
const halted = document.body.textContent.includes("keeps reloading itself")
const bootsBefore = count("nap.boot"), appendedBefore = appended.length
window.__nap_reload()
await flush(6)
const bootsReload = count("nap.boot") - bootsBefore
const appendedAfter = appended.length - appendedBefore
// three quick replacements right after still rebuild
for (let i = 0; i < 3; i++) await replaceAt(appended[appended.length - 1], 500)
return {
  Halted: halted, BootsReload: bootsReload, AppendedAfter: appendedAfter,
  Boots: count("nap.boot") - bootsBefore, Appended: appended.length - appendedBefore,
  Live: live().length, Body: document.body.textContent,
}
`, &got)
		if !got.Halted {
			t.Fatal("the loop did not halt before the dev reload")
		}
		if got.BootsReload != 1 || got.AppendedAfter != 1 {
			t.Errorf("dev reload after a halt: nap.boot %d, frames %d; want 1, 1", got.BootsReload, got.AppendedAfter)
		}
		if got.Boots != 4 || got.Appended != 4 || got.Live != 1 {
			t.Errorf("three quick replacements after the dev reload: nap.boot %d, frames %d, live %d; want 4, 4, 1",
				got.Boots, got.Appended, got.Live)
		}
		if strings.Contains(got.Body, "keeps reloading itself") {
			t.Errorf("the halt text survived the dev reload: %q", got.Body)
		}
	})

	t.Run("an overtaken boot answering late appends nothing", func(t *testing.T) {
		var got struct {
			Halted                   bool
			LateAppended, LateStarts int
		}
		runHost(t, reloadLoopSetup, `
await flush()
fireLoad(appended[0])
loadedOnce.add(appended[0])
await flush()
// a dev reload whose nap.boot answer is still out when the loop halts
hold("nap.boot")
window.__nap_reload()
await flush()
const late = held["nap.boot"].shift()
unhold("nap.boot")
for (let i = 0; i < 4; i++) await replaceAt(appended[i], 500)
const halted = document.body.textContent.includes("keeps reloading itself")
const appendedBefore = appended.length, startsBefore = count("nap.start")
late.resolve(JSON.stringify({ srcdoc: "late", title: "late" }))
await flush(6)
return { Halted: halted, LateAppended: appended.length - appendedBefore, LateStarts: count("nap.start") - startsBefore }
`, &got)
		if !got.Halted {
			t.Fatal("the loop did not halt")
		}
		if got.LateAppended != 0 || got.LateStarts != 0 {
			t.Errorf("a late nap.boot answer after the halt: %d frames, %d nap.start; want 0, 0", got.LateAppended, got.LateStarts)
		}
	})
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
