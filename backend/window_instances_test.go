package backend

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
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
