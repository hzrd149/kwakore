package main

import (
	"verdana/backend"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// A napplet whose NIP-01 latest event is invalid is unavailable (D-16): the
// backend lists it with a fixed reason phrase in Napp.Unavailable and keeps
// no paths, servers or actions for it. The store says so wherever the entry
// shows and never offers to try, install or update it. An installed copy
// keeps working: it stays openable, configurable and removable.

const (
	// unavailableStatus is the first line of every unavailable block.
	unavailableStatus = "Unavailable — the latest version is invalid"
	// unavailableInstalledLine ends the block for an installed copy.
	unavailableInstalledLine = "Your installed version still works."
	// tryOpeningLabel is what Try reads while the backend downloads and
	// verifies a napplet's files before opening it (UI-D9).
	tryOpeningLabel = "Opening…"
)

// unavailableLines is the text of n's unavailable block: nil while n is
// available, else the status, the backend's reason phrase with a period and,
// when installed, the line saying the installed copy still works. The reason
// is a catalogue phrase, never author text, so it is drawn as it comes.
func unavailableLines(n backend.Napp, installed bool) []string {
	if n.Unavailable == "" {
		return nil
	}
	lines := []string{unavailableStatus, n.Unavailable + "."}
	if installed {
		lines = append(lines, unavailableInstalledLine)
	}
	return lines
}

// tryAllowed says whether the store offers Try for n: only for a napplet that
// is not installed (an installed one opens) and is not unavailable.
func tryAllowed(n backend.Napp, installed bool) bool {
	return n.IsNapplet() && !installed && n.Unavailable == ""
}

// unavailableDrops says whether a button with this label is left off an
// unavailable entry's tile or card. The callers pass no such button already;
// this keeps a stale caller from drawing one anyway.
func unavailableDrops(n backend.Napp, label string) bool {
	if n.Unavailable == "" {
		return false
	}
	switch label {
	case "Try", tryOpeningLabel, "Install", "Update":
		return true
	}
	return false
}

// cardName is the name a tile, card or page draws for n. An unavailable
// entry's name comes from an event that failed validation, so it goes
// through the confirm dialogs' display-name rule (Name, else d, else
// "Unnamed napplet", sanitized and cut to 32 runes), never the id.
func cardName(n backend.Napp) string {
	if n.Unavailable != "" {
		return confirmName(n)
	}
	return n.Name
}

// pageActions is the napp page's action row: which buttons it draws, and
// with which labels. An empty label is no button.
type pageActions struct {
	open     string // "Open", "Try" or tryOpeningLabel
	primary  string // "Install", "Uninstall" or "Working…"
	update   bool
	settings bool
	copyAddr bool
}

// nappPageActions decides the napp page's action row for n. An unavailable
// entry gets no Try, Install or Update: installed it keeps Open, Uninstall,
// Settings and Copy address; not installed it keeps only Copy address.
func nappPageActions(n backend.Napp, installed, busy bool) pageActions {
	a := pageActions{
		settings: installed,
		copyAddr: n.Naddr() != "",
		update:   installedShowsUpdate(n),
	}
	switch {
	case installed:
		a.open = "Open"
	case tryAllowed(n, installed):
		a.open = tryLabel(n, busy)
	}
	switch {
	case installed && busy:
		a.primary = "Working…"
	case installed:
		a.primary = "Uninstall"
	case n.Unavailable != "":
	case busy:
		a.primary = "Working…"
	default:
		a.primary = "Install"
	}
	return a
}

// labels lists the row's buttons in drawing order, for tests.
func (a pageActions) labels() []string {
	var out []string
	for _, s := range []string{a.open, a.primary} {
		if s != "" {
			out = append(out, s)
		}
	}
	if a.update {
		out = append(out, "Update")
	}
	if a.settings {
		out = append(out, "Settings")
	}
	if a.copyAddr {
		out = append(out, "Copy address")
	}
	return out
}

// layoutUnavailableBlock draws lines from unavailableLines: the status in
// bold danger, then each other line 4dp down in subtle. maxLines caps every
// line on tiles and cards; 0 lets them wrap freely, as on the napp page.
// Colors are read from the theme on each frame.
func layoutUnavailableBlock(gtx layout.Context, th *material.Theme, lines []string, maxLines int) layout.Dimensions {
	if len(lines) == 0 {
		return layout.Dimensions{}
	}
	p := currentTheme()
	children := make([]layout.FlexChild, 0, 2*len(lines))
	for i, s := range lines {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout))
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(th, s)
			l.MaxLines = maxLines
			l.Color = p.subtle
			if i == 0 {
				l.Font.Weight = font.Bold
				l.Color = p.danger
			}
			return l.Layout(gtx)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// tryLabel is what a Try button for n reads: tryOpeningLabel while the
// backend is busy downloading and verifying its files (D-13), else "Try".
// It keeps its suggest colors either way, and requestTry ignores clicks
// while busy.
func tryLabel(n backend.Napp, busy bool) string {
	if busy && n.IsNapplet() {
		return tryOpeningLabel
	}
	return "Try"
}

// storeTry is what an accepted Try click runs. Tests swap it to observe the
// call without a running backend.
var storeTry = backend.TryNapplet

// requestTry is what every store Try button does: nothing while n is busy
// (a trial is already verifying its files) or when the store offers no Try
// for it (installed, unavailable, or a napp), else it starts the trial.
func requestTry(n backend.Napp, installed, busy bool) {
	if busy || !tryAllowed(n, installed) {
		return
	}
	storeTry(n)
}

// installedShowsUpdate says whether the installed tab offers Update for n.
// The backend never sets UpdateAvailable on an unavailable record; this
// keeps one that did from drawing the button.
func installedShowsUpdate(n backend.Napp) bool {
	return n.UpdateAvailable != nil && n.Unavailable == ""
}

// profileRowLabels are the labels of a profile page row's three buttons;
// an empty one is not drawn. open is Open for an installed entry and Try (or
// tryOpeningLabel) for a tryable napplet; action is Install, Uninstall or
// "Working…" while busy; update is Update when there is one. An unavailable
// entry gets no Try, Install or Update.
func profileRowLabels(n backend.Napp, installed, busy bool) (open, action, update string) {
	switch {
	case installed:
		open = "Open"
	case tryAllowed(n, installed):
		open = tryLabel(n, busy)
	}
	switch {
	case installed && busy, !installed && busy && n.Unavailable == "":
		action = "Working…"
	case installed:
		action = "Uninstall"
	case n.Unavailable == "":
		action = "Install"
	}
	if installed && installedShowsUpdate(n) {
		update = "Update"
	}
	return open, action, update
}
