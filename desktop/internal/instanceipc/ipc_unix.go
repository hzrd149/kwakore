//go:build !windows

package instanceipc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// getuid is the uid a peer must have. Tests swap it to force a mismatch.
var getuid = os.Getuid

// socketPath picks where the socket for dataDir lives and makes sure its
// directory can be trusted. With create false (Dial) a missing directory is
// reported as fs.ErrNotExist instead of being created.
func socketPath(dataDir string, create bool) (string, error) {
	dir := filepath.Join(dataDir, "ipc")
	if err := verifyDir(dir, create); err != nil {
		return "", err
	}
	return filepath.Join(dir, "launcher.sock"), nil
}

// verifyDir makes sure dir is a real directory (not a symlink) that only we
// can enter, creating it 0700 when create is set and tightening it to 0700
// when it is looser.
func verifyDir(dir string, create bool) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

// Listen opens the socket for dataDir. Call it only while holding the
// instancelock: it removes whatever socket file is already there, which is
// only safe when the lock proves its listener is gone.
//
// Every accepted connection whose peer is not our own uid is closed and
// reported through onReject (which may be nil); Accept keeps going.
func Listen(dataDir string, onReject func(error)) (net.Listener, error) {
	path, err := socketPath(dataDir, true)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		ln.Close()
		return nil, err
	}
	return &peerListener{UnixListener: ln, onReject: onReject}, nil
}

// Dial connects to the launcher listening for dataDir. It returns
// ErrNoInstance when nobody listens, and an error when the listener is not
// running as our uid (someone squatting the path).
func Dial(ctx context.Context, dataDir string) (net.Conn, error) {
	path, err := socketPath(dataDir, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoInstance
	}
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return nil, ErrNoInstance
		}
		return nil, err
	}
	if err := checkPeer(c.(*net.UnixConn)); err != nil {
		c.Close()
		return nil, fmt.Errorf("refusing the instance listener: %w", err)
	}
	return c, nil
}

// checkPeer fails unless the other end of c runs as our uid.
func checkPeer(c *net.UnixConn) error {
	uid, err := peerUID(c)
	if err != nil {
		return fmt.Errorf("reading peer credentials: %w", err)
	}
	if want := uint32(getuid()); uid != want {
		return fmt.Errorf("peer uid %d is not ours (%d)", uid, want)
	}
	return nil
}

type peerListener struct {
	*net.UnixListener
	onReject func(error)
}

// Accept returns the next connection made by our own uid, silently closing
// (and reporting) any other.
func (l *peerListener) Accept() (net.Conn, error) {
	for {
		c, err := l.UnixListener.AcceptUnix()
		if err != nil {
			return nil, err
		}
		if err := checkPeer(c); err != nil {
			c.Close()
			if l.onReject != nil {
				l.onReject(err)
			}
			continue
		}
		return c, nil
	}
}
