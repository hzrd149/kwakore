//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
	"verdana/backend/controlprotocol"
)

func run(args []string) error {
	if len(args) != 1 || args[0] != "status" {
		return errors.New("usage: kwakore status")
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" || !filepath.IsAbs(runtimeDir) {
		return errors.New("XDG_RUNTIME_DIR must name an absolute private 0700 directory")
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: filepath.Join(runtimeDir, "kwakore", "daemon.sock"), Net: "unix"})
	if err != nil {
		return errors.New("daemon unavailable")
	}
	defer conn.Close()
	uid, err := peerUID(conn)
	if err != nil || uid != uint32(os.Geteuid()) {
		return errors.New("unauthorized server")
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	request := []byte(`{"jsonrpc":"2.0","method":"service.status","id":1}` + "\n")
	if _, err := conn.Write(request); err != nil {
		return errors.New("daemon unavailable")
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return errors.New("daemon unavailable")
	}
	var response struct {
		JSONRPC string                 `json:"jsonrpc"`
		ID      json.RawMessage        `json:"id"`
		Result  json.RawMessage        `json:"result"`
		Error   *controlprotocol.Error `json:"error"`
	}
	if json.Unmarshal(line, &response) != nil || response.JSONRPC != "2.0" || !bytes.Equal(response.ID, []byte("1")) {
		return errors.New("invalid daemon response")
	}
	if response.Error != nil {
		return fmt.Errorf("daemon error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 {
		return errors.New("invalid daemon response")
	}
	_, err = os.Stdout.Write(append(response.Result, '\n'))
	return err
}

func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) { cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return cred.Uid, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}
