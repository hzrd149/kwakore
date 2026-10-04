//go:build !linux

package main

import "github.com/abemedia/go-webview"

// prepareEngine and hardenEngine have nothing to set on this engine.
func prepareEngine() {}

func hardenEngine(webview.WebView) {}
