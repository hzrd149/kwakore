//go:build linux

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"verdana/backend"
)

func TestHealthReportsLiveStateAndShutdown(t *testing.T) {
	s, err := Open(daemonPaths(t), "test-version")
	if err != nil {
		t.Fatal(err)
	}
	health := s.Health()
	if !health.Ready || health.Version != "test-version" || health.ConfigStatus != "valid" || health.StorageStatus != "open" {
		t.Fatalf("unexpected live health: %+v", health)
	}
	if health.UptimeSeconds < 0 || health.UptimeSeconds > 30 {
		t.Fatalf("implausible uptime: %v", health.UptimeSeconds)
	}
	if health.ActiveWindows != len(backend.OpenWindows()) {
		t.Fatalf("active windows = %d; open windows = %d", health.ActiveWindows, len(backend.OpenWindows()))
	}
	s.Close()
	health = s.Health()
	if health.Ready || health.StorageStatus != "closed" {
		t.Fatalf("closing service still claims readiness/storage: %+v", health)
	}
}

func TestDiagnosticsBoundsAndSanitizesReloadErrors(t *testing.T) {
	p := daemonPaths(t)
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	secret := "private-client-key-sentinel"
	for i := 0; i < 35; i++ {
		if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://private.example/`+secret+`?token=hidden"]}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := s.Reload(); err == nil {
			t.Fatal("invalid reload accepted")
		}
	}
	d := s.Diagnostics()
	if !d.Health.Ready || d.Warning == "" {
		t.Fatalf("rejected reload changed readiness or lost warning: %+v", d)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, "hidden", p.DataDir, p.ConfigFile, "client_key", "login", "bunker"} {
		if strings.Contains(strings.ToLower(string(b)), strings.ToLower(forbidden)) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, b)
		}
	}
	var report struct {
		ObservedFrom string `json:"observed_from"`
		RecentErrors []struct {
			Category string    `json:"category"`
			Time     time.Time `json:"time"`
			Detail   string    `json:"detail"`
		} `json:"recent_errors"`
	}
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if report.ObservedFrom != "live" || len(report.RecentErrors) != 32 {
		t.Fatalf("source/history = %q/%d; want live/32", report.ObservedFrom, len(report.RecentErrors))
	}
	for _, item := range report.RecentErrors {
		if item.Category != "config_reload" || item.Time.IsZero() || !strings.Contains(item.Detail, "relays") {
			t.Fatalf("unsafe or incomplete error summary: %+v", item)
		}
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := s.Diagnostics().Warning; got != "" {
		t.Fatalf("warning persisted after successful reload: %q", got)
	}
}

func TestDiagnosticsRedactsSensitiveConfigBasename(t *testing.T) {
	p := daemonPaths(t)
	secret := "private-client-key-sentinel"
	p.ConfigFile = filepath.Join(filepath.Dir(p.ConfigFile), secret+".json")
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["invalid"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	b, err := json.Marshal(s.Diagnostics())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(string(b), p.ConfigFile) {
		t.Fatalf("diagnostics exposed private config name: %s", b)
	}
}
