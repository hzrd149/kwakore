package backend

import (
	"sort"
	"sync"
)

var systemSearchSyncMu sync.Mutex

// GNOMESearchSupported reports whether this host can register Kwakore as a
// GNOME Shell search provider.
func GNOMESearchSupported() bool { return host.GNOMESearchSupported() }

// GNOMESearchEnabled is the persisted user preference. It defaults on so a
// GNOME install works after first launch without another setup step.
func GNOMESearchEnabled() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return state.GNOMESearchIntegration == nil || *state.GNOMESearchIntegration
}

// SyncGNOMESearchIntegration reconciles the current preference at startup.
func SyncGNOMESearchIntegration() error {
	if !host.GNOMESearchSupported() {
		return nil
	}
	return host.SetGNOMESearchIntegration(GNOMESearchEnabled())
}

// SetGNOMESearchIntegration applies and persists a preference change.
func SetGNOMESearchIntegration(enabled bool) error {
	if !host.GNOMESearchSupported() {
		return nil
	}
	if err := host.SetGNOMESearchIntegration(enabled); err != nil {
		return err
	}
	stateMu.Lock()
	state.GNOMESearchIntegration = &enabled
	saveState()
	stateMu.Unlock()
	return nil
}

// SyncSystemSearch publishes the discovered napplet catalog on systems whose
// search integration is backed by indexed launcher entries. GNOME serves the
// same catalog live over D-Bus, so its host implementation is a no-op.
func SyncSystemSearch() {
	systemSearchSyncMu.Lock()
	defer systemSearchSyncMu.Unlock()
	entries := systemSearchEntries(Snapshot())
	if err := host.SyncSearchNapplets(entries); err != nil {
		log.Warn().Err(err).Msg("could not synchronize system search napplets")
	}
}

func systemSearchEntries(st State) []AppShortcut {
	seen := make(map[string]bool, len(st.Discovery)+len(st.Installed))
	entries := make([]AppShortcut, 0, len(st.Discovery)+len(st.Installed))
	for li, list := range [][]Napp{st.Discovery, st.Installed} {
		for _, n := range list {
			if seen[n.ID] || !n.IsNapplet() {
				continue
			}
			// a discovered napplet whose latest event is invalid can never
			// be tried, so it gets no search entry. Its name and description
			// are arbitrary author text, and the OS writers
			// should only ever see what the store would let a user open. An
			// installed copy still runs, so the installed list keeps it.
			if li == 0 && n.Unavailable != "" {
				continue
			}
			seen[n.ID] = true
			description := n.Description
			if n.AuthorName != "" {
				if description != "" {
					description += " — "
				}
				description += "by " + n.AuthorName
			}
			entries = append(entries, AppShortcut{ID: n.ID, Token: LaunchToken(n.ID), Name: n.Label(), Description: description})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].ID < entries[j].ID
	})
	return entries
}
