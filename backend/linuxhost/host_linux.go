//go:build linux

// Package linuxhost runs the existing napplet child program for the service.
package linuxhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"verdana/backend"
)

const readyTimeout = 10 * time.Second

type Host struct{ Program string }

func New(program string) *Host { return &Host{Program: program} }

// DefaultProgramPath is relative to the daemon executable, not cwd or PATH.
// Phase 9 packages this sibling and its adjacent libwebview.so together.
func DefaultProgramPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "napplet")
}

func (h *Host) OpenWindow(spec backend.WindowSpec) (backend.Transport, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
	defer cancel()
	return h.OpenWindowContext(ctx, spec)
}

func (h *Host) OpenWindowContext(ctx context.Context, spec backend.WindowSpec) (backend.Transport, error) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil, backend.ErrServiceSessionUnavailable
	}
	if spec.Format != backend.FormatNapplet || spec.Instance == "" {
		return nil, backend.ErrServiceUnavailable
	}
	if err := checkProgram(h.Program); err != nil {
		return nil, backend.ErrWindowProgramUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	cmd := exec.Command(h.Program)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(),
		"VERDANA_NAPP_ID="+spec.NappID,
		"VERDANA_NAPP_DIR="+spec.Dir,
		"VERDANA_NAPP_URL="+spec.URL,
		"VERDANA_NAPP_NAME="+spec.Name,
		"VERDANA_NAPP_DESC="+spec.Description,
		"VERDANA_NAPP_STORAGE_FILE="+backend.StorageFile(spec.NappID),
		"VERDANA_INSTANCE_ID="+spec.Instance,
		"VERDANA_WINDOW_WIDTH="+strconv.Itoa(spec.Width),
		"VERDANA_WINDOW_HEIGHT="+strconv.Itoa(spec.Height),
		"VERDANA_NAPP_REQUIRES="+strings.Join(spec.Requires, ","),
		"VERDANA_NAPP_FORMAT="+spec.Format,
		"VERDANA_THEME="+spec.Theme,
		"VERDANA_THEME_VARS="+spec.ThemeVars,
		"WEBVIEW_PATH="+filepath.Dir(h.Program),
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, backend.ErrServiceUnavailable
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, backend.ErrServiceUnavailable
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, backend.ErrServiceUnavailable
	}
	transport := &childTransport{cmd: cmd, enc: json.NewEncoder(stdin), stdin: stdin, done: make(chan struct{})}
	ready := make(chan struct{}, 1)
	failed := make(chan struct{}, 1)
	go readChild(transport, spec.Instance, stdout, ready, failed)
	select {
	case <-failed:
		transport.killAndWait()
		return nil, backend.ErrServiceUnavailable
	case <-transport.done:
		return nil, backend.ErrServiceUnavailable
	case <-ready:
		select {
		case <-failed:
			transport.killAndWait()
			return nil, backend.ErrServiceUnavailable
		case <-transport.done:
			return nil, backend.ErrServiceUnavailable
		default:
			return transport, nil
		}
	case <-ctx.Done():
		transport.killAndWait()
		return nil, backend.ErrServiceTimeout
	}
}

type childTransport struct {
	cmd   *exec.Cmd
	stdin io.Closer
	enc   *json.Encoder
	mu    sync.Mutex
	done  chan struct{}
}

func (c *childTransport) Send(m backend.WireMsg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(m)
}
func (c *childTransport) Focus() {}
func (c *childTransport) Close() { c.Send(backend.WireMsg{T: "close"}) }
func (c *childTransport) killAndWait() {
	_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	<-c.done
}

func readChild(c *childTransport, instance string, stdout io.ReadCloser, ready, failed chan<- struct{}) {
	defer close(c.done)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), backend.MaxInboundWireMsg+1)
	for scanner.Scan() {
		var msg backend.WireMsg
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.T == "windowFailed" {
			backend.HandleMessage(instance, msg)
			select {
			case failed <- struct{}{}:
			default:
			}
			break
		}
		if msg.T == "rpc" && msg.Method == "nap.start" && msg.ID > 0 && msg.Params == "" {
			// This frame comes from the checked child executable. The child
			// token binding restricts nap.start to its own host page.
			backend.HandleMessage(instance, msg)
			select {
			case ready <- struct{}{}:
			default:
			}
			continue
		}
		backend.HandleMessage(instance, msg)
	}
	if scanner.Err() != nil {
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	}
	_ = stdout.Close()
	_ = c.stdin.Close()
	_ = c.cmd.Wait()
	backend.WindowClosed(instance)
}

// checkProgram rejects symlinks, shared-writable path components and files
// not owned by this user or root. The child independently checks the library.
func checkProgram(program string) error {
	if !filepath.IsAbs(program) || filepath.Clean(program) != program {
		return errors.New("window program path")
	}
	for _, path := range []string{program, filepath.Join(filepath.Dir(program), "libwebview.so")} {
		current := string(filepath.Separator)
		parts := strings.Split(strings.TrimPrefix(path, current), current)
		for i, part := range parts {
			if part == "" {
				return errors.New("empty path component")
			}
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
				return errors.New("unsafe window program path")
			}
			owner, ok := info.Sys().(*syscall.Stat_t)
			if !ok || (owner.Uid != 0 && owner.Uid != uint32(os.Geteuid())) {
				return errors.New("unsafe window program owner")
			}
			if i == len(parts)-1 {
				if !info.Mode().IsRegular() {
					return errors.New("window program is not regular")
				}
				if path == program && info.Mode().Perm()&0111 == 0 {
					return errors.New("window program is not executable")
				}
			} else if !info.IsDir() {
				return errors.New("window program parent is not a directory")
			}
		}
	}
	return nil
}

func (*Host) OpenDiscovery(string)                    {}
func (*Host) StateChanged()                           {}
func (*Host) PromptsChanged()                         {}
func (*Host) CopyText(string) error                   { return backend.ErrServiceUnavailable }
func (*Host) SaveFile(string, []byte) (string, error) { return "", backend.ErrServiceUnavailable }
func (*Host) SaveFileTarget() string                  { return "" }
func (*Host) OpenLink(string) error                   { return backend.ErrServiceUnavailable }
func (*Host) CreateShortcutFile(string, string) (string, error) {
	return "", backend.ErrServiceUnavailable
}
func (*Host) DeleteShortcutFile(string) error                                  { return nil }
func (*Host) ListShortcutFiles() []backend.ShortcutFile                        { return nil }
func (*Host) AutostartSupported() bool                                         { return false }
func (*Host) AutostartEnabled() bool                                           { return false }
func (*Host) SetAutostart(bool) error                                          { return backend.ErrServiceUnavailable }
func (*Host) AppShortcutsSupported() bool                                      { return false }
func (*Host) SyncAppShortcuts([]backend.AppShortcut) error                     { return nil }
func (*Host) SyncSearchNapplets([]backend.AppShortcut) error                   { return nil }
func (*Host) GNOMESearchSupported() bool                                       { return false }
func (*Host) SetGNOMESearchIntegration(bool) error                             { return backend.ErrServiceUnavailable }
func (*Host) AmberRequest(string, string, string, string, string, string) bool { return false }
func (*Host) NotificationControls() []string                                   { return nil }
func (*Host) RequestNotificationPermission() bool                              { return false }
func (*Host) SendNotification(backend.NotificationRequest) (backend.NotificationHandle, error) {
	return nil, backend.ErrServiceUnavailable
}
func (*Host) MediaPlay(backend.MediaRequest, func(backend.MediaState)) (backend.MediaPlayer, error) {
	return nil, backend.ErrServiceUnavailable
}
func (*Host) OpenSettings(backend.SettingsSpec) (backend.Transport, error) {
	return nil, backend.ErrServiceUnavailable
}
