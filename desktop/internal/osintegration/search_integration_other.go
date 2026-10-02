//go:build !linux

package osintegration

func GnomeSearchSupported() bool { return false }

func SetGNOMESearchIntegration(bool, string) error { return nil }
