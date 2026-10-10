package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/abemedia/go-webview"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/rs/zerolog"

	"kwakore/desktop/internal/wireline"
)

type wireMsg struct {
	T      string          `json:"t"`
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params string          `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	Code   string          `json:"code,omitempty"`
	Idx    *int            `json:"idx,omitempty"`
}

// nappMeta is what the launcher tells a napplet window about its napplet.
// The window never loads napplet files from disk or a url: the bytes arrive
// over nap.boot, after the backend verified them.
type nappMeta struct {
	ID        string
	Name      string
	Instance  string
	Theme     string
	ThemeVars string
}

var (
	meta      nappMeta
	outMu     sync.Mutex
	outEnc    *json.Encoder
	pending   = xsync.NewMapOf[int, chan wireMsg]()
	reqSerial atomic.Int64
	log       zerolog.Logger
)

func main() {
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().
		Int("_", os.Getpid()).
		Timestamp().
		Logger()

	meta = nappMeta{
		ID:        os.Getenv("KWAKORE_NAPP_ID"),
		Name:      os.Getenv("KWAKORE_NAPP_NAME"),
		Instance:  os.Getenv("KWAKORE_INSTANCE_ID"),
		Theme:     os.Getenv("KWAKORE_THEME"),
		ThemeVars: os.Getenv("KWAKORE_THEME_VARS"),
	}
	// this program only hosts napplets (D-10): the napp (35130) window and
	// the bundled settings page were retired with the Gio launcher, so any
	// other kind of window is refused before a webview exists
	if os.Getenv("KWAKORE_NAPP_FORMAT") != "napplet" {
		log.Error().Msg("napp cannot run in the napplet program")
		os.Exit(1)
	}
	if os.Getenv("KWAKORE_WINDOW_KIND") == "settings" {
		log.Error().Msg("settings cannot run in the napplet program")
		os.Exit(1)
	}
	if meta.Name == "" {
		meta.Name = meta.ID
	}
	if meta.Instance == "" {
		meta.Instance = meta.ID
	}

	log.Info().Str("kind", "napplet").Str("napp", meta.ID).Str("instance", meta.Instance).Msg("window program started")
	outEnc = json.NewEncoder(os.Stdout)

	// before the library is loaded (lazily, by the first webview.New)
	if err := checkWebviewLibrary(); err != nil {
		log.Error().Err(err).Msg("webview library is not where the launcher put it")
		os.Exit(1)
	}

	// engine-wide setup (WebView2's browser arguments) happens before the
	// first view exists
	prepareEngine()

	runtime.LockOSThread()

	w := webview.New(os.Getenv("WEBVIEW_DEBUG") == "true")
	w.SetSize(windowWidth(), windowHeight(), webview.HintNone)
	w.SetTitle(windowTitle(meta.Name))

	runNapplet(w)
}

func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// windowWidth/Height are the initial window size in pixels: the napp's
// initial_size via the launcher, falling back to a roomy default. Values are
// clamped like nostrapps sanitizes them (positive, capped at 2000, with a
// minimum that keeps the window usable).
func windowWidth() int { return clampWindowSize(envSize("KWAKORE_WINDOW_WIDTH", 1024), 320, 2000) }

func windowHeight() int { return clampWindowSize(envSize("KWAKORE_WINDOW_HEIGHT", 700), 240, 2000) }

func envSize(key string, fallback int) int {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return fallback
}

func clampWindowSize(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// windowTitle names the OS window after the napp.
func windowTitle(name string) string {
	if name == "" {
		return "Napp"
	}
	return name
}

// themeInitScript sets window.__nappTheme, which bridge.js applies as soon as
// it runs. varsJSON comes from the launcher, so it is already valid JSON.
func themeInitScript(name, varsJSON string) string {
	if name == "" {
		name = "light"
	}
	if strings.TrimSpace(varsJSON) == "" {
		varsJSON = "{}"
	}
	return "window.__nappTheme = {name:" + jsString(name) + ",vars:" + varsJSON + "};"
}

func rpcBound(method string, params string) string {
	result, err := rpc(method, params)
	if err != nil {
		wrapped, _ := json.Marshal(map[string]string{"__bridge_error": err.Error()})
		return string(wrapped)
	}
	if len(result) == 0 {
		return "null"
	}
	return string(result)
}

func rpc(method string, params string) (json.RawMessage, error) {
	id := int(reqSerial.Add(1))
	ch := make(chan wireMsg, 1)
	pending.Store(id, ch)

	writeMsg(wireMsg{T: "rpc", ID: id, Method: method, Params: params})

	resp := <-ch
	pending.Delete(id)

	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.Result, nil
}

// maxParentLine bounds one line from the launcher (D-17). Replies from the
// launcher, such as a resource.bytesMany carrying many blobs, may be far
// larger than anything a napplet sends up, so this is not the inbound cap:
// this direction is trusted, and the bound only stops a launcher bug from
// growing the child without limit.
const maxParentLine = 128 << 20

func reader(w webview.WebView) {
	err := wireline.Read(os.Stdin, maxParentLine, func(line []byte) {
		var m wireMsg
		if err := json.Unmarshal(line, &m); err != nil {
			log.Warn().Err(err).Msg("skipping unreadable line from launcher")
			return
		}
		switch m.T {
		case "prompt":
			// the prompt this napp asked for covers its own screen until
			// answered (empty params take a stale overlay down)
			if strings.TrimSpace(m.Params) == "" {
				w.Dispatch(func() { w.Eval(promptHideCode()) })
				return
			}
			var pv promptView
			if err := json.Unmarshal([]byte(m.Params), &pv); err != nil {
				log.Warn().Err(err).Msg("unreadable prompt from launcher")
				return
			}
			code := promptOverlayCode(pv)
			w.Dispatch(func() { w.Eval(code) })
		case "resp":
			if ch, ok := pending.Load(m.ID); ok {
				ch <- m
			}
		case "eval":
			code := m.Code
			w.Dispatch(func() { w.Eval(code) })
		case "action":
			// the launcher routed an action here: hand it to the napp's
			// registered handler (idx) and/or its popstate listener
			idx := "null"
			if m.Idx != nil {
				idx = strconv.Itoa(*m.Idx)
			}
			payload := m.Params
			if payload == "" {
				payload = "null"
			}
			code := "window.__bridge_dispatch_action(" +
				strconv.Itoa(m.ID) + "," + jsString(m.Method) + "," + jsString(payload) + "," + idx + ")"
			w.Dispatch(func() { w.Eval(code) })
		case "theme":
			// the launcher switched theme: apply it now, and make it the value
			// future page loads in this window start from
			init := themeInitScript(m.Method, m.Params)
			vars := m.Params
			if vars == "" {
				vars = "{}"
			}
			code := init +
				"if (window.__bridge_theme_change) window.__bridge_theme_change(" +
				jsString(m.Method) + "," + jsString(vars) + ");"
			w.Dispatch(func() {
				w.Init(init)
				w.Eval(code)
			})
		case "close":
			w.Dispatch(func() { w.Terminate() })
		}
	})
	if errors.Is(err, bufio.ErrTooLong) {
		log.Warn().Int("max", maxParentLine).Msg("launcher sent an overlong line; closing")
	} else if err != nil {
		log.Warn().Err(err).Msg("reading from launcher failed; closing")
	}
	w.Terminate()
}

func writeMsg(m wireMsg) {
	outMu.Lock()
	defer outMu.Unlock()
	outEnc.Encode(m)
}

// ─── prompt overlay ──────────────────────────────────────────────
//
// A prompt fired by this napp is shown over its own screen: an opaque
// overlay that hides the webview until the user answers it.

// promptView and promptOptionView mirror backend.Prompt as the overlay needs
// to see it.
type promptView struct {
	ID       int                `json:"id"`
	Title    string             `json:"title"`
	Detail   string             `json:"detail"`
	Code     string             `json:"code"`
	Options  []promptOptionView `json:"options"`
	Remember bool               `json:"remember"`
}

type promptOptionView struct {
	Label     string `json:"label"`
	Detail    string `json:"detail"`
	NappID    string `json:"nappId"`
	Instance  string `json:"instance"`
	Dev       bool   `json:"dev"`
	Suggested bool   `json:"suggested"`
}

// promptAnswer sends an answer the overlay's buttons made up to the
// launcher; if the launcher has another prompt queued for this window it will
// send it right back. scope is how long the answer holds: "once", "session" or
// "always" (see backend.Scope) — the launcher files the wider ones away and
// stops asking. It is never bound as it is: the window reaches it through
// nappletAnswer, a binding that checks the window's token.
func promptAnswer(id int, ok bool, index int, scope string) {
	b, _ := json.Marshal(map[string]any{"ok": ok, "index": index, "scope": scope})
	writeMsg(wireMsg{T: "promptAnswer", ID: id, Params: string(b)})
}

// overlayAnswer is the JavaScript function expression the prompt overlay's
// buttons call with (id, ok, index, scope). runNapplet sets it to the host
// page's top-frame wrapper, which carries the window token; the default
// answers nothing.
var overlayAnswer = "function(){}"

// newWindowToken is a random per-window secret.
func newWindowToken() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		log.Fatal().Err(err).Msg("no randomness for a window token")
	}
	return hex.EncodeToString(raw[:])
}

// promptOverlayCode draws the overlay for one prompt. The runtime is
// evaluated afresh every time, as a function expression called right away,
// rather than installed once as a page global a napp could replace before
// the first prompt and so read the answer call out of.
func promptOverlayCode(pv promptView) string {
	data, err := json.Marshal(pv)
	if err != nil {
		return ""
	}
	return ";(" + promptShowScript + ")(" + string(data) + "," + overlayAnswer + ");"
}

func promptHideCode() string {
	return ";(function(){var o = document.getElementById('__kwakore_prompt');" +
		"if (o && o.parentNode) o.parentNode.removeChild(o);" +
		"if (window.__nap_gamepad_focus_changed) window.__nap_gamepad_focus_changed();})();"
}

// promptShowScript is the overlay runtime, a function of the prompt and of
// the answer call. It draws an opaque full-viewport cover — the napp
// underneath stays hidden until the user answers.
const promptShowScript = "function(p, answer){" +
	"function tok(k, fallback) {" +
	"try { var v = (window.__nappTheme && window.__nappTheme.vars) || {};" +
	"return v[k] || fallback } catch(e) { return fallback }" +
	"};" +
	"var old = document.getElementById('__kwakore_prompt');" +
	"if (old && old.parentNode) old.parentNode.removeChild(old);" +
	"var dark = window.__nappTheme && window.__nappTheme.name === 'dark';" +
	"var bg = tok('surface', dark ? '#17181b' : '#ffffff');" +
	"var card = tok('surface-alt', dark ? '#23252b' : '#f2f2f2');" +
	"var fg = tok('text', dark ? '#e8e8ea' : '#000000');" +
	"var muted = tok('text-faint', dark ? '#7d818a' : '#999999');" +
	"var accent = tok('accent', dark ? '#5c6bc0' : '#3f51b5');" +
	"var accentText = tok('accent-text', '#ffffff');" +
	"var dev = tok('dev', dark ? '#5a3b1a' : '#ffe5b4');" +
	"var devText = tok('dev-text', dark ? '#ffd79a' : '#704000');" +
	"var suggest = tok('suggest', dark ? '#1e3a2a' : '#d8efdc');" +
	"var suggestText = tok('suggest-text', dark ? '#b6e3c1' : '#14532d');" +
	"var border = tok('border', dark ? '#3a3d45' : '#cccccc');" +
	"var o = document.createElement('div');" +
	"o.id = '__kwakore_prompt';" +
	"o.style.cssText = 'position:fixed;top:0;left:0;width:100vw;height:100vh;" +
	"z-index:2147483647;background:' + bg + ';color:' + fg" +
	"+ ';font:14px sans-serif;overflow:auto;padding:24px;box-sizing:border-box;" +
	"margin:0;border:0;display:flex;flex-direction:column;';" +
	"var box = document.createElement('div');" +
	"box.style.cssText = 'margin:auto 0;width:100%;max-width:520px;';" +
	"var h = document.createElement('div');" +
	"h.textContent = p.title;" +
	"h.style.cssText = 'font-size:17px;font-weight:700;margin:0 0 10px;';" +
	"box.appendChild(h);" +
	"if (p.detail) {" +
	"var d = document.createElement('div');" +
	"d.textContent = p.detail;" +
	"d.style.cssText = 'color:' + muted + ';margin-bottom:14px;line-height:1.45;';" +
	"box.appendChild(d);" +
	"}" +
	"if (p.code) {" +
	"var c = document.createElement('pre');" +
	"c.style.cssText = 'white-space:pre-wrap;word-break:break-word;background:' + card " +
	"+ ';border:1px solid ' + border + ';border-radius:8px;padding:10px" +
	";max-height:160px;overflow:auto;font:12px monospace;margin:0 0 14px;';" +
	"c.textContent = p.code;" +
	"box.appendChild(c);" +
	"}" +
	"function btn(label, tone, ok, index, scope) {" +
	"var bgc = card, fgc = fg;" +
	"if (tone === 'accent') { bgc = accent; fgc = accentText }" +
	"if (tone === 'dev') { bgc = dev; fgc = devText }" +
	"if (tone === 'suggest') { bgc = suggest; fgc = suggestText }" +
	"var b = document.createElement('button');" +
	"b.style.cssText = 'display:block;width:100%;max-width:360px;margin:0 auto 8px;padding:10px 14px" +
	";border:0;border-radius:8px;background:' + bgc + ';color:' + fgc + ';font-size:14px;text-align:left" +
	";cursor:pointer;';" +
	"b.textContent = label;" +
	"b.onclick = function() { answer(p.id, ok, index, scope) };" +
	"return b;" +
	"};" +
	// row puts buttons side by side, for the scopes of an approval prompt.
	"function row() {" +
	"var r = document.createElement('div');" +
	"r.style.cssText = 'display:flex;gap:8px;margin:0 0 8px;';" +
	"for (var i = 0; i < arguments.length; i++) {" +
	"var b = arguments[i];" +
	"b.style.maxWidth = 'none'; b.style.margin = '0'; b.style.flex = '1'; b.style.textAlign = 'center';" +
	"r.appendChild(b);" +
	"}" +
	"return r;" +
	"};" +
	"var isPicker = p.options && p.options.length;" +
	"if (isPicker) {" +
	"p.options.forEach(function(opt, i) {" +
	"var tone = opt.suggested ? 'suggest' : (opt.dev ? 'dev' : (opt.instance ? 'accent' : 'chip'));" +
	"var b = btn(opt.label, tone, true, i, 'once');" +
	"if (opt.detail) { var sub = document.createElement('div'); sub.textContent = opt.detail;" +
	"sub.style.cssText = 'margin-top:3px;color:' + (tone === 'suggest' ? suggestText : (tone === 'dev' ? devText : (tone === 'accent' ? accentText : muted)))" +
	"+ ';font-size:11px;opacity:.75;'; b.appendChild(sub); }" +
	"box.appendChild(b);" +
	"});" +
	"box.appendChild(btn('Cancel', 'chip', false, 0, 'once'));" +
	"} else if (p.remember) {" +
	"box.appendChild(row(btn('Allow', 'accent', true, 0, 'once'), btn('Deny', 'chip', false, 0, 'once')));" +
	"box.appendChild(row(btn('Allow this session', 'chip', true, 0, 'session')," +
	"btn('Deny this session', 'chip', false, 0, 'session')));" +
	"box.appendChild(row(btn('Always allow', 'chip', true, 0, 'always'), btn('Always deny', 'chip', false, 0, 'always')));" +
	"} else {" +
	"box.appendChild(btn('Allow', 'accent', true, 0, 'once'));" +
	"box.appendChild(btn('Deny', 'chip', false, 0, 'once'));" +
	"}" +
	"o.appendChild(box);" +
	"document.documentElement.appendChild(o);" +
	"if (window.__nap_gamepad_focus_changed) window.__nap_gamepad_focus_changed();" +
	"}"
