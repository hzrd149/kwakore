//go:build linux

package main

import (
	"errors"
	"strings"
	"testing"
)

// ─── hardening decisions, without a display ─────────────────────

// fakeWebKit is a WebKitGTK the hardening can drive without a display: a
// window holding a view holding settings, whose switches keep what was set
// unless stuck says a getter keeps reading back on.
type fakeWebKit struct {
	win, view, settings uintptr

	notView       bool // the window's child is not a WebKitWebView
	webrtc, media bool
	features      map[string]bool // the feature list; nil: no feature API
	nullList      bool            // webkit_settings_get_all_features returns NULL
	stuck         map[string]bool // "webrtc", "media" or a feature id
}

func newFakeWebKit() *fakeWebKit {
	return &fakeWebKit{
		win: 1, view: 2, settings: 3,
		// the engine defaults measured on 2.52: media stream and preconnect on
		media:    true,
		features: map[string]bool{"LinkPreconnect": true, "Other": true},
		stuck:    map[string]bool{},
	}
}

// gboolean is what the fake's getters answer: any truthy C int, here 0x100
// for true, whose low byte is 0 (IN-02: a getter read as a Go bool would
// take it for false).
func gboolean(on bool) int32 {
	if on {
		return 0x100
	}
	return 0
}

// api is the webkitAPI the fake answers through; feature handles are
// 1-based indexes into ids.
func (k *fakeWebKit) api() *webkitAPI {
	api := &webkitAPI{
		binGetChild: func(w uintptr) uintptr {
			if w == k.win {
				return k.view
			}
			return 0
		},
		getSettings: func(v uintptr) uintptr {
			// webkit_web_view_get_settings type-checks its argument
			if v != 0 && v == k.view && !k.notView {
				return k.settings
			}
			return 0
		},
		setWebRTC:      func(_ uintptr, on bool) { k.webrtc = on },
		getWebRTC:      func(uintptr) int32 { return gboolean(k.webrtc || k.stuck["webrtc"]) },
		setMediaStream: func(_ uintptr, on bool) { k.media = on },
		getMediaStream: func(uintptr) int32 { return gboolean(k.media || k.stuck["media"]) },
	}
	if k.features == nil {
		api.featureErr = errors.New("missing symbol webkit_settings_get_all_features")
		return api
	}
	var ids []string
	for id := range k.features {
		ids = append(ids, id)
	}
	api.allFeatures = func() uintptr {
		if k.nullList {
			return 0
		}
		return 100
	}
	api.featureCount = func(uintptr) uint { return uint(len(ids)) }
	api.featureAt = func(_ uintptr, i uint) uintptr { return uintptr(i) + 1 }
	api.featureID = func(f uintptr) string { return ids[f-1] }
	api.setFeature = func(_ uintptr, f uintptr, on bool) { k.features[ids[f-1]] = on }
	api.getFeature = func(_ uintptr, f uintptr) int32 { return gboolean(k.features[ids[f-1]] || k.stuck[ids[f-1]]) }
	return api
}

// TestHardenWindowFailsClosed pins which outcomes keep a napplet window
// from opening (WR-02): every one where WebRTC or media capture may still be
// on, where a link preconnect switch exists and reads back on, or where the
// feature API exists but has no LinkPreconnect feature (WR-05, iteration 2:
// a rename or removal on 2.42+, not a known old engine). The only outcome
// that opens with a channel on is a WebKitGTK with no feature API at all
// (older than 2.42), and it says so.
func TestHardenWindowFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(k *fakeWebKit)
		win      uintptr
		resolve  func(k *fakeWebKit) func() (*webkitAPI, error)
		wantErr  string // "" means the window may open
		degraded bool
	}{
		{name: "everything turned off", win: 1},
		{name: "no native window", win: 0, wantErr: "no native window"},
		{name: "the window holds no web view", win: 7, wantErr: "no web view"},
		{name: "the child is not a web view", win: 1, setup: func(k *fakeWebKit) { k.notView = true }, wantErr: "no settings"},
		{
			name: "webkitgtk cannot be resolved", win: 1, wantErr: "unavailable",
			resolve: func(*fakeWebKit) func() (*webkitAPI, error) {
				return func() (*webkitAPI, error) { return nil, errors.New("missing symbol gtk_bin_get_child") }
			},
		},
		{
			name: "a purego call panics", win: 1, wantErr: "panicked",
			resolve: func(k *fakeWebKit) func() (*webkitAPI, error) {
				return func() (*webkitAPI, error) {
					api := k.api()
					api.setMediaStream = func(uintptr, bool) { panic("bad call") }
					return api, nil
				}
			},
		},
		{name: "webrtc reads back on", win: 1, setup: func(k *fakeWebKit) { k.stuck["webrtc"] = true }, wantErr: "webrtc"},
		{name: "media capture reads back on", win: 1, setup: func(k *fakeWebKit) { k.stuck["media"] = true }, wantErr: "media capture"},
		{
			name: "the preconnect switch exists and reads back on", win: 1,
			setup: func(k *fakeWebKit) { k.stuck["LinkPreconnect"] = true }, wantErr: "link preconnect",
		},
		{name: "the feature list is NULL", win: 1, setup: func(k *fakeWebKit) { k.nullList = true }, wantErr: "no feature list"},
		{name: "no feature api (webkitgtk < 2.42)", win: 1, setup: func(k *fakeWebKit) { k.features = nil }, degraded: true},
		{
			name: "the feature api has no LinkPreconnect feature", win: 1, wantErr: "no feature LinkPreconnect",
			setup: func(k *fakeWebKit) { k.features = map[string]bool{"Other": true} },
		},
		{
			name: "the feature api has an empty feature list", win: 1, wantErr: "no feature LinkPreconnect",
			setup: func(k *fakeWebKit) { k.features = map[string]bool{} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := newFakeWebKit()
			if tc.setup != nil {
				tc.setup(k)
			}
			resolve := func() (*webkitAPI, error) { return k.api(), nil }
			if tc.resolve != nil {
				resolve = tc.resolve(k)
			}
			h, err := hardenWindow(resolve, tc.win)

			if tc.wantErr == "" && err != nil {
				t.Fatalf("hardenWindow failed: %v", err)
			}
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("hardenWindow let the window open (%+v), want an error about %q", h, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q does not mention %q", err, tc.wantErr)
				}
				return
			}
			if h.webrtc || h.mediaStream {
				t.Errorf("opened with webrtc=%v media_stream=%v", h.webrtc, h.mediaStream)
			}
			if !h.reached {
				t.Error("the settings were never read back")
			}
			if tc.degraded {
				if !h.linkPreconnect || !errors.Is(h.degraded, errNoSwitch) {
					t.Errorf("link_preconnect=%v degraded=%v, want on and errNoSwitch", h.linkPreconnect, h.degraded)
				}
			} else if h.linkPreconnect || h.degraded != nil {
				t.Errorf("link_preconnect=%v degraded=%v, want off and nil", h.linkPreconnect, h.degraded)
			}
		})
	}
}
