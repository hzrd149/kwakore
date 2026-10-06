//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
