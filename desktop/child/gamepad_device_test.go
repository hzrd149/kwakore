//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"kwakore/backend/gamepad"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This optional test creates a temporary game controller through Linux uinput.
// It exercises real device discovery, mapping and input through the daemon and WebKitGTK,
// rather than replacing navigator.getGamepads with a mock. It emits controller
// events only, and closes/destroys the device at cleanup.
func TestGamepadVirtualDevice(t *testing.T) {
	if os.Getenv("KWAKORE_GAMEPAD_UINPUT") != "1" {
		t.Skip("set KWAKORE_GAMEPAD_UINPUT=1 with access to /dev/uinput")
	}
	needWebKit(t)
	device, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open /dev/uinput: %v", err)
	}
	t.Cleanup(func() { _ = device.Close() })
	ioctl := func(request, value uintptr) {
		t.Helper()
		_, _, err := unix.Syscall(unix.SYS_IOCTL, device.Fd(), request, value)
		if err != 0 {
			t.Fatalf("uinput ioctl %#x: %v", request, err)
		}
	}
	// linux/uinput.h: _IOW('U', 100/101/103, int), _IO('U', 1/2).
	ioctl(0x40045564, unix.EV_KEY)
	ioctl(0x40045564, unix.EV_ABS)
	for _, code := range []uintptr{0x130, 0x131, 0x133, 0x134, 0x136, 0x137, 0x13a, 0x13b, 0x13c, 0x13d, 0x13e} {
		ioctl(0x40045565, code)
	}
	setup := make([]byte, 1116) // struct uinput_user_dev, including 64 ABS entries
	copy(setup, "Kwakore test controller")
	binary.NativeEndian.PutUint16(setup[80:], 3) // BUS_USB
	binary.NativeEndian.PutUint16(setup[82:], 0x045e)
	binary.NativeEndian.PutUint16(setup[84:], 0x028e) // standard Xbox 360 layout
	binary.NativeEndian.PutUint16(setup[86:], 0x0114)
	for _, axis := range []int{0, 1, 2, 3, 4, 5, 16, 17} {
		ioctl(0x40045567, uintptr(axis))
		lower, upper := int32(-32768), int32(32767)
		if axis == 2 || axis == 5 {
			lower, upper = 0, 255
		}
		if axis == 16 || axis == 17 {
			lower, upper = -1, 1
		}
		binary.NativeEndian.PutUint32(setup[92+4*axis:], uint32(upper))
		binary.NativeEndian.PutUint32(setup[348+4*axis:], uint32(lower))
	}
	if _, err := device.Write(setup); err != nil {
		t.Fatal(err)
	}
	ioctl(0x5501, 0)
	t.Cleanup(func() { _, _, _ = unix.Syscall(unix.SYS_IOCTL, device.Fd(), 0x5502, 0) })
	input := func(pressed bool) {
		t.Helper()
		var value int32
		if pressed {
			value = 1
		}
		for _, event := range []struct {
			Time  unix.Timeval
			Type  uint16
			Code  uint16
			Value int32
		}{{Type: unix.EV_KEY, Code: 0x130, Value: value}, {Type: unix.EV_SYN}} {
			if err := binary.Write(device, binary.NativeEndian, event); err != nil {
				t.Fatal(err)
			}
		}
	}
	html := []byte(`<script>
let controllerIndex=null;
window.napplet.gamepad.onChange(state=>{
  const pads=navigator.getGamepads();
  if (controllerIndex===null) controllerIndex=pads.filter(p=>p && p.id.includes("045e-028e")).at(-1)?.index ?? null;
  const pad=controllerIndex===null ? null : pads[controllerIndex];
  window.napplet.storage.instance.setItem("device-report",JSON.stringify({
    focused:state.focused,connected:!!pad,mapping:pad&&pad.mapping,pressed:!!pad&&pad.buttons[0].pressed,timestamp:pad&&pad.timestamp,axis:pad&&pad.axes[0],trigger:pad&&pad.buttons[6].value,up:!!pad&&pad.buttons[12].pressed,
  }));
});
</script>`)
	var groupMu sync.Mutex
	var group []*fakeLauncher
	// One daemon-style native monitor broadcasts to both trusted host pages;
	// local focus masking is the final isolation boundary.
	relay := func(sample gamepad.Snapshot) {
		raw, _ := json.Marshal(struct {
			Type string `json:"type"`
			gamepad.Snapshot
			Focused bool `json:"focused"`
		}{"gamepad.state", sample, true})
		groupMu.Lock()
		defer groupMu.Unlock()
		for _, target := range group {
			target.mu.Lock()
			gen := target.gen
			target.mu.Unlock()
			target.send(wireMsg{T: "eval", Code: fmt.Sprintf("window.__nap_push(%d,%s)", gen, raw)})
		}
	}
	f := newFakeLauncher(t, html)
	f.domains = []string{"gamepad", "storage"}
	group = append(group, f)
	bin := buildChild(t)
	f.start(bin)
	if !f.waitFor(30*time.Second, func() bool { return f.count("msg:gamepad.subscribe:") > 0 }) {
		t.Fatal("device binding did not subscribe")
	}
	focusGamepadWindow(t, f)
	f.mu.Lock()
	gen := f.gen
	f.mu.Unlock()
	f.send(wireMsg{T: "eval", Code: fmt.Sprintf(`document.querySelector("iframe").focus();window.__nap_push(%d,{type:"__kwakore.gamepad",subscribed:true})`, gen)})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); gamepad.Run(ctx, relay) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		input(false)
		time.Sleep(30 * time.Millisecond)
		input(true)
		if f.waitFor(100*time.Millisecond, func() bool {
			report, _ := f.stored("instance", "device-report")
			return strings.Contains(report, `"pressed":true`)
		}) {
			break
		}
	}
	report, _ := f.stored("instance", "device-report")
	if !strings.Contains(report, `"pressed":true`) || !strings.Contains(report, `"mapping":"standard"`) || !strings.Contains(report, `"focused":true`) {
		t.Fatalf("real controller press/mapping did not reach binding: %s", report)
	}
	input(false)
	if !f.waitFor(10*time.Second, func() bool {
		report, _ := f.stored("instance", "device-report")
		return strings.Contains(report, `"pressed":false`)
	}) {
		t.Fatal("real controller release did not reach binding")
	}
	// Analog normalization and SDL hat-to-button mapping traverse the same
	// native source as button presses.
	for _, event := range []struct {
		Time  unix.Timeval
		Type  uint16
		Code  uint16
		Value int32
	}{
		{Type: unix.EV_ABS, Code: 0, Value: 16384}, {Type: unix.EV_ABS, Code: 17, Value: -1}, {Type: unix.EV_ABS, Code: 2, Value: 128}, {Type: unix.EV_SYN},
	} {
		if err := binary.Write(device, binary.NativeEndian, event); err != nil {
			t.Fatal(err)
		}
	}
	if !f.waitFor(10*time.Second, func() bool {
		raw, _ := f.stored("instance", "device-report")
		var r struct {
			Axis    float64 `json:"axis"`
			Up      bool    `json:"up"`
			Trigger float64 `json:"trigger"`
		}
		return json.Unmarshal([]byte(raw), &r) == nil && r.Axis > 0.49 && r.Axis < 0.51 && r.Up && r.Trigger > 0.49 && r.Trigger < 0.51
	}) {
		t.Fatal("analog stick, trigger or D-pad mapping did not reach binding")
	}
	second := newFakeLauncher(t, html)
	second.domains = f.domains
	groupMu.Lock()
	group = append(group, second)
	groupMu.Unlock()
	second.start(bin)
	if !second.waitFor(30*time.Second, func() bool { return second.count("msg:gamepad.subscribe:") > 0 }) {
		t.Fatal("second window did not subscribe")
	}
	f.send(wireMsg{T: "eval", Code: fmt.Sprintf(`window.__nap_push(%d,{type:"__kwakore.gamepad",subscribed:true})`, gen)})
	focusGamepadWindow(t, second)
	second.mu.Lock()
	secondGen := second.gen
	second.mu.Unlock()
	second.send(wireMsg{T: "eval", Code: fmt.Sprintf(`window.__nap_push(%d,{type:"__kwakore.gamepad",subscribed:true})`, secondGen)})
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		input(false)
		time.Sleep(70 * time.Millisecond)
		input(true)
		if second.waitFor(100*time.Millisecond, func() bool {
			report, _ := second.stored("instance", "device-report")
			return strings.Contains(report, `"pressed":true`)
		}) {
			break
		}
	}
	report, _ = second.stored("instance", "device-report")
	if !strings.Contains(report, `"pressed":true`) {
		t.Fatalf("focus handoff lost controller input: %s", report)
	}
	if !f.waitFor(10*time.Second, func() bool {
		report, _ := f.stored("instance", "device-report")
		return strings.Contains(report, `"pressed":false`) && strings.Contains(report, `"focused":false`) && strings.Contains(report, `"timestamp":0`)
	}) {
		t.Fatal("inactive window retained live controller input")
	}
	// Disconnects reach the background subscriber too.
	ioctl(0x5502, 0)
	for _, target := range group {
		if !target.waitFor(10*time.Second, func() bool {
			report, _ := target.stored("instance", "device-report")
			return strings.Contains(report, `"connected":false`)
		}) {
			t.Fatal("controller disconnect did not reach every window")
		}
	}
	cancel()
	<-done
	second.close(10 * time.Second)
	f.close(10 * time.Second)
}
