package backend

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func gamepadRPC(t *testing.T, ci *Instance, fields map[string]any) {
	t.Helper()
	ci.nap.mu.Lock()
	fields["gen"] = ci.nap.gen
	ci.nap.mu.Unlock()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := napRPC(ci, "nap.gamepad", string(raw)); err != nil {
		t.Fatal(err)
	}
}

func testGamepadSample() map[string]any {
	return map[string]any{"available": true, "pads": []any{nil, map[string]any{
		"index": 42, "id": "Test controller", "mapping": "standard", "connected": true, "timestamp": 123.5,
		"axes": []any{-2.0, 0.5}, "buttons": []any{map[string]any{"value": 2.0, "pressed": true, "touched": true}},
	}}}
}

func assertGamepadValues(t *testing.T, env map[string]any, focused bool) {
	t.Helper()
	if env["focused"] != focused || env["available"] != true {
		t.Fatalf("state = %#v, want available and focused=%v", env, focused)
	}
	pads := env["pads"].([]any)
	if len(pads) != 2 || pads[0] != nil {
		t.Fatalf("slots = %#v", pads)
	}
	pad := pads[1].(map[string]any)
	if pad["index"] != float64(1) || pad["id"] != "Test controller" || pad["mapping"] != "standard" {
		t.Fatalf("controller metadata = %#v", pad)
	}
	button := pad["buttons"].([]any)[0].(map[string]any)
	wantAxes := []any{float64(0), float64(0)}
	wantTimestamp, wantValue := float64(0), float64(0)
	if focused {
		wantAxes = []any{float64(-1), float64(0.5)}
		wantTimestamp, wantValue = 123.5, 1
	}
	if !reflect.DeepEqual(pad["axes"], wantAxes) || pad["timestamp"] != wantTimestamp ||
		button["value"] != wantValue || button["pressed"] != focused || button["touched"] != focused {
		t.Fatalf("input values = %#v", pad)
	}
}

func setupGamepadTest(t *testing.T) {
	t.Helper()
	setupNapTest(t)
	old := gamepadRun
	gamepadRun = func(ctx context.Context, emit func(gamepadSample)) { <-ctx.Done() }
	t.Cleanup(func() { gamepadRun = old })
}
func injectGamepad(t *testing.T, fields map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(fields)
	var sample gamepadSample
	if err := json.Unmarshal(raw, &sample); err != nil {
		t.Fatal(err)
	}
	napGamepads.mu.Lock()
	epoch := napGamepads.epoch
	napGamepads.mu.Unlock()
	napGamepads.accept(epoch, sample)
}
func TestGamepadBrokerFocusIsolationAndLifecycle(t *testing.T) {
	setupGamepadTest(t)
	a, ar := openNapplet(t, "game-a")
	b, br := openNapplet(t, "game-b")
	ready(t, a, ar, 1)
	ready(t, b, br, 1)
	post(t, a, map[string]any{"type": "gamepad.subscribe"})
	ar.wait(t, "gamepad.state", 1)
	post(t, b, map[string]any{"type": "gamepad.subscribe"})
	br.wait(t, "gamepad.state", 1)
	gamepadRPC(t, a, map[string]any{"focused": true, "focusSeq": 1})
	ar.wait(t, "gamepad.state", 2)
	injectGamepad(t, testGamepadSample())
	assertGamepadValues(t, ar.wait(t, "gamepad.state", 3), true)
	assertGamepadValues(t, br.wait(t, "gamepad.state", 2), false)
	injectGamepad(t, testGamepadSample())
	gamepadRPC(t, b, map[string]any{"sample": map[string]any{"available": false}})
	if len(ar.find("gamepad.state")) != 3 || len(br.find("gamepad.state")) != 2 {
		t.Fatal("unchanged or forged samples were delivered")
	}
	gamepadRPC(t, b, map[string]any{"focused": true, "focusSeq": 1})
	assertGamepadValues(t, ar.wait(t, "gamepad.state", 4), false)
	assertGamepadValues(t, br.wait(t, "gamepad.state", 3), true)
	gamepadRPC(t, a, map[string]any{"focused": false, "focusSeq": 2})
	gamepadRPC(t, a, map[string]any{"focused": true, "focusSeq": 1})
	if len(br.find("gamepad.state")) != 3 {
		t.Fatal("old focus changed the active subscriber")
	}
	b.napReset()
	gamepadRPC(t, a, map[string]any{"focused": true, "focusSeq": 3})
	assertGamepadValues(t, ar.wait(t, "gamepad.state", 5), true)
	napGamepads.mu.Lock()
	epoch := napGamepads.epoch
	napGamepads.mu.Unlock()
	post(t, a, map[string]any{"type": "gamepad.unsubscribe"})
	if ar.wait(t, gamepadControlType, 2)["subscribed"] != false {
		t.Fatal("unsubscribe did not stop subscription")
	}
	napGamepads.accept(epoch, gamepadSample{Available: false})
	if len(ar.find("gamepad.state")) != 5 {
		t.Fatal("state after unsubscribe")
	}
}

func TestGamepadMalformedMessagesUnavailableAndResubscribe(t *testing.T) {
	setupGamepadTest(t)
	ci, rec := openNapplet(t, "game-errors")
	ready(t, ci, rec, 1)
	for _, message := range []map[string]any{
		{"type": "gamepad.subscribe", "id": "bad"},
		{"type": "gamepad.subscribe", "pads": []any{}},
		{"type": "gamepad.state", "available": true},
		{"type": gamepadControlType, "reader": true},
	} {
		post(t, ci, message)
	}
	post(t, ci, map[string]any{"type": "gamepad.subscribe"})
	rec.wait(t, gamepadControlType, 1)
	rec.wait(t, "gamepad.state", 1)
	if len(rec.find(gamepadControlType)) != 1 {
		t.Fatal("malformed subscriptions or forged controls were accepted")
	}
	injectGamepad(t, map[string]any{"available": false, "reason": "blocked", "pads": []any{testGamepadSample()}})
	state := rec.wait(t, "gamepad.state", 2)
	if state["available"] != false || state["focused"] != false || state["reason"] != "blocked" || len(state["pads"].([]any)) != 0 {
		t.Fatalf("blocked state = %#v", state)
	}
	post(t, ci, map[string]any{"type": "gamepad.subscribe"})
	rec.wait(t, "gamepad.state", 3)
	// Every accepted duplicate subscribe gets a fresh snapshot, but must not
	// erase the sequence that rejects old trusted focus messages.
	post(t, ci, map[string]any{"type": "gamepad.unsubscribe", "id": "bad"})
	injectGamepad(t, map[string]any{"available": false})
	state = rec.wait(t, "gamepad.state", 4)
	if state["reason"] != "unavailable" {
		t.Fatalf("missing API state = %#v", state)
	}
}

func TestGamepadSubscriptionRateLimitSurvivesReload(t *testing.T) {
	setupGamepadTest(t)
	ci, rec := openNapplet(t, "game-flood")
	ready(t, ci, rec, 1)
	for i := 0; i < 8; i++ {
		post(t, ci, map[string]any{"type": "gamepad.subscribe"})
	}
	rec.wait(t, "gamepad.state", 8)
	ci.napReset()
	ready(t, ci, rec, 2)
	post(t, ci, map[string]any{"type": "gamepad.subscribe"})
	// An ordered barrier tells us the ninth subscription was dispatched.
	post(t, ci, map[string]any{"type": "theme.get", "id": "barrier"})
	rec.wait(t, "theme.get.result", 1)
	if len(rec.find("gamepad.state")) != 8 {
		t.Fatal("reload refilled the subscription bucket")
	}
}

func TestGamepadLateBlurRestoresNewWindowAndOldSourceCannotRestart(t *testing.T) {
	setupGamepadTest(t)
	a, ar := openNapplet(t, "game-late-a")
	b, br := openNapplet(t, "game-late-b")
	ready(t, a, ar, 1)
	ready(t, b, br, 1)
	post(t, a, map[string]any{"type": "gamepad.subscribe"})
	ar.wait(t, "gamepad.state", 1)
	post(t, b, map[string]any{"type": "gamepad.subscribe"})
	br.wait(t, "gamepad.state", 1)
	gamepadRPC(t, b, map[string]any{"focused": true, "focusSeq": 1})
	br.wait(t, "gamepad.state", 2)
	// The old window's earlier focus update reaches the daemon after the new
	// window's focus. Its subsequent blur must restore the remaining candidate.
	gamepadRPC(t, a, map[string]any{"focused": true, "focusSeq": 1})
	br.wait(t, "gamepad.state", 3)
	gamepadRPC(t, a, map[string]any{"focused": false, "focusSeq": 2})
	if br.wait(t, "gamepad.state", 4)["focused"] != true {
		t.Fatal("late blur stranded focused window")
	}
	napGamepads.mu.Lock()
	epoch := napGamepads.epoch
	napGamepads.mu.Unlock()
	a.napReset()
	b.napReset()
	ready(t, b, br, 2)
	post(t, b, map[string]any{"type": "gamepad.subscribe"})
	br.wait(t, "gamepad.state", 5)
	napGamepads.accept(epoch, gamepadSample{Available: false, Reason: "blocked"})
	if len(br.find("gamepad.state")) != 5 {
		t.Fatal("revoked native monitor updated a new subscription")
	}
	// Focus updates from the previous document cannot focus its replacement.
	if err := napGamepads.update(b, `{"gen":1,"focused":true,"focusSeq":999}`); err != nil {
		t.Fatal(err)
	}
	if len(br.find("gamepad.state")) != 5 {
		t.Fatal("old document changed new subscription")
	}
}
