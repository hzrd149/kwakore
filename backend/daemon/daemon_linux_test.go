//go:build linux

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"verdana/backend"
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

func TestDaemonDoesNotPersistFileSecrets(t *testing.T) {
	p := daemonPaths(t)
	if err := os.MkdirAll(p.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(p.DataDir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"client_key":"legacy-secret","login":"legacy-login","secrets_location":"file"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "legacy-secret") || strings.Contains(string(data), "legacy-login") || strings.Contains(string(data), `"secrets_location": "file"`) {
		t.Fatalf("service persisted file-mode secrets: %s", data)
	}
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

func TestDaemonSettingUsesLiveConfigWithoutLegacyWrite(t *testing.T) {
	p := daemonPaths(t)
	if err := os.MkdirAll(p.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(p.DataDir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"relays":["legacy.example"],"blossom_servers":["https://legacy.example"],"discover_on_user_relays":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://file.example"],"blossom_servers":["https://file.example"],"discover_on_user_relays":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("relays", []string{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("blossom_servers", []string{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("discover_on_user_relays", false); err != nil {
		t.Fatal(err)
	}
	if got := backend.Relays(); len(got) != 0 {
		t.Fatalf("service relays: %v", got)
	}
	if got := backend.BlossomServers(); len(got) != 0 {
		t.Fatalf("service Blossom servers: %v", got)
	}
	if backend.DiscoverOnUserRelays() {
		t.Fatal("user relay discovery still enabled")
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("service setting mutated legacy state: %s", after)
	}
	if err := s.ClearSetting("relays"); err != nil {
		t.Fatal(err)
	}
	if got := backend.Relays(); !reflect.DeepEqual(got, []string{"wss://file.example"}) || len(backend.BlossomServers()) != 0 || backend.DiscoverOnUserRelays() {
		t.Fatalf("clearing relays altered other settings: %+v", s.Manager().Effective())
	}
	s.Close()
	s, err = Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := backend.Relays(); !reflect.DeepEqual(got, []string{"wss://file.example"}) || len(backend.BlossomServers()) != 0 || backend.DiscoverOnUserRelays() {
		t.Fatalf("legacy state overrode service config on restart: %+v", s.Manager().Effective())
	}
}

type settingsChangeHost struct {
	backend.Host
	changes int
}

func (h *settingsChangeHost) StateChanged() { h.changes++ }

func TestDaemonSettingNotifiesOnlyForEffectiveChange(t *testing.T) {
	p := daemonPaths(t)
	if err := os.MkdirAll(p.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := serviceconfig.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	h := &settingsChangeHost{}
	closeBackend, err := backend.Start(backend.Options{DataDir: p.DataDir, ServiceConfig: m, Host: h})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend()
	s := &Service{manager: m}
	if err := s.SetSetting("relays", []string{"wss://changed.example"}); err != nil {
		t.Fatal(err)
	}
	if h.changes != 1 {
		t.Fatalf("effective relay change sent %d notifications, want one", h.changes)
	}
	if err := s.SetSetting("relays", []string{"wss://changed.example"}); err != nil {
		t.Fatal(err)
	}
	if h.changes != 1 {
		t.Fatalf("unchanged relays sent notification: %d", h.changes)
	}
}

func TestDaemonReloadWarnsSafelyAndClears(t *testing.T) {
	p := daemonPaths(t)
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	secret := "https://private-token.example.invalid/path"
	if err := os.WriteFile(p.ConfigFile, []byte(`{"blossom_servers":["`+secret+`?token=hidden"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	warning := s.Diagnostics().Warning
	if !strings.Contains(warning, "config.json") || !strings.Contains(warning, "blossom_servers") || strings.Contains(warning, secret) || strings.Contains(warning, "hidden") {
		t.Fatalf("unsafe or incomplete warning: %q", warning)
	}
	if !s.Health().Ready {
		t.Fatal("rejected reload marked service unready")
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"blossom_servers":["https://safe.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := s.Diagnostics().Warning; got != "" {
		t.Fatalf("successful reload retained warning: %q", got)
	}
}

func TestDaemonReloadAndMutationKeepOverridePrecedence(t *testing.T) {
	p := daemonPaths(t)
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://file.example"],"blossom_servers":["https://file.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		results <- s.SetSetting("relays", []string{"wss://override.example"})
	}()
	go func() {
		defer wg.Done()
		<-start
		results <- s.Reload()
	}()
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := s.Manager().Effective()
	if !reflect.DeepEqual(got.Relays, []string{"wss://override.example"}) || !reflect.DeepEqual(got.BlossomServers, []string{"https://file.example"}) {
		t.Fatalf("mutation/reload lost precedence: %+v", got)
	}
}

func TestForegroundReadyAndStop(t *testing.T) {
	p := daemonPaths(t)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
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

func TestDaemonRejectsUnsafeLockAndDataPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T, *serviceconfig.Paths)
	}{
		{"public data directory", func(t *testing.T, p *serviceconfig.Paths) {
			if err := os.MkdirAll(p.DataDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p.DataDir, 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{"public lock", func(t *testing.T, p *serviceconfig.Paths) {
			if err := os.MkdirAll(p.DataDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p.DataDir, "daemon.lock"), nil, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlinked lock", func(t *testing.T, p *serviceconfig.Paths) {
			if err := os.MkdirAll(p.DataDir, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(filepath.Dir(p.DataDir), "target")
			if err := os.WriteFile(target, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(p.DataDir, "daemon.lock")); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlinked parent", func(t *testing.T, p *serviceconfig.Paths) {
			root := filepath.Dir(p.DataDir)
			actual := filepath.Join(root, "actual")
			if err := os.Mkdir(actual, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(actual, "kwakore"), 0700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(root, "link")
			if err := os.Symlink(actual, link); err != nil {
				t.Fatal(err)
			}
			p.DataDir = filepath.Join(link, "kwakore")
			p.OverrideFile = filepath.Join(p.DataDir, "settings-overrides.json")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := daemonPaths(t)
			tc.prepare(t, &p)
			s, err := Open(p, "test")
			if err == nil {
				s.Close()
				t.Fatal("unsafe path accepted")
			}
		})
	}
}

func TestDaemonCorrectedRestartAfterInvalidConfig(t *testing.T) {
	p := daemonPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["bad"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(p, "test"); err == nil {
		s.Close()
		t.Fatal("invalid config started")
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p, "test")
	if err != nil {
		t.Fatalf("corrected restart failed: %v", err)
	}
	s.Close()
}

func TestShutdownDuringRPCDrainsLeaseBeforeClosingStores(t *testing.T) {
	s, err := Open(daemonPaths(t), "test")
	if err != nil {
		t.Fatal(err)
	}
	leaseDone, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	closeBackend := s.closeBackend
	s.closeBackend = func() { close(closed); closeBackend() }
	finished := make(chan struct{})
	go func() { s.Close(); close(finished) }()
	time.Sleep(5200 * time.Millisecond)
	select {
	case <-closed:
		t.Fatal("stores closed while RPC lease active")
	default:
	}
	leaseDone()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("close did not drain")
	}
	s.Close()
}

func TestShutdownDuringRPCCancelsWork(t *testing.T) {
	s, err := Open(daemonPaths(t), "test")
	if err != nil {
		t.Fatal(err)
	}
	done, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	workCtx, cancel := s.registryContext(t.Context())
	defer cancel()
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-workCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("work not canceled")
	}
	if _, err := s.Begin(); err != ErrClosing {
		t.Fatalf("late lease: %v", err)
	}
	if _, rpcErr := s.dispatchRPC("service.status", nil); rpcErr == nil || rpcErr.Code != 1007 {
		t.Fatalf("late RPC: %+v", rpcErr)
	}
	select {
	case <-closed:
		t.Fatal("closed before RPC returned")
	default:
	}
	done()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not finish")
	}
}
