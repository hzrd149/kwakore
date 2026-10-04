package main

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
