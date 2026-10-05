package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"verdana/backend"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// storePage is a page the store opens over its lists: either a napp page or
// a profile page.
type storePage struct {
	kind   string // "napp" or "profile"
	title  string
	nappID string
	napp   backend.Napp // fallback copy for napps not in installed/discovery
	pubkey string       // hex, for profile pages (and napp author)
}

// nappDetailList scrolls the napp page, whose description and file list
// can run past the bottom of the window.
var nappDetailList = widget.List{List: layout.List{Axis: layout.Vertical}}

func openNappPage(n backend.Napp) {
	pubkey := ""
	if n.Author.Hex() != "" {
		pubkey = n.Author.Hex()
	}
	nappDetailList.Position = layout.Position{}
	ensureProfile(pubkey)
	pushStorePage(&storePage{kind: "napp", title: truncate(n.Label(), 32), nappID: n.ID, napp: n, pubkey: pubkey})
}

func openProfilePage(pubkeyHex string) {
	pubkeyHex = strings.TrimSpace(pubkeyHex)
	if pubkeyHex == "" {
		return
	}
	name := pubkeyHex
	if len(name) > 12 {
		name = name[:12] + "…"
	}
	if p := cachedProfile(pubkeyHex); p != nil && p.ShortName != "" {
		name = truncate(p.ShortName, 32)
	}
	ensureProfile(pubkeyHex)
	ensureAuthorNapps(pubkeyHex)
	pushStorePage(&storePage{kind: "profile", title: name, pubkey: pubkeyHex})
}

// ─── cached profile + author napps ──────────────────────────────
// Fetches run off the Gio loop; results land here and invalidate the
// window. Layout reads only the caches, never blocks.

var (
	profMu       sync.Mutex
	profileCache = make(map[string]backend.ProfileDetail)
	profileBusy  = make(map[string]bool)

	authorMu       sync.Mutex
	authorNapps    = make(map[string][]backend.Napp)
	authorFetching = make(map[string]bool)
	authorErr      = make(map[string]string)
)

func cachedProfile(pubkeyHex string) *backend.ProfileDetail {
	profMu.Lock()
	defer profMu.Unlock()
	if p, ok := profileCache[pubkeyHex]; ok {
		cp := p
		return &cp
	}
	return nil
}

func ensureProfile(pubkeyHex string) {
	if pubkeyHex == "" {
		return
	}
	profMu.Lock()
	if _, ok := profileCache[pubkeyHex]; ok {
		profMu.Unlock()
		return
	}
	if profileBusy[pubkeyHex] {
		profMu.Unlock()
		return
	}
	profileBusy[pubkeyHex] = true
	profMu.Unlock()
	go func() {
		p := backend.FetchProfileDetail(pubkeyHex)
		profMu.Lock()
		profileCache[pubkeyHex] = p
		delete(profileBusy, pubkeyHex)
		profMu.Unlock()
		invalidateAll()
	}()
}

func cachedAuthorNapps(pubkeyHex string) ([]backend.Napp, bool, string) {
	authorMu.Lock()
	defer authorMu.Unlock()
	n := authorNapps[pubkeyHex]
	return n, authorFetching[pubkeyHex], authorErr[pubkeyHex]
}

func ensureAuthorNapps(pubkeyHex string) {
	if pubkeyHex == "" {
		return
	}
	authorMu.Lock()
	if _, ok := authorNapps[pubkeyHex]; ok {
		authorMu.Unlock()
		return
	}
	if authorFetching[pubkeyHex] {
		authorMu.Unlock()
		return
	}
	authorFetching[pubkeyHex] = true
	delete(authorErr, pubkeyHex)
	authorMu.Unlock()
	go func() {
		list := backend.FetchAuthorNapps(pubkeyHex)
		authorMu.Lock()
		authorNapps[pubkeyHex] = list
		authorFetching[pubkeyHex] = false
		authorMu.Unlock()
		invalidateAll()
	}()
}

// detailNapp resolves the napp a napp page shows. An installed napp is its
// entry in st, the snapshot this frame draws: only the snapshot (and
// LookupNapp, which stamps the same way) carries UpdateAvailable and
// Unavailable, so a saved record would never show Update or the
// unavailable status. Otherwise it is the freshest copy the launcher knows
// (so install/uninstall reflect immediately), falling back to the copy taken
// when the page was opened.
func detailNapp(st backend.State, tab *storePage) backend.Napp {
	if tab == nil {
		return backend.Napp{}
	}
	for _, in := range st.Installed {
		if in.ID == tab.nappID {
			return in
		}
	}
	if n, ok := backend.LookupNapp(tab.nappID); ok {
		return n
	}
	return tab.napp
}

// ─── detail layouts ─────────────────────────────────────────────

func detailRow(th *material.Theme, label, value string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		if value == "" {
			return layout.Dimensions{}
		}
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := emph(material.Body2(th, label+": "))
					l.Color = currentTheme().subtle
					return l.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, value)
					return l.Layout(gtx)
				}),
			)
		})
	})
}

// layoutNappDetail is the ephemeral napp page: everything the launcher
// knows, plus install/uninstall and open (when installed).
func layoutNappDetail(
	gtx layout.Context,
	th *material.Theme,
	tab *storePage,
	openBtn, primaryBtn, updateBtn, authorBtn, copyAddrBtn, settingsBtn *widget.Clickable,
	installedSet map[string]bool,
	busy map[string]bool,
	st backend.State,
) layout.Dimensions {
	n := detailNapp(st, tab)
	if n.ID == "" {
		l := material.Body2(th, "Napp not found.")
		l.Color = currentTheme().muted
		return l.Layout(gtx)
	}
	installed := installedSet[n.ID]
	// the action row, with no Try, Install or Update for an unavailable
	// entry, whose status block takes the update line's place
	acts := nappPageActions(n, installed, busy[n.ID])
	unavailable := unavailableLines(n, installed)

	authorName, authorPic := "", ""
	if n.Author.Hex() != "" {
		if p := cachedProfile(n.Author.Hex()); p != nil && p.ShortName != "" {
			authorName, authorPic = p.ShortName, p.Picture
		} else {
			authorName = n.AuthorShortName()
			if authorName == "" {
				authorName = n.Author.Hex()
				if len(authorName) > 16 {
					authorName = authorName[:16] + "…"
				}
			}
		}
	}

	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return nappIcon(gtx, n, 56)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					title := n.Label()
					if n.Unavailable != "" {
						title = cardName(n)
					}
					t := material.H6(th, title)
					t.Font.Weight = font.Bold
					return t.Layout(gtx)
				}),
			)
		}),
	}
	if n.Description != "" {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(th, capRunes(n.Description, 4000))
				l.Color = currentTheme().subtle
				return l.Layout(gtx)
			})
		}))
	}
	// author row: the only tappable part of a card/page that leads to a
	// profile instead of a napp page
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			inner := func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return avatar(gtx, authorPic, 22)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						c := material.Body2(th, authorName)
						c.Color = currentTheme().muted
						return c.Layout(gtx)
					}),
				)
			}
			if authorBtn == nil || n.Author.Hex() == "" {
				return inner(gtx)
			}
			pointer.CursorPointer.Add(gtx.Ops)
			return authorBtn.Layout(gtx, inner)
		})
	}))
	// action buttons row
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if acts.open == "" || openBtn == nil {
						return layout.Dimensions{}
					}
					pointer.CursorPointer.Add(gtx.Ops)
					b := material.Button(th, openBtn, acts.open)
					b.Background = currentTheme().suggestBg
					b.Color = currentTheme().suggestFg
					return b.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if acts.open == "" || openBtn == nil {
						return layout.Dimensions{}
					}
					return layout.Spacer{Width: unit.Dp(8)}.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if acts.primary == "" || primaryBtn == nil {
						return layout.Dimensions{}
					}
					pointer.CursorPointer.Add(gtx.Ops)
					b := material.Button(th, primaryBtn, acts.primary)
					if installed {
						b.Background = currentTheme().chipBg
						b.Color = currentTheme().chipFg
					}
					return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, b.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !acts.update || updateBtn == nil {
						return layout.Dimensions{}
					}
					pointer.CursorPointer.Add(gtx.Ops)
					return material.Button(th, updateBtn, "Update").Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !acts.update || updateBtn == nil {
						return layout.Dimensions{}
					}
					return layout.Spacer{Width: unit.Dp(8)}.Layout(gtx)
				}),
				// its settings window: what it declared (NAP-CONFIG) and
				// what the user let it do
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !acts.settings || settingsBtn == nil {
						return layout.Dimensions{}
					}
					pointer.CursorPointer.Add(gtx.Ops)
					b := material.Button(th, settingsBtn, "Settings")
					b.Background = currentTheme().chipBg
					b.Color = currentTheme().chipFg
					return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, b.Layout)
				}),
				// the naddr is how a napp is shared: pasted into another
				// launcher's discovery filter, it finds this one
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !acts.copyAddr || copyAddrBtn == nil {
						return layout.Dimensions{}
					}
					pointer.CursorPointer.Add(gtx.Ops)
					b := material.Button(th, copyAddrBtn, "Copy address")
					b.Background = currentTheme().chipBg
					b.Color = currentTheme().chipFg
					return b.Layout(gtx)
				}),
			)
		})
	}))
	if unavailable != nil {
		// the reason wraps freely here: the page has room for it
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutUnavailableBlock(gtx, th, unavailable, 0)
			})
		}))
	} else if acts.update {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(th, "An update is available.")
				l.Color = currentTheme().subtle
				return l.Layout(gtx)
			})
		}))
	}
	children = append(children,
		detailRow(th, "ID", n.ID),
		detailRow(th, "Address", n.Naddr()),
		detailRow(th, "d", n.D),
		detailRow(th, "Author", n.Author.Hex()),
		detailRow(th, "Created", n.CreatedAt.Time().Format(time.RFC3339)),
		detailRow(th, "Actions", strings.Join(n.Actions, ", ")),
		detailRow(th, "Requires", strings.Join(n.Requires, ", ")),
		detailRow(th, "Servers", strings.Join(n.Servers, ", ")),
		detailRow(th, "Icon", n.Icon),
	)
	if n.IsNapplet() {
		children = append(children,
			detailRow(th, "Format", nappletFormatLabel(n)),
			detailRow(th, "Unsupported", strings.Join(n.MissingDomains(), ", ")),
			detailRow(th, "Artifact", n.ArtifactHash),
			detailRow(th, "Roles", strings.Join(n.Roles, ", ")),
			detailRow(th, "Domains", strings.Join(n.RequiredDomains, ", ")),
			detailRow(th, "Optional domains", strings.Join(n.OptionalDomains, ", ")),
			detailRow(th, "Source", strings.Join(n.Sources, ", ")),
		)
	}
	if len(n.Paths) > 0 {
		paths := make([]string, 0, len(n.Paths))
		for _, p := range n.Paths {
			short := p.Sha256
			if len(short) > 8 {
				short = short[:8]
			}
			paths = append(paths, fmt.Sprintf("%s (%s)", p.Path, short))
		}
		children = append(children, detailRow(th, "Files", strings.Join(paths, ", ")))
	}
	return material.List(th, &nappDetailList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutProfileDetail is the ephemeral profile page: name, about,
// picture, nip05, plus the napps fetched from the author's write relays
// and the discovery relays.
func layoutProfileDetail(
	gtx layout.Context,
	th *material.Theme,
	tab *storePage,
	list *widget.List,
	cardBtns, openBtns, actionBtns, updateBtns []widget.Clickable,
	installedSet map[string]bool,
	busy map[string]bool,
	st backend.State,
) layout.Dimensions {
	pubkey := ""
	if tab != nil {
		pubkey = tab.pubkey
	}
	p := backend.ProfileDetail{Pubkey: pubkey}
	if cp := cachedProfile(pubkey); cp != nil {
		p = *cp
	}
	napps, fetching, fetchErr := cachedAuthorNapps(pubkey)

	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return avatar(gtx, p.Picture, 56)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					name := p.ShortName
					if name == "" {
						name = pubkey
					}
					t := material.H6(th, name)
					t.Font.Weight = font.Bold
					return t.Layout(gtx)
				}),
			)
		}),
	}
	if p.DisplayName != "" && p.DisplayName != p.Name {
		children = append(children, detailRow(th, "Display name", p.DisplayName))
	}
	if p.Name != "" {
		children = append(children, detailRow(th, "Name", p.Name))
	}
	children = append(children,
		detailRow(th, "About", p.About),
		detailRow(th, "NIP-05", p.NIP05),
		detailRow(th, "Website", p.Website),
		detailRow(th, "npub", p.Npub),
		detailRow(th, "Pubkey", p.Pubkey),
	)
	children = append(children,
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			t := emph(material.Body2(th, "Published napps"))
			t.Color = currentTheme().subtle
			return t.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
	)
	if fetchErr != "" {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(th, fetchErr)
			l.Color = currentTheme().danger
			return l.Layout(gtx)
		}))
	} else if fetching {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(th, "Fetching napps…")
			l.Color = currentTheme().muted
			return l.Layout(gtx)
		}))
	} else if len(napps) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(th, "No napps found on their relays.")
			l.Color = currentTheme().muted
			return l.Layout(gtx)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		children[0],
		layout.Flexed(0, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children[1:]...)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(napps) == 0 {
				return layout.Dimensions{}
			}
			return material.List(th, list).Layout(gtx, len(napps), func(gtx layout.Context, i int) layout.Dimensions {
				// an installed entry as the snapshot stamps it, so its
				// Update button and unavailable status show here too
				n := installedOr(st, napps[i])
				var cardBtn, openBtn, actBtn, updBtn *widget.Clickable
				if i < len(cardBtns) {
					cardBtn = &cardBtns[i]
				}
				// no Try, Install or Update for an unavailable entry, and
				// Try reads Opening… while its files are verified
				openLabel, label, updLabel := profileRowLabels(n, installedSet[n.ID], busy[n.ID])
				if i < len(openBtns) && openLabel != "" {
					openBtn = &openBtns[i]
				}
				if i < len(actionBtns) && label != "" {
					actBtn = &actionBtns[i]
				}
				if i < len(updateBtns) && updLabel != "" {
					updBtn = &updateBtns[i]
				}
				// inside a profile the author row is the profile itself:
				// no nested author button
				return renderNappCard(gtx, th, cardBtn, nil, openBtn, nil, actBtn, updBtn, openLabel, label, updLabel, true, installedSet[n.ID], n)
			})
		}),
	)
}

// nappletFormatLabel says which napplet manifest an app was read from.
func nappletFormatLabel(n backend.Napp) string {
	schema := "NIP-5D manifest"
	if n.NappletSchema == backend.SchemaWebNapplet {
		schema = "web napplet"
	}
	return fmt.Sprintf("napplet (kind:%d, %s, sandboxed)", n.ManifestKind(), schema)
}
