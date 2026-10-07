//go:build linux

package linuxhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"verdana/backend"
)

func TestLinuxHostSession(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nsleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	host := New(program)
	spec := backend.WindowSpec{Instance: "1", Format: backend.FormatNapplet}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	start := time.Now()
	if _, err := host.OpenWindowContext(context.Background(), spec); !errors.Is(err, backend.ErrServiceSessionUnavailable) {
		t.Fatalf("headless: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("headless preflight blocked")
	}
	t.Setenv("DISPLAY", ":stale")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start = time.Now()
	if _, err := host.OpenWindowContext(ctx, spec); !errors.Is(err, backend.ErrServiceTimeout) {
		t.Fatalf("stale display: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("stale display exceeded deadline")
	}
}

func TestLinuxHostRejectsUnsafeProgram(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatalf("private program: %v", err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err == nil {
		t.Fatal("accepted shared-writable program directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(program); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/sh", program); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err == nil {
		t.Fatal("accepted symlinked executable")
	}
}
