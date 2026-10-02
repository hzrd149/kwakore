// Package themesystem follows the operating system's light/dark preference
// and accent color.
package themesystem

import "image/color"

// Appearance is what the OS says about its theme.
type Appearance struct {
	Dark      bool
	Accent    color.NRGBA
	HasAccent bool

	// Colors is the desktop's full named palette when it publishes one.
	// Most Linux desktops expose only Dark and Accent through the portal;
	// Omarchy additionally publishes colors.toml for native applications.
	Colors map[string]color.NRGBA
}
