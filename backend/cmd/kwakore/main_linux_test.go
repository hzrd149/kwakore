//go:build linux

package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
