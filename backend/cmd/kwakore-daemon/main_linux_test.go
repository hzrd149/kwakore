//go:build linux

package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
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
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
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
	if mode := os.Getenv("KWAKORE_TEST_LEASE"); mode != "" {
		foregroundServiceHook = func(s *daemon.Service) {
			done, err := s.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(os.Getenv("KWAKORE_TEST_MARKER"), []byte("leased"), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "cooperative" {
				time.AfterFunc(time.Second, done)
			}
		}
	}
	if err := run(nil); err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("KWAKORE_FOREGROUND_CHECK_SOCKET"); path != "" {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("socket still exists before foreground exit: %v", err)
		}
	}
	os.Exit(0)
}

func TestForegroundSignalDeadline(t *testing.T) {
	testForegroundSignalLease(t, "held", 124)
}

func TestForegroundGracefulShutdown(t *testing.T) {
	testForegroundSignalLease(t, "cooperative", 0)
}

func TestForcedExitRestartRecovery(t *testing.T) {
	root := testForegroundSignalLease(t, "held", 124)
	dataDir := filepath.Join(root, "data", "kwakore")
	id := "interrupted-install"
	sum := sha256.Sum256([]byte(id))
	name := hex.EncodeToString(sum[:])
	base := filepath.Join(dataDir, "napps", name)
	intentDir := filepath.Join(dataDir, "mutations")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(intentDir, 0700); err != nil {
		t.Fatal(err)
	}
	intent, err := json.Marshal(map[string]any{"version": 1, "id": id, "operation": "install", "token": "1234567890abcdef1234567890abcdef", "new_event": "interrupted-event", "base": name})
	if err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(intentDir, name+".json")
	if err := os.WriteFile(intentPath, intent, 0600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(root, "runtime")
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1", "XDG_RUNTIME_DIR="+runtimeDir, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_DATA_HOME="+filepath.Join(root, "data"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			ready <- scan.Text()
		}
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, " ready ") {
			t.Fatalf("restart readiness: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("restart did not become ready")
	}
	if _, err := os.Stat(intentPath); !os.IsNotExist(err) {
		t.Fatalf("ready before intent recovery: %v", err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("ready before uncommitted install rollback: %v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("restart shutdown: %v: %s", err, stderr.String())
	}
}

func testForegroundSignalLease(t *testing.T, mode string, wantExit int) string {
	t.Helper()
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "leased")
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1", "KWAKORE_TEST_LEASE="+mode, "KWAKORE_TEST_MARKER="+marker, "XDG_RUNTIME_DIR="+runtimeDir, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_DATA_HOME="+filepath.Join(root, "data"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			ready <- scan.Text()
		}
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, " ready ") {
			t.Fatalf("readiness: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ready timeout")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("lease marker: %v", err)
	}
	dataDir := filepath.Join(root, "data", "kwakore")
	paths := serviceconfig.Paths{ConfigFile: filepath.Join(root, "config", "kwakore", "config.json"), DataDir: dataDir, OverrideFile: filepath.Join(dataDir, "settings-overrides.json")}
	if second, err := daemon.Open(paths, "test"); err == nil {
		second.Close()
		t.Fatal("lock released before child exit")
	}
	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err := <-finished:
		elapsed := time.Since(start)
		if wantExit == 0 && err != nil {
			t.Fatalf("graceful exit: %v: %s", err, stderr.String())
		}
		if wantExit != 0 {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != wantExit {
				t.Fatalf("exit = %v, want %d: %s", err, wantExit, stderr.String())
			}
			if !strings.HasSuffix(strings.TrimSpace(stderr.String()), "shutdown deadline exceeded") {
				t.Fatalf("deadline stderr: %q", stderr.String())
			}
			if elapsed < 5*time.Second || elapsed > 6*time.Second {
				t.Fatalf("deadline elapsed %v", elapsed)
			}
		}
	case <-time.After(7 * time.Second):
		t.Fatal("child missed shutdown deadline")
	}
	second, err := daemon.Open(paths, "test")
	if err != nil {
		t.Fatalf("post-exit lock/recovery: %v", err)
	}
	second.Close()
	return root
}

func TestForegroundClientParity(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1", "XDG_RUNTIME_DIR="+runtimeDir, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_DATA_HOME="+filepath.Join(root, "data"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			ready <- scan.Text()
		}
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, "ready") {
			t.Fatalf("readiness: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ready timeout")
	}
	cli := filepath.Join(root, "kwakore")
	build := exec.Command("go", "build", "-o", cli, "../kwakore")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	for _, tc := range []struct {
		name string
		args []string
		key  string
	}{
		{"status", []string{"status"}, "protocol_version"},
		{"diagnostics", []string{"diagnostics"}, "observed_from"},
		{"settings", []string{"settings", "get"}, "relays"},
		{"discover", []string{"discover", "--query", "test"}, "items"},
		{"installed", []string{"installed"}, "items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := exec.Command(cli, tc.args...)
			call.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir)
			var errout bytes.Buffer
			call.Stderr = &errout
			out, err := call.Output()
			if err != nil || errout.Len() != 0 || bytes.Count(out, []byte{'\n'}) != 1 {
				t.Fatalf("output=%q stderr=%q err=%v", out, errout.String(), err)
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(out, &result); err != nil || len(result[tc.key]) == 0 {
				t.Fatalf("result=%q err=%v", out, err)
			}
		})
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shutdown: %v stderr=%s", err, stderr.String())
	}
}

func TestForegroundSocketShutdown(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(runtimeDir, "kwakore", "daemon.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1", "KWAKORE_FOREGROUND_CHECK_SOCKET="+socketPath, "XDG_RUNTIME_DIR="+runtimeDir, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_DATA_HOME="+filepath.Join(root, "data"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan struct{}, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			ready <- struct{}{}
		}
	}()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("ready timeout")
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// An incomplete frame holds a foreground client read while SIGTERM closes it.
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","method":"napplet.discover","params":{"refresh":true},"id":9`)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shutdown with held call: %v stderr=%s", err, stderr.String())
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket remained: %v", err)
	}
}

func TestSocketStatusCLIEndToEnd(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	configRoot := filepath.Join(root, "config")
	dataRoot := filepath.Join(root, "data")
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1", "XDG_RUNTIME_DIR="+runtimeDir, "XDG_CONFIG_HOME="+configRoot, "XDG_DATA_HOME="+dataRoot)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		if scan.Scan() {
			ready <- scan.Text()
		}
	}()
	select {
	case line := <-ready:
		if !strings.Contains(line, "ready") {
			t.Fatalf("unexpected readiness: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ready timeout")
	}
	conn, err := net.Dial("unix", filepath.Join(runtimeDir, "kwakore", "daemon.sock"))
	if err != nil {
		t.Fatalf("socket not available after ready: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 2; i++ {
		if _, err := conn.Write([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"service.status\",\"id\":7}\n")); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(line, &response); err != nil || response.ID != 7 || len(response.Result) == 0 {
			t.Fatalf("response: %s, %v", line, err)
		}
	}
	cli := filepath.Join(root, "kwakore")
	build := exec.Command("go", "build", "-o", cli, "../kwakore")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	status := exec.Command(cli, "status")
	status.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir)
	out, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI status: %v: %s", err, out)
	}
	var result struct {
		ProtocolVersion int `json:"protocol_version"`
		Health          struct {
			Ready bool `json:"ready"`
		} `json:"health"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.ProtocolVersion != 1 || !result.Health.Ready {
		t.Fatalf("status: %s, %v", out, err)
	}
}

func TestForegroundSIGHUPReloadsAndSanitizesWarning(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	dataRoot := filepath.Join(root, "data")
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configRoot, "kwakore", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundHelper$")
	cmd.Env = append(os.Environ(), "KWAKORE_FOREGROUND_HELPER=1", "XDG_CONFIG_HOME="+configRoot, "XDG_DATA_HOME="+dataRoot, "XDG_RUNTIME_DIR="+runtimeDir)
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
			Warning        *string  `json:"warning"`
			RecentErrors   *[]any   `json:"recent_errors"`
		}
		if err := json.Unmarshal(out, &report); err != nil {
			t.Fatalf("%s output %q: %v", command, out, err)
		}
		if report.ObservedFrom != "files" || report.Ready != nil || report.UptimeSeconds != nil || report.ActiveWindows != nil || report.Warning != nil || report.RecentErrors != nil {
			t.Fatalf("%s claimed live state: %s", command, out)
		}
		if bytes.Contains(out, []byte(configRoot)) || bytes.Contains(out, []byte(dataRoot)) {
			t.Fatalf("%s leaked private path: %s", command, out)
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
	configPath := filepath.Join(configRoot, "kwakore", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"relays":["wss://file.example"],"discover_on_user_relays":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "settings-overrides.json"), []byte(`{"relays":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	check("status", "valid", "valid", "private")
	out, err := captureRunOutput([]string{"diagnostics"})
	if err != nil {
		t.Fatal(err)
	}
	var effective struct {
		Settings serviceconfig.Effective `json:"settings"`
	}
	if err := json.Unmarshal(out, &effective); err != nil {
		t.Fatal(err)
	}
	if len(effective.Settings.Relays) != 0 || effective.Settings.DiscoverOnUserRelays || len(effective.Settings.BlossomServers) != len(serviceconfig.Defaults().BlossomServers) {
		t.Fatalf("wrong effective settings: %s", out)
	}
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
