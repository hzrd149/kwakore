//go:build linux

package gamepad

import "testing"

func TestControllerNormalizationAndTriggerTravel(t *testing.T) {
	if normalizeAxis(-32768) != -1 || normalizeAxis(32767) != 1 || normalizeAxis(0) != 0 {
		t.Fatal("axis endpoints")
	}
	released, quarter, half, full := trigger(0), trigger(8192), trigger(16384), trigger(32767)
	if released.Value != 0 || released.Pressed || released.Touched || quarter.Pressed || quarter.Touched || quarter.Value < 0.24 || half.Value < 0.5 || half.Value > 0.51 || !half.Pressed || !half.Touched || full.Value != 1 {
		t.Fatal("analog trigger travel")
	}
	if standardButtons[2] != 2 || standardButtons[12] != 11 || standardButtons[16] != 5 {
		t.Fatal("SDL to W3C button mapping")
	}
}

func TestNativeSnapshotsRetainSlotsAndDetachInput(t *testing.T) {
	m := &monitor{}
	m.slots[3] = &device{pad: Pad{Index: 3, Connected: true, Axes: []float64{0.5}, Buttons: []Button{{Pressed: true, Value: 1}}}}
	before := m.snapshot()
	m.slots[3].pad.Axes[0] = -1
	m.slots[3].pad.Buttons[0].Pressed = false
	after := m.snapshot()
	if len(before.Pads) != 8 || before.Pads[0] != nil || before.Pads[3].Axes[0] != 0.5 || !before.Pads[3].Buttons[0].Pressed || after.Pads[3].Axes[0] != -1 {
		t.Fatal("snapshot ownership or stable slots violated")
	}
	m.slots[3] = nil
	if m.snapshot().Pads[3] != nil {
		t.Fatal("disconnected slot retained controller")
	}
}
