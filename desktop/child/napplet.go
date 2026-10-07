package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/abemedia/go-webview"

	nappbridge "kwakore/backend/webview"
)

// A napplet window loads no napp files and gets no bridge.js. Its main frame
// is the launcher's host page, which puts the napplet in a sandboxed srcdoc
// iframe and carries NAP envelopes to the backend (nap.boot / nap.msg).
//
// The webview's bindings are the one thing the sandbox does not cover on its
// own: depending on the engine, a bound function (or the message handler
// behind it) can be reachable from child frames too. So in a napplet window
// both bindings demand a per-window secret that only the host page's
// top-frame init script knows. A frame that calls them without it is ignored.

var nappletToken string

func runNapplet(w webview.WebView) {
	// engine switches first, before the page or any binding exists. It
	// fails closed: an untrusted napplet never runs with WebRTC or media
	// capture at the engine's defaults, so the window does not open and the
	// launcher sees it close.
	if err := hardenEngine(w); err != nil {
		log.Error().Err(err).Msg("napplet window not opened: webkit hardening failed")
		reportWindowFailed(windowFailedEngineHardening)
		os.Exit(1)
	}

	nappletToken = newWindowToken()
	// the host page is the launcher's own, so its top-frame wrapper (which
	// holds the token) is what the overlay answers through
	overlayAnswer = "window.__verdana_prompt_answer"

	_ = w.Bind("__verdana_napplet_rpc", nappletRPC)
	_ = w.Bind("__verdana_napplet_answer", nappletAnswer)

	// Everything below runs in the top frame only. The token lives in this
	// closure and in the two wrappers it defines; the napplet's frame cannot
	// read another frame's script source, and an opaque-origin frame cannot
	// touch this window's globals.
	w.Init("(function(){if (window !== window.top) return;" +
		"var t = " + jsString(nappletToken) + ";" +
		"window.__verdanaNappletRPC = function(m, p){ return window.__verdana_napplet_rpc(t, m, p) };" +
		// the prompt overlay (promptShowScript) answers through this name
		"window.__verdana_prompt_answer = function(id, ok, index, scope){" +
		"return window.__verdana_napplet_answer(t, id, ok, index, scope) };" +
		"window.name = " + jsString(meta.Instance) + ";" +
		themeInitScript(meta.Theme, meta.ThemeVars) +
		"})();")
	w.Init(nappbridge.NappletHostJS())

	url := startNappletHostServer()
	if url == "" {
		log.Fatal().Msg("could not start the napplet host page")
	}
	w.Navigate(url)

	go reader(w)

	w.Run()
	w.Destroy()
	os.Exit(0)
}

// windowFailedEngineHardening is the code a napplet window reports when it
// exits because its engine hardening failed (backend HandleMessage,
// "windowFailed").
const windowFailedEngineHardening = "engine-hardening"

// reportWindowFailed tells the launcher, just before this process exits on
// its own, why the window is going away, so the user sees a reason instead
// of a window that flashes and vanishes. Only a fixed code travels: the
// launcher owns the words it shows, and the error itself stays in the log.
func reportWindowFailed(code string) {
	writeMsg(wireMsg{T: "windowFailed", Code: code})
}

func validToken(token string) bool {
	return nappletToken != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(nappletToken)) == 1
}

func nappletRPC(token, method, params string) string {
	if !validToken(token) {
		if logIt, n := tokenMisses.note(time.Now()); logIt {
			log.Warn().Str("method", method).Int("suppressed", n).
				Msg("napplet window: rpc without the window token, ignored")
		}
		wrapped, _ := json.Marshal(map[string]string{"__bridge_error": errors.New("forbidden").Error()})
		return string(wrapped)
	}
	return rpcBound(method, params)
}

func nappletAnswer(token string, id int, ok bool, index int, scope string) {
	if !validToken(token) {
		if logIt, n := tokenMisses.note(time.Now()); logIt {
			log.Warn().Int("prompt", id).Int("suppressed", n).
				Msg("napplet window: prompt answer without the window token, ignored")
		}
		return
	}
	promptAnswer(id, ok, index, scope)
}

// startNappletHostServer serves the host page, and nothing else, on loopback.
// The napplet's own bytes never come from here: they arrive over nap.boot as
// a srcdoc, after the backend verified them against the event's hash. The
// page's policy and why it equals the napplet's plus frame-ancestors are on
// nappletHostHandler.
func startNappletHostServer() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Error().Err(err).Msg("failed to listen for the napplet host page")
		return ""
	}
	go http.Serve(ln, nappletHostHandler([]byte(nappbridge.NappletHostHTML())))
	return "http://" + ln.Addr().String() + "/"
}
