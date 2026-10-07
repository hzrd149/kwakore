//go:build linux

// Package linuxhost runs the existing napplet child program for the service.
package linuxhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"verdana/backend"
	"verdana/backend/fileutil"
	"verdana/backend/media"
	"verdana/backend/netguard"
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
	exe, err = filepath.EvalSymlinks(exe)
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
	backend.AttachServiceWindowTransport(spec.Instance, transport)
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

// OpenSettings starts the launcher's trusted settings page in the sibling
// napp child. Settings never run in the napplet executable or load napp code.
func (h *Host) OpenSettings(spec backend.SettingsSpec) (backend.Transport, error) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil, backend.ErrServiceSessionUnavailable
	}
	if spec.Window == "" {
		return nil, backend.ErrServiceUnavailable
	}
	program := filepath.Join(filepath.Dir(h.Program), "napp")
	if err := checkProgram(program); err != nil {
		return nil, backend.ErrWindowProgramUnavailable
	}
	cmd := exec.Command(program)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(),
		"VERDANA_WINDOW_KIND=settings",
		"VERDANA_NAPP_ID="+spec.NappID,
		"VERDANA_NAPP_NAME="+spec.Name,
		"VERDANA_INSTANCE_ID="+spec.Window,
		"VERDANA_WINDOW_WIDTH=860",
		"VERDANA_WINDOW_HEIGHT=720",
		"VERDANA_THEME="+spec.Theme,
		"VERDANA_THEME_VARS="+spec.ThemeVars,
		"VERDANA_NAPP_FORMAT=",
		"WEBVIEW_PATH="+filepath.Dir(program),
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
	go readSettingsChild(transport, spec.Window, stdout)
	return transport, nil
}

func readSettingsChild(c *childTransport, window string, stdout io.ReadCloser) {
	defer close(c.done)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), backend.MaxInboundWireMsg+1)
	for scanner.Scan() {
		var msg backend.WireMsg
		if json.Unmarshal(scanner.Bytes(), &msg) == nil {
			backend.HandleSettingsMessage(window, msg)
		}
	}
	if scanner.Err() != nil {
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	}
	_ = stdout.Close()
	_ = c.stdin.Close()
	_ = c.cmd.Wait()
	backend.SettingsClosed(window)
}

type childTransport struct {
	cmd       *exec.Cmd
	stdin     io.Closer
	enc       *json.Encoder
	mu        sync.Mutex
	closeOnce sync.Once
	done      chan struct{}
}

func (c *childTransport) Send(m backend.WireMsg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(m)
}
func (c *childTransport) Focus() {}
func (c *childTransport) Close() {
	c.closeOnce.Do(func() {
		// A child can stop reading stdin. Never make a stop RPC wait on a
		// pipe write, and reap the selected child if it ignores the request.
		go c.Send(backend.WireMsg{T: "close"})
		go func() {
			select {
			case <-c.done:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
			}
		}()
	})
}
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
		if msg.T == "rpc" && msg.Method == "nap.start" && msg.ID > 0 && (msg.Params == "" || msg.Params == "null") {
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

func (*Host) OpenDiscovery(string) {}
func (*Host) StateChanged()        {}
func (*Host) PromptsChanged()      {}
func (*Host) CopyText(value string) error {
	program := "xclip"
	args := []string{"-selection", "clipboard"}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		program, args = "wl-copy", nil
	}
	cmd := exec.Command(program, args...)
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}

func downloadsDir() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_DOWNLOAD_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return backend.DataDir()
	}
	if candidate := filepath.Join(home, "Downloads"); isDirectory(candidate) {
		return candidate
	}
	return home
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (*Host) SaveFileTarget() string { return downloadsDir() }

func (*Host) SaveFile(name string, data []byte) (string, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return "", errors.New("invalid download filename")
	}
	dir := downloadsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i <= 999; i++ {
		candidate := name
		if i > 0 {
			candidate = stem + "-" + strconv.Itoa(i) + ext
		}
		err := fileutil.WriteFileNew(filepath.Join(dir, candidate), data, 0644)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("could not find a free filename")
}

func (*Host) OpenLink(raw string) error {
	url, err := netguard.ExternalLink(raw)
	if err != nil {
		return err
	}
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
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
func (*Host) NotificationControls() []string                                   { return []string{"system"} }
func (*Host) RequestNotificationPermission() bool                              { return true }
func (*Host) SendNotification(req backend.NotificationRequest) (backend.NotificationHandle, error) {
	cmd := exec.Command("notify-send", req.NappName+": "+req.Title, req.Body)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return linuxNotification{}, nil
}

type linuxNotification struct{}

func (linuxNotification) Dismiss() error { return nil }
func (*Host) MediaPlay(req backend.MediaRequest, onState func(backend.MediaState)) (backend.MediaPlayer, error) {
	return media.Play(req, onState)
}
