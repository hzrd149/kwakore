//go:build !linux

package themesystem

// Native observers for Windows and macOS are added independently. Until
// then system mode safely follows the light fallback on those platforms.
func Watch() (Appearance, <-chan Appearance, func()) {
	changes := make(chan Appearance)
	close(changes)
	return Appearance{}, changes, func() {}
}
