package backend

import (
	"strconv"
	"strings"
	"testing"
)

// TestHandleWireMessageDropsOverlong pins Android's inbound cap: a message
// one byte over MaxInboundWireMsg never reaches the window (the prompt it
// answers stays pending), while one of exactly the cap is handled.
func TestHandleWireMessageDropsOverlong(t *testing.T) {
	setupNapTest(t)
	ci, _ := openNapplet(t, "overlong")

	p := newPrompt("", "test prompt", "", "", nil)
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
