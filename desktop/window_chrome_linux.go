//go:build linux

package main

import (
	"os"
	"strings"
)

const nativeWaylandEnv = "VERDANA_NATIVE_WAYLAND"

// configureNativeWindowChrome selects the Linux display backend before Gio
// creates a window. Gio uses native decorations on X11, but on Wayland it
// draws a generic Material title bar whenever the compositor asks clients to
// decorate themselves (notably on GNOME). Prefer XWayland in that situation so
// the desktop's window manager supplies Verdana's frame and controls.
//
// Pure Wayland sessions have no DISPLAY and continue to use Gio's Wayland
// backend. Users who prefer native Wayland rendering over a system-managed
// title bar can set VERDANA_NATIVE_WAYLAND=1.
func configureNativeWindowChrome() string {
	if envEnabled(os.Getenv(nativeWaylandEnv)) {
		return "wayland"
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return ""
	}
	if os.Getenv("DISPLAY") == "" {
		return "wayland"
	}
	if err := os.Unsetenv("WAYLAND_DISPLAY"); err != nil {
		return "wayland"
	}
	return "x11"
}

func envEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
