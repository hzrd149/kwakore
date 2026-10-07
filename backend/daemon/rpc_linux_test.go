//go:build linux

package daemon

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"verdana/backend"
	"verdana/backend/controlprotocol"
	"verdana/backend/serviceconfig"
)

func TestRPCLinuxHostLaunch(t *testing.T) {
	paths := daemonPaths(t)
	key := nostr.MustSecretKeyFromHex(strings.Repeat("0", 63) + "1")
	napp := backend.Napp{D: "window", Name: "Window", Format: backend.FormatNapplet,
		Kind: backend.KindNapplet, Author: key.Public(), ArtifactHash: strings.Repeat("a", 64)}
	napp.ID = napp.Address()
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(backend.AppState{InstalledNapps: map[string]backend.Napp{napp.ID: napp}})
	if err := os.WriteFile(filepath.Join(paths.DataDir, "state.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(napp.ID))
	dir := filepath.Join(paths.DataDir, "napps", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>ready</p>"), 0600); err != nil {
		t.Fatal(err)
	}

	programDir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-window-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(programDir) })
	if err := os.Chmod(programDir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(programDir, "napplet")
	script := "#!/bin/sh\nsleep 0.2\nprintf '{\"t\":\"rpc\",\"id\":1,\"method\":\"nap.start\"}\\n'\nwhile IFS= read -r line; do case \"$line\" in *'\"t\":\"close\"'*) sleep 0.2; exit 0;; esac; done\n"
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(programDir, "libwebview.so"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":99")
	t.Setenv("WAYLAND_DISPLAY", "")
	oldProgram := windowProgramPath
	windowProgramPath = func() string { return program }
	t.Cleanup(func() { windowProgramPath = oldProgram })

	s, err := Open(paths, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: filepath.Join(runtimeDir, "kwakore", "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	result, rpcErr, _ := rpcCall(t, bufio.NewReader(conn), conn, "napplet.launch", `{"address":"`+napp.Address()+`"}`)
	if rpcErr != nil || !strings.Contains(string(result), `"outcome":"opened"`) || !strings.Contains(string(result), `"window_id":`) {
		t.Fatalf("launch response: %s %+v", result, rpcErr)
	}
	var opened backend.ServiceLaunchResult
	if err := json.Unmarshal(result, &opened); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = backend.ServiceStop(ctx, opened.WindowID)
	})
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("launch returned before host-page nap.start")
	}
	start = time.Now()
	stopped, rpcErr, _ := rpcCall(t, bufio.NewReader(conn), conn, "napplet.stop", `{"window_id":"`+opened.WindowID+`"}`)
	if rpcErr != nil || !strings.Contains(string(stopped), `"closed":true`) || time.Since(start) < 150*time.Millisecond {
		t.Fatalf("stop returned before child closed: %s %+v", stopped, rpcErr)
	}
	_, rpcErr, _ = rpcCall(t, bufio.NewReader(conn), conn, "napplet.stop", `{"window_id":"`+opened.WindowID+`"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.NotFound {
		t.Fatalf("second stop: %+v", rpcErr)
	}
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, rpcErr, _ = rpcCall(t, bufio.NewReader(conn), conn, "napplet.launch", `{"address":"`+napp.Address()+`"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.Unavailable || len(backend.OpenWindows()) != 0 {
		t.Fatalf("failed child left a window or returned success: %+v, windows=%v", rpcErr, backend.OpenWindows())
	}
	_, rpcErr, _ = rpcCall(t, bufio.NewReader(conn), conn, "napplet.launch", `{"address":"bad"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
		t.Fatalf("accepted bad address: %+v", rpcErr)
	}
	other := "35129:" + strings.Repeat("a", 64) + ":missing"
	_, rpcErr, _ = rpcCall(t, bufio.NewReader(conn), conn, "napplet.launch", `{"address":"`+other+`"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.NotFound {
		t.Fatalf("accepted uninstalled address: %+v", rpcErr)
	}
	t.Setenv("DISPLAY", "")
	_, rpcErr, raw := rpcCall(t, bufio.NewReader(conn), conn, "napplet.launch", `{"address":"`+napp.Address()+`"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.Unavailable || !strings.Contains(raw, `"reason":"session_unavailable"`) {
		t.Fatalf("headless launch: %s %+v", raw, rpcErr)
	}
}

func TestRPCWindowStop(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	_, rpcErr, _ := rpcCall(t, reader, conn, "napplet.stop", `{"window_id":"1"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.NotFound {
		t.Fatalf("unknown window stop: %+v", rpcErr)
	}
	for _, params := range []string{`{}`, `{"window_id":"x"}`, `{"window_id":"01"}`, `{"window_id":null}`, `{"window_id":"1","extra":true}`} {
		_, rpcErr, _ = rpcCall(t, reader, conn, "napplet.stop", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("accepted invalid stop %s: %+v", params, rpcErr)
		}
	}
}

func TestRPCSettingsMutateReload(t *testing.T) {
	s, reader, conn, paths := rpcService(t)
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"relays":["wss://file.example"],"blossom_servers":["https://file.example"],"discover_on_user_relays":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, rpcErr, _ := rpcCall(t, reader, conn, "settings.reload", "{}"); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	for _, tc := range []struct{ field, value string }{
		{"relays", `[]`}, {"blossom_servers", `[]`}, {"discover_on_user_relays", `false`},
	} {
		result, rpcErr, _ := rpcCall(t, reader, conn, "settings.set", `{"field":"`+tc.field+`","value":`+tc.value+`}`)
		if rpcErr != nil || !strings.Contains(string(result), `"settings":`) {
			t.Fatalf("set %s: %s %+v", tc.field, result, rpcErr)
		}
	}
	if got := s.Manager().Effective(); len(got.Relays) != 0 || len(got.BlossomServers) != 0 || got.DiscoverOnUserRelays {
		t.Fatalf("empty and false overrides lost: %+v", got)
	}
	data, err := os.ReadFile(paths.OverrideFile)
	var persisted struct {
		Relays   []string `json:"relays"`
		Discover bool     `json:"discover_on_user_relays"`
	}
	if err != nil || json.Unmarshal(data, &persisted) != nil || persisted.Relays == nil || len(persisted.Relays) != 0 || persisted.Discover {
		t.Fatalf("override not persisted atomically: %s %v", data, err)
	}
	for _, field := range []string{"relays", "blossom_servers", "discover_on_user_relays"} {
		if _, rpcErr, _ := rpcCall(t, reader, conn, "settings.clear", `{"field":"`+field+`"}`); rpcErr != nil {
			t.Fatalf("clear %s: %+v", field, rpcErr)
		}
		if _, rpcErr, _ := rpcCall(t, reader, conn, "settings.clear", `{"field":"`+field+`"}`); rpcErr != nil {
			t.Fatalf("no-op clear %s: %+v", field, rpcErr)
		}
	}
	if got := s.Manager().Effective(); !reflect.DeepEqual(got.Relays, []string{"wss://file.example"}) || !reflect.DeepEqual(got.BlossomServers, []string{"https://file.example"}) || !got.DiscoverOnUserRelays {
		t.Fatalf("file precedence lost: %+v", got)
	}
	secret := "secret-path.invalid"
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"relays":["wss://`+secret+`/?token=hidden"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, rpcErr, raw := rpcCall(t, reader, conn, "settings.reload", "{}")
	if rpcErr == nil || rpcErr.Code != controlprotocol.ConfigInvalid || strings.Contains(raw, secret) || strings.Contains(raw, paths.ConfigFile) {
		t.Fatalf("unsafe reload error: %s", raw)
	}
	if got := s.Manager().Effective(); !reflect.DeepEqual(got.Relays, []string{"wss://file.example"}) {
		t.Fatalf("failed reload changed settings: %+v", got)
	}
}

func TestRPCSettingsRejectsInvalidParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	for _, params := range []string{
		`{"field":"login","value":"secret"}`, `{"field":"relays","value":null}`,
		`{"field":"relays","value":false}`, `{"field":"relays","value":[12]}`,
		`{"field":"discover_on_user_relays","value":[]}`,
		`{"field":"relays","value":[],"extra":1}`,
		`{"field":"relays","field":"blossom_servers","value":[]}`,
	} {
		_, rpcErr, _ := rpcCall(t, reader, conn, "settings.set", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("accepted %s: %+v", params, rpcErr)
		}
	}
	for _, params := range []string{`{"field":"login"}`, `{"field":null}`, `{"field":"relays","extra":1}`, `{"field":"relays","field":"relays"}`} {
		_, rpcErr, _ := rpcCall(t, reader, conn, "settings.clear", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("accepted clear %s: %+v", params, rpcErr)
		}
	}
	if _, rpcErr, raw := rpcCall(t, reader, conn, "settings.set", `{"field":"relays","value":["wss://secret.invalid/"]}`); rpcErr == nil || rpcErr.Code != controlprotocol.ConfigInvalid || strings.Contains(raw, "secret.invalid") {
		t.Fatalf("unsafe validation error: %s", raw)
	}
}

func TestRPCSettingsNotificationChangesBackend(t *testing.T) {
	paths := daemonPaths(t)
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := serviceconfig.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	host := &settingsChangeHost{}
	closeBackend, err := backend.Start(backend.Options{DataDir: paths.DataDir, ServiceConfig: manager, Host: host})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend()
	service := &Service{manager: manager}
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := service.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: filepath.Join(runtimeDir, "kwakore", "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	frame := []byte(`{"jsonrpc":"2.0","method":"settings.set","params":{"field":"relays","value":["wss://changed.example"]}}` + "\n")
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	if _, rpcErr, raw := rpcCall(t, reader, conn, "settings.get", "{}"); rpcErr != nil || !strings.Contains(raw, "wss://changed.example") {
		t.Fatalf("notification did not change settings: %s %+v", raw, rpcErr)
	}
	if host.changes != 1 {
		t.Fatalf("notification produced %d backend changes", host.changes)
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
	if _, rpcErr, raw := rpcCall(t, reader, conn, "settings.get", "{}"); rpcErr != nil || !strings.Contains(raw, "wss://changed.example") || host.changes != 1 {
		t.Fatalf("no-op notification responded or notified: %s %d", raw, host.changes)
	}
}

func rpcService(t *testing.T) (*Service, *bufio.Reader, *net.UnixConn, serviceconfig.Paths) {
	t.Helper()
	paths := daemonPaths(t)
	service, err := Open(paths, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := service.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: filepath.Join(runtimeDir, "kwakore", "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return service, bufio.NewReader(conn), conn, paths
}

func rpcCall(t *testing.T, reader *bufio.Reader, conn *net.UnixConn, method, params string) (json.RawMessage, *controlprotocol.Error, string) {
	t.Helper()
	request := `{"jsonrpc":"2.0","method":"` + method + `","id":1`
	if params != "" {
		request += `,"params":` + params
	}
	request += "}\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result json.RawMessage        `json:"result"`
		Error  *controlprotocol.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		t.Fatal(err)
	}
	return response.Result, response.Error, line
}

func TestRPCReadLiveSafeDTO(t *testing.T) {
	s, reader, conn, paths := rpcService(t)
	if err := s.SetSetting("relays", []string{"wss://override.example"}); err != nil {
		t.Fatal(err)
	}
	secret := "private-token.example.invalid"
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"blossom_servers":["https://`+secret+`?token=hidden"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	result, rpcErr, raw := rpcCall(t, reader, conn, "service.diagnostics", "{}")
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	var diagnostics Diagnostics
	if err := json.Unmarshal(result, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if diagnostics.ObservedFrom != "live" || !diagnostics.Health.Ready || !reflect.DeepEqual(diagnostics.Settings.Relays, []string{"wss://override.example"}) || diagnostics.Warning == "" {
		t.Fatalf("not live: %+v", diagnostics)
	}
	if strings.Contains(raw, secret) || strings.Contains(raw, paths.ConfigFile) || strings.Contains(raw, "login") || strings.Contains(raw, "client_key") {
		t.Fatalf("unsafe diagnostics: %s", raw)
	}
	result, rpcErr, raw = rpcCall(t, reader, conn, "settings.get", "")
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	var effective serviceconfig.Effective
	if err := json.Unmarshal(result, &effective); err != nil || !reflect.DeepEqual(effective.Relays, []string{"wss://override.example"}) {
		t.Fatalf("settings: %s, %v", raw, err)
	}
	if strings.Contains(raw, "warning") || strings.Contains(raw, "observed_from") {
		t.Fatalf("get leaked diagnostics: %s", raw)
	}
}

func TestRPCReadRejectsParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	for _, method := range []string{"service.status", "service.diagnostics", "settings.get"} {
		for _, params := range []string{`{"extra":1}`, `{"extra":1,"extra":2}`, `[]`, `null`} {
			_, rpcErr, _ := rpcCall(t, reader, conn, method, params)
			if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
				t.Fatalf("%s %s: %+v", method, params, rpcErr)
			}
		}
	}
}

func TestRPCInstalledPageAndValidation(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.installed", `{}`)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	var page backend.ServicePage
	if err := json.Unmarshal(result, &page); err != nil || page.Items == nil || page.Total != 0 || page.NextOffset != nil {
		t.Fatalf("empty installed page: %s %v", raw, err)
	}
	for _, params := range []string{`{"offset":-1}`, `{"offset":1.5}`, `{"offset":"1"}`, `{"limit":0}`, `{"limit":501}`, `{"limit":null}`, `{"offset":1,"offset":2}`, `{"extra":1}`} {
		_, rpcErr, _ := rpcCall(t, reader, conn, "napplet.installed", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("accepted params %s: %+v", params, rpcErr)
		}
	}
}

func TestRPCDiscoveryCachedAndValidation(t *testing.T) {
	s, reader, conn, _ := rpcService(t)
	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.discover", `{}`)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	var page backend.ServiceDiscoveryPage
	if err := json.Unmarshal(result, &page); err != nil || page.Items == nil || page.Complete || page.FetchedAt != nil {
		t.Fatalf("uncached catalog: %s %v", raw, err)
	}
	for _, params := range []string{`{"query":null}`, `{"query":4}`, `{"refresh":null}`, `{"refresh":"true"}`, `{"limit":501}`, `{"offset":-1}`, `{"query":"x","query":"y"}`, `{"extra":1}`} {
		_, rpcErr, _ := rpcCall(t, reader, conn, "napplet.discover", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("accepted params %s: %+v", params, rpcErr)
		}
	}
	if err := s.SetSetting("relays", []string{}); err != nil {
		t.Fatal(err)
	}
	_, rpcErr, _ = rpcCall(t, reader, conn, "napplet.discover", `{"refresh":true}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.Unavailable {
		t.Fatalf("no relay refresh: %+v", rpcErr)
	}
}

func TestRPCInstallValidationAndFixedErrors(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	for _, params := range []string{`{}`, `{"address":null}`, `{"address":"bad"}`, `{"address":"bad","extra":1}`, `{"address":"a","address":"b"}`} {
		_, rpcErr, _ := rpcCall(t, reader, conn, "napplet.install", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("accepted %s: %+v", params, rpcErr)
		}
	}
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	_, rpcErr, raw := rpcCall(t, reader, conn, "napplet.install", `{"address":"`+address+`"}`)
	if rpcErr == nil || (rpcErr.Code != controlprotocol.NotFound && rpcErr.Code != controlprotocol.Unavailable) || strings.Contains(raw, "relay") {
		t.Fatalf("unsafe missing install: %s %+v", raw, rpcErr)
	}
}

func TestRPCUpdateValidationAndNotFound(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	_, rpcErr, _ := rpcCall(t, reader, conn, "napplet.update", `{"address":"bad"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
		t.Fatalf("invalid: %+v", rpcErr)
	}
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	_, rpcErr, _ = rpcCall(t, reader, conn, "napplet.update", `{"address":"`+address+`"}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.NotFound {
		t.Fatalf("missing: %+v", rpcErr)
	}
}

func TestRPCUninstallRequiresConfirmation(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	for _, params := range []string{`{"address":"` + address + `"}`, `{"address":"` + address + `","confirm":false}`, `{"address":"` + address + `","confirm":"true"}`} {
		_, rpcErr, _ := rpcCall(t, reader, conn, "napplet.uninstall", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.ConfirmationRequired {
			t.Fatalf("unconfirmed %s: %+v", params, rpcErr)
		}
	}
	_, rpcErr, _ := rpcCall(t, reader, conn, "napplet.uninstall", `{"address":"`+address+`","confirm":true}`)
	if rpcErr == nil || rpcErr.Code != controlprotocol.NotFound {
		t.Fatalf("confirmed missing: %+v", rpcErr)
	}
}

func TestRPCUninstallPartialCleanupWire(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	old := serviceUninstall
	serviceUninstall = func(_ context.Context, got string) (backend.ServiceUninstallResult, error) {
		if got != address {
			t.Errorf("address = %q", got)
		}
		return backend.ServiceUninstallResult{Address: got, RecordRemoved: true, CleanupComplete: false}, errors.Join(backend.ErrServicePartialCleanup, errors.New("private cleanup path /home/user/secret"))
	}
	t.Cleanup(func() { serviceUninstall = old })
	_, reader, conn, _ := rpcService(t)
	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.uninstall", `{"address":"`+address+`","confirm":true}`)
	if len(result) != 0 {
		t.Fatalf("error has result: %s", result)
	}
	if rpcErr == nil || rpcErr.Code != controlprotocol.PartialCleanup || rpcErr.Message != controlprotocol.FixedError(controlprotocol.PartialCleanup).Message {
		t.Fatalf("error: %s", raw)
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	var errorFields map[string]json.RawMessage
	if err := json.Unmarshal(response["error"], &errorFields); err != nil {
		t.Fatal(err)
	}
	want := `{"address":"` + address + `","record_removed":true,"cleanup_complete":false}`
	if string(errorFields["data"]) != want || len(errorFields) != 3 || len(response) != 3 {
		t.Fatalf("unsafe or incomplete wire response: %s", raw)
	}
	if strings.Contains(raw, "private") || strings.Contains(raw, "/home/user/secret") {
		t.Fatalf("internal text leaked: %s", raw)
	}
}
