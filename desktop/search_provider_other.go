//go:build !linux

package main

func startSearchProvider() func() { return func() {} }
