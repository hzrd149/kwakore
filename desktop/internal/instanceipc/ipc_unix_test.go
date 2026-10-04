//go:build !windows

package instanceipc

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortTempDir is a temp dir with a short path: t.TempDir on macOS is long
// enough on its own to push a socket path past the 104-byte limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "v")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// noRuntimeDir keeps the socket under the data dir.
func noRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
}

// listenAndAccept serves dataDir and sends every accepted conn's first line
// to the returned channel.
func listenAndAccept(t *testing.T, dataDir string, onReject func(error)) (net.Listener, chan string) {
	t.Helper()
	ln, err := Listen(dataDir, onReject)
	if err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 64)
			n, _ := c.Read(buf)
			lines <- string(buf[:n])
			c.Write([]byte("ok\n"))
			c.Close()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
	})
	return ln, lines
}

func dialAndSend(t *testing.T, dataDir, msg string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := Dial(ctx, dataDir)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 3)
	if _, err := io.ReadFull(c, reply); err != nil || string(reply) != "ok\n" {
		t.Fatalf("reply %q, %v", reply, err)
	}
}

func TestSocketPathRuntimeDir(t *testing.T) {
	xdg := shortTempDir(t)
	if err := os.Chmod(xdg, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	data := shortTempDir(t)
	path, err := socketPath(data, true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg, "verdana", dataDirHash(data)[:16]+".sock")
	if path != want {
		t.Fatalf("socket path %q, want %q", path, want)
	}
	fi, err := os.Stat(filepath.Join(xdg, "verdana"))
	if err != nil || fi.Mode().Perm() != 0700 {
		t.Fatalf("runtime subdir mode %v, %v; want 0700", fi.Mode(), err)
	}
	_, lines := listenAndAccept(t, data, nil)
	dialAndSend(t, data, "hello\n")
	if got := <-lines; got != "hello\n" {
		t.Fatalf("server read %q", got)
	}
}

func TestSocketPathRuntimeDirUntrusted(t *testing.T) {
	loose := shortTempDir(t)
	if err := os.Chmod(loose, 0755); err != nil {
		t.Fatal(err)
	}
	target := shortTempDir(t)
	os.Chmod(target, 0700)
	link := filepath.Join(shortTempDir(t), "rt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for name, xdg := range map[string]string{
		"group and other bits": loose,
		"relative":             "run/user",
		"symlink":              link,
		"missing":              filepath.Join(loose, "nope"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_RUNTIME_DIR", xdg)
			data := shortTempDir(t)
			path, err := socketPath(data, true)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(data, "ipc", "launcher.sock"); path != want {
				t.Fatalf("socket path %q, want the data dir fallback %q", path, want)
			}
		})
	}
}

func TestSocketPathLongDataDir(t *testing.T) {
	noRuntimeDir(t)
	data := filepath.Join(shortTempDir(t), strings.Repeat("d", 100))
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	if n := len(filepath.Join(data, "ipc", "launcher.sock")); n < 104 {
		t.Fatalf("test data dir too short: %d bytes", n)
	}
	path, err := socketPath(data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) >= 104 {
		t.Fatalf("fallback path is %d bytes: %q", len(path), path)
	}
	if strings.HasPrefix(path, data) {
		t.Fatalf("fallback path %q is still under the data dir", path)
	}
	fi, err := os.Lstat(filepath.Dir(path))
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0700 {
		t.Fatalf("fallback dir %v, %v; want a 0700 dir", fi.Mode(), err)
	}
	_, lines := listenAndAccept(t, data, nil)
	dialAndSend(t, data, "long\n")
	if got := <-lines; got != "long\n" {
		t.Fatalf("server read %q", got)
	}
}

func TestListenTightensLooseDir(t *testing.T) {
	noRuntimeDir(t)
	data := shortTempDir(t)
	dir := filepath.Join(data, "ipc")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0755)
	listenAndAccept(t, data, nil)
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0700 {
		t.Fatalf("ipc dir mode %v, %v; want 0700", fi.Mode(), err)
	}
	sock, err := os.Lstat(filepath.Join(dir, "launcher.sock"))
	if err != nil || sock.Mode().Perm() != 0600 {
		t.Fatalf("socket mode %v, %v; want 0600", sock.Mode(), err)
	}
}

func TestSymlinkedDirRefused(t *testing.T) {
	noRuntimeDir(t)
	data := shortTempDir(t)
	target := shortTempDir(t)
	os.Chmod(target, 0700)
	if err := os.Symlink(target, filepath.Join(data, "ipc")); err != nil {
		t.Fatal(err)
	}
	if ln, err := Listen(data, nil); err == nil {
		ln.Close()
		t.Fatal("Listen accepted a symlinked ipc dir")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c, err := Dial(ctx, data); err == nil {
		c.Close()
		t.Fatal("Dial accepted a symlinked ipc dir")
	}
}

func TestDirOwnedBySomeoneElseRefused(t *testing.T) {
	noRuntimeDir(t)
	data := shortTempDir(t)
	saved := dirUID
	dirUID = func() int { return os.Getuid() + 1 }
	defer func() { dirUID = saved }()
	if ln, err := Listen(data, nil); err == nil {
		ln.Close()
		t.Fatal("Listen accepted a dir owned by another uid")
	}
}

func TestDialWithoutListener(t *testing.T) {
	noRuntimeDir(t)
	data := shortTempDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Dial(ctx, data); !errors.Is(err, ErrNoInstance) {
		t.Fatalf("Dial with no ipc dir: %v, want ErrNoInstance", err)
	}
	// a dead listener's socket file is still nobody listening
	ln, err := Listen(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*peerListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := Dial(ctx, data); !errors.Is(err, ErrNoInstance) {
		t.Fatalf("Dial to a stale socket: %v, want ErrNoInstance", err)
	}
}

func TestListenReplacesStaleSocket(t *testing.T) {
	noRuntimeDir(t)
	data := shortTempDir(t)
	ln, err := Listen(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*peerListener).SetUnlinkOnClose(false)
	ln.Close()
	path, _ := socketPath(data, false)
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket missing before the test: %v", err)
	}
	_, lines := listenAndAccept(t, data, nil)
	dialAndSend(t, data, "again\n")
	if got := <-lines; got != "again\n" {
		t.Fatalf("server read %q", got)
	}
}

func TestAcceptRefusesOtherUID(t *testing.T) {
	noRuntimeDir(t)
	data := shortTempDir(t)
	// the seam is set before the accept goroutine starts and, cleanups
	// running last-in first-out, restored only after it ends
	saved := getuid
	t.Cleanup(func() { getuid = saved })
	getuid = func() int { return os.Getuid() + 1 }
	rejected := make(chan error, 1)
	_, lines := listenAndAccept(t, data, func(err error) { rejected <- err })
	path, _ := socketPath(data, false)
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	c.Write([]byte("sneaky\n"))
	// the server closes without reading: EOF, or a reset because the
	// line it never read was still queued
	if n, err := c.Read(make([]byte, 8)); n != 0 || err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("other-uid client read %d bytes, %v; want the connection closed", n, err)
	}
	select {
	case err := <-rejected:
		if !strings.Contains(err.Error(), "uid") {
			t.Fatalf("rejection %v does not name the uid", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onReject was not called")
	}
	select {
	case line := <-lines:
		t.Fatalf("a refused connection reached the server: %q", line)
	default:
	}
}

func TestDialRefusesOtherUIDListener(t *testing.T) {
	noRuntimeDir(t)
	data := shortTempDir(t)
	ln, err := Listen(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	// nobody accepts: the kernel completes the connection from the backlog,
	// so the only thing that can refuse it is Dial's own server check
	saved := getuid
	getuid = func() int { return os.Getuid() + 1 }
	defer func() { getuid = saved }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := Dial(ctx, data)
	if err == nil {
		c.Close()
		t.Fatal("Dial talked to a listener of another uid")
	}
	if errors.Is(err, ErrNoInstance) {
		t.Fatalf("a foreign listener was reported as no instance: %v", err)
	}
}
