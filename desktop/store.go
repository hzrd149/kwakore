package main

import (
	"slices"
	"strings"
	"sync"
	"verdana/backend"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// The store is the launcher's catalogue window, kept apart from the manager
// so it has room to grow: the installed napps and everything discovery found,
// each opening into a napp or profile page with a way back.

const (
	storeInstalled = iota
	storeDiscover
)

// storeState is what belongs to the store window alone. It outlives the
// window, so closing and reopening the store lands where the user left it.
type storeState struct {
	mu sync.Mutex
	// view is the list the store shows (storeInstalled or storeDiscover);
	// viewSet is false until the first opening picks one.
	view    int
	viewSet bool
	// pages is the stack of napp and profile pages opened over the list,
	// the one on screen last.
	pages []*storePage

	// discoveryArchetype is set by an intent dispatch off the UI thread and
	// consumed by the next frame.
	discoveryArchetype string
	// discoveryQuery is the equivalent handoff from GNOME Shell's search
	// provider when the user asks Verdana to show every matching result.
	discoveryQuery string
}

var store storeState

var (
	// filterEd and installedFilterEd are the discovery and installed lists'
	// filter boxes (one store window, so one of each is enough).
	filterEd          widget.Editor
	installedFilterEd widget.Editor

	// discoKind is which apps the discovery list shows (one of the
	// discoKind* constants), switched by discoKindBtns.
	discoKind     int
	discoKindBtns [3]widget.Clickable
	// discoScope is whose apps the discovery list shows: everyone's, or only
	// those by people the user follows (one of the discoScope* constants),
	// switched by discoScopeBtns.
	discoScope     int
	discoScopeBtns [2]widget.Clickable
	// discoCols and installedCols are how many napps a row of the
	// discovery and installed lists held last frame (see nappGrid).
	discoCols, installedCols int
)

const (
	discoKindAll = iota
	discoKindNapps
	discoKindNapplets
)

// discoKindLabels name the discovery list's kind buttons, in discoKind order.
var discoKindLabels = [3]string{"All", "Napps", "Napplets"}

const (
	discoScopeGlobal = iota
	discoScopeFriends
)

// discoScopeLabels name the discovery list's scope buttons, in discoScope
// order.
var discoScopeLabels = [2]string{"Global", "Friends"}

// showStoreView brings up the store on one of its lists.
func showStoreView(view int) {
	setStoreView(view)
	showStore()
}

// setStoreView switches the store to one of its lists, closing any page
// opened over it.
func setStoreView(view int) {
	store.mu.Lock()
	store.view, store.viewSet = view, true
	store.pages = nil
	store.mu.Unlock()
	invalidateAll()
}

// pushStorePage opens a napp or profile page over whatever the store shows.
func pushStorePage(p *storePage) {
	store.mu.Lock()
	store.pages = append(store.pages, p)
	store.mu.Unlock()
	showStore()
}

// popStorePage goes back to what was under the page on screen.
func popStorePage() {
	store.mu.Lock()
	if len(store.pages) > 0 {
		store.pages = store.pages[:len(store.pages)-1]
	}
	store.mu.Unlock()
	nappDetailList.Position = layout.Position{}
	invalidateAll()
}

// currentStorePage is the page on screen, or nil while a list is.
func currentStorePage() *storePage {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.pages) == 0 {
		return nil
	}
	return store.pages[len(store.pages)-1]
}

// followSet is the State's follows as a set, for the friends filter.
func followSet(st backend.State) map[string]bool {
	set := make(map[string]bool, len(st.Follows))
	for _, pk := range st.Follows {
		set[pk] = true
	}
	return set
}

// inDiscoScope says whether a napp belongs under the chosen discovery scope.
func inDiscoScope(n backend.Napp, follows map[string]bool) bool {
	return discoScope == discoScopeGlobal || follows[n.Author.Hex()]
}

// matchesKind says whether a napp belongs under a discovery kind tab.
func matchesKind(n backend.Napp, kind int) bool {
	switch kind {
	case discoKindNapps:
		return !n.IsNapplet()
	case discoKindNapplets:
		return n.IsNapplet()
	}
	return true
}

// discoveryFilter returns the indices of st.Discovery that pass the filter
// editor's text: a case-insensitive substring on name, description, author
// pubkey and author name. Clicks and rendering both walk this same index
// list, so buttons stay glued to their napp no matter what the filter hides.
//
// A napp address (an naddr, a nostr: link) typed there is looked up on
// relays too; once found, it is listed and it alone passes the filter.
//
// The scope and kind tabs narrow it further to the user's friends' apps and
// to napps or napplets, except for an address: that names one app, whoever
// published it and whatever its kind.
func discoveryFilter(st backend.State, archetype string) []int {
	q := strings.ToLower(strings.TrimSpace(filterEd.Text()))
	if role, ok := strings.CutPrefix(q, "archetype:"); ok {
		archetype = strings.TrimSpace(role)
		q = ""
		backend.LookupAddress("")
	} else {
		backend.LookupAddress(filterEd.Text())
	}
	vis := nappFilter(st.Discovery, q)
	if archetype != "" {
		vis = slices.DeleteFunc(vis, func(i int) bool {
			return !st.Discovery[i].HandlesArchetype(archetype)
		})
	}
	if backend.IsNappAddress(q) {
		return vis
	}
	follows := followSet(st)
	out := vis[:0]
	for _, i := range vis {
		if inDiscoScope(st.Discovery[i], follows) && matchesKind(st.Discovery[i], discoKind) {
			out = append(out, i)
		}
	}
	return out
}

// installedFilter is the same thing for the installed list.
func installedFilter(st backend.State) []int {
	q := strings.ToLower(strings.TrimSpace(installedFilterEd.Text()))
	return nappFilter(st.Installed, q)
}

func nappFilter(list []backend.Napp, q string) []int {
	out := make([]int, 0, len(list))
	for i, n := range list {
		if n.MatchesQuery(q) {
			out = append(out, i)
		}
	}
	return out
}

func runStoreWindow() {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(fontCollection()))
	th.Face = "vFont"

	w := new(app.Window)
	w.Option(
		app.Title("Verdana Store"),
		app.Size(unit.Dp(1000), unit.Dp(720)),
		app.Decorated(true),
	)
	setStoreWindow(w)
	defer setStoreWindow(nil)

	// the first opening shows what the user has, or else what there is
	store.mu.Lock()
	if !store.viewSet {
		store.view, store.viewSet = storeDiscover, true
		if len(backend.Snapshot().Installed) > 0 {
			store.view = storeInstalled
		}
	}
	store.mu.Unlock()

	var (
		backBtn               widget.Clickable
		viewBtns              [2]widget.Clickable
		managerBtn            widget.Clickable
		fetchBtn              widget.Clickable
		checkUpdBtn           widget.Clickable
		installedList         widget.List
		discoveryList         widget.List
		cardBtns              []widget.Clickable
		uninstBtns            []widget.Clickable
		installedUpdateBtns   []widget.Clickable
		installedOpenBtns     []widget.Clickable
		installedAuthorBtns   []widget.Clickable
		installedSettingsBtns []widget.Clickable
		discoCardBtns         []widget.Clickable
		discoOpenBtns         []widget.Clickable
		discoAuthorBtns       []widget.Clickable
		detailOpenBtn         widget.Clickable
		detailPrimaryBtn      widget.Clickable
		detailUpdateBtn       widget.Clickable
		detailAuthorBtn       widget.Clickable
		detailCopyAddrBtn     widget.Clickable
		detailSettingsBtn     widget.Clickable
		profileList           widget.List
		profileCardBtns       []widget.Clickable
		profileOpenBtns       []widget.Clickable
		profileActionBtns     []widget.Clickable
		profileUpdateBtns     []widget.Clickable
	)
	filterEd.SingleLine = true
	installedFilterEd.SingleLine = true
	installedList.Axis = layout.Vertical
	discoveryList.Axis = layout.Vertical
	profileList.Axis = layout.Vertical

	var ops op.Ops
	for {
		switch e := w.Event(); e := e.(type) {
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			if backend.ThemeMode() != appliedThemeMode() {
				applyThemeMode()
			}
			pal := currentTheme()
			pal.apply(th)
			paint.Fill(gtx.Ops, pal.bg)
			drainClipboard(gtx)

			st := backend.Snapshot()
			if st.Phase != backend.PhaseMain {
				if managerBtn.Clicked(gtx) {
					showManager()
				}
				layoutStoreLoggedOut(gtx, th, &managerBtn, st.Phase == backend.PhaseLogin)
				e.Frame(gtx.Ops)
				continue
			}

			store.mu.Lock()
			discoveryArchetype := store.discoveryArchetype
			store.discoveryArchetype = ""
			discoveryQuery := store.discoveryQuery
			store.discoveryQuery = ""
			store.mu.Unlock()
			if discoveryArchetype != "" {
				filterEd.SetText("archetype:" + discoveryArchetype)
				discoKind = discoKindNapplets
				discoScope = discoScopeGlobal
			}
			if discoveryQuery != "" {
				filterEd.SetText(discoveryQuery)
				discoKind = discoKindNapplets
				discoScope = discoScopeGlobal
			}

			if backBtn.Clicked(gtx) {
				popStorePage()
			}
			for v := range viewBtns {
				if viewBtns[v].Clicked(gtx) {
					setStoreView(v)
				}
			}
			store.mu.Lock()
			view := store.view
			store.mu.Unlock()
			page := currentStorePage()

			installedSet := make(map[string]bool, len(st.Installed))
			for _, n := range st.Installed {
				installedSet[n.ID] = true
			}
			busy := make(map[string]bool, len(st.Busy))
			for _, id := range st.Busy {
				busy[id] = true
			}

			if fetchBtn.Clicked(gtx) {
				go backend.Discover()
			}
			for k := range discoKindBtns {
				if discoKindBtns[k].Clicked(gtx) {
					discoKind = k
				}
			}
			for k := range discoScopeBtns {
				if discoScopeBtns[k].Clicked(gtx) {
					discoScope = k
				}
			}
			for len(cardBtns) < len(st.Installed) {
				cardBtns = append(cardBtns, widget.Clickable{})
				uninstBtns = append(uninstBtns, widget.Clickable{})
				installedUpdateBtns = append(installedUpdateBtns, widget.Clickable{})
				installedOpenBtns = append(installedOpenBtns, widget.Clickable{})
				installedAuthorBtns = append(installedAuthorBtns, widget.Clickable{})
				installedSettingsBtns = append(installedSettingsBtns, widget.Clickable{})
			}
			for len(discoCardBtns) < len(st.Discovery) {
				discoCardBtns = append(discoCardBtns, widget.Clickable{})
				discoOpenBtns = append(discoOpenBtns, widget.Clickable{})
				discoAuthorBtns = append(discoAuthorBtns, widget.Clickable{})
			}
			vis := discoveryFilter(st, discoveryArchetype)
			instVis := installedFilter(st)

			if page == nil && view == storeInstalled {
				// buttons on top of the card's own click area go
				// first: a click that hit a button must not also
				// count as opening the napp page. The author row
				// goes first of all: it opens a profile, not a napp.
				acted := false
				for _, i := range instVis {
					if installedAuthorBtns[i].Clicked(gtx) {
						openProfilePage(st.Installed[i].Author.Hex())
						acted = true
					}
				}
				if !acted {
					for _, i := range instVis {
						if st.Installed[i].IsNapplet() && installedSettingsBtns[i].Clicked(gtx) {
							if err := backend.OpenSettings(st.Installed[i].ID); err != nil {
								log.Warn().Err(err).Str("napp", st.Installed[i].ID).Msg("could not open settings")
							}
							acted = true
						}
					}
				}
				if !acted {
					for _, i := range instVis {
						if installedOpenBtns[i].Clicked(gtx) {
							backend.Launch(st.Installed[i])
							acted = true
						}
					}
				}
				if !acted {
					for _, i := range instVis {
						if installedUpdateBtns[i].Clicked(gtx) {
							go backend.Update(st.Installed[i].ID)
							acted = true
						}
					}
				}
				if !acted {
					for _, i := range instVis {
						if uninstBtns[i].Clicked(gtx) {
							go backend.Uninstall(st.Installed[i].ID)
							acted = true
						}
					}
				}
				if !acted {
					for _, i := range instVis {
						if cardBtns[i].Clicked(gtx) {
							openNappPage(st.Installed[i])
						}
					}
				}
				if checkUpdBtn.Clicked(gtx) && !st.UpdateCheckRunning {
					go backend.CheckForUpdates()
				}
			} else if page == nil {
				acted := false
				for _, i := range vis {
					if discoAuthorBtns[i].Clicked(gtx) {
						openProfilePage(st.Discovery[i].Author.Hex())
						acted = true
					}
				}
				if !acted {
					for _, i := range vis {
						if !installedSet[st.Discovery[i].ID] && st.Discovery[i].IsNapplet() && discoOpenBtns[i].Clicked(gtx) {
							backend.TryNapplet(st.Discovery[i])
							acted = true
						}
					}
				}
				if !acted {
					for _, i := range vis {
						if discoCardBtns[i].Clicked(gtx) {
							openNappPage(st.Discovery[i])
						}
					}
				}
			} else if page.kind == "napp" {
				n := detailNapp(page)
				if detailAuthorBtn.Clicked(gtx) && n.Author.Hex() != "" {
					openProfilePage(n.Author.Hex())
				} else if detailOpenBtn.Clicked(gtx) && (installedSet[n.ID] || n.IsNapplet()) {
					if in, ok := backend.InstalledNapp(n.ID); ok {
						backend.Launch(in)
					} else {
						backend.TryNapplet(n)
					}
				} else if detailPrimaryBtn.Clicked(gtx) {
					if busy[n.ID] {
					} else if installedSet[n.ID] {
						go backend.Uninstall(n.ID)
					} else if dn, ok := backend.DiscoveredNapp(n.ID); ok {
						go backend.Install(dn)
					} else {
						go backend.Install(n)
					}
				} else if detailUpdateBtn.Clicked(gtx) {
					go backend.Update(n.ID)
				} else if detailCopyAddrBtn.Clicked(gtx) && n.Naddr() != "" {
					gioHost{}.CopyText(n.Naddr())
				} else if detailSettingsBtn.Clicked(gtx) && installedSet[n.ID] {
					id := n.ID
					go func() {
						if err := backend.OpenSettings(id); err != nil {
							log.Warn().Err(err).Str("napp", id).Msg("could not open settings")
						}
					}()
				}
			} else {
				if cp := cachedProfile(page.pubkey); cp != nil && cp.ShortName != "" {
					page.title = truncate(cp.ShortName, 32)
				}
				// size the buttons to the profile's napps list
				pan, _, _ := cachedAuthorNapps(page.pubkey)
				for len(profileCardBtns) < len(pan) {
					profileCardBtns = append(profileCardBtns, widget.Clickable{})
					profileOpenBtns = append(profileOpenBtns, widget.Clickable{})
					profileActionBtns = append(profileActionBtns, widget.Clickable{})
					profileUpdateBtns = append(profileUpdateBtns, widget.Clickable{})
				}
				pacted := false
				for i, pn := range pan {
					if (installedSet[pn.ID] || pn.IsNapplet()) && profileOpenBtns[i].Clicked(gtx) {
						if in, ok := backend.InstalledNapp(pn.ID); ok {
							backend.Launch(in)
						} else {
							backend.TryNapplet(pn)
						}
						pacted = true
					}
				}
				if !pacted {
					for i, pn := range pan {
						if profileActionBtns[i].Clicked(gtx) {
							if busy[pn.ID] {
								continue
							}
							if installedSet[pn.ID] {
								go backend.Uninstall(pn.ID)
							} else {
								go backend.Install(pn)
							}
							pacted = true
						}
						if profileUpdateBtns[i].Clicked(gtx) {
							go backend.Install(pn)
							pacted = true
						}
					}
				}
				if !pacted {
					for i, pn := range pan {
						if profileCardBtns[i].Clicked(gtx) {
							openNappPage(pn)
						}
					}
				}
			}
			// a click above may have opened a page or gone back
			page = currentStorePage()

			layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutStoreHeader(gtx, th, &backBtn, &viewBtns, page, view, len(st.Installed))
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(24), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						switch {
						case page != nil && page.kind == "napp":
							return readableColumn(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutNappDetail(gtx, th, page, &detailOpenBtn, &detailPrimaryBtn, &detailUpdateBtn, &detailAuthorBtn, &detailCopyAddrBtn, &detailSettingsBtn, installedSet, busy, st)
							})
						case page != nil:
							return readableColumn(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutProfileDetail(gtx, th, page, &profileList, profileCardBtns, profileOpenBtns, profileActionBtns, profileUpdateBtns, installedSet, busy)
							})
						case view == storeInstalled:
							return layoutNappsTab(gtx, th, &installedList, &installedFilterEd, cardBtns, uninstBtns, installedUpdateBtns, installedOpenBtns, installedAuthorBtns, installedSettingsBtns, &checkUpdBtn, instVis, st)
						default:
							return layoutDiscoveryTab(gtx, th, &discoveryList, &filterEd, &fetchBtn,
								discoCardBtns, discoOpenBtns, discoAuthorBtns, vis, st.FetchErr, st.Fetching, st.Discovery, st.Lookup, installedSet, followSet(st))
						}
					})
				}),
			)
			e.Frame(gtx.Ops)

		case app.DestroyEvent:
			return
		}
	}
}
