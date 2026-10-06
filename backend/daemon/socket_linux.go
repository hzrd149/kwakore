//go:build linux

package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

var runtimeUID = func() uint32 { return uint32(os.Geteuid()) }
var socketPeerUID = peerUID

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
	if len(path) >= 108 {
		return nil, errors.New("XDG_RUNTIME_DIR path is too long for a Unix socket")
	}
	runtimeDir := filepath.Dir(filepath.Dir(path))
	if err := checkRuntimePath(runtimeDir); err != nil {
		return nil, err
	}
	child := filepath.Dir(path)
	if err := os.Mkdir(child, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, errors.New("create XDG_RUNTIME_DIR/kwakore as a private 0700 directory")
	}
	if err := checkPrivateRuntimeDir(child); err != nil {
		return nil, errors.New("XDG_RUNTIME_DIR/kwakore must be a real current-user-owned 0700 directory")
	}
	if err := checkExistingSocket(path); err != nil {
		return nil, err
	}
	server, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, errors.New("cannot bind private runtime socket")
	}
	server.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0600); err != nil {
		_ = server.Close()
		return nil, errors.New("cannot secure runtime socket")
	}
	inode, err := os.Lstat(path)
	if err != nil {
		_ = server.Close()
		return nil, errors.New("cannot inspect runtime socket")
	}
	if inode.Mode()&os.ModeSocket == 0 {
		_ = server.Close()
		return nil, errors.New("runtime socket inode changed unexpectedly")
	}
	l := &Listener{server: server, path: path, inode: inode, closed: make(chan struct{}), connections: make(chan struct{}, 64), clients: make(map[*net.UnixConn]struct{})}
	go l.serve(s)
	return l, nil
}

func checkRuntimePath(path string) error {
	current := string(filepath.Separator)
	for _, part := range splitPath(path) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("XDG_RUNTIME_DIR must be a real current-user-owned 0700 directory; create or fix it before starting the daemon")
		}
	}
	return checkPrivateRuntimeDir(path)
}

func checkPrivateRuntimeDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("XDG_RUNTIME_DIR must be a real current-user-owned 0700 directory; create or fix it before starting the daemon")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != runtimeUID() {
		return errors.New("XDG_RUNTIME_DIR must be owned by the current user")
	}
	return nil
}

func checkExistingSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot inspect existing runtime socket")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSocket == 0 || !ok || owner.Uid != runtimeUID() {
		return errors.New("runtime socket path is not an owned socket")
	}
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return errors.New("daemon socket is already active")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return errors.New("cannot verify runtime socket is stale")
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		return errors.New("runtime socket inode changed unexpectedly")
	}
	if err := os.Remove(path); err != nil {
		return errors.New("cannot remove stale runtime socket")
	}
	return nil
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
	uid, err := socketPeerUID(conn)
	if err != nil || uid != runtimeUID() {
		_ = writeResponse(conn, controlprotocol.ErrorResponse(controlprotocol.Unauthorized, nil))
		return
	}
	reader := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		line, err := readFrame(reader)
		if errors.Is(err, errFrameTooLarge) {
			_ = writeResponse(conn, controlprotocol.ErrorResponse(controlprotocol.InvalidRequest, nil))
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
