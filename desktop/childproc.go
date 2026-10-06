package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"verdana/backend"

	"fiatjaf.com/verdana/desktop/internal/childbin"
	"fiatjaf.com/verdana/desktop/internal/webviewlib"
	"fiatjaf.com/verdana/desktop/internal/wireline"
)

// On the desktop each app window is its own process: a dedicated napp or
// napplet webview program that we talk to over stdin/stdout with one JSON wire message
// per line. This file is that pipe — the backend never learns about it.

type childTransport struct {
	instance string
	cmd      *exec.Cmd
	// settings is a napp's settings window rather than a napp: instance is
	// then its backend.SettingsSpec.Window
	settings bool

	mu  sync.Mutex
	enc *json.Encoder
}

var (
	childrenMu sync.Mutex
	children   []*childTransport
)

// startChild spawns the webview process for a napp. It returns as soon as the
// process is up: the napp's own readiness is observed later, when it registers
// its actions.
func startChild(spec backend.WindowSpec) (backend.Transport, error) {
	kind := spec.Format
	if kind == "" {
		kind = "napp"
	}
	if kind != "napp" && kind != "napplet" {
		return nil, fmt.Errorf("unsupported window format %q", kind)
	}
	exe, dir, err := prepareWindowProgram(kind)
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(),
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
		// last, so it wins over an inherited value (os/exec keeps the last
		// duplicate): the child loads libwebview only from the verified dir
		"WEBVIEW_PATH="+dir,
	)
	ct, err := spawnChild(childCmd(exe, env), spec.Instance, false)
	if err != nil {
		return nil, err
	}
	log.Info().Str("napp", spec.NappID).Str("instance", spec.Instance).
		Int("pid", ct.cmd.Process.Pid).Msg("napp window started")
	return ct, nil
}

// startSettingsChild spawns the napp program in settings mode to serve the
// launcher's settings page.
func startSettingsChild(spec backend.SettingsSpec) (backend.Transport, error) {
	exe, dir, err := prepareWindowProgram("napp")
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(),
		"VERDANA_WINDOW_KIND=settings",
		"VERDANA_NAPP_ID="+spec.NappID,
		"VERDANA_NAPP_NAME="+spec.Name,
		"VERDANA_INSTANCE_ID="+spec.Window,
		"VERDANA_WINDOW_WIDTH=860",
		"VERDANA_WINDOW_HEIGHT=720",
		"VERDANA_THEME="+spec.Theme,
		"VERDANA_THEME_VARS="+spec.ThemeVars,
		// last, like in startChild
		"WEBVIEW_PATH="+dir,
	)
	ct, err := spawnChild(childCmd(exe, env), spec.Window, true)
	if err != nil {
		return nil, err
	}
	log.Info().Str("napp", spec.NappID).Str("window", spec.Window).
		Int("pid", ct.cmd.Process.Pid).Msg("settings window started")
	return ct, nil
}

// childCmd builds a fresh command for exe on every call.
func childCmd(exe string, env []string) func() *exec.Cmd {
	return func() *exec.Cmd {
		cmd := exec.Command(exe)
		cmd.Env = env
		return cmd
	}
}

// cmdStart starts a command; a test seam.
var cmdStart = (*exec.Cmd).Start

// textBusyRetries is how many times spawnChild starts a child that failed
// with "text file busy". Right after prepareChild writes a new child, a fork
// running in another goroutine can still hold the temp file's write
// descriptor for a moment, and exec then fails with ETXTBSY (golang/go#22315).
const textBusyRetries = 3

func spawnChild(newCmd func() *exec.Cmd, instance string, settings bool) (*childTransport, error) {
	for attempt := 1; ; attempt++ {
		// an exec.Cmd cannot be started twice, and its pipes belong to it
		cmd := newCmd()
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		cmd.Stderr = os.Stderr
		if err := cmdStart(cmd); err != nil {
			if errors.Is(err, syscall.ETXTBSY) && attempt < textBusyRetries {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return nil, err
		}

		ct := &childTransport{instance: instance, cmd: cmd, settings: settings, enc: json.NewEncoder(stdin)}

		childrenMu.Lock()
		children = append(children, ct)
		childrenMu.Unlock()

		go readChild(ct, stdout)
		return ct, nil
	}
}

func (ct *childTransport) Send(m backend.WireMsg) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if err := ct.enc.Encode(m); err != nil {
		log.Debug().Str("instance", ct.instance).Err(err).Msg("could not write to child")
	}
}

func (ct *childTransport) Close() {
	ct.Send(backend.WireMsg{T: "close"})
}

// The child webview library has no cross-platform raise operation. Dispatch
// still reaches the existing window; compositors that prevent focus stealing
// are allowed to leave this hint unfulfilled.
func (ct *childTransport) Focus() {}

// readChild hands everything the child says to the backend, until it exits.
// The child renders untrusted napplet content, so each line it writes is
// capped at backend.MaxInboundWireMsg; one that is longer ends the window.
func readChild(ct *childTransport, stdout io.ReadCloser) {
	err := wireline.Read(stdout, backend.MaxInboundWireMsg, func(line []byte) {
		var m backend.WireMsg
		if err := json.Unmarshal(line, &m); err != nil {
			log.Debug().Str("instance", ct.instance).Err(err).Msg("skipping unreadable line from child")
			return
		}
		if ct.settings {
			backend.HandleSettingsMessage(ct.instance, m)
		} else {
			backend.HandleMessage(ct.instance, m)
		}
	})
	if errors.Is(err, bufio.ErrTooLong) {
		// we stop reading here, so the child may block forever writing to
		// a full pipe and cmd.Wait below would never return (the window
		// would stay on screen): kill it before anything waits on it
		log.Error().Str("instance", ct.instance).Msg("child sent an overlong line; closing its window")
		ct.cmd.Process.Kill()
	} else {
		log.Debug().Str("instance", ct.instance).Err(err).Msg("child stdout ended")
	}

	if ct.settings {
		backend.SettingsClosed(ct.instance)
	} else {
		backend.WindowClosed(ct.instance)
	}
	ct.cmd.Wait()

	childrenMu.Lock()
	for i, c := range children {
		if c == ct {
			children = append(children[:i], children[i+1:]...)
			break
		}
	}
	childrenMu.Unlock()
}

// killAllChildren is the last resort when the launcher itself is going away:
// the windows were asked to close first.
func killAllChildren() {
	childrenMu.Lock()
	snapshot := append([]*childTransport(nil), children...)
	childrenMu.Unlock()
	for _, ct := range snapshot {
		if ct.cmd != nil && ct.cmd.Process != nil {
			ct.cmd.Process.Kill()
		}
	}
	log.Info().Int("count", len(snapshot)).Msg("killed all child processes")
}

// childCacheDir is the per-user directory prepareChild keeps the child
// under, one subdirectory per build contents (childbin.EnsureVersion); a
// test seam.
var childCacheDir = childbin.CacheDir

// childFiles is the one list of files the child needs next to it in the
// cache dir. The child program comes first.
func childFiles(data []byte, sum [32]byte) []childbin.File {
	return windowFiles("napplet", data, sum)
}

func windowFiles(kind string, data []byte, sum [32]byte) []childbin.File {
	name := kind + "-" + hex.EncodeToString(sum[:])
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return []childbin.File{
		{Name: name, Data: data, Sum: sum, Exec: true},
		// the webview library the child loads, under the fixed name
		// go-webview's loader probes for; on Windows it sits next to the
		// child exe, where LoadLibrary("webview.dll") looks first. A target
		// without a library has empty Data, which Ensure refuses.
		{Name: webviewlib.Name, Data: webviewlib.Data, Sum: webviewlib.Sum(), Exec: false},
	}
}

// prepareChild makes sure the child program in the per-user cache dir holds
// exactly the bytes this launcher carries (hashed in this call) and returns
// its path and the directory, which is this build's own version directory
// (WEBVIEW_PATH points there), so other builds never touch its files. It
// runs before every spawn. In prod every error wraps
// backend.ErrWindowProgramUnavailable: the window then fails to open and
// nothing else is executed in its place.
func prepareChild() (exe, dir string, err error) {
	return prepareWindowProgram("napplet")
}

func prepareWindowProgram(kind string) (exe, dir string, err error) {
	defer func() {
		if err == nil {
			return
		}
		log.Error().Err(err).Str("dir", dir).Str("path", exe).Msg("napp window program unavailable")
		exe, dir = "", ""
		if failClosed {
			err = fmt.Errorf("%w: %v", backend.ErrWindowProgramUnavailable, err)
			// the backend raises the child-unavailable notice; bring the
			// manager up so an open from a shortcut, the tray or the store
			// never fails silently (showManager is safe off the UI loop)
			showManager()
		}
	}()

	data, sum, err := windowSource(kind)
	if err != nil {
		return "", "", err
	}
	base, err := childCacheDir()
	if err != nil {
		return "", "", err
	}
	files := windowFiles(kind, data, sum)
	dir = filepath.Join(base, childbin.Version(files))
	exe = filepath.Join(dir, files[0].Name)
	if _, err := childbin.EnsureVersion(base, files); err != nil {
		return exe, dir, err
	}
	return exe, dir, nil
}
