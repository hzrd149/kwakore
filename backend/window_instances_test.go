package backend

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestHandleWireMessageDropsOverlong pins Android's inbound cap: a message
// one byte over MaxInboundWireMsg never reaches the window (the prompt it
// answers stays pending), while one of exactly the cap is handled.
func TestHandleWireMessageDropsOverlong(t *testing.T) {
	setupNapTest(t)
	ci, _ := openNapplet(t, "overlong")

	// the window answers its own prompt (a window may answer no other)
	p := newPrompt("", "test prompt", "", "", nil)
	p.Instance = ci.instance
	enqueuePrompt(p)
	t.Cleanup(func() { AnswerPrompt(p.ID, Answer{}) })

	msg := `{"t":"promptAnswer","id":` + strconv.Itoa(p.ID) + `,"params":"{\"ok\":true}"}`
	// whitespace after the object is still valid JSON, so only the length
	// decides
	pad := func(n int) string { return msg + strings.Repeat(" ", n-len(msg)) }

	over := pad(MaxInboundWireMsg + 1)
	if len(over) != MaxInboundWireMsg+1 {
		t.Fatalf("over-cap message is %d bytes", len(over))
	}
	HandleWireMessage(ci.instance, over)
	if cur := CurrentPrompt(); cur == nil || cur.ID != p.ID {
		t.Fatalf("a message of MaxInboundWireMsg+1 bytes answered the prompt; it must be dropped before parsing")
	}

	at := pad(MaxInboundWireMsg)
	if len(at) != MaxInboundWireMsg {
		t.Fatalf("at-cap message is %d bytes", len(at))
	}
	HandleWireMessage(ci.instance, at)
	if cur := CurrentPrompt(); cur != nil && cur.ID == p.ID {
		t.Fatal("a message of exactly MaxInboundWireMsg bytes was dropped; it must be handled")
	}
	select {
	case a := <-p.resp:
		if !a.OK {
			t.Fatalf("prompt answered %+v, want ok", a)
		}
	default:
		t.Fatal("the at-cap promptAnswer did not reach the prompt")
	}
}

// TestPromptAnswerOnlyFromOwner: a window's promptAnswer settles only a prompt
// shown over that window. Another window's prompt and the launcher's own
// prompts (Instance "") stay pending whatever a window sends up, and prompt
// ids are not a serial a window could walk (CR-01).
func TestPromptAnswerOnlyFromOwner(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	owner, _ := openNapplet(t, "prompt-owner")
	other, _ := openNapplet(t, "prompt-other")

	launcher := newPrompt("", "install something", "", "", nil)
	enqueuePrompt(launcher)
	mine := newPrompt("", "publish something", "", "", nil)
	mine.Instance = owner.instance
	enqueuePrompt(mine)

	answer := func(ci *Instance, p *Prompt) {
		HandleMessage(ci.instance, WireMsg{T: "promptAnswer", ID: p.ID, Params: `{"ok":true,"scope":"always"}`})
	}
	pending := func(p *Prompt) bool {
		promptMu.Lock()
		defer promptMu.Unlock()
		return findPromptLocked(p.ID) == p
	}

	// no window answers the launcher's prompt, not even one with a prompt of
	// its own pending
	answer(owner, launcher)
	answer(other, launcher)
	if !pending(launcher) {
		t.Fatal("a window answered the launcher's prompt")
	}
	// another window cannot answer this window's prompt
	answer(other, mine)
	if !pending(mine) {
		t.Fatal("a window answered a prompt shown over another window")
	}
	select {
	case a := <-mine.resp:
		t.Fatalf("a forged answer reached the asker: %+v", a)
	default:
	}
	// the owner can
	answer(owner, mine)
	if pending(mine) {
		t.Fatal("the owning window could not answer its own prompt")
	}
	if a := <-mine.resp; !a.OK {
		t.Fatalf("owner's answer %+v, want ok", a)
	}
	// the launcher's UI still answers its own prompt directly
	AnswerPrompt(launcher.ID, Answer{OK: false})
	if pending(launcher) {
		t.Fatal("the launcher could not answer its own prompt")
	}

	// ids are random, JS-safe and distinct, not a serial (compared as int64
	// so the bound compiles where int is 32-bit)
	a, b := newPromptID(), newPromptID()
	if a <= 0 || b <= 0 || int64(a) >= 1<<53 || int64(b) >= 1<<53 || a == b || b == a+1 {
		t.Fatalf("prompt ids %d, %d", a, b)
	}
	// the mask fits this platform's int, so no draw truncates to a negative
	// or out-of-range id (half of them did on 32-bit before the mask did)
	if promptIDMask > uint64(math.MaxInt) || promptIDMask >= 1<<53 {
		t.Fatalf("prompt id mask %#x does not fit int and 2^53", promptIDMask)
	}
	for range 1000 {
		if id := newPromptID(); id <= 0 || int64(id) >= 1<<53 {
			t.Fatalf("prompt id %d out of range", id)
		}
	}
}

// respTransport records the rpc answers a window is sent.
type respTransport struct {
	mu    sync.Mutex
	resps []WireMsg
}

func (r *respTransport) Send(m WireMsg) {
	if m.T != "resp" {
		return
	}
	r.mu.Lock()
	r.resps = append(r.resps, m)
	r.mu.Unlock()
}
func (r *respTransport) Focus() {}
func (r *respTransport) Close() {}

// TestHandleWireMessageAnswersOversizedRPC: on Android an rpc over
// MaxInboundWireMsg is not parsed, but it is answered "too-large" by the id
// at its start, so the host page's ordered lane is not blocked behind a
// request that never settles (WR-04). Anything whose id cannot be read
// cheaply, or that is not an rpc, is still dropped.
func TestHandleWireMessageAnswersOversizedRPC(t *testing.T) {
	setupNapTest(t)
	ci, _ := openNapplet(t, "oversized-rpc")
	rt := &respTransport{}
	ci.attach(rt)

	big := strings.Repeat("x", MaxInboundWireMsg)
	HandleWireMessage(ci.instance, `{"t":"rpc","id":7,"method":"nap.msg","params":"`+big+`"}`)
	// the params ahead of the id: nothing cheap to answer by
	HandleWireMessage(ci.instance, `{"t":"rpc","params":"`+big+`","id":8}`)
	// not an rpc
	HandleWireMessage(ci.instance, `{"t":"promptAnswer","id":9,"params":"`+big+`"}`)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.resps) != 1 || rt.resps[0].ID != 7 || rt.resps[0].Error != "too-large" {
		t.Fatalf("answers %+v, want one too-large for id 7", rt.resps)
	}
}

// TestWindowFailedRaisesNotice: a napplet window that closes itself because
// its engine hardening failed says so on the wire, with the exact line the
// desktop child writes (desktop/child TestReportWindowFailed), and the
// launcher shows its own error notice for it (IN-06). An unknown code, or a
// message for a window that is not open, shows nothing.
func TestWindowFailedRaisesNotice(t *testing.T) {
	setupNapTest(t)
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")
	ci, _ := openNapplet(t, "hardening-failed")

	HandleWireMessage(ci.instance, `{"t":"windowFailed","code":"something-else"}`)
	HandleWireMessage("no-such-window", `{"t":"windowFailed","code":"engine-hardening"}`)
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("notices after an unknown code and an unknown window = %v", noticeIDs(got))
	}

	HandleWireMessage(ci.instance, `{"t":"windowFailed","code":"engine-hardening"}`)
	HandleWireMessage(ci.instance, `{"t":"windowFailed","code":"engine-hardening"}`)
	got := Snapshot().Notices
	if ids := noticeIDs(got); !slices.Equal(ids, []string{"napplet-hardening"}) {
		t.Fatalf("notices = %v, want one napplet-hardening", ids)
	}
	if n := got[0]; n.Kind != "error" || n.Title != nappletHardeningTitle || n.Detail != nappletHardeningDetail || n.Path != "" {
		t.Fatalf("napplet-hardening notice = %+v", n)
	}

	// errors come first, child-unavailable ahead of this one
	setKeyringFallbackNotice(true)
	raiseChildUnavailable()
	if ids := noticeIDs(Snapshot().Notices); !slices.Equal(ids, []string{"child-unavailable", "napplet-hardening", "keyring-fallback"}) {
		t.Fatalf("notice order = %v", ids)
	}

	// session-only, like child-unavailable: a dismissal is not remembered
	DismissNotice("napplet-hardening")
	stateMu.Lock()
	persisted := slices.Contains(state.DismissedNotices, "napplet-hardening")
	stateMu.Unlock()
	if persisted {
		t.Fatal("napplet-hardening dismissal was persisted")
	}
}

// TestWindowFailedClosesQuietly: a trial napplet window that failed closed
// gets no "Did you like it? Install it" prompt when it closes, and is not
// kept listed for reopening; a trial that ran and closed normally still gets
// the prompt (IN-09).
func TestWindowFailedClosesQuietly(t *testing.T) {
	setupNapTest(t)
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")
	resetPrompts := func() {
		promptMu.Lock()
		promptActive = nil
		promptQueue = nil
		promptMu.Unlock()
	}
	resetPrompts()
	t.Cleanup(resetPrompts)

	trial := func(d string) *Instance {
		ci, _ := openNapplet(t, d)
		ci.trial = true
		ci.trialStorage = make(map[string]*nappStorage)
		putWindow(windowRecord{Instance: ci.instance, NappID: ci.napp.ID})
		t.Cleanup(func() { windows.Delete(ci.instance) })
		return ci
	}
	failed, ran := trial("failed-trial"), trial("ran-trial")

	HandleWireMessage(failed.instance, `{"t":"windowFailed","code":"engine-hardening"}`)
	WindowClosed(failed.instance)
	WindowClosed(ran.instance)

	var p *Prompt
	deadline := time.Now().Add(time.Second)
	for p == nil && time.Now().Before(deadline) {
		p = CurrentPrompt()
		time.Sleep(time.Millisecond)
	}
	if p == nil || p.Title != "Did you like ran-trial?" {
		t.Fatalf("prompt for the trial that ran = %+v", p)
	}
	// give a stray prompt for the failed trial time to show up
	time.Sleep(50 * time.Millisecond)
	if got := pendingPrompts(); len(got) != 1 {
		titles := []string{}
		for _, q := range got {
			titles = append(titles, q.Title)
		}
		t.Fatalf("pending prompts = %q, want only the one for the trial that ran", titles)
	}
	if _, ok := windows.Load(failed.instance); ok {
		t.Fatal("a napplet window that failed closed is still listed for reopening")
	}
	if _, ok := windows.Load(ran.instance); !ok {
		t.Fatal("the trial that ran lost its window record before the user answered")
	}
	AnswerPrompt(p.ID, Answer{OK: false, Scope: ScopeOnce})
}

// TestWindowFailedOnlyFromNapplets: windowFailed is a napplet window's line
// to send. A napp (35130) window's page writes its own wire JSON on Android
// (NappWebView.kt __verdanaHost), so from a non-napplet window the message
// raises no notice and logs nothing past a sampled Debug line (WR-06).
func TestWindowFailedOnlyFromNapplets(t *testing.T) {
	setupNapTest(t)
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")
	logged := captureLog(t)
	saved := windowFailedBurst
	windowFailedBurst = &zerolog.BurstSampler{Burst: 3, Period: time.Hour}
	t.Cleanup(func() { windowFailedBurst = saved })

	ci := &Instance{
		instance:   "napp-page-" + randomID()[:6],
		napp:       Napp{ID: "napp~0123456789abcdef~forger", D: "forger", Name: "forger", Kind: KindNapp},
		subs:       map[int]context.CancelFunc{},
		actions:    map[string]int{},
		changed:    make(chan struct{}),
		dispatches: map[int]chan WireMsg{},
		gone:       make(chan struct{}),
	}
	registerInstance(ci)
	t.Cleanup(func() { WindowClosed(ci.instance) })

	for range 20 {
		HandleWireMessage(ci.instance, `{"t":"windowFailed","code":"engine-hardening"}`)
	}
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("a napp window raised notices: %v", noticeIDs(got))
	}
	out := logged.String()
	if strings.Contains(out, `"level":"error"`) || strings.Contains(out, "hardening failed") {
		t.Fatalf("a napp window's windowFailed was logged as a hardening failure:\n%s", out)
	}
	if n := strings.Count(out, "ignoring windowFailed from a non-napplet window"); n != 3 {
		t.Fatalf("dropped windowFailed logged %d times, want the burst of 3:\n%s", n, out)
	}
}

// TestWindowFailedUnknownCodeLog: an unknown code from a napplet window is
// logged sampled, cut to maxLoggedWireCode bytes and stripped of control
// characters, with its real length beside it (WR-06).
func TestWindowFailedUnknownCodeLog(t *testing.T) {
	setupNapTest(t)
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")
	logged := captureLog(t)
	saved := windowFailedBurst
	windowFailedBurst = &zerolog.BurstSampler{Burst: 2, Period: time.Hour}
	t.Cleanup(func() { windowFailedBurst = saved })
	ci, _ := openNapplet(t, "unknown-code")

	code := "\x1b[31mspoof\n" + strings.Repeat("x", 1<<20)
	msg, err := json.Marshal(WireMsg{T: "windowFailed", Code: code})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		HandleWireMessage(ci.instance, string(msg))
	}
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("an unknown code raised notices: %v", noticeIDs(got))
	}
	out := logged.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("unknown code logged %d lines, want the burst of 2:\n%.2000s", len(lines), out)
	}
	for _, line := range lines {
		var entry struct {
			Code    string `json:"code"`
			CodeLen int    `json:"code_len"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		want := "?[31mspoof?" + strings.Repeat("x", maxLoggedWireCode-11)
		if entry.Code != want {
			t.Fatalf("logged code = %q, want %q", entry.Code, want)
		}
		if entry.CodeLen != 1<<20+11 {
			t.Fatalf("logged code_len = %d, want %d", entry.CodeLen, 1<<20+11)
		}
	}
	if len(out) > 4096 {
		t.Fatalf("unknown-code log is %d bytes", len(out))
	}
}
