package main

import (
	"strings"
	"unicode"
	"verdana/backend"
)

// Updating or uninstalling a napplet deletes its saved data: an update runs
// it under a new artifact with fresh storage and settings, and an uninstall
// removes them along with its permissions. Neither is one click any more for
// a napplet: the store parks a storeConfirm and draws a dialog that says so,
// and only its destructive button reaches the backend. Napps keep their
// one-click update and uninstall.

const (
	confirmUpdate    = "update"
	confirmUninstall = "uninstall"
)

// storeConfirm is a napplet update or uninstall waiting for the user. There
// is at most one, in store.confirm, guarded by store.mu.
type storeConfirm struct {
	// kind is confirmUpdate or confirmUninstall.
	kind string
	// id is the napplet's id, which is what Update and Uninstall take.
	id string
	// name is the napplet's display name for the dialog (see confirmName).
	name string
	// target is what Install gets when viaInstall is set: the profile list
	// updates a napplet by installing the version it shows.
	target     backend.Napp
	viaInstall bool
}

// The backend calls the store makes after a confirmation, or straight away
// for a napp, off the frame goroutine. Tests swap them to observe the calls
// without a running backend.
var (
	storeUpdate    = func(id string) { go backend.Update(id) }
	storeInstall   = func(n backend.Napp) { go backend.Install(n) }
	storeUninstall = func(id string) { go backend.Uninstall(id) }
)

// confirmName is the name a confirm dialog calls a napplet by: its Name, else
// its d, else "Unnamed napplet". Both are relay text, so control and format
// runes (bidi overrides among them) go, whitespace is collapsed and the
// result is cut to 32 runes. It is never the kind:pubkey:d id.
func confirmName(n backend.Napp) string {
	for _, s := range []string{n.Name, n.D} {
		if s = truncate(stripControl(s), 32); s != "" {
			return s
		}
	}
	return "Unnamed napplet"
}

// stripControl turns whitespace into spaces and drops every other control
// or format rune, so author text cannot reorder or break a dialog line.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// dialogCopy is the title, body and two button labels of the dialog.
func (c *storeConfirm) dialogCopy() (title, body, yesLabel, noLabel string) {
	if c.kind == confirmUninstall {
		return "Uninstall " + c.name + "?",
			"Uninstalling deletes this napplet's saved data, settings and permissions, and closes its open windows.",
			"Uninstall napplet", "Keep napplet"
	}
	return "Update " + c.name + "?",
		"Updating resets this napplet's saved data, including its settings. The permissions you gave it are kept.",
		"Update and reset data", "Keep current version"
}

// requestUpdate is what every store Update button does. While the napp is
// busy it does nothing. A napp updates at once; a napplet parks a
// confirmation, replacing any other one. viaInstall updates by installing n
// itself (the profile list) rather than through backend.Update.
func requestUpdate(n backend.Napp, viaInstall, busy bool) {
	if busy {
		return
	}
	if !n.IsNapplet() {
		runConfirmed(&storeConfirm{kind: confirmUpdate, id: n.ID, target: n, viaInstall: viaInstall})
		return
	}
	parkConfirm(&storeConfirm{kind: confirmUpdate, id: n.ID, name: confirmName(n), target: n, viaInstall: viaInstall})
}

// parkConfirm puts c up as the store's one pending confirmation. The click
// that asked for it is handled mid-frame, so the window is asked for another
// frame to draw the dialog without waiting for the next input event.
func parkConfirm(c *storeConfirm) {
	store.mu.Lock()
	store.confirm = c
	store.mu.Unlock()
	if w := storeWindow(); w != nil {
		w.Invalidate()
	}
}

// pendingConfirm is the confirmation on screen, if any.
func pendingConfirm() *storeConfirm {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.confirm
}

// takeConfirm clears the pending confirmation and hands it back, so the
// dialog is gone before anything acts on it and one confirmation acts once.
func takeConfirm() *storeConfirm {
	store.mu.Lock()
	defer store.mu.Unlock()
	c := store.confirm
	store.confirm = nil
	return c
}

// confirmYes is the destructive button: the dialog goes, then the action runs.
func confirmYes() {
	if c := takeConfirm(); c != nil {
		runConfirmed(c)
	}
}

// confirmNo is the dismiss chip: the dialog goes and nothing else happens.
func confirmNo() {
	takeConfirm()
}

// runConfirmed starts the backend call c stands for.
func runConfirmed(c *storeConfirm) {
	switch {
	case c.kind == confirmUninstall:
		storeUninstall(c.id)
	case c.viaInstall:
		storeInstall(c.target)
	default:
		storeUpdate(c.id)
	}
}
