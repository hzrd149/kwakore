//go:build !linux && !windows

package main

import "github.com/abemedia/go-webview"

// WKWebView has no public switch for WebRTC, media capture or preconnect,
// so there is nothing to set here; the residual is recorded under NIP-5D
// Non-Guarantees in spec/CONFORMANCE.md.
func prepareEngine() {}

func hardenEngine(webview.WebView) {}
