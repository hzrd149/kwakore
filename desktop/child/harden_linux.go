//go:build linux

package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/abemedia/go-webview"
	"github.com/ebitengine/purego"
)

// ─── WebKitGTK engine hardening ─────────────────────────────────
//
// The napplet frame runs under connect-src 'none', but on WebKitGTK that
// directive does not govern every way a page can reach the network:
// WebRTC peer connections, media capture (navigator.mediaDevices is exposed
// by default on 2.52) and <link rel=preconnect>, which makes the engine do a
// DNS lookup and a TCP connect to any host the page names. NIP-5D limits
// what a shell may inject into the frame to window.napplet, so these
// channels are not closed by deleting RTCPeerConnection or mediaDevices in
// page script: they are closed in the engine's own WebKitSettings, which no
// page can see or undo.
//
// go-webview has no settings API, so the WebKitWebView is reached through
// purego (already loaded by go-webview itself; the child stays cgo-free):
// the GtkWindow go-webview hands out has the WebKitWebView as its only
// child. Every symbol is looked up with purego.Dlsym before it is
// registered, because RegisterLibFunc panics on a missing one; the feature
// API (WebKitGTK 2.42+) is optional, and a WebKitGTK without it only leaves
// link preconnect on, which is logged.
//
// No decide-policy handler is installed to refuse sub-frame navigations.
// The host page's CSP (frame-src 'none' from the napplet baseline) plus the
// replaced-document rebuild (D-01) already cover every navigation the
// research spike measured; a stateless policy cannot tell a script's
// navigation to about:srcdoc from a new frame's initial srcdoc load (both
// arrive as the same navigation type); and it would add a native callback
// on the UI thread for no measured gain.

// webkitAPI is the slice of libgtk-3 and libwebkit2gtk-4.1 the hardening
// needs. The feature fields are nil when that API is missing.
type webkitAPI struct {
	binGetChild    func(uintptr) uintptr
	getSettings    func(uintptr) uintptr
	setWebRTC      func(uintptr, bool)
	getWebRTC      func(uintptr) bool
	setMediaStream func(uintptr, bool)
	getMediaStream func(uintptr) bool

	allFeatures   func() uintptr
	featureCount  func(uintptr) uint
	featureAt     func(uintptr, uint) uintptr
	featureID     func(uintptr) string
	setFeature    func(uintptr, uintptr, bool)
	getFeature    func(uintptr, uintptr) bool
	unrefFeatures func(uintptr) // nil when absent: a small leak only

	// featureErr says why link preconnect cannot be turned off, when the
	// feature API is missing
	featureErr error
}

const (
	libGTK    = "libgtk-3.so.0"
	libWebKit = "libwebkit2gtk-4.1.so.0"
)

var resolveWebKit = sync.OnceValues(loadWebKit)

// loadWebKit opens the two libraries (libwebview already has them loaded,
// so dlopen hands back the same handles) and resolves every symbol. It never
// panics: a panic from purego is turned into an error.
func loadWebKit() (api *webkitAPI, err error) {
	defer func() {
		if r := recover(); r != nil {
			api, err = nil, fmt.Errorf("resolving webkitgtk symbols panicked: %v", r)
		}
	}()
	gtk, err := purego.Dlopen(libGTK, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", libGTK, err)
	}
	wk, err := purego.Dlopen(libWebKit, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", libWebKit, err)
	}

	api = &webkitAPI{}
	required := []struct {
		lib  uintptr
		fn   any
		name string
	}{
		{gtk, &api.binGetChild, "gtk_bin_get_child"},
		{wk, &api.getSettings, "webkit_web_view_get_settings"},
		{wk, &api.setWebRTC, "webkit_settings_set_enable_webrtc"},
		{wk, &api.getWebRTC, "webkit_settings_get_enable_webrtc"},
		{wk, &api.setMediaStream, "webkit_settings_set_enable_media_stream"},
		{wk, &api.getMediaStream, "webkit_settings_get_enable_media_stream"},
	}
	for _, s := range required {
		if err := bindSymbol(s.lib, s.fn, s.name); err != nil {
			return nil, err
		}
	}

	var feature struct {
		all    func() uintptr
		count  func(uintptr) uint
		at     func(uintptr, uint) uintptr
		id     func(uintptr) string
		set    func(uintptr, uintptr, bool)
		get    func(uintptr, uintptr) bool
		unref  func(uintptr)
		failed error
	}
	for _, s := range []struct {
		fn   any
		name string
	}{
		{&feature.all, "webkit_settings_get_all_features"},
		{&feature.count, "webkit_feature_list_get_length"},
		{&feature.at, "webkit_feature_list_get"},
		{&feature.id, "webkit_feature_get_identifier"},
		{&feature.set, "webkit_settings_set_feature_enabled"},
		{&feature.get, "webkit_settings_get_feature_enabled"},
	} {
		if err := bindSymbol(wk, s.fn, s.name); err != nil {
			feature.failed = err
			break
		}
	}
	if feature.failed != nil {
		api.featureErr = fmt.Errorf("webkitgtk has no feature api: %w", feature.failed)
		return api, nil
	}
	if bindSymbol(wk, &feature.unref, "webkit_feature_list_unref") != nil {
		feature.unref = nil
	}
	api.allFeatures, api.featureCount, api.featureAt = feature.all, feature.count, feature.at
	api.featureID, api.setFeature, api.getFeature = feature.id, feature.set, feature.get
	api.unrefFeatures = feature.unref
	return api, nil
}

// bindSymbol registers name into fn only after Dlsym found it.
func bindSymbol(lib uintptr, fn any, name string) error {
	if _, err := purego.Dlsym(lib, name); err != nil {
		return fmt.Errorf("missing symbol %s: %w", name, err)
	}
	purego.RegisterLibFunc(fn, lib, name)
	return nil
}

// disableFeature turns the named WebKit feature off and returns its value
// read back (true when it stayed on or could not be found).
func (api *webkitAPI) disableFeature(settings uintptr, id string) (bool, error) {
	if api.featureErr != nil {
		return true, api.featureErr
	}
	list := api.allFeatures()
	if list == 0 {
		return true, errors.New("webkitgtk returned no feature list")
	}
	if api.unrefFeatures != nil {
		defer api.unrefFeatures(list)
	}
	n := api.featureCount(list)
	for i := uint(0); i < n; i++ {
		f := api.featureAt(list, i)
		if f == 0 || api.featureID(f) != id {
			continue
		}
		api.setFeature(settings, f, false)
		return api.getFeature(settings, f), nil
	}
	return true, fmt.Errorf("webkitgtk has no feature %s", id)
}

// prepareEngine has nothing to do before webview.New on WebKitGTK: its
// switches are per view, set by hardenEngine.
func prepareEngine() {}

// hardenEngine turns off WebRTC, media capture and link preconnect in this
// window's WebKitSettings and logs the values read back. It runs on the UI
// thread, after webview.New and before Navigate, for napplet and settings
// windows only (napp windows are not CSP-confined; their policy is
// deferred). It never panics and never stops the window from opening.
func hardenEngine(w webview.WebView) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).
				Msg("webkit hardening panicked; webrtc, media capture and link preconnect stay at engine defaults")
		}
	}()
	api, err := resolveWebKit()
	if err != nil {
		log.Warn().Err(err).
			Msg("webkit hardening unavailable; webrtc, media capture and link preconnect stay at engine defaults")
		return
	}
	win := uintptr(w.Window())
	if win == 0 {
		log.Warn().Msg("webkit hardening: no native window; webrtc, media capture and link preconnect stay at engine defaults")
		return
	}
	view := api.binGetChild(win)
	if view == 0 {
		log.Warn().Msg("webkit hardening: window has no web view; webrtc, media capture and link preconnect stay at engine defaults")
		return
	}
	settings := api.getSettings(view)
	if settings == 0 {
		log.Warn().Msg("webkit hardening: web view has no settings; webrtc, media capture and link preconnect stay at engine defaults")
		return
	}

	api.setWebRTC(settings, false)
	api.setMediaStream(settings, false)
	preconnect, featureErr := api.disableFeature(settings, "LinkPreconnect")
	webrtc := api.getWebRTC(settings)
	media := api.getMediaStream(settings)

	log.Info().Bool("webrtc", webrtc).Bool("media_stream", media).Bool("link_preconnect", preconnect).
		Msg("webkit hardening applied")

	var on []string
	if webrtc {
		on = append(on, "webrtc")
	}
	if media {
		on = append(on, "media capture")
	}
	if preconnect {
		on = append(on, "link preconnect")
	}
	if len(on) > 0 {
		log.Warn().Err(featureErr).Str("still_on", strings.Join(on, ", ")).
			Msg("webkit hardening incomplete: " + strings.Join(on, ", ") + " could not be turned off")
	}
}
