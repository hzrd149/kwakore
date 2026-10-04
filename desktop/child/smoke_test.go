//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	nappbridge "verdana/backend/webview"
)

// ─── fake launcher ──────────────────────────────────────────────
//
// fakeLauncher stands where the launcher stands for a real napplet window:
// it starts the child binary (buildChild) under the installed WebKitGTK and
// speaks the wire protocol on its stdin and stdout. It answers every rpc, so
// no host-page promise ever hangs, serves NAP storage from memory, and keeps
// an ordered log of what reached it, which the tests assert on:
//
//	rpc:<method>          every rpc but nap.msg (rpc:nap.boot, rpc:nap.start, ...)
//	msg:<type>:<key>      every NAP envelope, with its storage key if any
//	exit                  the child process ended
//
// GTK must own the process's main thread (Pitfall 8), which is why the child
// runs as a subprocess and nothing here calls webview.New.
type fakeLauncher struct {
	t    *testing.T
	html []byte
	cmd  *exec.Cmd

	inMu  sync.Mutex
	stdin io.WriteCloser

	stderr lockedBuffer

	mu      sync.Mutex
	changed chan struct{} // closed and replaced on every change below
	events  []string
	gen     int
	live    bool // a session is started and not reset
	store   map[string]string
	exited  bool
	exitErr error
}

// lockedBuffer is a bytes.Buffer the child's stderr copier and the test can
// share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// storeKey is where a storage key lives in the fake launcher's map: shared
// storage under "shared/", per-instance storage under "instance/".
func storeKey(scope, key string) string {
	if scope != "instance" {
		scope = "shared"
	}
	return scope + "/" + key
}

// newFakeLauncher prepares a launcher for one napplet window; seed storage
// before start.
func newFakeLauncher(t *testing.T, html []byte) *fakeLauncher {
	return &fakeLauncher{t: t, html: html, changed: make(chan struct{}), store: map[string]string{}}
}

// seed puts a value in storage before the napplet runs.
func (f *fakeLauncher) seed(scope, key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[storeKey(scope, key)] = value
}

// start runs the child built at bin as a napplet window. The process is
// killed at cleanup if the test did not close it, and a failed test gets the
// event log and the child's log.
func (f *fakeLauncher) start(bin string, extra ...string) {
	t := f.t
	t.Helper()
	env := append([]string{"VERDANA_WINDOW_KIND=", "VERDANA_NAPP_FORMAT=napplet", "NO_AT_BRIDGE=1"}, extra...)
	cmd := exec.Command(bin)
	cmd.Env = childEnv(filepath.Dir(bin), env...)
	cmd.Stderr = &f.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}
	f.cmd, f.stdin = cmd, stdin

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		f.read(stdout)
	}()
	go func() {
		<-readerDone
		err := cmd.Wait()
		f.mu.Lock()
		f.exited, f.exitErr = true, err
		f.events = append(f.events, "exit")
		f.bump()
		f.mu.Unlock()
	}()

	t.Cleanup(func() {
		if !f.hasExited() {
			_ = cmd.Process.Kill()
			f.waitFor(5*time.Second, f.hasExited)
		}
		if t.Failed() {
			t.Logf("launcher events:\n  %s", strings.Join(f.log(), "\n  "))
			t.Logf("child log:\n%s", f.childLog())
		}
	})
}

// bump wakes every waitFor; f.mu must be held.
func (f *fakeLauncher) bump() {
	close(f.changed)
	f.changed = make(chan struct{})
}

func (f *fakeLauncher) record(event string) {
	f.mu.Lock()
	f.events = append(f.events, event)
	f.bump()
	f.mu.Unlock()
}

// log is a copy of the event log so far.
func (f *fakeLauncher) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.events)
}

// count is how many times event is in the log.
func (f *fakeLauncher) count(event string) int {
	return f.countIn(f.log(), event)
}

// countIn is how many times event is in events.
func (f *fakeLauncher) countIn(events []string, event string) int {
	n := 0
	for _, e := range events {
		if e == event {
			n++
		}
	}
	return n
}

// stored reads storage the way the napplet left it.
func (f *fakeLauncher) stored(scope, key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.store[storeKey(scope, key)]
	return v, ok
}

func (f *fakeLauncher) hasExited() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exited
}

// childLog is the child's stderr so far, colors stripped.
func (f *fakeLauncher) childLog() string {
	return ansiEscape.ReplaceAllString(f.stderr.String(), "")
}

// waitFor waits until cond holds or timeout passes, and says which.
func (f *fakeLauncher) waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		f.mu.Lock()
		ch := f.changed
		f.mu.Unlock()
		if cond() {
			return true
		}
		select {
		case <-ch:
		case <-deadline.C:
			return cond()
		}
	}
}

// send writes one line to the child.
func (f *fakeLauncher) send(m wireMsg) {
	line, err := json.Marshal(m)
	if err != nil {
		f.t.Errorf("encoding a wire message: %v", err)
		return
	}
	f.inMu.Lock()
	defer f.inMu.Unlock()
	_, _ = f.stdin.Write(append(line, '\n'))
}

// close asks the window to close, as the launcher does, and waits for the
// child to exit with status 0; past the deadline it kills it and fails.
func (f *fakeLauncher) close(timeout time.Duration) {
	t := f.t
	t.Helper()
	f.send(wireMsg{T: "close"})
	if !f.waitFor(timeout, f.hasExited) {
		_ = f.cmd.Process.Kill()
		t.Fatalf("the child did not exit within %s of close", timeout)
	}
	f.mu.Lock()
	err := f.exitErr
	f.mu.Unlock()
	if err != nil {
		t.Fatalf("the child exited with %v after close", err)
	}
}

// read handles the child's stdout, one JSON line at a time, with no line
// limit (a napplet may send large envelopes).
func (f *fakeLauncher) read(stdout io.Reader) {
	r := bufio.NewReader(stdout)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var m wireMsg
			if jerr := json.Unmarshal(line, &m); jerr != nil {
				f.t.Errorf("unreadable line from the child: %v: %q", jerr, line)
			} else if m.T == "rpc" {
				f.rpc(m)
			} else {
				f.record(m.T)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				f.t.Logf("reading the child's stdout: %v", err)
			}
			return
		}
	}
}

// rpc answers one rpc from the host page as the launcher would.
func (f *fakeLauncher) rpc(m wireMsg) {
	var result any
	switch m.Method {
	case "nap.boot":
		f.record("rpc:nap.boot")
		// rebuilt on every call, as the launcher verifies and rebuilds it
		doc, err := nappbridge.NappletSrcdoc(f.html, []string{"storage"})
		if err != nil {
			f.t.Errorf("NappletSrcdoc: %v", err)
			f.send(wireMsg{T: "resp", ID: m.ID, Error: err.Error()})
			return
		}
		result = map[string]string{"srcdoc": doc, "title": "smoke"}
	case "nap.start":
		f.mu.Lock()
		f.gen++
		f.live = true
		gen := f.gen
		f.events = append(f.events, "rpc:nap.start")
		f.bump()
		f.mu.Unlock()
		result = map[string]int{"gen": gen}
	case "nap.reset":
		f.mu.Lock()
		f.live = false
		f.events = append(f.events, "rpc:nap.reset")
		f.bump()
		f.mu.Unlock()
	case "nap.msg":
		f.envelope(m.Params)
	default:
		f.record("rpc:" + m.Method)
	}

	resp := wireMsg{T: "resp", ID: m.ID}
	if result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			f.t.Errorf("encoding the %s result: %v", m.Method, err)
		}
		resp.Result = raw
	}
	f.send(resp)
}

// envelope records one NAP envelope and serves storage. The host page sends
// the envelope's JSON as a JSON string, so params decodes twice. Like the
// backend, an envelope that arrives between nap.reset and the next nap.start
// belongs to no session and gets no answer (it is still logged, so a test
// sees it).
func (f *fakeLauncher) envelope(params string) {
	var text string
	if err := json.Unmarshal([]byte(params), &text); err != nil {
		f.t.Errorf("nap.msg params are not a JSON string: %v: %q", err, params)
		return
	}
	var env struct {
		Type  string  `json:"type"`
		ID    any     `json:"id"`
		Key   string  `json:"key"`
		Value *string `json:"value"`
		Scope string  `json:"scope"`
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		f.t.Errorf("nap.msg envelope is not JSON: %v: %q", err, text)
		return
	}

	f.mu.Lock()
	f.events = append(f.events, "msg:"+env.Type+":"+env.Key)
	f.bump()
	live, gen := f.live, f.gen
	var reply map[string]any
	if live {
		switch env.Type {
		case "storage.get":
			reply = map[string]any{"type": "storage.get.result", "id": env.ID, "value": nil}
			if v, ok := f.store[storeKey(env.Scope, env.Key)]; ok {
				reply["value"] = v
			}
		case "storage.set":
			value := ""
			if env.Value != nil {
				value = *env.Value
			}
			f.store[storeKey(env.Scope, env.Key)] = value
			reply = map[string]any{"type": "storage.set.result", "id": env.ID}
		case "storage.remove":
			delete(f.store, storeKey(env.Scope, env.Key))
			reply = map[string]any{"type": "storage.remove.result", "id": env.ID}
		case "storage.keys":
			prefix := storeKey(env.Scope, "")
			keys := []string{}
			for k := range f.store {
				if rest, ok := strings.CutPrefix(k, prefix); ok {
					keys = append(keys, rest)
				}
			}
			slices.Sort(keys)
			reply = map[string]any{"type": "storage.keys.result", "id": env.ID, "keys": keys}
		}
	}
	f.mu.Unlock()

	if reply != nil {
		f.push(gen, reply)
	}
}

// push sends one envelope to the napplet as the launcher does: an eval of
// __nap_push tagged with the session it is for.
func (f *fakeLauncher) push(gen int, env map[string]any) {
	raw, err := json.Marshal(env)
	if err != nil {
		f.t.Errorf("encoding a push: %v", err)
		return
	}
	code := "window.__nap_push && window.__nap_push(" + jsonInt(gen) + ", " + jsString(string(raw)) + ")"
	f.send(wireMsg{T: "eval", Code: code})
}

func jsonInt(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

// indexOf is the position of the first event equal to want at or after
// from, or -1.
func indexOf(events []string, want string, from int) int {
	for i := from; i < len(events); i++ {
		if events[i] == want {
			return i
		}
	}
	return -1
}

// ─── tests ──────────────────────────────────────────────────────

// TestWebKitNappletBoots drives one real napplet window end to end the way
// the launcher does: the host page boots the napplet (nap.boot, nap.start,
// nap.loaded), the napplet's storage.set reaches the launcher, the answer
// pushed back with the session's gen reaches the frame (only then does the
// napplet send its second set), nothing rebuilds, and close ends the
// process cleanly.
func TestWebKitNappletBoots(t *testing.T) {
	needWebKit(t)
	bin := buildChild(t)

	html := []byte(`<!doctype html><html><head><meta charset="utf-8"><title>smoke</title></head><body><script>
;(() => {
  window.napplet.storage.instance.setItem("hello", "1")
    .then(() => window.napplet.storage.instance.setItem("hello2", "2"))
})()
</script></body></html>`)

	f := newFakeLauncher(t, html)
	f.start(bin)

	deadline := time.Now().Add(60 * time.Second)
	left := func() time.Duration { return time.Until(deadline) }

	if !f.waitFor(left(), func() bool { return f.count("msg:storage.set:hello2") > 0 || f.hasExited() }) {
		t.Fatal("the napplet's second storage.set never arrived: the first set's answer did not reach the frame")
	}
	if f.hasExited() {
		t.Fatal("the child exited before the napplet ran")
	}

	events := f.log()
	if len(events) < 2 || events[0] != "rpc:nap.boot" || events[1] != "rpc:nap.start" {
		t.Fatalf("the window did not start with nap.boot, nap.start: %v", events)
	}
	first := indexOf(events, "msg:storage.set:hello", 0)
	second := indexOf(events, "msg:storage.set:hello2", 0)
	if first < 0 || second < first {
		t.Fatalf("storage.set hello then hello2 expected in order: %v", events)
	}
	if v, _ := f.stored("instance", "hello2"); v != "2" {
		t.Errorf("instance storage hello2 = %q, want 2", v)
	}
	if !f.waitFor(left(), func() bool { return f.count("rpc:nap.loaded") > 0 }) {
		t.Fatalf("the host page never reported the frame's load: %v", f.log())
	}

	// a quiet napplet stays in its first session
	time.Sleep(3 * time.Second)
	if n := f.count("rpc:nap.start"); n != 1 {
		t.Errorf("%d nap.start, want 1: %v", n, f.log())
	}
	if n := f.count("rpc:nap.reset"); n != 0 {
		t.Errorf("%d nap.reset, want 0: %v", n, f.log())
	}

	f.close(15 * time.Second)
}

// ─── adversarial smoke ──────────────────────────────────────────

// attackerListener stands in for a host a napplet wants to reach: a
// loopback TCP listener that counts every connection it accepts (and keeps
// the first line each one sent, for diagnosis).
type attackerListener struct {
	ln    net.Listener
	mu    sync.Mutex
	lines []string
}

func newAttackerListener(t *testing.T) *attackerListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := &attackerListener{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			a.mu.Lock()
			i := len(a.lines)
			a.lines = append(a.lines, "(connected, nothing read yet)")
			a.mu.Unlock()
			go func() {
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				first, _ := bufio.NewReader(conn).ReadString('\n')
				a.mu.Lock()
				a.lines[i] = strings.TrimSpace(first)
				a.mu.Unlock()
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return a
}

// connections is every connection so far, by the first line it sent.
func (a *attackerListener) connections() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.lines)
}

// advResult is one line of the fixture's results (adv.results).
type advResult struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// TestWebKitNappletAdversarial runs the committed adversarial napplet
// (backend/testdata/adversarial-napplet) in the real child under WebKitGTK,
// with this fake launcher on the wire and a loopback listener as the
// attacker's host, and re-runs every escape the phase research measured as
// a regression test (D-14, D-16):
//
//   - no navigation, fetch, image, preconnect, prefetch, beacon, form, font
//     or CSS load ever connects to the attacker (SBOX-02, SBOX-04)
//   - every replaced document is a nap.reset followed by nap.start before
//     any envelope, and the load-delayed reloaded document's envelope
//     (adv.leak) never reaches the launcher (SBOX-01, D-18)
//   - the first boot is exactly one load before any reset (Pitfall 5)
//   - the reload loop ends after exactly 4 resets and 3 starts, then the
//     window stays up and boots nothing more (D-03)
//   - the forged binding calls reached the bindings and were refused: the
//     child logs a refusal (whichever call won the sampled Warn) and the
//     launcher never gets nap.openSettings (D-15)
//   - RTCPeerConnection and navigator.mediaDevices are undefined in the
//     frame (D-09, D-20), and the fixture reports no FAIL at all
//   - the marker-less replacements (nav-js, doc-open-unclosed) ran, and
//     the report each replacing document posts before its (held back) load
//     reached the launcher: that is the recorded residual (CONFORMANCE
//     NIP-5D-reload-residual), reproduced on purpose so the policy the
//     report shows (eval and WebSocket refused) is checked, and the
//     attacker must see nothing from them
func TestWebKitNappletAdversarial(t *testing.T) {
	needWebKit(t)
	html, err := os.ReadFile(filepath.Join("..", "..", "backend", "testdata", "adversarial-napplet", "index.html"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	bin := buildChild(t)
	attacker := newAttackerListener(t)
	target := "http://" + attacker.ln.Addr().String() + "/"

	f := newFakeLauncher(t, html)
	f.seed("instance", "adv.target", target)
	f.seed("instance", "adv.auto", "1")
	f.seed("instance", "adv.pause", "3500")
	f.start(bin)
	deadline := time.Now().Add(180 * time.Second)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("attacker connections: %q", attacker.connections())
		}
	})

	// the step machine, through every step to its verdict
	stored := func(key string) string {
		v, _ := f.stored("instance", key)
		return v
	}
	done := f.waitFor(min(150*time.Second, time.Until(deadline)), func() bool {
		return stored("adv.done") == "1" || f.hasExited()
	})
	var results []advResult
	if raw := stored("adv.results"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &results); err != nil {
			t.Errorf("adv.results is not JSON: %v: %q", err, raw)
		}
	}
	for _, r := range results {
		t.Logf("%s %s: %s", r.Status, r.Step, r.Detail)
	}
	if !done || f.hasExited() {
		t.Fatalf("the fixture did not reach its verdict (adv.done %q, adv.step %q, exited %v)",
			stored("adv.done"), stored("adv.step"), f.hasExited())
	}
	resetsAtDone, startsAtDone := f.count("rpc:nap.reset"), f.count("rpc:nap.start")

	for _, r := range results {
		if r.Status == "FAIL" {
			t.Errorf("the fixture failed a check: %s: %s", r.Step, r.Detail)
		}
	}
	passed := func(step, detail string) bool {
		for _, r := range results {
			if r.Status == "PASS" && r.Step == step && strings.Contains(r.Detail, detail) {
				return true
			}
		}
		return false
	}
	for _, want := range []struct{ step, detail string }{
		{"scope", ""},
		{"parent", ""},
		{"network", "RTCPeerConnection is undefined"},
		{"network", "navigator.mediaDevices is undefined"},
		{"nav-http", "replaced by a fresh document"},
		{"nav-meta", "replaced by a fresh document"},
		{"nav-anchor", "replaced by a fresh document"},
		{"nav-blank", "replaced by a fresh document"},
		{"doc-open", "replaced by a fresh document"},
		{"reload", "replaced by a fresh document"},
		{"reload-delayed", "replaced by a fresh document"},
		{"verdict", "no envelope from a replaced document"},
	} {
		if !passed(want.step, want.detail) {
			t.Errorf("no PASS for %s %q", want.step, want.detail)
		}
	}
	// data: and blob: navigations: WebKitGTK 2.52 fires a load and the
	// frame is rebuilt, an engine may also refuse them and keep the
	// document; either is a PASS, no result at all is not. Their documents
	// hold back their load, so a post they make before it is tested too
	// (adv.leak, below)
	for _, step := range []string{"nav-data", "nav-blob"} {
		if !passed(step, "replaced by a fresh document") && !passed(step, "refused by the engine") {
			t.Errorf("no PASS for %s (replaced by a fresh document, or refused by the engine)", step)
		}
	}

	// the recorded residual: these ran, and their documents reported that
	// the inherited policy held. The report must be there: the probe holds
	// back its document's load, so the post-load javascript: document's
	// report reaches the launcher before that load rebuilds the frame (WR-04,
	// iteration 2: without the hold it never did, and its policy went
	// unchecked), and the unclosed document.open() fires no load at all.
	for _, step := range []string{"nav-js", "doc-open-unclosed"} {
		ran := false
		for _, r := range results {
			ran = ran || r.Step == step
		}
		if !ran {
			t.Errorf("the residual step %s left no result", step)
		}
		if !checkResidualReport(t, f, step) {
			t.Errorf("residual %s: the replacing document's report never reached the launcher, so its policy was not "+
				"checked; if the engine now rebuilds before the report, the residual may be closed: update NIP-5D-reload-residual", step)
		}
	}

	// the reload loop: idle 10.5 s, then three rebuilds and a stop
	if !f.waitFor(min(25*time.Second, time.Until(deadline)), func() bool {
		return f.count("rpc:nap.reset") >= resetsAtDone+4 || f.hasExited()
	}) {
		t.Errorf("the reload loop never reached its fourth replacement: %d resets, %d starts since the verdict",
			f.count("rpc:nap.reset")-resetsAtDone, f.count("rpc:nap.start")-startsAtDone)
	}
	boots := f.count("rpc:nap.boot")
	time.Sleep(5 * time.Second)
	if f.hasExited() {
		t.Fatal("the child exited during the reload loop")
	}
	if n := f.count("rpc:nap.boot"); n != boots {
		t.Errorf("%d more nap.boot after the loop was stopped", n-boots)
	}
	if n := f.count("rpc:nap.reset") - resetsAtDone; n != 4 {
		t.Errorf("the loop added %d nap.reset, want 4", n)
	}
	if n := f.count("rpc:nap.start") - startsAtDone; n != 3 {
		t.Errorf("the loop added %d nap.start, want 3", n)
	}

	events := f.log()
	// Pitfall 5: the initial srcdoc load is one load, never a replacement
	firstStart := indexOf(events, "rpc:nap.start", 0)
	firstReset := indexOf(events, "rpc:nap.reset", 0)
	if firstStart < 0 || firstReset < firstStart {
		t.Errorf("no nap.start before the first nap.reset: %v", events)
	} else if n := f.countIn(events[firstStart:firstReset], "rpc:nap.loaded"); n != 1 {
		t.Errorf("%d nap.loaded between the first nap.start and the first nap.reset, want 1", n)
	}
	// every replaced document ends its session before anything else of
	// the next one reaches the launcher
	for i, e := range events {
		if e != "rpc:nap.reset" {
			continue
		}
		start := indexOf(events, "rpc:nap.start", i+1)
		for j := i + 1; j < len(events); j++ {
			if strings.HasPrefix(events[j], "msg:") {
				if start < 0 || start > j {
					t.Errorf("%s at %d reached the launcher after the nap.reset at %d and before any nap.start", events[j], j, i)
				}
				break
			}
		}
	}
	if n := f.countIn(events, "msg:storage.set:adv.leak"); n != 0 {
		t.Errorf("a replaced document's adv.leak reached the launcher %d times", n)
	}

	// the forged binding calls reached the bindings and were refused
	if n := f.countIn(events, "rpc:nap.openSettings"); n != 0 {
		t.Errorf("the forged nap.openSettings reached the launcher %d times", n)
	}
	// The fixture forges the rpc and then the prompt answer, but go-webview
	// runs every binding call on its own goroutine, so either may be the
	// first to reach the 5 s sampled Warn, and the other is then counted
	// as suppressed. Either line is a refusal; the guarantee itself is the
	// nap.openSettings count above.
	refused := false
	for _, line := range strings.Split(f.childLog(), "\n") {
		if !strings.Contains(line, "without the window token") {
			continue
		}
		if strings.Contains(line, "nap.openSettings") || strings.Contains(line, "prompt answer") {
			refused = true
		}
	}
	if !refused {
		t.Errorf("the child never logged refusing a forged binding call (the nap.openSettings rpc or the prompt answer):\n%s", f.childLog())
	}

	if conns := attacker.connections(); len(conns) != 0 {
		t.Errorf("the attacker host got %d connections: %q", len(conns), conns)
	}

	f.close(15 * time.Second)
	if conns := attacker.connections(); len(conns) != 0 && !t.Failed() {
		t.Errorf("the attacker host got %d connections by the end: %q", len(conns), conns)
	}
}

// residualReport is what a document the napplet made for itself (the
// fixture's residualProbe) says about the policy it runs under.
type residualReport struct {
	Napplet   string `json:"napplet"`
	Eval      string `json:"eval"`
	WebSocket string `json:"websocket"`
}

// checkResidualReport asserts that the report step's replacing document
// left in instance storage, if it got one there, shows the inherited policy
// held: eval and WebSocket refused. It returns whether there was one, and
// the callers fail when there was not. Its arriving at all is the recorded
// residual (NIP-5D-reload-residual).
func checkResidualReport(t *testing.T, f *fakeLauncher, step string) bool {
	t.Helper()
	raw, ok := f.stored("instance", "adv.residual."+step)
	if !ok {
		t.Logf("residual %s: no report from the replacing document reached the launcher", step)
		return false
	}
	var r residualReport
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Errorf("residual %s: report is not JSON: %v: %q", step, err, raw)
		return true
	}
	t.Logf("residual %s: the replacing document's envelope reached the live session (window.napplet %s, eval %s, WebSocket %s)",
		step, r.Napplet, r.Eval, r.WebSocket)
	if r.Eval != "refused" || r.WebSocket != "refused" {
		t.Errorf("residual %s: the replacing document is not under the napplet policy: eval %s, WebSocket %s", step, r.Eval, r.WebSocket)
	}
	return true
}

// TestWebKitNappletJavascriptBeforeLoad pins the half of the recorded
// residual (CONFORMANCE NIP-5D-reload-residual) the adversarial step
// machine cannot reach: the napplet's first script replaces its document
// through a javascript: URL before the frame's first load. That document
// posts no marker and its load is the frame's first, so today the host page
// takes it for the boot, never rebuilds, and its envelope reaches the live
// session without window.napplet. None of that is asserted, only logged.
// What is asserted is containment: the report must arrive (nothing races it
// here), must show the napplet policy inherited (eval and WebSocket
// refused), and its fetch, image, preconnect and beacon must never reach
// the attacker's listener.
func TestWebKitNappletJavascriptBeforeLoad(t *testing.T) {
	needWebKit(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "backend", "testdata", "adversarial-napplet", "index.html"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	bin := buildChild(t)
	attacker := newAttackerListener(t)
	target := "http://" + attacker.ln.Addr().String() + "/"

	// the early mode is chosen on <html>, which NappletSrcdoc merges onto
	// its own <html>: the fixture's script reads it before any envelope
	html := strings.Replace(string(fixture), "<html>",
		`<html data-adv-mode="nav-js-early" data-adv-target="`+target+`">`, 1)
	if html == string(fixture) {
		t.Fatal("the fixture has no <html> tag to put the early mode on")
	}

	f := newFakeLauncher(t, []byte(html))
	f.start(bin)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("attacker connections: %q", attacker.connections())
		}
	})

	if !f.waitFor(30*time.Second, func() bool {
		return f.count("msg:storage.set:adv.residual.nav-js-early") > 0 || f.hasExited()
	}) || f.hasExited() {
		t.Fatalf("the replacing document's report never reached the launcher (exited %v); if the engine now "+
			"stops this, the residual may be closed: update NIP-5D-reload-residual", f.hasExited())
	}
	if !checkResidualReport(t, f, "nav-js-early") {
		t.Error("the report reached the launcher outside a live session")
	}

	// the network attempts follow the report; give them time to land
	time.Sleep(4 * time.Second)
	if f.hasExited() {
		t.Fatal("the child exited")
	}
	t.Logf("observed: %d nap.start, %d nap.loaded, %d nap.reset (today the replacing document keeps the session)",
		f.count("rpc:nap.start"), f.count("rpc:nap.loaded"), f.count("rpc:nap.reset"))
	if conns := attacker.connections(); len(conns) != 0 {
		t.Errorf("the attacker host got %d connections: %q", len(conns), conns)
	}

	f.close(15 * time.Second)
	if conns := attacker.connections(); len(conns) != 0 && !t.Failed() {
		t.Errorf("the attacker host got %d connections by the end: %q", len(conns), conns)
	}
}
