//go:build linux

package linuxhost

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestColorSchemeDark(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    any
		dark, ok bool
	}{
		{"prefer dark", uint32(1), true, true},
		{"no preference", uint32(0), false, true},
		{"prefer light", uint32(2), false, true},
		{"unknown number", uint32(3), false, false},
		// the older Read method wraps the value in one more variant
		{"nested read reply", dbus.MakeVariant(dbus.MakeVariant(uint32(1))), true, true},
		{"one variant", dbus.MakeVariant(uint32(2)), false, true},
		{"string", "dark", false, false},
		{"signed", int32(1), false, false},
		{"nil", nil, false, false},
		{"three variants", dbus.MakeVariant(dbus.MakeVariant(dbus.MakeVariant(uint32(1)))), false, false},
	} {
		dark, ok := colorSchemeDark(tc.value)
		if dark != tc.dark || ok != tc.ok {
			t.Errorf("%s: colorSchemeDark = (%v, %v), want (%v, %v)", tc.name, dark, ok, tc.dark, tc.ok)
		}
	}
}
