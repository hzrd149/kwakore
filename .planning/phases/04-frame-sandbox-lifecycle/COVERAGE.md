# API Coverage — WebKitGTK 4.1 C API (via purego), WebView2 loader environment, WKWebView

> Full coverage by default. Opt-outs are explicit, reasoned decisions.
> Scope: the engine surfaces that can close channels bypassing the napplet's `connect-src` or frame navigation (SBOX-04, D-09/D-10/D-19/D-20). Plans: WebKitGTK and WebView2 in 04-02 (desktop/child/harden_*.go); host-page policy in 04-04.
> WebKitFeature API symbols used: webkit_settings_get_all_features, webkit_feature_list_get_length, webkit_feature_list_get, webkit_feature_get_identifier, webkit_settings_set_feature_enabled, webkit_settings_get_feature_enabled, webkit_feature_list_unref.
> decide-policy symbols not used: g_signal_connect_data, webkit_navigation_policy_decision_get_navigation_action, webkit_navigation_action_get_request, webkit_navigation_action_get_navigation_type, webkit_uri_request_get_uri, webkit_policy_decision_ignore, webkit_web_view_get_uri.

| capability | decision | reason |
|---|---|---|
| gtk_bin_get_child (GtkWindow to WebKitWebView) | INTEGRATE | |
| webkit_web_view_get_settings | INTEGRATE | |
| WebKitSettings enable-webrtc (set, read back) | INTEGRATE | |
| WebKitSettings enable-media-stream (set, read back) | INTEGRATE | |
| WebKitFeature API: LinkPreconnect lookup and toggle | INTEGRATE | |
| other WebKitFeature toggles | OPT-OUT | not needed: only LinkPreconnect was measured to bypass connect-src (04-RESEARCH leak matrix); everything else measured was blocked by CSP |
| WebKitSettings enable-dns-prefetching | OPT-OUT | not needed: deprecated and always FALSE on 2.4x; no DNS prefetch observed from the frame |
| WebKitSettings media / mediasource / encrypted-media | OPT-OUT | not needed: media loads are governed by the napplet CSP media-src 'none' |
| WebKitSettings script and window-opening behavior | OPT-OUT | explicitly out of scope: napplets need script; popups and top navigation are already blocked by sandbox allow-scripts |
| WebKitSettings file-URL access settings | OPT-OUT | not needed: pages are loopback http and about:srcdoc, and both settings default to false |
| decide-policy navigation hook | OPT-OUT | D-20 discretion: host CSP frame-src/child-src 'none' plus the D-01 rebuild cover every measured navigation; a stateless policy cannot tell a script's about:srcdoc navigation from the initial load |
| WebKitWebContext / WebKitNetworkSession settings | OPT-OUT | explicitly out of scope: per-window settings suffice, and the shared context also serves napp windows whose policy is deferred |
| WebView2 env WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS | INTEGRATE | |
| WebView2 env WEBVIEW2_USER_DATA_FOLDER | OPT-OUT | not needed: arguments are identical for every window kind (D-19), so one shared user data folder per build stays valid |
| WebView2 env browser folder / release channel | OPT-OUT | explicitly out of scope: runtime selection is unchanged |
| WebView2 COM options and settings interfaces | OPT-OUT | not reachable: libwebview 0.12.0 passes null options and go-webview exposes no COM handle; the environment variable is the supported path |
| WKWebView WebRTC / media capture switches | OPT-OUT | no public switch exists and private preference keys are not used; residual recorded as CONFORMANCE 5D-NG-wkwebview (04-06) |
