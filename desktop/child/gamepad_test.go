//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWebKitGamepadPolicy(t *testing.T) {
	needWebKit(t)
	f := newFakeLauncher(t, []byte(`<script>
let result = "missing";
try { navigator.getGamepads(); result = "allowed" } catch(e) { result = e.name }
window.napplet.storage.instance.setItem("gamepad-policy", result);
</script>`))
	f.start(buildChild(t))
	if !f.waitFor(30*time.Second, func() bool { return f.count("msg:storage.set:gamepad-policy") > 0 || f.hasExited() }) {
		t.Fatal("controller policy probe never arrived")
	}
	result, _ := f.stored("instance", "gamepad-policy")
	if result != "SecurityError" {
		t.Fatalf("native API in sandbox: %s, want SecurityError", result)
	}
	f.send(wireMsg{T: "eval", Code: `(() => {
let result="missing";
try { result=typeof navigator.getGamepads === "function" ? "available:"+navigator.getGamepads().length : "missing" } catch(e) { result=e.name }
window.__kwakoreNappletRPC("nap.msg", JSON.stringify(JSON.stringify({type:"storage.set",id:"probe",scope:"instance",key:"gamepad-host",value:result})));
})()`})
	if !f.waitFor(10*time.Second, func() bool { return f.count("msg:storage.set:gamepad-host") > 0 }) {
		t.Fatal("host controller probe never arrived")
	}
	host, _ := f.stored("instance", "gamepad-host")
	if host != "available:0" {
		// A real controller may already be attached; any successful slot list
		// proves the trusted host retains native access.
		if len(host) < len("available:") || host[:len("available:")] != "available:" {
			t.Fatalf("native API in host: %s, want available slot list", host)
		}
	}
	f.close(10 * time.Second)
}

// Real browser event dispatch and WebIDL receiver checks cannot be proved by
// Node mocks. Inject snapshots through the trusted host using the normal wire.
func TestWebKitGamepadBinding(t *testing.T) {
	needWebKit(t)
	f := newFakeLauncher(t, []byte(`<script>
const events=[];
window.addEventListener("gamepadconnected",e=>events.push(e instanceof GamepadEvent && e.gamepad instanceof Gamepad && e.gamepad.connected));
window.ongamepadconnected=e=>events.push(e instanceof GamepadEvent);
window.addEventListener("gamepaddisconnected",e=>events.push(e instanceof GamepadEvent && !e.gamepad.connected));
let receiver=false;
try { navigator.getGamepads.call(Object.create(Navigator.prototype)) } catch(e) { receiver=e.name==="TypeError" }
window.napplet.gamepad.onChange(state=>{
  let blocked=false, pads=[];
  try { pads=navigator.getGamepads() } catch(e) { blocked=e.name==="SecurityError" }
  const pad=pads[1];
  window.napplet.storage.instance.setItem("gamepad-state",JSON.stringify({
    available:state.available,focused:state.focused,blocked,receiver,events:events.slice(),
    pads:pads.length,empty:pads[0]===null,name:navigator.getGamepads.name,length:navigator.getGamepads.length,
    proto:!!pad && pad instanceof Gamepad && pad.buttons[0] instanceof GamepadButton,
    pressed:!!pad && pad.buttons[0].pressed,timestamp:pad && pad.timestamp,
  }));
});
</script>`))
	f.domains = []string{"storage", "gamepad"}
	f.start(buildChild(t))
	if !f.waitFor(30*time.Second, func() bool { return f.count("msg:gamepad.subscribe:") > 0 || f.hasExited() }) {
		t.Fatal("binding did not subscribe")
	}
	f.mu.Lock()
	gen := f.gen
	f.mu.Unlock()
	push := func(env any) {
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		f.send(wireMsg{T: "eval", Code: fmt.Sprintf("window.__nap_push(%d,%s)", gen, raw)})
	}
	push(map[string]any{"type": "__kwakore.gamepad", "subscribed": true})
	// Focus the actual GTK window, then the iframe. No napplet-supplied focus
	// boolean is involved in the host's decision to expose live values.
	focusGamepadWindow(t, f)
	pad := map[string]any{"index": 1, "id": "Test controller", "mapping": "standard", "connected": true,
		"timestamp": 25, "axes": []any{0.5}, "buttons": []any{map[string]any{"value": 1, "pressed": true, "touched": true}}}
	state := map[string]any{"type": "gamepad.state", "available": true, "focused": true, "pads": []any{nil, pad}}
	push(state)
	if !f.waitFor(10*time.Second, func() bool {
		value, _ := f.stored("instance", "gamepad-state")
		return strings.Contains(value, `"pressed":true`)
	}) {
		value, _ := f.stored("instance", "gamepad-state")
		t.Fatalf("live controller state did not reach focused frame: %s", value)
	}
	var result struct {
		Available, Focused, Receiver, Empty, Proto, Pressed, Blocked bool
		Events                                                       []bool
		Pads, Length, Timestamp                                      int
		Name                                                         string
	}
	value, _ := f.stored("instance", "gamepad-state")
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Available || !result.Focused || !result.Receiver || !result.Empty || !result.Proto ||
		!result.Pressed || result.Timestamp != 25 || result.Pads != 2 || result.Name != "getGamepads" || result.Length != 0 ||
		len(result.Events) != 2 || !result.Events[0] || !result.Events[1] {
		t.Fatalf("native compatibility: %s", value)
	}
	// A trusted overlay must clear input before its user answers, even if the
	// iframe remains the active element and a live daemon push arrives late.
	f.send(wireMsg{T: "eval", Code: `const overlay=document.createElement("div");overlay.id="__kwakore_prompt";document.body.appendChild(overlay)`})
	push(state)
	if !f.waitFor(10*time.Second, func() bool {
		value, _ := f.stored("instance", "gamepad-state")
		return strings.Contains(value, `"focused":false`) && strings.Contains(value, `"timestamp":0`)
	}) {
		t.Fatal("prompt overlay did not neutralize input")
	}
	push(map[string]any{"type": "gamepad.state", "available": false, "focused": false, "reason": "blocked", "pads": []any{}})
	if !f.waitFor(10*time.Second, func() bool {
		value, _ := f.stored("instance", "gamepad-state")
		return strings.Contains(value, `"blocked":true`) && strings.Contains(value, `"events":[true,true,true]`)
	}) {
		value, _ := f.stored("instance", "gamepad-state")
		t.Fatalf("blocked state or native disconnect event missing: %s", value)
	}
	push(map[string]any{"type": "__kwakore.gamepad", "subscribed": false})
	f.close(10 * time.Second)
}

func focusGamepadWindow(t *testing.T, f *fakeLauncher) {
	t.Helper()
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Fatal("gamepad focus tests require xdotool under Xvfb")
	}
	windows, err := exec.Command("xdotool", "search", "--onlyvisible", "--pid", fmt.Sprint(f.cmd.Process.Pid)).Output()
	if err != nil || len(strings.Fields(string(windows))) == 0 {
		t.Fatalf("finding gamepad window (run under xvfb-run): %v: %s", err, windows)
	}
	windowID := strings.Fields(string(windows))[0]
	if out, err := exec.Command("xdotool", "windowfocus", "--sync", windowID).CombinedOutput(); err != nil {
		t.Fatalf("focusing gamepad window: %v: %s", err, out)
	}
	f.send(wireMsg{T: "eval", Code: `document.querySelector("iframe").focus()`})
}
