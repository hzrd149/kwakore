package main

import (
	"context"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/verdana/desktop/internal/icon"
	"fiatjaf.com/verdana/desktop/internal/osintegration"
	"github.com/gogpu/systray"
	"verdana/backend"
)

// trayUser is the menu item naming the logged-in user; clicking it opens
// their profile in whatever profile napplet handles napplet:profile/open.
var trayUser struct {
	mu       sync.Mutex
	item     *systray.MenuItem
	label    string
	disabled bool
	// changed coalesces state changes for the one goroutine that applies
	// them, so StateChanged never reads the snapshot under a caller's locks
	changed chan struct{}
}

// trayStateChanged asks for the user item to be refreshed.
func trayStateChanged() {
	select {
	case trayUser.changed <- struct{}{}:
	default:
	}
}

func init() { trayUser.changed = make(chan struct{}, 1) }

func newTray() *systray.SystemTray {
	trayPNG := icon.PNG()
	menu := systray.NewMenu()
	userItem := menu.Add("Not logged in", openUserProfile)
	userItem.SetDisabled(true)
	trayUser.mu.Lock()
	trayUser.item, trayUser.label, trayUser.disabled = userItem, "Not logged in", true
	trayUser.mu.Unlock()
	go func() {
		for range trayUser.changed {
			refreshTrayUser()
		}
	}()
	trayStateChanged()
	menu.AddSeparator()
	menu.Add("Open Verdana", showManager)
	menu.Add("Verdana Store", showStore)
	menu.Add("Settings", func() {
		if err := backend.OpenLauncherSettings(); err != nil {
			log.Warn().Err(err).Msg("could not open settings from tray")
		}
	})
	var autostartItem *systray.MenuItem
	autostartItem = menu.AddCheckbox("Launch at login", osintegration.AutostartEnabled(), func() {
		enabled := !autostartItem.IsChecked()
		if err := (gioHost{}).SetAutostart(enabled); err != nil {
			log.Warn().Err(err).Msg("could not change launch at login")
			return
		}
		autostartItem.SetChecked(enabled)
	})
	menu.AddSeparator()
	menu.Add("About Verdana", func() {
		if err := backend.OpenAbout(); err != nil {
			log.Warn().Err(err).Msg("could not open about from tray")
		}
	})
	menu.Add("Quit Verdana", quitDesktop)

	tray := systray.New().
		SetIcon(trayPNG).
		SetTemplateIcon(icon.TemplatePNG()).
		SetAppName(APP_TITLE).
		SetTooltip(APP_TITLE).
		SetMenu(menu).
		OnClick(showManager).
		Show()
	return tray
}

func openUserProfile() {
	go func() {
		// long enough for the user to answer the handler picker
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := backend.OpenUserProfile(ctx); err != nil {
			log.Warn().Err(err).Msg("could not open the profile from tray")
		}
	}()
}

// refreshTrayUser brings the user item in line with the launcher state. It
// runs on every state change, so it only touches the menu when it differs.
func refreshTrayUser() {
	st := backend.Snapshot()
	label := trayUserLabel(st.ProfileName, st.Pubkey)
	disabled := st.Pubkey == ""

	trayUser.mu.Lock()
	defer trayUser.mu.Unlock()
	if trayUser.item == nil {
		return
	}
	if label != trayUser.label {
		trayUser.item.SetLabel(label)
		trayUser.label = label
	}
	if disabled != trayUser.disabled {
		trayUser.item.SetDisabled(disabled)
		trayUser.disabled = disabled
	}
}

// trayUserLabel names the logged-in user: their profile name, or a shortened
// npub while the name is only the hex key the login falls back to.
func trayUserLabel(name, pubkey string) string {
	if pubkey == "" {
		return "Not logged in"
	}
	if name != "" && name != pubkey {
		return name
	}
	pk, err := nostr.PubKeyFromHex(pubkey)
	if err != nil {
		return pubkey
	}
	npub := nip19.EncodeNpub(pk)
	return npub[:10] + "…" + npub[len(npub)-4:]
}
