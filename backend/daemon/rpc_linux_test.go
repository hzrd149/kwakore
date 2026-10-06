//go:build linux

package daemon

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"verdana/backend/controlprotocol"
	"verdana/backend/serviceconfig"
)

func rpcService(t *testing.T) (*Service, *bufio.Reader, *net.UnixConn, serviceconfig.Paths) {
	t.Helper()
	paths := daemonPaths(t)
	service, err := Open(paths, "test")
	if err != nil { t.Fatal(err) }
	t.Cleanup(service.Close)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil { t.Fatal(err) }
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := service.Listen()
	if err != nil { t.Fatal(err) }
	t.Cleanup(func(){ _ = listener.Close() })
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: filepath.Join(runtimeDir, "kwakore", "daemon.sock"), Net: "unix"})
	if err != nil { t.Fatal(err) }
	t.Cleanup(func(){ _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5*time.Second))
	return service, bufio.NewReader(conn), conn, paths
}

func rpcCall(t *testing.T, reader *bufio.Reader, conn *net.UnixConn, method, params string) (json.RawMessage, *controlprotocol.Error, string) {
	t.Helper()
	request := `{"jsonrpc":"2.0","method":"` + method + `","id":1`
	if params != "" { request += `,"params":` + params }
	request += "}\n"
	if _, err := conn.Write([]byte(request)); err != nil { t.Fatal(err) }
	line, err := reader.ReadString('\n')
	if err != nil { t.Fatal(err) }
	var response struct { Result json.RawMessage `json:"result"`; Error *controlprotocol.Error `json:"error"` }
	if err := json.Unmarshal([]byte(line), &response); err != nil { t.Fatal(err) }
	return response.Result, response.Error, line
}

func TestRPCReadLiveSafeDTO(t *testing.T) {
	s, reader, conn, paths := rpcService(t)
	if err := s.SetSetting("relays", []string{"wss://override.example"}); err != nil { t.Fatal(err) }
	secret := "private-token.example.invalid"
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"blossom_servers":["https://`+secret+`?token=hidden"]}`), 0600); err != nil { t.Fatal(err) }
	if err := s.Reload(); err == nil { t.Fatal("invalid reload accepted") }
	result, rpcErr, raw := rpcCall(t, reader, conn, "service.diagnostics", "{}")
	if rpcErr != nil { t.Fatal(rpcErr) }
	var diagnostics Diagnostics
	if err := json.Unmarshal(result, &diagnostics); err != nil { t.Fatal(err) }
	if diagnostics.ObservedFrom != "live" || !diagnostics.Health.Ready || !reflect.DeepEqual(diagnostics.Settings.Relays, []string{"wss://override.example"}) || diagnostics.Warning == "" { t.Fatalf("not live: %+v", diagnostics) }
	if strings.Contains(raw, secret) || strings.Contains(raw, paths.ConfigFile) || strings.Contains(raw, "login") || strings.Contains(raw, "client_key") { t.Fatalf("unsafe diagnostics: %s", raw) }
	result, rpcErr, raw = rpcCall(t, reader, conn, "settings.get", "")
	if rpcErr != nil { t.Fatal(rpcErr) }
	var effective serviceconfig.Effective
	if err := json.Unmarshal(result, &effective); err != nil || !reflect.DeepEqual(effective.Relays, []string{"wss://override.example"}) { t.Fatalf("settings: %s, %v", raw, err) }
	if strings.Contains(raw, "warning") || strings.Contains(raw, "observed_from") { t.Fatalf("get leaked diagnostics: %s", raw) }
}

func TestRPCReadRejectsParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	for _, method := range []string{"service.status", "service.diagnostics", "settings.get"} {
		for _, params := range []string{`{"extra":1}`, `{"extra":1,"extra":2}`, `[]`, `null`} {
			_, rpcErr, _ := rpcCall(t, reader, conn, method, params)
			if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams { t.Fatalf("%s %s: %+v", method, params, rpcErr) }
		}
	}
}
