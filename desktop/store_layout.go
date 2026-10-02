package main

import (
	"image"
	"strconv"
	"strings"
	"verdana/backend"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// storeViewLabels name the store's lists, in storeInstalled/storeDiscover
// order.
var storeViewLabels = [2]string{"Installed", "Discover"}

// readableWidth caps napp and profile pages, so their text stays readable
// however wide the store window gets.
const readableWidth = unit.Dp(760)

// layoutStoreHeader is the store's top bar: Back and the page's name on the
// left while a page is open, the switch between the lists in the middle, and
// the signed-in user's profile and launcher settings on the right.
func layoutStoreHeader(
	gtx layout.Context,
	th *material.Theme,
	backBtn *widget.Clickable,
	viewBtns *[2]widget.Clickable,
	settingsBtn *widget.Clickable,
	page *storePage,
	view,
	nInstalled int,
	profileName,
	profilePicture string,
) layout.Dimensions {
	p := currentTheme()
	return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(24), Right: unit.Dp(24)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// the two flexed sides share what is left equally, which keeps the
		// switcher centred whatever sits at the left
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if page == nil {
					return layout.Dimensions{Size: image.Point{X: gtx.Constraints.Min.X}}
				}
				return layout.W.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							pointer.CursorPointer.Add(gtx.Ops)
							b := material.Button(th, backBtn, "← Back")
							b.Background = p.chipBg
							b.Color = p.chipFg
							b.TextSize = unit.Sp(13)
							b.Inset = layout.UniformInset(unit.Dp(8))
							return b.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Body1(th, page.title)
							l.Font.Weight = font.Bold
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btn := func(v int) layout.FlexChild {
					return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						pointer.CursorPointer.Add(gtx.Ops)
						label := storeViewLabels[v]
						if v == storeInstalled && nInstalled > 0 {
							label += " (" + strconv.Itoa(nInstalled) + ")"
						}
						b := material.Button(th, &viewBtns[v], label)
						b.Inset = layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(18), Right: unit.Dp(18)}
						if view != v || page != nil {
							b.Background = p.chipBg
							b.Color = p.chipFg
						}
						return b.Layout(gtx)
					})
				}
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					btn(storeDiscover),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					btn(storeInstalled),
				)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return avatar(gtx, profilePicture, 32)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Body2(th, profileName)
							l.Font.Weight = font.Bold
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							pointer.CursorPointer.Add(gtx.Ops)
							b := material.Button(th, settingsBtn, "⚙ Settings")
							b.Background = p.chipBg
							b.Color = p.chipFg
							b.TextSize = unit.Sp(13)
							b.Inset = layout.UniformInset(unit.Dp(8))
							return b.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

// readableColumn lays w out no wider than readableWidth, centred in the
// space it is given.
func readableColumn(gtx layout.Context, w layout.Widget) layout.Dimensions {
	maxW := gtx.Dp(readableWidth)
	full := gtx.Constraints.Max.X
	if full <= maxW {
		return w(gtx)
	}
	gtx.Constraints.Max.X = maxW
	gtx.Constraints.Min.X = min(gtx.Constraints.Min.X, maxW)
	defer op.Offset(image.Pt((full-maxW)/2, 0)).Push(gtx.Ops).Pop()
	dims := w(gtx)
	dims.Size.X = full
	return dims
}

// layoutStoreLoggedOut stands in for the store while nobody is logged in:
// installing and trying napps need an account, and logging in happens in the
// main window.
func layoutStoreLoggedOut(gtx layout.Context, th *material.Theme, managerBtn *widget.Clickable, login bool) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if !login {
			return material.Body1(th, "Loading…").Layout(gtx)
		}
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body1(th, "Log in to Verdana to browse and install napps.")
				l.Color = currentTheme().muted
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				pointer.CursorPointer.Add(gtx.Ops)
				return material.Button(th, managerBtn, "Log in").Layout(gtx)
			}),
		)
	})
}

func layoutNappsTab(
	gtx layout.Context,
	th *material.Theme,
	list *widget.List,
	filterEd *widget.Editor,
	cardBtns,
	uninstBtns []widget.Clickable,
	installedUpdateBtns []widget.Clickable,
	openBtns,
	authorBtns,
	settingsBtns []widget.Clickable,
	checkUpdBtn *widget.Clickable,
	vis []int,
	st backend.State,
) layout.Dimensions {
	if len(st.Installed) == 0 {
		l := material.Body2(th, "No napps installed yet. Find some under Discover.")
		l.Color = currentTheme().muted
		return l.Layout(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// the filter box, narrowing the entries below by name, author,
		// author name or description, and the manual update check at its
		// right (the automatic one runs on its own after startup).
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return editorBox(gtx, th, filterEd, "filter by name, author or description")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					pointer.CursorPointer.Add(gtx.Ops)
					p := currentTheme()
					b := material.Button(th, checkUpdBtn, "\u21bb")
					b.Background = p.chipBg
					b.Color = p.chipFg
					if st.UpdateCheckRunning {
						b.Color = p.muted
					}
					b.TextSize = unit.Sp(15)
					b.Inset = layout.UniformInset(unit.Dp(8))
					return b.Layout(gtx)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(vis) == 0 {
				l := material.Body2(th, "Nothing matches the filter.")
				l.Color = currentTheme().muted
				return l.Layout(gtx)
			}
			// a grid of tiles, as many across as the window fits; a narrow
			// window gets the one-per-row cards instead
			return nappGrid(gtx, th, list, &installedCols, vis, func(gtx layout.Context, row int, tile bool) layout.Dimensions {
				var cardBtn, uninstBtn *widget.Clickable
				if row < len(cardBtns) {
					cardBtn = &cardBtns[row]
				}
				if row < len(uninstBtns) {
					uninstBtn = &uninstBtns[row]
				}
				// the card opens the napp page; Open launches it
				var updateBtn, openBtn, authorBtn, settingsBtn *widget.Clickable
				if row < len(installedUpdateBtns) && st.Installed[row].UpdateAvailable != nil {
					updateBtn = &installedUpdateBtns[row]
				}
				if row < len(openBtns) {
					openBtn = &openBtns[row]
				}
				if row < len(authorBtns) {
					authorBtn = &authorBtns[row]
				}
				if row < len(settingsBtns) && st.Installed[row].IsNapplet() {
					settingsBtn = &settingsBtns[row]
				}
				if tile {
					return renderNappTile(gtx, th, cardBtn, authorBtn, openBtn, settingsBtn, uninstBtn, updateBtn, "Open", "Uninstall", "Update", false, st.Installed[row])
				}
				return renderNappCard(gtx, th, cardBtn, authorBtn, openBtn, settingsBtn, uninstBtn, updateBtn, "Open", "Uninstall", "Update", false, st.Installed[row])
			})
		}),
	)
}

func layoutDiscoveryTab(
	gtx layout.Context,
	th *material.Theme,
	list *widget.List,
	filterEd *widget.Editor,
	fetchBtn *widget.Clickable,
	cardBtns,
	openBtns,
	authorBtns []widget.Clickable,
	vis []int,
	fetchErr string,
	fetching bool,
	discovery []backend.Napp,
	lookup *backend.AddressLookup,
	installedSet,
	follows map[string]bool,
) layout.Dimensions {
	chip := func(gtx layout.Context, btn *widget.Clickable, label string, on bool) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		b := material.Button(th, btn, label)
		b.TextSize = unit.Sp(13)
		b.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}
		if on {
			b.Background = th.Palette.ContrastBg
		} else {
			b.Background = currentTheme().chipBg
			b.Color = currentTheme().chipFg
		}
		return b.Layout(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// the filter box comes first, narrowing the entries below by name,
		// author, author name or description, or naming one by its address,
		// with the scope beside it: everyone's apps or just friends'.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			scopeBtn := func(k int) layout.FlexChild {
				return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return chip(gtx, &discoScopeBtns[k], discoScopeLabels[k], discoScope == k)
					})
				})
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return editorBox(gtx, th, filterEd, "filter by name, author or description, or paste an naddr")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				scopeBtn(discoScopeFriends),
				scopeBtn(discoScopeGlobal),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
		// the kind tabs (all, napps, napplets) and, at the other end, the
		// refresh button that asks the relays again
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var counts [3]int
			for _, n := range discovery {
				if !inDiscoScope(n, follows) {
					continue
				}
				counts[discoKindAll]++
				if matchesKind(n, discoKindNapplets) {
					counts[discoKindNapplets]++
				} else {
					counts[discoKindNapps]++
				}
			}
			kindBtn := func(k int) layout.FlexChild {
				return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						label := discoKindLabels[k]
						if counts[k] > 0 {
							label += " (" + strconv.Itoa(counts[k]) + ")"
						}
						return chip(gtx, &discoKindBtns[k], label, discoKind == k)
					})
				})
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				kindBtn(discoKindAll),
				kindBtn(discoKindNapps),
				kindBtn(discoKindNapplets),
				layout.Flexed(1, layout.Spacer{}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					pointer.CursorPointer.Add(gtx.Ops)
					label := "Refresh"
					if fetching {
						label = "Refreshing\u2026"
					}
					b := material.Button(th, fetchBtn, label)
					b.TextSize = unit.Sp(13)
					b.Inset = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}
					return b.Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if fetchErr == "" {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(th, fetchErr)
				l.Color = currentTheme().danger
				return l.Layout(gtx)
			})
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(vis) == 0 {
				msg := "No napps yet. Pick some good relays in Settings and click \"Refresh\"."
				if fetching {
					msg = "Searching relays\u2026"
				}
				if len(discovery) > 0 {
					msg = "Nothing matches the filter."
					if strings.TrimSpace(filterEd.Text()) == "" {
						noun := strings.ToLower(discoKindLabels[discoKind])
						if discoKind == discoKindAll {
							noun = "apps"
						}
						switch {
						case discoScope == discoScopeFriends && len(follows) == 0:
							msg = "Loading who you follow…"
						case discoScope == discoScopeFriends:
							msg = "No " + noun + " from people you follow yet. Switch to Global to see everyone's."
						default:
							msg = "No " + noun + " found on these relays."
						}
					}
				}
				if lookup != nil {
					switch {
					case lookup.Pending:
						msg = "Looking up that address\u2026"
					case lookup.Err != "":
						msg = "Couldn't open that address: " + lookup.Err + "."
					}
				}
				l := material.Body2(th, msg)
				l.Color = currentTheme().muted
				return l.Layout(gtx)
			}
			// a grid of tiles, as many across as the window fits; a narrow
			// window gets the one-per-row cards instead. The only button is
			// Try: installing, updating and opening live in the detail tab
			// the card opens.
			card := func(gtx layout.Context, row int, tile bool) layout.Dimensions {
				n := discovery[row]
				var cardBtn, tryBtn, authorBtn *widget.Clickable
				if row < len(cardBtns) {
					cardBtn = &cardBtns[row]
				}
				if row < len(openBtns) && !installedSet[n.ID] && n.IsNapplet() {
					tryBtn = &openBtns[row]
				}
				if row < len(authorBtns) {
					authorBtn = &authorBtns[row]
				}
				if tile {
					return renderNappTile(gtx, th, cardBtn, authorBtn, tryBtn, nil, nil, nil, "Try", "", "", false, n)
				}
				return renderNappCard(gtx, th, cardBtn, authorBtn, tryBtn, nil, nil, nil, "Try", "", "", false, n)
			}
			return nappGrid(gtx, th, list, &discoCols, vis, card)
		}),
	)
}
