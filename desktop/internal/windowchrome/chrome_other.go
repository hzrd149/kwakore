//go:build !linux

package windowchrome

// Windows and macOS provide native decorations directly to Gio.
func Configure() string { return "native" }
