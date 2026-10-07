package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"verdana/backend/media"
	"fiatjaf.com/verdana/desktop/internal/osintegration"
	"verdana/backend"
	"verdana/backend/fileutil"
	"verdana/backend/netguard"
)

// gioHost is this launcher's answer to everything platform-shaped the backend
// needs: a napp window is an OS window backed by its own webview process, a
// redraw is a Gio invalidation, a download goes to ~/Downloads and a link
// goes to whatever the desktop uses to open links.
type gioHost struct{}

func (gioHost) OpenWindow(spec backend.WindowSpec) (backend.Transport, error) {
	return startChild(spec)
}

func (gioHost) OpenSettings(spec backend.SettingsSpec) (backend.Transport, error) {
	return startSettingsChild(spec)
}

func (gioHost) MediaPlay(req backend.MediaRequest, onState func(backend.MediaState)) (backend.MediaPlayer, error) {
	return media.Play(req, onState)
}

func (gioHost) StateChanged() {
	trayStateChanged()
	showPendingPrimary()
	invalidateAll()
}

func (gioHost) OpenDiscovery(archetype string) {
	store.mu.Lock()
	store.discoveryArchetype = archetype
	store.mu.Unlock()
	showStoreView(storeDiscover)
}

func showDiscoverySearch(query string) {
	store.mu.Lock()
	store.discoveryQuery = strings.TrimSpace(query)
	store.mu.Unlock()
	showStoreView(storeDiscover)
}

func (gioHost) PromptsChanged() {
	if p := backend.CurrentPrompt(); p != nil && p.Instance == "" {
		showManager()
	}
	invalidateAll()
}

// CopyText parks the text for the next Gio frame of either launcher window
// (see drainClipboard): writing to the clipboard is a frame command
// (clipboard.WriteCmd), not something an rpc goroutine can do on its own.
func (gioHost) CopyText(text string) error {
	ui.mu.Lock()
	ui.clipboard = append(ui.clipboard, text)
	ui.mu.Unlock()
	invalidateAll()
	return nil
}

// writeNewFile creates a download and fails with fs.ErrExist instead of
// replacing a file. Tests wrap it to simulate a file appearing in between.
var writeNewFile = fileutil.WriteFileNew

// SaveFile writes into the user's download directory, never clobbering:
// file.txt, file-1.txt, file-2.txt… There is no separate "is this name
// free" check: the exclusive write is the check, so a file that appears
// under the chosen name just before the write moves the download to the next
// name instead of being overwritten.
func (gioHost) SaveFile(name string, data []byte) (string, error) {
	dir := downloadsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	dest := filepath.Join(dir, name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		err := writeNewFile(dest, data, 0644)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if i > 999 {
			return "", errors.New("could not find a free filename")
		}
		dest = filepath.Join(dir, stem+"-"+strconv.Itoa(i)+ext)
	}

	log.Info().Str("path", dest).Int("bytes", len(data)).Msg("saved file for napp")
	return filepath.Base(dest), nil
}

func (gioHost) SaveFileTarget() string { return downloadsDir() }

// AmberRequest is a NIP-55 phone-signer concept: the desktop has no signer
// app to reach, so there is nothing to launch.
func (gioHost) AmberRequest(string, string, string, string, string, string) bool {
	return false
}

// executableEnv names a launcher path for the Exec lines of the OS entries
// verdana writes: shortcuts, autostart, app shortcuts and search providers.
const executableEnv = "VERDANA_EXECUTABLE"

// launcherExecutable is the program OS entries should run. Under a packaging
// wrapper such as Nix's makeWrapper, /proc/self/exe names the wrapped binary,
// which skips the wrapper's environment and lives at a store path that
// disappears after an upgrade and garbage collection. Packagers point
// VERDANA_EXECUTABLE at a stable launcher path instead. It is honored only as
// an absolute path to an executable regular file (symlinks followed for the
// check, but returned as given so a stable link stays stable); anything else
// falls back to the running binary. The value still goes through
// quoteExecField, which refuses control characters.
func launcherExecutable() (string, error) {
	if path := os.Getenv(executableEnv); path != "" {
		if filepath.IsAbs(path) {
			if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
				return path, nil
			}
		}
		log.Warn().Msg("ignoring VERDANA_EXECUTABLE, it is not an absolute path to an executable file")
	}
	return os.Executable()
}

// CreateShortcutFile writes an OS shortcut whose whole job is calling
// verdana with one quoted argument: the bundle token. The OS-specific file
// shapes live in the shortcutfile_<goos>.go files.
func (gioHost) CreateShortcutFile(name, token string) (string, error) {
	exe, err := launcherExecutable()
	if err != nil {
		return "", err
	}
	return osintegration.WriteShortcutFile(name, exe, token)
}

func (gioHost) DeleteShortcutFile(path string) error {
	if err := osintegration.DeleteShortcutFile(path); err != nil {
		return err
	}
	// refresh what the desktop environment has indexed, when one exists
	osintegration.RefreshShortcutParent(filepath.Dir(path))
	log.Info().Str("path", path).Msg("removed shortcut file")
	return nil
}

// ListShortcutFiles reads back every verdana shortcut in the OS shortcut
// folder, the file being the whole record: the bundle's name, and the token it
// runs.
func (gioHost) ListShortcutFiles() []backend.ShortcutFile {
	files := osintegration.ListShortcutFiles()
	log.Info().Int("count", len(files)).Msg("shortcut files found")
	return files
}

func (gioHost) AutostartSupported() bool { return true }

func (gioHost) AutostartEnabled() bool { return osintegration.AutostartEnabled() }

func (gioHost) SetAutostart(enabled bool) error {
	exe, err := launcherExecutable()
	if err != nil {
		return err
	}
	return osintegration.SetAutostart(enabled, exe)
}

func (gioHost) AppShortcutsSupported() bool { return true }

func (gioHost) SyncAppShortcuts(shortcuts []backend.AppShortcut) error {
	exe, err := launcherExecutable()
	if err != nil {
		return err
	}
	return osintegration.SyncAppShortcuts(shortcuts, exe)
}

func (gioHost) SyncSearchNapplets(napplets []backend.AppShortcut) error {
	exe, err := launcherExecutable()
	if err != nil {
		return err
	}
	return osintegration.SyncSearchNapplets(napplets, exe)
}

func (gioHost) GNOMESearchSupported() bool { return osintegration.GnomeSearchSupported() }

func (gioHost) SetGNOMESearchIntegration(enabled bool) error {
	exe, err := launcherExecutable()
	if err != nil {
		return err
	}
	return osintegration.SetGNOMESearchIntegration(enabled, exe)
}

// startCommand runs the OS link opener. It reaps the process in the
// background so a finished opener never lingers as a zombie. Tests swap it
// for a recorder.
var startCommand = func(c *exec.Cmd) error {
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait()
	return nil
}

// OpenLink validates the link itself instead of trusting its callers: the
// OS opener would happily run file:, custom-scheme or argv-looking strings,
// so only the normalized http(s) form from netguard's ExternalLink is passed.
func (gioHost) OpenLink(raw string) error {
	url, err := netguard.ExternalLink(raw)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return startCommand(cmd)
}

// downloadsDir is where saveFile writes: the user's XDG download directory
// when it exists, the home directory otherwise.
func downloadsDir() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_DOWNLOAD_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return backend.DataDir()
	}
	candidate := filepath.Join(home, "Downloads")
	if st, err := os.Stat(candidate); err == nil && st.IsDir() {
		return candidate
	}
	return home
}
