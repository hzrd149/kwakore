//go:build !linux

package main

// Windows and macOS provide native decorations directly to Gio.
func configureNativeWindowChrome() string { return "native" }
