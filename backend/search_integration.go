package backend

// GNOMESearchSupported reports whether this host can register Verdana as a
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
