package main

import (
	"sync"
	"time"
)

// webview2BrowserArgs are the browser arguments every WebView2 window of
// this build starts with (D-19). libwebview keeps WebView2's user data in
// %APPDATA%\<exe name>, and the child is child-<sha256>.exe, so napp,
// napplet and settings windows of one build share one browser process; a
// window asking for different arguments than the running process has fails
// to open (ERROR_INVALID_STATE). So there is one value, for every kind.
//
// It stops WebRTC from using non-proxied UDP, which would otherwise reach
// the network around the napplet's connect-src. It is best effort:
// Microsoft calls browser flags unsupported in production, its effect inside
// WebView2 is unverified until it runs on Windows, and it does not stop TURN
// over TCP.
const webview2BrowserArgs = "--force-webrtc-ip-handling-policy=disable_non_proxied_udp"

// ─── forged binding calls ───────────────────────────────────────

// missLogInterval is how often a refused binding call may log.
const missLogInterval = 5 * time.Second

// missLog samples the log line for binding calls that came without the
// window token. Every such call is still refused; only the logging is
// thinned, so a page hammering the bindings cannot flood the launcher's log
// (or spend its CPU on it).
type missLog struct {
	mu         sync.Mutex
	last       time.Time
	suppressed int
}

// note records one miss at now. It reports whether to log it, and how many
// misses went unlogged since the last one that was.
func (m *missLog) note(now time.Time) (logIt bool, suppressed int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.last.IsZero() && now.Sub(m.last) < missLogInterval {
		m.suppressed++
		return false, 0
	}
	suppressed = m.suppressed
	m.last, m.suppressed = now, 0
	return true, suppressed
}

// tokenMisses is shared by every token-checked binding of this process.
var tokenMisses missLog
