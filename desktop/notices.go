package main

import (
	"image"
	"io"
	"strings"
	"verdana/backend"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// The notice stack (03-UI-SPEC S1) sits at the top of the manager window's
// login and main screens: launcher-level problems the user should know about,
// such as a login kept in a file because the keyring was unavailable, a
// state.json that could not be read, or napp windows that failed closed.
// The store window draws the napplet-scoped ones too, in a strip of the same
// cards (storeNoticeFilter). The backend owns the list, its order and its
// copy (State.Notices); this file only draws it and turns clicks into
// backend calls.

// noticeStateCorruptPrefix starts the ID of the corrupt-state notice, the
// only one with a path to copy.
const noticeStateCorruptPrefix = "state-corrupt:"

// noticeWidget is the clickable state of one notice card, kept across frames.
type noticeWidget struct {
	dismiss  widget.Clickable
	copyPath widget.Clickable
}

// noticeState is one window's notice stack state. The manager and the store
// run separate frame goroutines, so each owns one and only its own frames
// touch it: managerNotices is used by the manager's main and login screens,
// storeNotices by the store window. copied remembers the notices whose path
// was copied, so their chip reads "Copied" for the rest of the process.
type noticeState struct {
	widgets map[string]*noticeWidget
	copied  map[string]bool
}

func newNoticeState() *noticeState {
	return &noticeState{
		widgets: map[string]*noticeWidget{},
		copied:  map[string]bool{},
	}
}

var (
	managerNotices = newNoticeState()
	storeNotices   = newNoticeState()
)

// The napplet-scoped notices (05-UI-SPEC S4) are also shown in the store
// window, where the Try or launch that raised them usually happened. These
// ids are the backend's; the desktop keeps its own copy, as it does for
// noticeStateCorruptPrefix.
const (
	noticeTrialFailed     = "napplet-trial-failed"
	noticeRequiresPrefix  = "napplet-requires:"
	noticeTrialDataPrefix = "trial-data-discarded:"
)

// storeNoticeFilter keeps the notices the store strip shows, in the order
// given: the trial failure and the per-napplet requires and trial-data
// warnings. Launcher-level notices (keyring, corrupt state, the window
// program, hardening, reinstall) stay in the manager alone.
func storeNoticeFilter(notices []backend.Notice) []backend.Notice {
	var out []backend.Notice
	for _, n := range notices {
		if n.ID == noticeTrialFailed ||
			strings.HasPrefix(n.ID, noticeRequiresPrefix) ||
			strings.HasPrefix(n.ID, noticeTrialDataPrefix) {
			out = append(out, n)
		}
	}
	return out
}

// onDismissNotice is what Dismiss runs, off the frame goroutine. Tests swap
// it to observe the call without a running backend.
var onDismissNotice = backend.DismissNotice

// chipButton is the launcher's secondary button: chip colors, 13sp text and
// an 8dp inset, used for every action that is not the screen's primary one.
func chipButton(th *material.Theme, btn *widget.Clickable, label string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		t := currentTheme()
		b := material.Button(th, btn, label)
		b.Background = t.chipBg
		b.Color = t.chipFg
		b.TextSize = unit.Sp(13)
		b.Inset = layout.UniformInset(unit.Dp(8))
		return b.Layout(gtx)
	}
}

// layoutNotices draws the notices in the order given (the backend sorts
// them), at most one card per ID, 8dp apart and with 16dp below the last,
// keeping its widgets in ns, the calling window's state. With no notices it
// draws nothing and takes no space, so the screens below look exactly as
// they do without it. Clicks are handled here, in the frame; Dismiss goes
// to the backend, so the card leaves every window on its next frame.
func layoutNotices(gtx layout.Context, th *material.Theme, ns *noticeState, notices []backend.Notice) layout.Dimensions {
	seen := make(map[string]bool, len(notices))
	shown := make([]backend.Notice, 0, len(notices))
	for _, n := range notices {
		if n.ID == "" || seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		shown = append(shown, n)
	}
	// forget the widgets of notices that are gone, but not what was copied
	for id := range ns.widgets {
		if !seen[id] {
			delete(ns.widgets, id)
		}
	}
	if len(shown) == 0 {
		return layout.Dimensions{}
	}

	for _, n := range shown {
		w := ns.widgetFor(n.ID)
		if w.dismiss.Clicked(gtx) {
			go onDismissNotice(n.ID)
		}
		if w.copyPath.Clicked(gtx) && n.Path != "" {
			gtx.Execute(clipboard.WriteCmd{
				Type: "application/text",
				Data: io.NopCloser(strings.NewReader(n.Path)),
			})
			ns.copied[n.ID] = true
		}
	}

	// the stack is as tall as its cards, whatever the caller allows
	gtx.Constraints.Min.Y = 0
	children := make([]layout.FlexChild, 0, 2*len(shown))
	for i, n := range shown {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout))
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutNoticeCard(gtx, th, n, ns.widgetFor(n.ID), ns.copied[n.ID])
		}))
	}
	children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// widgetFor is the clickable state of the card for id in this window.
func (ns *noticeState) widgetFor(id string) *noticeWidget {
	w := ns.widgets[id]
	if w == nil {
		w = new(noticeWidget)
		ns.widgets[id] = w
	}
	return w
}

// layoutNoticeCard draws one notice: a full-width card with the title, the
// detail and the path box on the left and its buttons on the right. No text
// is ever cut short: titles, details and paths wrap. copied makes the path
// chip read "Copied".
func layoutNoticeCard(gtx layout.Context, th *material.Theme, n backend.Notice, w *noticeWidget, copied bool) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Min.Y = 0
	macro := op.Record(gtx.Ops)
	dims := layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layoutNoticeText(gtx, th, n)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !strings.HasPrefix(n.ID, noticeStateCorruptPrefix) {
					return chipButton(th, &w.dismiss, "Dismiss")(gtx)
				}
				label := "Copy path"
				if copied {
					label = "Copied"
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(chipButton(th, &w.copyPath, label)),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(chipButton(th, &w.dismiss, "Dismiss")),
				)
			}),
		)
	})
	call := macro.Stop()
	defer clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, currentTheme().card)
	call.Add(gtx.Ops)
	return dims
}

// layoutNoticeText is a notice's text column: the title in bold (danger for
// an error, the normal text color for a warning), the detail in subtle text
// and, when there is one, the path in a code box that wraps at any character.
func layoutNoticeText(gtx layout.Context, th *material.Theme, n backend.Notice) layout.Dimensions {
	t := currentTheme()
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(th, n.Title)
			l.Font.Weight = font.Bold
			l.Color = t.fg
			if n.Kind == "error" {
				l.Color = t.danger
			}
			return l.Layout(gtx)
		}),
	}
	if n.Detail != "" {
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(th, n.Detail)
				l.Color = t.subtle
				return l.Layout(gtx)
			}),
		)
	}
	if n.Path != "" {
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				macro := op.Record(gtx.Ops)
				dims := layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, n.Path)
					l.Color = t.codeFg
					l.WrapPolicy = text.WrapGraphemes
					return l.Layout(gtx)
				})
				call := macro.Stop()
				defer clip.UniformRRect(image.Rectangle{Max: dims.Size}, gtx.Dp(unit.Dp(6))).Push(gtx.Ops).Pop()
				paint.Fill(gtx.Ops, t.codeBg)
				call.Add(gtx.Ops)
				return dims
			}),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
