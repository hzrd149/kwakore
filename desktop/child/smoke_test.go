//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	n := 0
	for _, e := range f.log() {
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
