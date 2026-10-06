//go:build linux

package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"verdana/backend/controlprotocol"
)

func TestSocketFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rpc.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		handleSocketConn(conn, func(method string, params json.RawMessage) (any, *controlprotocol.Error) {
			calls++
			return map[string]int{"count": calls}, nil
		})
	}()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { conn.Close(); <-done }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(conn)
	requests := []string{
		`{"jsonrpc":"2.0","method":"tick"}`,
		`[{"jsonrpc":"2.0","method":"tick"},{"jsonrpc":"2.0","method":"tick","id":7}]`,
		`{"jsonrpc":"2.0","method":"tick","id":8}`,
		`{"jsonrpc":`,
		`{"jsonrpc":"2.0","method":"tick","id":9}`,
	}
	for _, request := range requests {
		if _, err := conn.Write([]byte(request + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{`"id":7`, `"id":8`, `"code":-32700`, `"id":9`} {
		line, err := reader.ReadBytes('\n')
		if err != nil || !bytes.Contains(line, []byte(want)) {
			t.Fatalf("frame %s: %s, %v", want, line, err)
		}
	}
	if calls != 5 {
		t.Fatalf("notification was not dispatched: %d", calls)
	}
}

func TestSocketFramesOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rpc.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.AcceptUnix()
		if err == nil {
			handleSocketConn(conn, func(string, json.RawMessage) (any, *controlprotocol.Error) { return nil, nil })
		}
	}()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte(strings.Repeat("x", controlprotocol.MaxRequestLine+1) + "\n"))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil || !bytes.Contains(line, []byte(`"code":-32600`)) {
		t.Fatalf("oversize: %s, %v", line, err)
	}
}
