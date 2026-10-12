//go:build linux

package daemon

import (
	"fmt"
	"os"

	"kwakore/backend"
	"kwakore/backend/linuxhost"
)

// watchColorScheme follows the desktop's light/dark preference. Tests never
// reach it, because only Run starts the watch.
var watchColorScheme = linuxhost.WatchColorScheme

// applyTheme pushes the effective window theme into the backend: "light" and
// "dark" are used as they are, "system" follows the desktop preference the
// appearance watch last reported (light when there is none). SetTheme sends
// the theme to every open window and does nothing when it did not change;
// new windows read it at launch.
func (s *Service) applyTheme() {
	s.themeMu.Lock()
	defer s.themeMu.Unlock()
	name := s.manager.Effective().Theme
	if name == backend.ThemeSystem || name == "" {
		name = backend.ThemeLight
		if s.systemDark.Load() {
			name = backend.ThemeDark
		}
	}
	backend.SetTheme(name, "{}")
}

// setSystemDark records the desktop preference and reapplies the theme when
// the setting follows the system.
func (s *Service) setSystemDark(dark bool) {
	s.systemDark.Store(dark)
	if theme := s.manager.Effective().Theme; theme == backend.ThemeSystem || theme == "" {
		s.applyTheme()
	}
}

// StartAppearanceWatch follows the desktop portal's color-scheme preference
// on workContext. Without a session bus or portal it gives up quietly and
// "system" stays light.
func (s *Service) StartAppearanceWatch() {
	s.appearanceWait.Add(1)
	go func() {
		defer s.appearanceWait.Done()
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintln(os.Stderr, "appearance watch stopped unexpectedly")
			}
		}()
		_ = watchColorScheme(s.workContext, s.setSystemDark)
	}()
}
