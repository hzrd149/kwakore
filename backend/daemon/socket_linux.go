//go:build linux

package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"verdana/backend/controlprotocol"
)

const socketName = "daemon.sock"

type Listener struct {
	server *net.UnixListener
	path   string
	inode  os.FileInfo
	closed chan struct{}
	once   sync.Once
}

func SocketPath() (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" || !filepath.IsAbs(runtimeDir) {
		return "", errors.New("XDG_RUNTIME_DIR must name an absolute private 0700 directory")
	}
	return filepath.Join(runtimeDir, "kwakore", socketName), nil
}

func (s *Service) Listen() (*Listener, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create private runtime directory: %w", err)
	}
	server, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on private runtime socket: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		server.Close()
		return nil, err
	}
	inode, err := os.Lstat(path)
	if err != nil {
		server.Close()
		return nil, err
	}
	l := &Listener{server: server, path: path, inode: inode, closed: make(chan struct{})}
	go l.serve(s)
	return l, nil
}

func (l *Listener) Close() error {
	var err error
	l.once.Do(func() {
		err = l.server.Close()
		<-l.closed
		if info, e := os.Lstat(l.path); e == nil && os.SameFile(info, l.inode) {
			_ = os.Remove(l.path)
		}
	})
	return err
}

func (l *Listener) serve(s *Service) {
	defer close(l.closed)
	for {
		conn, err := l.server.AcceptUnix()
		if err != nil {
			return
		}
		go handleSocketConn(conn, func(method string, params json.RawMessage) (any, *controlprotocol.Error) {
			if method != "service.status" {
				return nil, controlprotocol.FixedError(controlprotocol.MethodNotFound)
			}
			if len(params) > 0 && !bytes.Equal(params, []byte("{}")) {
				return nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
			}
			return struct {
				ProtocolVersion int    `json:"protocol_version"`
				Health          Health `json:"health"`
			}{controlprotocol.Version, s.Health()}, nil
		})
	}
}

func handleSocketConn(conn *net.UnixConn, dispatch controlprotocol.Dispatch) {
	defer conn.Close()
	uid, err := peerUID(conn)
	if err != nil || uid != uint32(os.Geteuid()) {
		_, _ = conn.Write(append(controlprotocol.ProcessFrame([]byte(`{"jsonrpc":"2.0","method":"invalid","id":null}`), func(string, json.RawMessage) (any, *controlprotocol.Error) {
			return nil, controlprotocol.FixedError(controlprotocol.Unauthorized)
		}), '\n'))
		return
	}
	reader := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		response := controlprotocol.ProcessFrame(bytes.TrimSuffix(line, []byte{'\n'}), dispatch)
		if response == nil {
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := conn.Write(append(response, '\n')); err != nil {
			return
		}
	}
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

var _ = syscall.Stat_t{}
