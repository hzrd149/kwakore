package backend

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
	"unicode"
)

const (
	AppShortcutNamePlain  = "plain"
	AppShortcutNameHosted = "hosted"
)

var appShortcutSyncMu sync.Mutex

func AppShortcutsEnabled() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return state.ExposeInstalledApps
}

func AppShortcutNaming() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	if state.AppShortcutNameStyle == AppShortcutNameHosted {
		return AppShortcutNameHosted
	}
	return AppShortcutNamePlain
}

func SetAppShortcutSettings(enabled bool, naming string) {
	if naming != AppShortcutNameHosted {
		naming = AppShortcutNamePlain
	}
	stateMu.Lock()
	state.ExposeInstalledApps = enabled
	state.AppShortcutNameStyle = naming
	saveState()
	stateMu.Unlock()
	backgroundSyncs.Go(syncAppShortcuts)
}

// syncAppShortcuts serializes complete reconciliation passes. Icon loading is
// best-effort: a missing or temporarily unavailable icon never hides an app.
// The service always publishes its installed napplets (D-07), whatever the
// launcher's shortcut setting says, through syncNativeEntries.
func syncAppShortcuts() {
	if serviceConfig != nil {
		syncNativeEntries()
		return
	}
	appShortcutSyncMu.Lock()
	defer appShortcutSyncMu.Unlock()

	if !AppShortcutsEnabled() {
		if err := host.SyncAppShortcuts(nil); err != nil {
			log.Warn().Err(err).Msg("could not remove system app shortcuts")
		}
		return
	}

	naming := AppShortcutNaming()
	napps := installedNapps()
	sort.Slice(napps, func(i, j int) bool { return napps[i].ID < napps[j].ID })
	shortcuts := make([]AppShortcut, 0, len(napps))
	for _, napp := range napps {
		name := napp.Label()
		if naming == AppShortcutNameHosted {
			name += " — Verdana"
		}
		shortcut := AppShortcut{ID: napp.ID, Token: LaunchToken(napp.ID), Name: name, Description: napp.Description}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		shortcut.Icon, _ = napp.IconBlob(ctx)
		cancel()
		shortcuts = append(shortcuts, shortcut)
	}
	if err := host.SyncAppShortcuts(shortcuts); err != nil {
		log.Warn().Err(err).Msg("could not synchronize system app shortcuts")
	}
}

// ─── service native entries ──────────────────────────────────────

// errNativeEntryAddress reports an installed record that cannot get a native
// entry because its address is not canonical. It names no address: the
// caller only learns how many records were skipped.
var errNativeEntryAddress = errors.New("installed napplet has a noncanonical address")

// serviceNativeEntries reads the committed installed records once and
// returns one Address-only shortcut per installed napplet, sorted by address
// (then record key) so every pass hands the host the same order. A napp
// (kind 35130) gets no entry, since napplet.launch only opens napplets. A
// napplet whose address ParseCanonicalServiceAddress refuses is left out and
// counted in the returned error; the others are still published.
func serviceNativeEntries() ([]AppShortcut, error) {
	type row struct {
		key  string
		napp Napp
	}
	stateMu.Lock()
	rows := make([]row, 0, len(state.InstalledNapps))
	for key, n := range state.InstalledNapps {
		rows = append(rows, row{key: key, napp: n})
	}
	stateMu.Unlock()
	sort.Slice(rows, func(i, j int) bool {
		ai, aj := rows[i].napp.Address(), rows[j].napp.Address()
		if ai != aj {
			return ai < aj
		}
		return rows[i].key < rows[j].key
	})

	var errs []error
	shortcuts := make([]AppShortcut, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if !r.napp.IsNapplet() {
			continue
		}
		address := r.napp.Address()
		if _, err := ParseCanonicalServiceAddress(address); err != nil {
			errs = append(errs, errNativeEntryAddress)
			continue
		}
		if seen[address] {
			continue
		}
		seen[address] = true
		shortcuts = append(shortcuts, AppShortcut{
			Address:     address,
			Name:        nativeEntryText(r.napp.Name, 256),
			Description: nativeEntryText(r.napp.Description, 1024),
		})
	}
	return shortcuts, errors.Join(errs...)
}

// nativeEntryText drops control and format runes from author text and caps
// it in runes. The host's writer reduces it further to one display line;
// this keeps what the backend hands over bounded and free of anything that
// could read differently than it is.
func nativeEntryText(value string, limit int) string {
	out := make([]rune, 0, min(len(value), limit))
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return string(out)
}

// syncNativeEntries is one complete service pass: the committed installed
// napplets, in address order, handed to the host as the whole desired set.
// Passes are serialized with appShortcutSyncMu and each reads the registry
// only after taking it, so the last pass to run always publishes the latest
// committed state. A host that cannot show native entries is left alone.
func syncNativeEntries() error {
	appShortcutSyncMu.Lock()
	defer appShortcutSyncMu.Unlock()
	if !host.AppShortcutsSupported() {
		return nil
	}
	shortcuts, err := serviceNativeEntries()
	err = errors.Join(err, host.SyncAppShortcuts(shortcuts))
	if err != nil {
		log.Warn().Err(err).Int("entries", len(shortcuts)).Msg("native desktop entries not fully reconciled")
	}
	return err
}
