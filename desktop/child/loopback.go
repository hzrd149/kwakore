package main

import (
	"net/http"

	nappbridge "verdana/backend/webview"
)

// Every page the child serves on loopback (the napplet host page, a napp's
// files, the settings page) goes through loopbackHeaders, so each response,
// a 404 or an SPA fallback included, carries its page's policy and turns off
// DNS prefetching (D-06, D-11). The policies themselves live in
// backend/webview, where Android reads them too (D-07).

// loopbackHeaders sets the Content-Security-Policy and X-DNS-Prefetch-Control
// headers on every response before next writes anything.
func loopbackHeaders(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		h := wr.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-DNS-Prefetch-Control", "off")
		next.ServeHTTP(wr, r)
	})
}

// nappletHostHandler serves the napplet host page at "/" and nothing else.
//
// Its policy is NappletHostCSP: the napplet's own NIP-5D policy plus
// frame-ancestors 'none'. The srcdoc frame inherits the host page's policy
// on top of its own, so the two must allow the same scripts and styles or
// every napplet's inline scripts stop (D-17); the baseline's frame-src and
// child-src 'none' are what keep the napplet frame from being navigated to
// an http(s) page, a meta refresh or an anchor target while about:srcdoc
// still loads (D-04, D-05), and frame-ancestors, which only a header can
// carry, keeps other pages from embedding this one.
func nappletHostHandler(page []byte) http.Handler {
	return loopbackHeaders(nappbridge.NappletHostCSP(), http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(wr, r)
			return
		}
		wr.Header().Set("Content-Type", "text/html; charset=utf-8")
		wr.Header().Set("Cache-Control", "no-store")
		_, _ = wr.Write(page)
	}))
}
