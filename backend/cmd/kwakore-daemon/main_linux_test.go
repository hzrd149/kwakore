//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"verdana/backend"
	"verdana/backend/daemon"
	"verdana/backend/serviceconfig"
)

func TestForegroundStartReadyAndStop(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	dataRoot := filepath.Join(root, "data")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_DATA_HOME", dataRoot)
	configPath := filepath.Join(configRoot, "kwakore", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"discover_on_user_relays":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := serviceconfig.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	s, err := daemon.Open(paths, "test")
	if err != nil {
		t.Fatal(err)
	}
	if backend.DiscoverOnUserRelays() {
		t.Fatal("configured false did not reach backend")
	}
	s.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	allLines := make(chan []string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		var lines []string
		for scan.Scan() {
			lines = append(lines, scan.Text())
			if len(lines) == 1 {
				line <- lines[0]
			}
		}
		allLines <- lines
	}()
	select {
	case got := <-line:
		if got != "kwakore-daemon development ready (config: "+configPath+")" {
			t.Fatalf("ready line: %q", got)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("ready timeout")
	}
	second, err := daemon.Open(paths, "test")
	if err == nil {
		second.Close()
		t.Fatal("second launch acquired lock")
	}
	if !strings.Contains(err.Error(), "inspect status or stop") {
		t.Fatalf("missing guidance: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shutdown: %v; stderr: %s", err, stderr.String())
	}
	if lines := <-allLines; len(lines) != 1 {
		t.Fatalf("expected exactly one ready line, got %q", lines)
	}
	s, err = daemon.Open(paths, "test")
	if err != nil {
		t.Fatalf("lock retained after signal: %v", err)
	}
	s.Close()
}

func TestForegroundHelper(t *testing.T) {
	if os.Getenv("KWAKORE_FOREGROUND_HELPER") != "1" {
		return
	}
	if err := run(nil); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestForegroundSIGHUPReloadsAndSanitizesWarning(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	dataRoot := filepath.Join(root, "data")
	configPath := filepath.Join(configRoot, "kwakore", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1", "XDG_CONFIG_HOME="+configRoot, "XDG_DATA_HOME="+dataRoot)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			ready <- scan.Text()
		}
	}()
	lines := make(chan string, 32)
	go func() {
		scan := bufio.NewScanner(stderr)
		for scan.Scan() {
			lines <- scan.Text()
		}
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, "ready") {
			t.Fatalf("startup line: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ready timeout")
	}
	if err := os.WriteFile(configPath, []byte(`{"relays":["wss://private-token.example/secret?token=hidden"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitLog := func(needle string) string {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case line := <-lines:
				if strings.Contains(line, needle) {
					return line
				}
			case <-deadline:
				t.Fatalf("timeout waiting for %q", needle)
			}
		}
	}
	warning := waitLog("configuration reload rejected")
	if !strings.Contains(warning, "config.json") || !strings.Contains(warning, "relays") || strings.Contains(warning, "secret") || strings.Contains(warning, "hidden") {
		t.Fatalf("unsafe SIGHUP warning: %q", warning)
	}
	if err := os.WriteFile(configPath, []byte(`{"relays":["wss://valid.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitLog("configuration reloaded")
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMissingAndInvalidConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	if err := run([]string{"validate"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config", "kwakore", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"private_key":"bad"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"validate"}); err == nil {
		t.Fatal("secret field accepted")
	}
}

func TestOfflineReportsOnlyFileObservations(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	dataRoot := filepath.Join(root, "data")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_DATA_HOME", dataRoot)
	check := func(command, configStatus, overrideStatus, storageStatus string) {
		t.Helper()
		out, err := captureRunOutput([]string{command})
		if err != nil {
			t.Fatal(err)
		}
		var report struct {
			ObservedFrom   string   `json:"observed_from"`
			Ready          *bool    `json:"ready"`
			UptimeSeconds  *float64 `json:"uptime_seconds"`
			ActiveWindows  *int     `json:"active_windows"`
			ConfigStatus   string   `json:"config_status"`
			OverrideStatus string   `json:"override_status"`
			StorageStatus  string   `json:"storage_status"`
		}
		if err := json.Unmarshal(out, &report); err != nil {
			t.Fatalf("%s output %q: %v", command, out, err)
		}
		if report.ObservedFrom != "files" || report.Ready != nil || report.UptimeSeconds != nil || report.ActiveWindows != nil {
			t.Fatalf("%s claimed live state: %s", command, out)
		}
		if report.ConfigStatus != configStatus || report.OverrideStatus != overrideStatus || report.StorageStatus != storageStatus {
			t.Fatalf("%s file status: %s", command, out)
		}
	}
	check("status", "missing", "missing", "missing")
	check("diagnostics", "missing", "missing", "missing")
	if _, err := os.Stat(configRoot); !os.IsNotExist(err) {
		t.Fatalf("inspection created config directory or failed to stat: %v", err)
	}
	if _, err := os.Stat(dataRoot); !os.IsNotExist(err) {
		t.Fatalf("inspection created data directory or failed to stat: %v", err)
	}
	dataDir := filepath.Join(dataRoot, "kwakore")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "daemon.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	check("status", "missing", "missing", "private")
	check("diagnostics", "missing", "missing", "private")
}

func TestValidateUsesStrictConfigAndOverrideLoader(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	dataRoot := filepath.Join(root, "data")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_DATA_HOME", dataRoot)
	configPath := filepath.Join(configRoot, "kwakore", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"relays":["not-a-relay"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureRunOutput([]string{"validate"}); err == nil || !strings.Contains(err.Error(), "config.json") || !strings.Contains(err.Error(), "relays") {
		t.Fatalf("invalid config feedback: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"relays":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dataRoot, "kwakore")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(dataDir, "settings-overrides.json")
	if err := os.WriteFile(overridePath, []byte(`{"discover_on_user_relays":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureRunOutput([]string{"validate"}); err == nil || !strings.Contains(err.Error(), "settings-overrides.json") || !strings.Contains(err.Error(), "discover_on_user_relays") {
		t.Fatalf("invalid override feedback: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(`{"discover_on_user_relays":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := captureRunOutput([]string{"validate"}); err != nil || string(out) != "valid\n" {
		t.Fatalf("valid files: output=%q error=%v", out, err)
	}
}

func captureRunOutput(args []string) ([]byte, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	old := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	runErr := run(args)
	writer.Close()
	var buf bytes.Buffer
	_, copyErr := buf.ReadFrom(reader)
	reader.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	return buf.Bytes(), runErr
}
