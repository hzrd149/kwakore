//go:build linux

package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"verdana/backend/controlprotocol"
)

const socketName = "daemon.sock"

type Listener struct {
	server      *net.UnixListener
	path        string
	inode       os.FileInfo
	closed      chan struct{}
	once        sync.Once
	connections chan struct{}
	clients     map[*net.UnixConn]struct{}
	mu          sync.Mutex
	work        sync.WaitGroup
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
	l := &Listener{server: server, path: path, inode: inode, closed: make(chan struct{}), connections: make(chan struct{}, 64), clients: make(map[*net.UnixConn]struct{})}
	go l.serve(s)
	return l, nil
}

func (l *Listener) Close() error {
	var err error
	l.once.Do(func() {
		err = l.server.Close()
		<-l.closed
		l.mu.Lock()
		for conn := range l.clients {
			_ = conn.Close()
		}
		l.mu.Unlock()
		l.work.Wait()
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
		select {
		case l.connections <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		l.mu.Lock()
		l.clients[conn] = struct{}{}
		l.work.Add(1)
		l.mu.Unlock()
		go func() {
			defer func() {
				l.mu.Lock()
				delete(l.clients, conn)
				l.mu.Unlock()
				<-l.connections
				l.work.Done()
			}()
			handleSocketConn(conn, func(method string, params json.RawMessage) (any, *controlprotocol.Error) {
				if method != "service.status" {
					return nil, controlprotocol.FixedError(controlprotocol.MethodNotFound)
				}
				if rpcErr := controlprotocol.ValidateNamedParams(params); rpcErr != nil {
					return nil, rpcErr
				}
				return struct {
					ProtocolVersion int    `json:"protocol_version"`
					Health          Health `json:"health"`
				}{controlprotocol.Version, s.Health()}, nil
			})
		}()
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
		line, err := readFrame(reader)
		if errors.Is(err, errFrameTooLarge) {
			writeResponse(conn, controlprotocol.ProcessFrame(bytes.Repeat([]byte{' '}, controlprotocol.MaxRequestLine+1), dispatch))
			return
		}
		if err != nil {
			return
		}
		response := controlprotocol.ProcessFrame(line, dispatch)
		if response == nil {
			continue
		}
		if err := writeResponse(conn, response); err != nil {
			return
		}
	}
}

var errFrameTooLarge = errors.New("request frame too large")

func readFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(frame)+len(part) > controlprotocol.MaxRequestLine+1 {
			return nil, errFrameTooLarge
		}
		frame = append(frame, part...)
		if err == nil {
			return bytes.TrimSuffix(frame, []byte{'\n'}), nil
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
	}
}

func writeResponse(conn *net.UnixConn, response []byte) error {
	if response == nil {
		return nil
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err := io.Copy(conn, bytes.NewReader(append(response, '\n')))
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
