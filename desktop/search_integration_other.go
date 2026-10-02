//go:build !linux

package main

func gnomeSearchSupported() bool { return false }

func setGNOMESearchIntegration(bool, string) error { return nil }
