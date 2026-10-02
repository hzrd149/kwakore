//go:build linux

package main

import (
	"os"
	"testing"
)

func TestConfigureNativeWindowChromePrefersX11Decorations(t *testing.T) {
	t.Setenv(nativeWaylandEnv, "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", ":0")

	if got := configureNativeWindowChrome(); got != "x11" {
		t.Fatalf("backend = %q, want x11", got)
	}
	if got := os.Getenv("WAYLAND_DISPLAY"); got != "" {
		t.Fatalf("WAYLAND_DISPLAY = %q, want empty", got)
	}
}

func TestConfigureNativeWindowChromeKeepsPureWayland(t *testing.T) {
	t.Setenv(nativeWaylandEnv, "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", "")

	if got := configureNativeWindowChrome(); got != "wayland" {
		t.Fatalf("backend = %q, want wayland", got)
	}
	if got := os.Getenv("WAYLAND_DISPLAY"); got != "wayland-0" {
		t.Fatalf("WAYLAND_DISPLAY = %q, want wayland-0", got)
	}
}

func TestConfigureNativeWindowChromeHonorsWaylandOverride(t *testing.T) {
	t.Setenv(nativeWaylandEnv, "true")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", ":0")

	if got := configureNativeWindowChrome(); got != "wayland" {
		t.Fatalf("backend = %q, want wayland", got)
	}
	if got := os.Getenv("WAYLAND_DISPLAY"); got != "wayland-0" {
		t.Fatalf("WAYLAND_DISPLAY = %q, want wayland-0", got)
	}
}
