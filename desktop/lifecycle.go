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
	show           chan struct{}
	quit           chan struct{}
	once           sync.Once
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

func showPendingPrimary() {
	phase := backend.Phase()
	if phase == backend.PhaseLoading {
		return
	}
	desktopLifecycle.Lock()
	if !desktopLifecycle.primaryPending {
		desktopLifecycle.Unlock()
		return
	}
	desktopLifecycle.primaryPending = false
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
