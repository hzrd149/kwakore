//go:build linux

package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"verdana/backend/controlprotocol"
)

func TestSocketPeerDisconnectCancelsWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go watchSocketPeer(server, ctx, cancel)
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("write half-close canceled a live response reader")
	case <-time.After(250 * time.Millisecond):
	}
	client.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("peer close did not cancel request context")
	}
}

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

func TestSocketAccessRuntimeValidation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := (&Service{}).Listen(); err == nil {
		t.Fatal("missing runtime directory accepted")
	}
	t.Setenv("XDG_RUNTIME_DIR", "relative")
	if _, err := (&Service{}).Listen(); err == nil {
		t.Fatal("relative runtime directory accepted")
	}
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	if _, err := (&Service{}).Listen(); err == nil {
		t.Fatal("public runtime directory accepted")
	}
	if err := os.Chmod(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	previousUID := runtimeUID
	runtimeUID = func() uint32 { return previousUID() + 1 }
	if _, err := (&Service{}).Listen(); err == nil {
		t.Fatal("wrong-owner runtime directory accepted")
	}
	runtimeUID = previousUID
	if err := os.Symlink(runtimeDir, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "link"))
	if _, err := (&Service{}).Listen(); err == nil {
		t.Fatal("symlinked runtime directory accepted")
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	if err := os.Symlink(filepath.Join(root, "other"), filepath.Join(runtimeDir, "kwakore")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Service{}).Listen(); err == nil {
		t.Fatal("symlinked child accepted")
	}
}

func TestSocketAccessRejectsForeignPeerBeforeDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rpc.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	previous := socketPeerUID
	socketPeerUID = func(*net.UnixConn) (uint32, error) { return runtimeUID() + 1, nil }
	defer func() { socketPeerUID = previous }()
	var called atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err == nil {
			handleSocketConn(conn, func(string, json.RawMessage) (any, *controlprotocol.Error) { called.Add(1); return nil, nil })
		}
	}()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil || !bytes.Contains(line, []byte(`"code":1001`)) || !bytes.Contains(line, []byte(`"id":null`)) {
		t.Fatalf("foreign peer: %s, %v", line, err)
	}
	<-done
	if called.Load() != 0 {
		t.Fatal("foreign peer reached dispatcher")
	}
}

func TestSocketClosePreservesUnexpectedInode(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	s := &Service{}
	listener, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runtimeDir, "kwakore", "daemon.sock")
	if info, err := os.Lstat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("socket must be 0600: %v, %v", info, err)
	}
	if info, err := os.Lstat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("runtime child must be 0700: %v, %v", info, err)
	}
	if _, err := s.Listen(); err == nil {
		t.Fatal("second listener replaced active socket")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "replacement" {
		t.Fatalf("replacement removed: %q, %v", got, err)
	}
}

func TestSocketAccessReplacesOwnedStaleSocket(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	path := filepath.Join(child, "daemon.sock")
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := (&Service{}).Listen()
	if err != nil {
		t.Fatalf("owned stale socket was not replaced: %v", err)
	}
	defer listener.Close()
}
