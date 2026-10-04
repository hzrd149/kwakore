//go:build windows

package main

import (
	"os"

	"github.com/abemedia/go-webview"
)

// prepareEngine sets the WebView2 browser arguments for this process before
// the first webview.New: the WebView2 loader appends this variable to the
// options libwebview passes. An inherited value is replaced, not extended,
// so every window kind of a build asks for exactly the same arguments.
func prepareEngine() {
	if err := os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", webview2BrowserArgs); err != nil {
		log.Warn().Err(err).Msg("could not set the webview2 browser arguments; webrtc keeps its default ip handling")
	}
}

// hardenEngine has nothing to set per view: WebView2 has no per-view
// WebRTC switch, only the browser arguments prepareEngine sets.
func hardenEngine(webview.WebView) error { return nil }
