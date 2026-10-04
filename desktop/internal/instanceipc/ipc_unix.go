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
	"runtime"
	"syscall"
)

// getuid is the uid a peer must have. Tests swap it to force a mismatch.
var getuid = os.Getuid

// dirUID is the uid that must own every directory on the socket path. It is
// separate from getuid so tests can fake a foreign directory without also
// faking a foreign peer.
var dirUID = os.Getuid

// maxSocketPath is the size of sun_path on macOS, in bytes (Linux has 108).
// A path this long or longer cannot be bound everywhere, so it is replaced by
// a short per-user one.
const maxSocketPath = 104

// socketPath picks where the socket for dataDir lives and makes sure its
// directory can be trusted:
//
//   - $XDG_RUNTIME_DIR/verdana/<hash>.sock when XDG_RUNTIME_DIR is an absolute
//     path to a real directory of ours that nobody else can enter;
//   - else <dataDir>/ipc/launcher.sock;
//   - and when that is maxSocketPath bytes or longer, a short per-user path
//     (under the per-user os.TempDir on macOS, /tmp/verdana-<uid> elsewhere).
//
// With create false (Dial) a missing directory is reported as fs.ErrNotExist
// instead of being created.
func socketPath(dataDir string, create bool) (string, error) {
	hash := dataDirHash(dataDir)[:16]
	var dir, path string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" && filepath.IsAbs(xdg) && trustedRuntimeDir(xdg) {
		dir = filepath.Join(xdg, "verdana")
		path = filepath.Join(dir, hash+".sock")
	} else {
		dir = filepath.Join(dataDir, "ipc")
		path = filepath.Join(dir, "launcher.sock")
	}
	if len(path) >= maxSocketPath {
		if runtime.GOOS == "darwin" {
			dir = filepath.Join(os.TempDir(), "verdana-"+hash)
			path = filepath.Join(dir, "s.sock")
		} else {
			dir = fmt.Sprintf("/tmp/verdana-%d", dirUID())
			path = filepath.Join(dir, hash+".sock")
		}
		if len(path) >= maxSocketPath {
			return "", fmt.Errorf("no socket path shorter than %d bytes for %s", maxSocketPath, dataDir)
		}
	}
	if err := verifyDir(dir, create); err != nil {
		return "", err
	}
	return path, nil
}

// trustedRuntimeDir reports whether dir is a real directory owned by us with
// no group or other permission bits. It is never modified: a runtime dir that
// fails the check is just not used.
func trustedRuntimeDir(dir string) bool {
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return false
	}
	return ownedByUs(fi) == nil && fi.Mode().Perm()&0o077 == 0
}

// ownedByUs fails unless fi belongs to dirUID.
func ownedByUs(fi fs.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot tell who owns %s", fi.Name())
	}
	if uid := int(st.Uid); uid != dirUID() {
		return fmt.Errorf("%s is owned by uid %d, not us", fi.Name(), uid)
	}
	return nil
}

// verifyDir makes sure dir is a real directory (not a symlink) owned by us
// that only we can enter, creating it 0700 when create is set and tightening
// it to 0700 when it is looser. A directory someone else owns is refused:
// they could have pre-created it to block us, which costs single-instance
// forwarding but never hands them our socket.
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
	if err := ownedByUs(fi); err != nil {
		return err
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
