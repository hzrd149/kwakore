package backend

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
)

// openBridgeNapp registers a (non-napplet) napp window, which speaks the
// window.nostr bridge, with a transport that records its rpc answers.
func openBridgeNapp(t *testing.T, d string) (*Instance, *respTransport) {
	t.Helper()
	ci := &Instance{
		instance:   d + "-" + randomID()[:6],
		napp:       Napp{ID: "napp-" + d, D: d, Name: d},
		subs:       map[int]context.CancelFunc{},
		actions:    map[string]int{},
		changed:    make(chan struct{}),
		dispatches: map[int]chan WireMsg{},
		gone:       make(chan struct{}),
	}
	registerInstance(ci)
	rt := &respTransport{}
	ci.attach(rt)
	t.Cleanup(func() { WindowClosed(ci.instance) })
	return ci, rt
}

// withUserKeyer swaps the logged-in keyer for the test.
func withUserKeyer(t *testing.T, k nostr.Keyer, pk nostr.PubKey) {
	t.Helper()
	savedKeyer, savedPk := userKeyer, userPubkey
	userKeyer, userPubkey = k, pk
	t.Cleanup(func() { userKeyer, userPubkey = savedKeyer, savedPk })
}

func (r *respTransport) waitResp(t *testing.T, id int) WireMsg {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		for _, m := range r.resps {
			if m.ID == id {
				r.mu.Unlock()
				return m
			}
		}
		r.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no answer to rpc %d", id)
		}
		time.Sleep(time.Millisecond)
	}
}

// panicKeyer embeds a nil Keyer: every call panics with a nil dereference,
// the panic a handler used to hit when a logout cleared userKeyer between
// its nil check and its call.
type panicKeyer struct{ nostr.Keyer }

// a napp rpc whose handler panics is answered with an error, and the panic
// does not escape handleRPC: it runs on its own goroutine, where it would
// take the whole launcher down.
func TestHandleRPCRecoversPanic(t *testing.T) {
	setupNapTest(t)
	ci, rt := openBridgeNapp(t, "rpc-panic")
	withUserKeyer(t, panicKeyer{}, nostr.PubKey{})

	ci.handleRPC(WireMsg{T: "rpc", ID: 3, Method: "getPublicKey"})

	resp := rt.waitResp(t, 3)
	if resp.Error == "" || resp.Result != nil {
		t.Fatalf("answer %+v, want an error", resp)
	}
}

// signEvent reads the keyer once: a logout while the approval prompt is up
// clears userKeyer, and the handler still signs with the keyer it checked
// (the user approved this signature) instead of calling a nil one.
func TestSignEventKeepsKeyerAcrossLogout(t *testing.T) {
	setupNapTest(t)
	cleanPrompts(t)
	ci, rt := openBridgeNapp(t, "sign-logout")
	sk := nostr.Generate()
	withUserKeyer(t, keyer.NewPlainKeySigner(sk), sk.Public())

	params, err := json.Marshal(nostr.Event{Kind: 1, Content: "hello", CreatedAt: nostr.Now()})
	if err != nil {
		t.Fatal(err)
	}
	go ci.handleRPC(WireMsg{T: "rpc", ID: 4, Method: "signEvent", Params: string(params)})

	// the handler is waiting on the user: the logout lands now
	p := waitPromptsFor(t, ci.instance, 1)[0]
	userKeyer, userPubkey = nil, nostr.PubKey{}
	AnswerPrompt(p.ID, Answer{OK: true})

	resp := rt.waitResp(t, 4)
	if resp.Error != "" {
		t.Fatalf("signEvent answered %q, want the signed event", resp.Error)
	}
	var evt nostr.Event
	if err := json.Unmarshal(resp.Result, &evt); err != nil {
		t.Fatal(err)
	}
	if evt.PubKey != sk.Public() || !evt.VerifySignature() {
		t.Fatalf("event %+v is not signed by the approved key", evt)
	}
}
