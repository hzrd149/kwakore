//go:build linux

package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"verdana/backend/controlprotocol"
)

const socketName = "daemon.sock"

var runtimeUID = func() uint32 { return uint32(os.Geteuid()) }
var socketPeerUID = peerUID

// activationFD is the first descriptor systemd passes to an activated service
// (SD_LISTEN_FDS_START). Tests point it at a descriptor they own.
var activationFD = 3

// activationEnv names the sd_listen_fds(3) variables. Any one of them being
// present means the process was started by socket activation; a malformed or
// partial set is refused rather than treated as a direct foreground start.
var activationEnv = []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}

type Listener struct {
	server *net.UnixListener
	path   string
	inode  os.FileInfo
	// owned is true only when this process bound the socket itself. An
	// inherited listener belongs to the user manager, which keeps listening
	// after the daemon exits, so Close must never unlink its inode.
	owned       bool
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

// Listen serves the control socket. Under systemd socket activation it adopts
// the single inherited listener after validating it; otherwise it binds the
// socket directly. Activation metadata that fails validation is an error and
// never falls back to binding a second socket.
func (s *Service) Listen() (*Listener, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	if len(path) >= 108 {
		return nil, errors.New("XDG_RUNTIME_DIR path is too long for a Unix socket")
	}
	var l *Listener
	if activationRequested() {
		l, err = adoptActivatedSocket(path)
	} else {
		l, err = bindDirectSocket(path)
	}
	if err != nil {
		return nil, err
	}
	l.closed = make(chan struct{})
	l.connections = make(chan struct{}, 64)
	l.clients = make(map[*net.UnixConn]struct{})
	go l.serve(s)
	return l, nil
}

func activationRequested() bool {
	for _, name := range activationEnv {
		if _, ok := os.LookupEnv(name); ok {
			return true
		}
	}
	return false
}

// bindDirectSocket is the foreground path: it creates the private runtime child
// and binds an owner-only socket that this process owns and unlinks on close.
func bindDirectSocket(path string) (*Listener, error) {
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
	return &Listener{server: server, path: path, inode: inode, owned: true}, nil
}

var errActivation = errors.New("systemd socket activation metadata is invalid; refusing to serve or bind another socket")

// adoptActivatedSocket validates the sd_listen_fds(3) contract and the passed
// descriptor before serving it. It requires LISTEN_PID to name this process,
// exactly one descriptor at activationFD, and a listening Unix stream socket
// bound at path, created by the current user, whose inode is 0600 inside a
// real 0700 current-user-owned directory. The activation variables are removed
// from the environment in every case so napplet children never inherit them.
func adoptActivatedSocket(path string) (*Listener, error) {
	pid, hasPID := os.LookupEnv("LISTEN_PID")
	fds, hasFDs := os.LookupEnv("LISTEN_FDS")
	names, hasNames := os.LookupEnv("LISTEN_FDNAMES")
	for _, name := range activationEnv {
		_ = os.Unsetenv(name)
	}
	if !hasPID || !hasFDs || pid != strconv.Itoa(os.Getpid()) || fds != "1" || (hasNames && (names == "" || strings.Contains(names, ":"))) {
		return nil, errActivation
	}
	fd := activationFD
	if err := checkActivatedDescriptor(fd, path); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "systemd-socket")
	// FileListener duplicates the descriptor with close-on-exec; closing the
	// original keeps the inherited number out of napplet children.
	ln, err := net.FileListener(file)
	_ = file.Close()
	if err != nil {
		return nil, errActivation
	}
	server, ok := ln.(*net.UnixListener)
	if !ok {
		_ = ln.Close()
		return nil, errActivation
	}
	server.SetUnlinkOnClose(false)
	return &Listener{server: server, path: path, owned: false}, nil
}

func checkActivatedDescriptor(fd int, path string) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return errors.New("inherited descriptor is not a socket")
	}
	domain, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_DOMAIN)
	if err != nil || domain != unix.AF_UNIX {
		return errors.New("inherited descriptor is not a Unix socket")
	}
	kind, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_STREAM {
		return errors.New("inherited descriptor is not a stream socket")
	}
	listening, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ACCEPTCONN)
	if err != nil || listening != 1 {
		return errors.New("inherited descriptor is not listening")
	}
	sa, err := unix.Getsockname(fd)
	addr, ok := sa.(*unix.SockaddrUnix)
	if err != nil || !ok || addr.Name != path {
		return errors.New("inherited socket is not bound at XDG_RUNTIME_DIR/kwakore/daemon.sock")
	}
	// A listening Unix socket reports the credentials captured at listen().
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || cred.Uid != runtimeUID() {
		return errors.New("inherited socket was not created by the current user")
	}
	if err := checkRuntimePath(filepath.Dir(filepath.Dir(path))); err != nil {
		return err
	}
	if err := checkPrivateRuntimeDir(filepath.Dir(path)); err != nil {
		return errors.New("XDG_RUNTIME_DIR/kwakore must be a real current-user-owned 0700 directory; set DirectoryMode=0700 on the socket unit")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return errors.New("inherited socket path is not a socket")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != runtimeUID() || info.Mode().Perm() != 0600 {
		return errors.New("inherited socket must be a current-user-owned 0600 inode; set SocketMode=0600 on the socket unit")
	}
	return nil
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
		if !l.owned {
			return
		}
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
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go watchSocketPeer(conn, ctx, cancel)
			handleSocketConn(conn, func(method string, params json.RawMessage) (any, *controlprotocol.Error) {
				return s.dispatchRPCContext(ctx, method, params)
			})
		}()
	}
}

// watchSocketPeer cancels in-flight network and staging work when a client
// disconnects. Polling the socket does not consume pipelined request bytes.
func watchSocketPeer(conn *net.UnixConn, ctx context.Context, cancel context.CancelFunc) {
	raw, err := conn.SyscallConn()
	if err != nil {
		cancel()
		return
	}
	for ctx.Err() == nil {
		var revents int16
		err = raw.Control(func(fd uintptr) {
			poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLHUP | unix.POLLERR}}
			if _, pollErr := unix.Poll(poll, 200); pollErr != nil {
				err = pollErr
				return
			}
			revents = poll[0].Revents
		})
		if err != nil || revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			cancel()
			return
		}
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
