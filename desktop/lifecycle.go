package main

import (
	"sync"
	"verdana/backend"

	"gioui.org/app"
	"gioui.org/io/system"
)

var desktopLifecycle = struct {
	sync.Mutex
	window *app.Window
	// store is the store window while it is open; storeStarting covers the
	// moment between asking for one and it existing, so two clicks don't
	// make two.
	store          *app.Window
	storeStarting  bool
	primaryPending bool
	// keyringShown is the KeyringWait the manager was last opened for
	// while a primary was pending, so a slow keyring raises it once per
	// state instead of on every state change.
	keyringShown string
	show         chan struct{}
	quit         chan struct{}
	once         sync.Once
}{
	show: make(chan struct{}, 1),
	quit: make(chan struct{}),
}

func setManagerWindow(w *app.Window) {
	desktopLifecycle.Lock()
	desktopLifecycle.window = w
	desktopLifecycle.Unlock()
}

func managerWindow() *app.Window {
	desktopLifecycle.Lock()
	defer desktopLifecycle.Unlock()
	return desktopLifecycle.window
}

// showManager raises the manager when it exists, or asks the desktop loop to
// create it. It is safe for tray and single-instance callbacks to call.
func showManager() {
	if w := managerWindow(); w != nil {
		w.Perform(system.ActionRaise)
		w.Invalidate()
		return
	}
	select {
	case desktopLifecycle.show <- struct{}{}:
	default:
	}
}

// showPrimary opens the window that represents Verdana's normal entry point:
// login while signed out, and the store once a session has been restored. A
// restored login completes asynchronously, so a request made during loading is
// remembered and fulfilled by the next backend state change.
func showPrimary() {
	desktopLifecycle.Lock()
	desktopLifecycle.primaryPending = true
	desktopLifecycle.Unlock()
	showPendingPrimary()
}

// currentPhase and currentKeyringWait read the backend for
// showPendingPrimary; tests swap them. Both only take the backend's ls.mu,
// so they are safe from a StateChanged callback.
var (
	currentPhase       = backend.Phase
	currentKeyringWait = backend.KeyringWait
)

func showPendingPrimary() {
	phase := currentPhase()
	wait := ""
	if phase == backend.PhaseLoading {
		wait = currentKeyringWait()
	}
	desktopLifecycle.Lock()
	if !desktopLifecycle.primaryPending {
		desktopLifecycle.Unlock()
		return
	}
	if phase == backend.PhaseLoading {
		// A resume can wait up to 2 minutes on a keyring unlock prompt
		// (D-11). Without a window the user would see nothing, so once the
		// backend reports a keyring wait the manager opens to show it, and
		// again if that wait fails (D-19). The primary stays owed: when
		// loading ends it opens as usual.
		raise := wait != "" && wait != desktopLifecycle.keyringShown
		desktopLifecycle.keyringShown = wait
		desktopLifecycle.Unlock()
		if raise {
			showManager()
		}
		return
	}
	desktopLifecycle.primaryPending = false
	desktopLifecycle.keyringShown = ""
	desktopLifecycle.Unlock()
	if phase == backend.PhaseMain {
		showStore()
	} else {
		showManager()
	}
}

func setStoreWindow(w *app.Window) {
	desktopLifecycle.Lock()
	desktopLifecycle.store = w
	desktopLifecycle.storeStarting = false
	desktopLifecycle.Unlock()
}

func storeWindow() *app.Window {
	desktopLifecycle.Lock()
	defer desktopLifecycle.Unlock()
	return desktopLifecycle.store
}

// showStore raises the store window, or opens one when there is none. Like
// showManager, it is safe to call from any goroutine.
func showStore() {
	desktopLifecycle.Lock()
	w, starting := desktopLifecycle.store, desktopLifecycle.storeStarting
	if w == nil && !starting {
		desktopLifecycle.storeStarting = true
	}
	desktopLifecycle.Unlock()
	switch {
	case w != nil:
		w.Perform(system.ActionRaise)
		w.Invalidate()
	case !starting:
		go runStoreWindow()
	}
}

// invalidateAll redraws every open launcher window, for changes any of them
// may be showing: launcher state, images, the theme.
func invalidateAll() {
	for _, w := range []*app.Window{managerWindow(), storeWindow()} {
		if w != nil {
			w.Invalidate()
		}
	}
}

func quitDesktop() {
	desktopLifecycle.once.Do(func() { close(desktopLifecycle.quit) })
	for _, w := range []*app.Window{managerWindow(), storeWindow()} {
		if w != nil {
			w.Perform(system.ActionClose)
		}
	}
}

func desktopLoop(background bool) {
	if !background {
		showPrimary()
	}
	for {
		select {
		case <-desktopLifecycle.show:
			select {
			case <-desktopLifecycle.quit:
				return
			default:
			}
			gioMain()
		case <-desktopLifecycle.quit:
			return
		}
	}
}
