//go:build linux

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"verdana/backend/serviceconfig"
)

func daemonPaths(t *testing.T) serviceconfig.Paths {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	return serviceconfig.Paths{ConfigFile: filepath.Join(root, "config.json"), DataDir: data, OverrideFile: filepath.Join(data, "settings-overrides.json")}
}

func TestDaemonStartLockAndStop(t *testing.T) {
	p := daemonPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"discover_on_user_relays":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Health().Ready {
		t.Fatal("not ready")
	}
	if s.Manager().Effective().DiscoverOnUserRelays {
		t.Fatal("config not applied")
	}
	if _, err := Open(p, "test"); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second start: %v", err)
	}
	s.Close()
	s2, err := Open(p, "test")
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	s2.Close()
}

func TestDaemonSettingsAndReload(t *testing.T) {
	p := daemonPaths(t)
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetSetting("discover_on_user_relays", false); err != nil {
		t.Fatal(err)
	}
	if s.Manager().Effective().DiscoverOnUserRelays {
		t.Fatal("override absent")
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["invalid"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	if s.Diagnostics().Warning == "" || !s.Health().Ready {
		t.Fatal("warning/readiness wrong")
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if s.Diagnostics().Warning != "" || len(s.Manager().Effective().Relays) != 0 {
		t.Fatal("reload not applied")
	}
}

func TestForegroundReadyAndStop(t *testing.T) {
	p := daemonPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, p, "test", func(line string) { ready <- line }) }()
	select {
	case line := <-ready:
		if !strings.Contains(line, "ready") {
			t.Fatal(line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ready timeout")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stop timeout")
	}
}
