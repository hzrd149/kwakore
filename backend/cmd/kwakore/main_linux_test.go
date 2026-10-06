//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"verdana/backend/controlprotocol"
)

func TestCLISettingsCommands(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, tc := range []struct {
		args           []string
		method, params string
	}{
		{[]string{"settings", "reload"}, "settings.reload", ""},
		{[]string{"settings", "set", "relays", `[]`}, "settings.set", `{"field":"relays","value":[]}`},
		{[]string{"settings", "set", "blossom_servers", `[]`}, "settings.set", `{"field":"blossom_servers","value":[]}`},
		{[]string{"settings", "set", "discover_on_user_relays", `false`}, "settings.set", `{"field":"discover_on_user_relays","value":false}`},
		{[]string{"settings", "clear", "relays"}, "settings.clear", `{"field":"relays"}`},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			seen := make(chan string, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- err.Error()
					return
				}
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					seen <- err.Error()
					return
				}
				var request struct {
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				_ = json.Unmarshal(line, &request)
				seen <- request.Method + " " + string(request.Params)
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"settings":{"relays":[],"blossom_servers":[],"discover_on_user_relays":false}}}` + "\n"))
			}()
			old := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			callErr := run(tc.args)
			_ = w.Close()
			os.Stdout = old
			out, _ := io.ReadAll(r)
			_ = r.Close()
			if callErr != nil || !strings.Contains(string(out), `"settings":`) {
				t.Fatalf("run: %s %v", out, callErr)
			}
			want := tc.method + " " + tc.params
			if got := <-seen; got != want {
				t.Fatalf("request %q, want %q", got, want)
			}
		})
	}
}

func TestCLIContract(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "kwakore")
	build := exec.Command("go", "build", "-o", cli, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	for _, tc := range []struct {
		name           string
		args           []string
		method, params string
	}{
		{"status", []string{"status"}, "service.status", ""},
		{"diagnostics", []string{"diagnostics"}, "service.diagnostics", ""},
		{"get", []string{"settings", "get"}, "settings.get", ""},
		{"reload", []string{"settings", "reload"}, "settings.reload", ""},
		{"set", []string{"settings", "set", "relays", `[]`}, "settings.set", `{"field":"relays","value":[]}`},
		{"clear", []string{"settings", "clear", "relays"}, "settings.clear", `{"field":"relays"}`},
		{"discover", []string{"discover", "--query", "hello", "--refresh", "--offset", "2", "--limit", "3"}, "napplet.discover", `{"query":"hello","refresh":true,"offset":2,"limit":3}`},
		{"installed", []string{"installed", "--offset", "2", "--limit", "3"}, "napplet.installed", `{"offset":2,"limit":3}`},
		{"install", []string{"install", address}, "napplet.install", `{"address":"` + address + `"}`},
		{"update", []string{"update", address}, "napplet.update", `{"address":"` + address + `"}`},
		{"uninstall", []string{"uninstall", "--yes", address}, "napplet.uninstall", `{"address":"` + address + `","confirm":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan []byte, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- nil
					return
				}
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadBytes('\n')
				seen <- line
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"method":"` + tc.method + `"}}` + "\n"))
			}()
			cmd := exec.Command(cli, append([]string{"--socket", listener.Addr().String()}, tc.args...)...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil || stderr.Len() != 0 || string(out) != `{"method":"`+tc.method+`"}`+"\n" {
				t.Fatalf("out=%q stderr=%q err=%v", out, stderr.String(), err)
			}
			var request controlprotocol.Request
			if err := json.Unmarshal(<-seen, &request); err != nil || request.JSONRPC != "2.0" || string(request.ID) != "1" || request.Method != tc.method || string(request.Params) != tc.params {
				t.Fatalf("request=%+v, err=%v", request, err)
			}
		})
	}
	for _, tc := range []struct{ name, response string }{
		{"wrong ID", `{"jsonrpc":"2.0","id":2,"result":true}`},
		{"missing version", `{"id":1,"result":true}`},
		{"both fields", `{"jsonrpc":"2.0","id":1,"result":true,"error":{"code":1004,"message":"Unavailable"}}`},
		{"malformed", `{`},
		{"remote error", `{"jsonrpc":"2.0","id":1,"error":{"code":1006,"message":"secret"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = bufio.NewReader(conn).ReadBytes('\n')
				_, _ = conn.Write([]byte(tc.response + "\n"))
			}()
			cmd := exec.Command(cli, "--socket", listener.Addr().String(), "status")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err == nil || stdout.Len() != 0 {
				t.Fatalf("accepted response: %q %v", stdout.String(), err)
			}
			var got struct {
				Error controlprotocol.Error `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &got); err != nil || got.Error.Code == 0 || bytes.Contains(stderr.Bytes(), []byte("secret")) || bytes.Count(stderr.Bytes(), []byte{'\n'}) != 1 {
				t.Fatalf("stderr=%q err=%v", stderr.String(), err)
			}
		})
	}
	for _, args := range [][]string{{"--timeout", "0s", "status"}, {"--timeout", "garbage", "status"}, {"status"}} {
		cmd := exec.Command(cli, args...)
		cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR=")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err == nil || stdout.Len() != 0 {
			t.Fatalf("accepted %v: %v", args, err)
		}
		var got struct {
			Error controlprotocol.Error `json:"error"`
		}
		if err := json.Unmarshal(stderr.Bytes(), &got); err != nil || got.Error.Code == 0 {
			t.Fatalf("stderr=%q err=%v", stderr.String(), err)
		}
	}
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		time.Sleep(100 * time.Millisecond)
	}()
	cmd := exec.Command(cli, "--socket", listener.Addr().String(), "--timeout", "10ms", "install", address)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil || stdout.Len() != 0 {
		t.Fatalf("timeout accepted: %v, %q", err, stdout.String())
	}
	var timed struct {
		Error controlprotocol.Error `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &timed); err != nil || timed.Error.Code != controlprotocol.Timeout || !strings.Contains(strings.ToLower(timed.Error.Message), "unknown") || !strings.Contains(strings.ToLower(timed.Error.Message), "installed") {
		t.Fatalf("timeout stderr=%q err=%v", stderr.String(), err)
	}
}

func TestCLIContractPeerUIDOverride(t *testing.T) {
	root := t.TempDir()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	old := serverPeerUID
	serverPeerUID = func(*net.UnixConn) (uint32, error) { return uint32(os.Geteuid() + 1), nil }
	defer func() { serverPeerUID = old }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err == nil {
			defer conn.Close()
		}
	}()
	if err := run([]string{"--socket", listener.Addr().String(), "status"}); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("foreign peer accepted: %v", err)
	}
	<-done
}

func TestCLIContractCatalog(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	commands := [][]string{
		{"status"}, {"diagnostics"}, {"settings", "get"}, {"settings", "reload"},
		{"settings", "set", "relays", `[]`}, {"settings", "clear", "relays"},
		{"discover"}, {"installed"}, {"install", address}, {"update", address},
		{"uninstall", "--yes", address},
	}
	methods := make([]string, 0, len(commands))
	for _, args := range commands {
		method, _, _, err := command(args)
		if err != nil {
			t.Fatalf("command %v: %v", args, err)
		}
		methods = append(methods, method)
	}
	catalog := controlprotocol.MethodNames()
	slices.Sort(methods)
	slices.Sort(catalog)
	if !slices.Equal(methods, catalog) {
		t.Fatalf("CLI methods %v differ from protocol catalog %v", methods, catalog)
	}
}

func TestCLIInstalledCommand(t *testing.T) {
	method, params, _, err := command([]string{"installed", "--offset", "2", "--limit", "50"})
	if err != nil || method != "napplet.installed" || string(params) != `{"offset":2,"limit":50}` {
		t.Fatalf("installed command: %s %s %v", method, params, err)
	}
	for _, args := range [][]string{{"installed", "--offset", "-1"}, {"installed", "--limit", "501"}, {"installed", "--limit", "1.5"}, {"installed", "extra"}} {
		if _, _, _, err := command(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestCLIInstallCommand(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	method, params, _, err := command([]string{"install", address})
	if err != nil || method != "napplet.install" || string(params) != `{"address":"`+address+`"}` {
		t.Fatalf("install: %s %s %v", method, params, err)
	}
	if _, _, _, err := command([]string{"install"}); err == nil {
		t.Fatal("missing address accepted")
	}
}

func TestCLIUpdateCommand(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	method, params, _, err := command([]string{"update", address})
	if err != nil || method != "napplet.update" || string(params) != `{"address":"`+address+`"}` {
		t.Fatalf("update: %s %s %v", method, params, err)
	}
	if _, _, _, err := command([]string{"update"}); err == nil {
		t.Fatal("missing address accepted")
	}
}

func TestCLIUninstallRequiresYes(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	if _, _, _, err := command([]string{"uninstall", address}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unconfirmed: %v", err)
	}
	method, params, _, err := command([]string{"uninstall", "--yes", address})
	if err != nil || method != "napplet.uninstall" || string(params) != `{"address":"`+address+`","confirm":true}` {
		t.Fatalf("confirmed: %s %s %v", method, params, err)
	}
}

func TestCLIDiscoveryCommand(t *testing.T) {
	method, params, _, err := command([]string{"discover", "--query", "hello", "--refresh", "--offset", "2", "--limit", "50"})
	if err != nil || method != "napplet.discover" || string(params) != `{"query":"hello","refresh":true,"offset":2,"limit":50}` {
		t.Fatalf("discover command: %s %s %v", method, params, err)
	}
	for _, args := range [][]string{{"discover", "--offset", "-1"}, {"discover", "--limit", "501"}, {"discover", "--refresh=maybe"}, {"discover", "extra"}} {
		if _, _, _, err := command(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestCLISettingsStructuredErrors(t *testing.T) {
	for _, args := range [][]string{
		{"settings", "set", "relays", `null`},
		{"settings", "set", "discover_on_user_relays", `[]`},
		{"settings", "set", "blossom_servers", `[true]`},
	} {
		err := run(args)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
		var out bytes.Buffer
		writeCLIError(&out, err)
		if !strings.Contains(out.String(), `"code":-32602`) {
			t.Fatalf("error %v: %s", args, out.String())
		}
	}
}

func TestCLISettingsRemoteErrorIsFixed(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":1006,"message":"private-url.invalid/secret"}}` + "\n"))
	}()
	err = run([]string{"settings", "reload"})
	<-done
	if err == nil {
		t.Fatal("remote error accepted")
	}
	var out bytes.Buffer
	writeCLIError(&out, err)
	if !strings.Contains(out.String(), `"code":1006`) || strings.Contains(out.String(), "private-url.invalid") {
		t.Fatalf("unsafe error: %s", out.String())
	}
}

func TestCLIReadMethods(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, tc := range []struct {
		args           []string
		method, result string
	}{
		{[]string{"diagnostics"}, "service.diagnostics", `{"observed_from":"live","warning":"safe"}`},
		{[]string{"settings", "get"}, "settings.get", `{"relays":[],"blossom_servers":[],"discover_on_user_relays":false}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			seen := make(chan string, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- err.Error()
					return
				}
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					seen <- err.Error()
					return
				}
				var request struct {
					Method string `json:"method"`
				}
				_ = json.Unmarshal(line, &request)
				seen <- request.Method
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + tc.result + "}\n"))
			}()
			old := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			callErr := run(tc.args)
			_ = w.Close()
			os.Stdout = old
			out, _ := io.ReadAll(r)
			_ = r.Close()
			if callErr != nil || string(out) != tc.result+"\n" || <-seen != tc.method {
				t.Fatalf("run %v: output=%s err=%v", tc.args, out, callErr)
			}
		})
	}
}

func TestStatusRejectsMismatchedResponseID(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		request := make([]byte, 256)
		_, _ = conn.Read(request)
		_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"protocol_version":1}}` + "\n"))
	}()
	if err := run([]string{"status"}); err == nil || !strings.Contains(err.Error(), "invalid daemon response") {
		t.Fatalf("mismatched ID: %v", err)
	}
	<-done
}
