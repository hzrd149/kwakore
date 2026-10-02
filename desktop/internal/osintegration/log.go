// Package osintegration puts Verdana into the operating system: shortcut
// files, per-napplet app shortcuts, autostart at login, and desktop search.
package osintegration

import "github.com/rs/zerolog"

var log = zerolog.Nop()

// SetLogger sets where integration trouble is logged.
func SetLogger(l zerolog.Logger) { log = l }
