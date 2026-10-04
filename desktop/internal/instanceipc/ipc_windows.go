//go:build windows

package instanceipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// sidMatches compares a pipe peer's SID with ours. Tests swap it to fake a
// pipe owned by someone else.
var sidMatches = func(peer, ours *windows.SID) bool { return peer.Equals(ours) }

// currentUser is the SID of the user running this process.
func currentUser() (*windows.SID, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("reading our user SID: %w", err)
	}
	return tu.User.Sid.Copy()
}

// pipeName is \\.\pipe\verdana-<hash> for this user and data dir. The name
// is guessable by anyone, which is why the DACL and the server check exist.
func pipeName(sid *windows.SID, dataDir string) string {
	return `\\.\pipe\verdana-` + dataDirHash(sid.String(), dataDir)[:32]
}

// securityDescriptor grants generic access to sid alone; the P flag stops
// any inherited entry from widening it. Never empty: an empty descriptor
// would mean the default DACL.
func securityDescriptor(sid *windows.SID) string {
	return "D:P(A;;GA;;;" + sid.String() + ")"
}

// Listen creates the pipe for dataDir with an owner-only DACL. A second
// Listen on the same name fails: the first pipe instance is created with
// FILE_FLAG_FIRST_PIPE_INSTANCE, so whoever got there first keeps it.
//
// Each accepted connection whose client process does not run as our user is
// closed and reported through onReject (which may be nil).
func Listen(dataDir string, onReject func(error)) (net.Listener, error) {
	sid, err := currentUser()
	if err != nil {
		return nil, err
	}
	ln, err := winio.ListenPipe(pipeName(sid, dataDir), &winio.PipeConfig{
		SecurityDescriptor: securityDescriptor(sid),
		InputBufferSize:    64 << 10,
		OutputBufferSize:   4096,
	})
	if err != nil {
		return nil, err
	}
	return &peerListener{Listener: ln, sid: sid, onReject: onReject}, nil
}

// Dial connects to the launcher's pipe for dataDir. It returns ErrNoInstance
// when nobody listens, and an error when the pipe server is not running as
// our user (someone created the name first to collect shortcut tokens):
// nothing is ever written to such a pipe.
func Dial(ctx context.Context, dataDir string) (net.Conn, error) {
	sid, err := currentUser()
	if err != nil {
		return nil, err
	}
	c, err := winio.DialPipeContext(ctx, pipeName(sid, dataDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil, ErrNoInstance
		}
		return nil, err
	}
	if err := checkPeer(c, sid, windows.GetNamedPipeServerProcessId); err != nil {
		c.Close()
		return nil, fmt.Errorf("refusing the instance pipe: %w", err)
	}
	return c, nil
}

// checkPeer fails unless the process on the other end of c (found with
// peerPID) runs as sid.
func checkPeer(c net.Conn, sid *windows.SID, peerPID func(windows.Handle, *uint32) error) error {
	f, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("pipe has no handle")
	}
	var pid uint32
	if err := peerPID(windows.Handle(f.Fd()), &pid); err != nil {
		return fmt.Errorf("finding the pipe peer: %w", err)
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return fmt.Errorf("opening pipe peer %d: %w", pid, err)
	}
	defer windows.CloseHandle(proc)
	var token windows.Token
	if err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("opening pipe peer %d token: %w", pid, err)
	}
	defer token.Close()
	tu, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("reading pipe peer %d user: %w", pid, err)
	}
	if !sidMatches(tu.User.Sid, sid) {
		return fmt.Errorf("pipe peer %d runs as %s, not us", pid, tu.User.Sid)
	}
	return nil
}

type peerListener struct {
	net.Listener
	sid      *windows.SID
	onReject func(error)
}

// Accept returns the next connection from a process of our user. The DACL
// already keeps other users out; this is the same check Dial makes.
func (l *peerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if err := checkPeer(c, l.sid, windows.GetNamedPipeClientProcessId); err != nil {
			c.Close()
			if l.onReject != nil {
				l.onReject(err)
			}
			continue
		}
		return c, nil
	}
}
