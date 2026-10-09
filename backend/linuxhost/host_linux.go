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
	"unicode"
	"unicode/utf8"

	"kwakore/backend"
	"kwakore/backend/desktopentry"
	"kwakore/backend/fileutil"
	"kwakore/backend/media"
	"kwakore/backend/netguard"
)

const readyTimeout = 10 * time.Second

// Host is the service's Linux platform. Program is the napplet child; CLI is
// the control CLI that native desktop entries run (empty when none is
// installed beside the daemon, which makes entry reconciliation fail
// visibly instead of writing entries that cannot start).
type Host struct {
	Program string
	CLI     string
}

func New(program string) *Host { return &Host{Program: program, CLI: DefaultCLIPath()} }

// cliName is the control CLI's file name inside the installed bundle.
const cliName = "kwakore"

// entryCLIEnv names an optional stable path for the CLI that native entries
// carry, for packages whose bundle directory does not outlive an upgrade.
// The NixOS module sets it to /run/current-system/sw/bin/kwakore: the store
// path beside the daemon changes with every rebuild and is garbage collected,
// and entries are only rewritten when the daemon starts, so an entry naming
// the store path could outlive its CLI and with it the user's way to start
// the daemon. See stableCLI for when it is used.
const entryCLIEnv = "KWAKORE_ENTRY_CLI"

// DefaultCLIPath is the kwakore CLI shipped beside the running daemon, or ""
// when there is none. See cliPath.
func DefaultCLIPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return cliPath(os.Getenv(entryCLIEnv), os.Args[0], exe)
}

// cliPath prefers the stable path from entryCLIEnv when it names this very
// daemon's CLI, and otherwise picks the CLI beside the daemon.
func cliPath(stable, arg0, exe string) string {
	if path := stableCLI(stable, exe); path != "" {
		return path
	}
	return cliBeside(arg0, exe)
}

// stableCLI returns path when it is an absolute, clean path that resolves to
// the kwakore CLI in the running daemon's own bundle (an executable regular
// file), and "" otherwise. The path itself may go through symlinks, which is
// the point: it keeps naming the current CLI after the next upgrade. A value
// that resolves anywhere else, including another generation's bundle, is
// ignored, so an entry never names some other installation's binary.
func stableCLI(path, exe string) string {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ""
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil || !filepath.IsAbs(real) {
		return ""
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil || target != filepath.Join(filepath.Dir(real), cliName) {
		return ""
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return ""
	}
	return path
}

// cliBeside picks the CLI path native entries carry. The daemon's real
// executable decides which bundle counts: the CLI must be a regular
// executable file in that same directory once symlinks are resolved, so an
// entry never names some other installation's binary. When the daemon was
// started through an absolute, clean path whose directory resolves to that
// same bundle (systemd starts it as .../lib/kwakore/current/kwakore-daemon),
// the entry keeps that unresolved directory: the `current` link survives an
// upgrade, while a release directory is pruned two upgrades later.
func cliBeside(arg0, exe string) string {
	real, err := filepath.EvalSymlinks(exe)
	if err != nil || !filepath.IsAbs(real) {
		return ""
	}
	bundle := filepath.Dir(real)
	if filepath.IsAbs(arg0) && filepath.Clean(arg0) == arg0 {
		if dir, err := filepath.EvalSymlinks(filepath.Dir(arg0)); err == nil && dir == bundle {
			if candidate := filepath.Join(filepath.Dir(arg0), cliName); cliInBundle(candidate, bundle) {
				return candidate
			}
		}
	}
	if candidate := filepath.Join(bundle, cliName); cliInBundle(candidate, bundle) {
		return candidate
	}
	return ""
}

// cliInBundle reports whether path resolves to an executable regular file
// directly inside bundle.
func cliInBundle(path, bundle string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Dir(real) != bundle {
		return false
	}
	info, err := os.Stat(real)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

// maxWindowTitleRunes caps the napplet name the child shows as its title.
const maxWindowTitleRunes = 256

// windowTitleText reduces an author's napplet name to one title line:
// invalid UTF-8 is replaced, control and format runes (NUL among them)
// become spaces, whitespace collapses, and the result is capped in runes.
func windowTitleText(name string) string {
	name = strings.ToValidUTF8(name, "\uFFFD")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > maxWindowTitleRunes {
		name = strings.TrimSpace(string([]rune(name)[:maxWindowTitleRunes]))
	}
	return name
}

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
	// Only what the napplet-only program reads (D-10). The name is author
	// data and is reduced to a title line first: exec refuses an environment
	// entry holding NUL and Linux refuses one over 128 KiB, so a raw name
	// could keep a napplet from ever opening a window.
	cmd.Env = append(os.Environ(),
		"KWAKORE_NAPP_ID="+spec.NappID,
		"KWAKORE_NAPP_NAME="+windowTitleText(spec.Name),
		"KWAKORE_INSTANCE_ID="+spec.Instance,
		"KWAKORE_WINDOW_WIDTH="+strconv.Itoa(spec.Width),
		"KWAKORE_WINDOW_HEIGHT="+strconv.Itoa(spec.Height),
		"KWAKORE_NAPP_FORMAT="+spec.Format,
		"KWAKORE_THEME="+spec.Theme,
		"KWAKORE_THEME_VARS="+spec.ThemeVars,
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

// checkProgram rejects symlinks, shared-writable path components (other than
// sticky directories) and files not owned by this user or root. The child
// independently checks the library.
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
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("unsafe window program path")
			}
			// A shared-writable directory is accepted only with the sticky
			// bit, as on the root-owned 1775 /nix/store: others can add
			// entries there but cannot rename or remove the entry below it,
			// and that entry must itself pass the owner check.
			sharedSticky := info.IsDir() && info.Mode()&os.ModeSticky != 0 && i < len(parts)-1
			if info.Mode().Perm()&0022 != 0 && !sharedSticky {
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
func (*Host) DeleteShortcutFile(string) error                { return nil }
func (*Host) ListShortcutFiles() []backend.ShortcutFile      { return nil }
func (*Host) AutostartSupported() bool                       { return false }
func (*Host) AutostartEnabled() bool                         { return false }
func (*Host) SetAutostart(bool) error                        { return backend.ErrServiceUnavailable }
func (*Host) SyncSearchNapplets([]backend.AppShortcut) error { return nil }

// AppShortcutsSupported is true: the service publishes one native desktop
// entry per installed napplet (D-07).
func (*Host) AppShortcutsSupported() bool { return true }

// SyncAppShortcuts makes the user's applications directory hold exactly one
// kwakore-napplet-<hash>.desktop per shortcut address and no other managed
// entry. Only the canonical Address and the display text reach the writer:
// ID and Token are internal launcher fields and are ignored, so a shortcut
// without an address is reported as invalid rather than written. Errors from
// individual entries are joined; the valid entries beside them are written.
func (h *Host) SyncAppShortcuts(shortcuts []backend.AppShortcut) error {
	dir, err := desktopentry.ApplicationsDir()
	if err != nil {
		return err
	}
	entries := make([]desktopentry.Entry, 0, len(shortcuts))
	for _, s := range shortcuts {
		entries = append(entries, desktopentry.Entry{Address: s.Address, Title: s.Name, Description: s.Description})
	}
	return desktopentry.Reconcile(dir, h.CLI, entries)
}
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
