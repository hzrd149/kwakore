// Package themesystem follows the operating system's light/dark preference
// and accent color.
package themesystem

import "image/color"

// Appearance is what the OS says about its theme.
type Appearance struct {
	Dark      bool
	Accent    color.NRGBA
	HasAccent bool
}
