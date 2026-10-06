//go:build linux

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
