package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"verdana/backend"

	"fiatjaf.com/verdana/desktop/internal/instancelock"
	"fiatjaf.com/verdana/desktop/internal/media"
	"fiatjaf.com/verdana/desktop/internal/osintegration"
	"fiatjaf.com/verdana/desktop/internal/windowchrome"
	"gioui.org/app"
	"gioui.org/io/clipboard"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/rs/zerolog"
)

// This is the desktop launcher: a resident backend and tray, plus a Gio
// manager window and a store window (store.go), each of which may be closed
// and recreated. Everything the manager
// shows comes from backend.Snapshot(), everything it does is a backend call,
// and every napp window is a child process (see childproc.go).

// gioState is what belongs to this window alone — the backend owns the rest.
type gioState struct {
	mu  sync.Mutex
	tab int

	// confirmLogout parks the "log out?" dialog over the main screen until
	// the user answers it: logging out closes every napp.
	confirmLogout bool

	// clipboard holds texts napp.utils.copyText asked for: only a Gio frame
	// can execute clipboard.WriteCmd, so the host parks them here and the
	// next frame of either window drains them.
	clipboard []string

	// shortcutEditing is the bundle shortcut being created (nil checked
	// windows) or edited (a stored one), parked over the Windows tab the
	// same dialogs are; nil the rest of the time.
	shortcutEditing *shortcutEditState

	// shortcutErr is why the last shortcut create/edit attempt didn't work,
	// shown on the Windows tab (the editor keeps itself open on error).
	shortcutErr string
}

// shortcutEditState is the editor overlay for one bundle: the fields it is
// built from (one napp, its action textarea) and the name of the shortcut
// when it is a stored one being edited ("": creating a new one).
type shortcutEditState struct {
	name    string // the stored shortcut being edited, "" for a new one
	entries []shortcutEditEntry
}

type shortcutEditEntry struct {
	nappID string
	label  string
	ed     widget.Editor
}

// checked is the bundle-creation checkboxes of the Windows tab, keyed by
// window instance, living across frames so state survives redraws.
var bundleChecks = make(map[string]*widget.Bool)

var (
	ui  gioState
	log zerolog.Logger
)

// the manager's tabs; Dev only exists in dev builds.
const (
	tabWindows = iota
	tabDev
)

const APP_TITLE = "Verdana"

func main() {
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().
		Int("_", os.Getpid()).
		Timestamp().
		Logger()
	log.Info().Msg("starting verdana")
	media.SetLogger(log)
	osintegration.SetLogger(log)
	if backend := windowchrome.Configure(); backend != "" {
		log.Info().Str("window_backend", backend).Msg("configured native window chrome")
	}

	dataDir, err := app.DataDir()
	if err != nil {
		log.Fatal().Err(err).Msg("no data dir")
	}

	// startup arguments are bundle shortcut invocations: tokens like
	// "<napp-id> +<action> …" coming from a bundle's OS shortcut file. When
	// a launcher is already running they were forwarded there and we never
	// got this far; with none running, this process serves it. Tool and
	// toolkit flags are left for gio and friends to chew on.
	background, startupToken, trialID := startupArgs(os.Args[1:])

	verdanaDir := filepath.Join(dataDir, "Verdana")
	if err := os.MkdirAll(verdanaDir, 0700); err != nil {
		log.Fatal().Err(err).Msg("could not create data directory")
	}

	forwarded := instanceCommand{Command: commandOpenManager}
	if startupToken != "" {
		forwarded.Command = commandRunShortcut
		forwarded.Token = startupToken
	} else if trialID != "" {
		forwarded.Command = commandTryNapplet
		forwarded.Token = trialID
	} else if background {
		forwarded.Command = commandEnsureRunning
	}
	if forwardToInstance(verdanaDir, forwarded) {
		log.Info().Str("command", forwarded.Command).Msg("forwarded invocation to the running launcher")
		return
	}
	releaseInstanceLock, acquired, err := instancelock.Acquire(verdanaDir)
	if err != nil {
		log.Fatal().Err(err).Msg("could not acquire the launcher instance lock")
	}
	if !acquired {
		// The lock holder may be between acquiring the lock and publishing its
		// forwarding port. Give that cold-start window time to finish.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if forwardToInstance(verdanaDir, forwarded) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		log.Warn().Msg("another Verdana launcher is starting; leaving it as the sole instance")
		return
	}
	defer releaseInstanceLock()

	// the listener goes up before the backend, so a shortcut clicked while
	// this launcher is still starting finds someone to forward to instead of
	// starting a second one; the tokens it accepts meanwhile wait for the
	// backend to be up (see runBundleToken).
	stopInstanceListener := startInstanceListener(verdanaDir)
	defer stopInstanceListener()

	closeStores, err := backend.Start(backend.Options{
		DataDir: verdanaDir,
		Host:    gioHost{},
		Log:     &log,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("could not start the backend")
	}
	defer closeStores()
	if err := backend.SyncGNOMESearchIntegration(); err != nil {
		log.Warn().Err(err).Msg("could not synchronize GNOME search integration")
	}
	stopSearchProvider := osintegration.StartSearchProvider(showDiscoverySearch)
	defer stopSearchProvider()

	// the backend is up: the token this launcher was started with opens its
	// napps, and the ones forwarded in while it was starting stop waiting.
	close(launcherReady)
	if startupToken != "" {
		log.Info().Str("token", previewToken(startupToken)).Msg("bundle invocation at startup")
		go runBundleToken(startupToken)
	} else if trialID != "" {
		go tryNappletWhenReady(trialID)
	}

	// Resolve the user's system/light/dark preference and keep system mode in
	// sync with OS appearance changes for the lifetime of the launcher.
	stopTheme := startThemeController()
	defer stopTheme()

	runDesktop(background)

	backend.CloseAllWindows()
	backend.CloseAllSettings()
	killAllChildren()
}

func startupArgs(args []string) (background bool, token, trialID string) {
	var tokenArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--background" {
			background = true
			continue
		}
		if arg == "--launch-napp" && i+1 < len(args) {
			i++
			tokenArgs = append(tokenArgs, args[i])
			continue
		}
		if arg == "--try-napplet" && i+1 < len(args) {
			i++
			trialID = args[i]
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		tokenArgs = append(tokenArgs, arg)
	}
	return background, strings.Join(tokenArgs, " "), trialID
}

func setTab(t int) {
	ui.tab = t
	if w := managerWindow(); w != nil {
		w.Invalidate()
	}
}

func setConfirmLogout(v bool) {
	ui.confirmLogout = v
	if w := managerWindow(); w != nil {
		w.Invalidate()
	}
}

func setShortcutErr(msg string) {
	ui.mu.Lock()
	ui.shortcutErr = msg
	ui.mu.Unlock()
	if w := managerWindow(); w != nil {
		w.Invalidate()
	}
}

// currentShortcutEdit is the bundle editor parked over the Windows tab, or
// nil. Background saves put their editor back through it, so the field is
// never touched off the Gio loop without the mutex.
func currentShortcutEdit() *shortcutEditState {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return ui.shortcutEditing
}

func setShortcutEdit(edit *shortcutEditState) {
	ui.mu.Lock()
	ui.shortcutEditing = edit
	ui.mu.Unlock()
	if w := managerWindow(); w != nil {
		w.Invalidate()
	}
}

// drainClipboard writes the texts parked by gioHost.CopyText: whichever
// launcher window draws first does it.
func drainClipboard(gtx layout.Context) {
	ui.mu.Lock()
	pending := ui.clipboard
	ui.clipboard = nil
	ui.mu.Unlock()
	for _, text := range pending {
		gtx.Execute(clipboard.WriteCmd{
			Type: "application/text",
			Data: io.NopCloser(strings.NewReader(text)),
		})
	}
}

func gioMain() {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(fontCollection()))
	th.Face = "vFont"

	w := new(app.Window)
	w.Option(
		app.Title(APP_TITLE),
		app.Size(unit.Dp(560), unit.Dp(640)),
		app.Decorated(true),
	)
	setManagerWindow(w)
	defer setManagerWindow(nil)

	var (
		tabWindowsBtn  widget.Clickable
		tabDevBtn      widget.Clickable
		storeBtn       widget.Clickable
		themeBtn       widget.Clickable
		settingsBtn    widget.Clickable
		logoutBtn      widget.Clickable
		confirmYesBtn  widget.Clickable
		confirmNoBtn   widget.Clickable
		windowsList    widget.List
		devList        widget.List
		devURLed       widget.Editor
		devPathEd      widget.Editor
		loadURLBtn     widget.Clickable
		browseBtn      widget.Clickable
		loadFolderBtn  widget.Clickable
		devOpenBtns    []widget.Clickable
		devUnloadBtns  []widget.Clickable
		devPublishBtns []widget.Clickable
		closeBtns      []widget.Clickable
		reopenBtns     []widget.Clickable
		promptBtns     promptButtons
		optBtns        []widget.Clickable

		bundleNameEd      widget.Editor
		createShortcutBtn widget.Clickable
		saveShortcutBtn   widget.Clickable
		cancelShortcutBtn widget.Clickable
		shortcutDelBtns   []widget.Clickable
		shortcutEditBtns  []widget.Clickable
	)
	loginScr := newLoginScreen()
	devURLed.SingleLine = true
	devPathEd.SingleLine = true
	devList.Axis = layout.Vertical
	windowsList.Axis = layout.Vertical

	var ops op.Ops
	for {
		switch e := w.Event(); e := e.(type) {
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			if backend.ThemeMode() != appliedThemeMode() {
				applyThemeMode()
			}

			// the palette is re-read every frame, so a theme switch (which can
			// come from any goroutine) never touches th concurrently
			pal := currentTheme()
			pal.apply(th)
			paint.Fill(gtx.Ops, pal.bg)

			st := backend.Snapshot()
			activePrompt := backend.CurrentPrompt()

			ui.mu.Lock()
			tab := ui.tab
			ui.mu.Unlock()
			drainClipboard(gtx)

			layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				// a prompt generated by a napp is shown over that napp's own
				// window (it covers the webview until answered), not here.
				// Only launcher-originated prompts take over this window.
				if activePrompt != nil && activePrompt.Instance == "" {
					for len(optBtns) < len(activePrompt.Options) {
						optBtns = append(optBtns, widget.Clickable{})
					}
					// allow and deny, each of them for this prompt only, for
					// this session or always
					for _, c := range []struct {
						ok    bool
						scope backend.Scope
						btn   *widget.Clickable
					}{
						{true, backend.ScopeOnce, &promptBtns.allow},
						{true, backend.ScopeSession, &promptBtns.sessionAllow},
						{true, backend.ScopeAlways, &promptBtns.alwaysAllow},
						{false, backend.ScopeOnce, &promptBtns.deny},
						{false, backend.ScopeSession, &promptBtns.sessionDeny},
						{false, backend.ScopeAlways, &promptBtns.alwaysDeny},
					} {
						if c.btn.Clicked(gtx) {
							backend.AnswerPrompt(activePrompt.ID, backend.Answer{OK: c.ok, Scope: c.scope})
							if !c.ok && activePrompt.CloseOnReject {
								w.Perform(system.ActionClose)
							}
						}
					}
					for i := range activePrompt.Options {
						if optBtns[i].Clicked(gtx) {
							backend.AnswerPrompt(activePrompt.ID, backend.Answer{OK: true, Index: i, Scope: backend.ScopeOnce})
						}
					}
					return layoutPrompt(gtx, th, activePrompt, &promptBtns, optBtns)
				}

				if ui.confirmLogout {
					if confirmYesBtn.Clicked(gtx) {
						setConfirmLogout(false)
						go backend.Logout()
					}
					if confirmNoBtn.Clicked(gtx) {
						setConfirmLogout(false)
					}
					return layoutConfirmLogout(gtx, th, &confirmYesBtn, &confirmNoBtn)
				}

				switch st.Phase {
				case backend.PhaseLogin:
					return loginScr.layout(gtx, th, st)
				case backend.PhaseMain:
					if tabWindowsBtn.Clicked(gtx) {
						setTab(tabWindows)
					}
					if devEnabled && tabDevBtn.Clicked(gtx) {
						setTab(tabDev)
					}
					if storeBtn.Clicked(gtx) {
						showStore()
					}
					if themeBtn.Clicked(gtx) {
						toggleTheme()
					}
					if settingsBtn.Clicked(gtx) {
						go func() {
							if err := backend.OpenLauncherSettings(); err != nil {
								log.Warn().Err(err).Msg("could not open settings")
							}
						}()
					}
					if logoutBtn.Clicked(gtx) {
						setConfirmLogout(true)
					}
					for len(devOpenBtns) < len(st.Dev) {
						devOpenBtns = append(devOpenBtns, widget.Clickable{})
					}
					for len(devUnloadBtns) < len(st.Dev) {
						devUnloadBtns = append(devUnloadBtns, widget.Clickable{})
					}
					for len(devPublishBtns) < len(st.Dev) {
						devPublishBtns = append(devPublishBtns, widget.Clickable{})
					}
					for len(closeBtns) < len(st.ManagedWindows) {
						closeBtns = append(closeBtns, widget.Clickable{})
						reopenBtns = append(reopenBtns, widget.Clickable{})
					}
					for len(shortcutDelBtns) < len(st.Shortcuts) {
						shortcutDelBtns = append(shortcutDelBtns, widget.Clickable{})
						shortcutEditBtns = append(shortcutEditBtns, widget.Clickable{})
					}
					// a checkbox per window row, living across frames
					for _, w := range st.ManagedWindows {
						if bundleChecks[w.Instance] == nil {
							bundleChecks[w.Instance] = new(widget.Bool)
						}
					}
					if tab == tabWindows {
						for i, w := range st.ManagedWindows {
							if w.Open {
								if closeBtns[i].Clicked(gtx) {
									backend.CloseWindow(w.Instance)
								}
							} else if reopenBtns[i].Clicked(gtx) {
								backend.ReopenWindow(w.Instance)
							}
						}
						if ui.shortcutEditing != nil {
							if saveShortcutBtn.Clicked(gtx) {
								name := strings.TrimSpace(bundleNameEd.Text())
								oldName := ui.shortcutEditing.name
								spec, err := shortcutSpecJSON(ui.shortcutEditing)
								switch {
								case err != nil:
									setShortcutErr(err.Error())
								case name == "":
									setShortcutErr("give the bundle a name")
								default:
									// validation passed: the selection is consumed
									// here, on the Gio loop; the shortcut itself is
									// written in the background
									editing := ui.shortcutEditing
									ui.shortcutEditing = nil
									clearBundleChecks()
									go saveShortcut(name, oldName, spec, editing)
								}
							}
							if cancelShortcutBtn.Clicked(gtx) {
								ui.shortcutEditing = nil
							}
						} else {
							if createShortcutBtn.Clicked(gtx) {
								entries := pickedBundleWindows(st)
								if len(entries) > 0 {
									setShortcutEdit(newShortcutEditState(entries))
									setShortcutErr("")
								}
							}
							for i, sc := range st.Shortcuts {
								if shortcutDelBtns[i].Clicked(gtx) {
									go backend.DeleteShortcut(sc.Name)
								}
								if shortcutEditBtns[i].Clicked(gtx) {
									setShortcutEdit(editShortcutEditState(sc))
									bundleNameEd.SetText(sc.Name)
									setShortcutErr("")
								}
							}
						}
					} else {
						// the dev tab (only reachable in dev builds): load a
						// napp from a folder or a dev-server url, open and
						// unload the ephemeral ones below
						if loadURLBtn.Clicked(gtx) {
							if u := strings.TrimSpace(devURLed.Text()); u != "" {
								go backend.DevLoadURL(u)
							}
						}
						if loadFolderBtn.Clicked(gtx) {
							if p := strings.TrimSpace(devPathEd.Text()); p != "" {
								go backend.DevLoadFolder(p)
							}
						}
						if browseBtn.Clicked(gtx) {
							go pickAndLoadFolder(&devPathEd)
						}
						acted := false
						for i := range st.Dev {
							if devUnloadBtns[i].Clicked(gtx) {
								backend.DevUnload(st.Dev[i].ID)
								acted = true
							}
							if devPublishBtns[i].Clicked(gtx) {
								openDevPublishWindow(st.Dev[i].ID)
								acted = true
							}
						}
						if !acted {
							for i := range st.Dev {
								if devOpenBtns[i].Clicked(gtx) {
									backend.LaunchDev(st.Dev[i].ID)
								}
							}
						}
					}
					var devBtn *widget.Clickable
					if devEnabled {
						devBtn = &tabDevBtn
					}
					return layoutMain(gtx,
						th,
						&tabWindowsBtn,
						devBtn,
						&storeBtn,
						&themeBtn,
						&settingsBtn,
						&logoutBtn,
						tab,

						&windowsList,
						&devList,

						&devURLed,
						&devPathEd,
						&loadURLBtn,
						&browseBtn,
						&loadFolderBtn,

						closeBtns,
						reopenBtns,
						devOpenBtns,
						devUnloadBtns,
						devPublishBtns,

						&bundleNameEd,
						&createShortcutBtn,
						&saveShortcutBtn,
						&cancelShortcutBtn,
						shortcutDelBtns,
						shortcutEditBtns,

						st,
					)
				default:
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return material.Body1(th, "Loading\u2026").Layout(gtx)
					})
				}
			})

			e.Frame(gtx.Ops)

		case app.DestroyEvent:
			return
		}
	}
}
