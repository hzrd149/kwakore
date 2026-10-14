package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"
	"kwakore/backend/webview"
)

// BuiltinDefinition is supplied by native code, never by a napplet or manifest.
// Key identifies persistent storage across upgrades. Same-user native clients
// are administrative authorities; a key does not authenticate an application.
type BuiltinDefinition struct {
	Key        string            `json:"key"`
	HTML       string            `json:"html"`
	SHA256     string            `json:"sha256"`
	Name       string            `json:"name"`
	Width      int               `json:"width,omitempty"`
	Height     int               `json:"height,omitempty"`
	Domains    []string          `json:"domains"`
	Extensions []ExtensionDomain `json:"extensions"`
	// Permissions pre-grants explicitly supported stock permissions for this
	// instance only. Currently fetch requires the resource domain.
	Permissions []Permission `json:"permissions,omitempty"`
}

// ExtensionDomain installs trusted binding code into only its owning document.
// V1 supports unary requests with <request>.result responses, not pushes.
type ExtensionDomain struct {
	Name    string           `json:"name"`
	Version string           `json:"version"`
	Prelude string           `json:"prelude"`
	Routes  []ExtensionRoute `json:"routes"`
}
type ExtensionRoute struct {
	Type              string `json:"type"`
	MaxRequestBytes   int    `json:"max_request_bytes"`
	MaxResponseBytes  int    `json:"max_response_bytes"`
	Concurrency       int    `json:"concurrency"`
	RequestsPerSecond int    `json:"requests_per_second"`
	DeadlineMS        int    `json:"deadline_ms"`
}

// ExtensionError is the public error shape. Internal errors never expose raw
// handler errors. Codes -32000..-32004 are reserved for runtime failures.
type ExtensionError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}
type ExtensionRequest struct {
	Instance   string          `json:"instance"`
	Generation int             `json:"generation"`
	Domain     string          `json:"domain"`
	Envelope   json.RawMessage `json:"envelope"`
}

// ExtensionHandler must respect ctx before committing queued work. Kwakore
// never retries a request. A running mutation is not rolled back on cancel.
// Return an envelope containing exactly type, id and either result or error.
type ExtensionHandler func(context.Context, ExtensionRequest) (json.RawMessage, error)

type builtinRegistry struct {
	definition BuiltinDefinition
	routes     map[string]*napRoute
	preludes   []string
	mu         sync.Mutex
	pending    map[string]bool
	active     chan struct{}
}

var extensionName = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)
var extensionType = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-zA-Z0-9]*)+$`)

func prepareBuiltin(d BuiltinDefinition, handler ExtensionHandler) (*builtinRegistry, error) {
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > 900*1024 {
		return nil, errors.New("built-in definition exceeds 900 KiB")
	}
	if d.Key == "" || len(d.Key) > 128 || strings.ContainsAny(d.Key, "\x00\r\n") || !utf8.ValidString(d.Key) {
		return nil, errors.New("invalid built-in key")
	}
	if !utf8.ValidString(d.HTML) || !utf8.ValidString(d.Name) || d.Name == "" || len(d.Name) > 256 {
		return nil, errors.New("invalid built-in document or name")
	}
	sum := sha256.Sum256([]byte(d.HTML))
	if d.SHA256 != hex.EncodeToString(sum[:]) {
		return nil, errors.New("built-in document digest mismatch")
	}
	if handler == nil && len(d.Extensions) > 0 {
		return nil, errors.New("extension owner required")
	}
	if len(d.Extensions) > 16 {
		return nil, errors.New("too many extension domains")
	}
	r := &builtinRegistry{definition: d, routes: map[string]*napRoute{}, pending: map[string]bool{}, active: make(chan struct{}, 64)}
	seen := map[string]bool{}
	for _, domain := range d.Domains {
		if seen[domain] || !slices.Contains([]string{"theme", "storage", "gamepad", "resource"}, domain) {
			return nil, fmt.Errorf("unsupported or duplicate built-in domain %q", domain)
		}
		seen[domain] = true
		for typ, route := range napRoutes {
			if strings.HasPrefix(typ, domain+".") {
				r.routes[typ] = route
			}
		}
	}
	permissions := map[Permission]bool{}
	for _, perm := range d.Permissions {
		// Keep this explicit: new APIs must never become pre-grantable just
		// because they introduce another Permission constant or route.
		if perm != PermFetch || !seen["resource"] || permissions[perm] {
			return nil, fmt.Errorf("unsupported or duplicate built-in permission %q (fetch requires resource)", perm)
		}
		permissions[perm] = true
	}
	for _, domain := range d.Extensions {
		if !extensionName.MatchString(domain.Name) || seen[domain.Name] || slices.Contains(napDomains, domain.Name) || slices.Contains([]string{"nap", "napp", "runtime", "extension", "kwakore"}, domain.Name) || domain.Version == "" || len(domain.Version) > 64 || !utf8.ValidString(domain.Version) || !utf8.ValidString(domain.Prelude) || len(domain.Prelude) > 128*1024 || len(domain.Routes) == 0 || len(domain.Routes) > 32 {
			return nil, errors.New("invalid or reserved extension domain")
		}
		seen[domain.Name] = true
		r.preludes = append(r.preludes, domain.Prelude)
		for _, spec := range domain.Routes {
			if len(spec.Type) > 128 || !extensionType.MatchString(spec.Type) || !strings.HasPrefix(spec.Type, domain.Name+".") || strings.HasSuffix(spec.Type, ".result") || r.routes[spec.Type] != nil || spec.MaxRequestBytes < 256 || spec.MaxRequestBytes > napDefaultMaxRaw || spec.MaxResponseBytes < 2048 || spec.MaxResponseBytes > napDefaultMaxRaw || spec.Concurrency < 1 || spec.Concurrency > 32 || spec.RequestsPerSecond < 1 || spec.RequestsPerSecond > 100 || spec.DeadlineMS < 1 || spec.DeadlineMS > 180000 {
				return nil, errors.New("invalid extension route bounds or type")
			}
			slots := make(chan struct{}, spec.Concurrency)
			limiter := rate.NewLimiter(rate.Limit(spec.RequestsPerSecond), spec.RequestsPerSecond)
			route := &napRoute{gate: openGate("native-owned extension"), fail: failShape(failErr), maxRaw: spec.MaxRequestBytes, extension: true}
			route.h = func(c *napCall) {
				key := strconv.Itoa(c.gen) + ":" + c.Type + ":" + extensionIDKey(c.ID)
				r.mu.Lock()
				duplicate := r.pending[key]
				if !duplicate {
					r.pending[key] = true
				}
				r.mu.Unlock()
				if duplicate {
					c.drop()
					return
				}
				release := func() { r.mu.Lock(); delete(r.pending, key); r.mu.Unlock() }
				if !limiter.Allow() {
					release()
					c.failWith(napErrRateLimited)
					return
				}
				select {
				case slots <- struct{}{}:
				default:
					release()
					c.failWith(napErrRateLimited)
					return
				}
				select {
				case r.active <- struct{}{}:
				default:
					<-slots
					release()
					c.failWith(napErrRateLimited)
					return
				}
				c.async(func(parent context.Context) {
					defer release()
					ctx, cancel := context.WithTimeout(parent, time.Duration(spec.DeadlineMS)*time.Millisecond)
					defer cancel()
					// The bounded slot remains held until the handler exits, even if it
					// ignores cancellation. The waiter can still settle on its deadline.
					type answer struct {
						raw json.RawMessage
						err error
					}
					done := make(chan answer, 1)
					go func() {
						defer func() { <-slots; <-r.active }()
						defer func() {
							if recover() != nil {
								done <- answer{err: errors.New("handler panic")}
							}
						}()
						if ctx.Err() != nil {
							done <- answer{err: ctx.Err()}
							return
						}
						raw, err := handler(ctx, ExtensionRequest{c.ci.ID(), c.gen, domain.Name, append(json.RawMessage(nil), c.raw...)})
						done <- answer{raw, err}
					}()
					select {
					case a := <-done:
						if ctx.Err() != nil {
							c.failWith("timeout")
							return
						}
						if a.err != nil {
							c.failWith(napErrInternal)
							return
						}
						if len(a.raw) > spec.MaxResponseBytes {
							c.failWith(napErrTooLarge)
							return
						}
						fields, err := validateExtensionResponse(a.raw, c.Type, c.ID, spec.MaxResponseBytes)
						if err != nil {
							c.failWith(napErrInvalid)
							return
						}
						final, err := json.Marshal(c.envelope(c.Type+".result", fields))
						if err != nil {
							c.failWith(napErrInvalid)
							return
						}
						if len(final) > spec.MaxResponseBytes {
							c.failWith(napErrTooLarge)
							return
						}
						c.reply(fields)
					case <-ctx.Done():
						c.failWith("timeout")
					}
				})
			}
			r.routes[spec.Type] = route
		}
	}
	if _, err := webview.NappletSrcdocWithPreludes([]byte(d.HTML), d.Domains, r.preludes); err != nil {
		return nil, err
	}
	// Clone caller-owned slices before retaining a definition.
	var cloned BuiltinDefinition
	_ = json.Unmarshal(encoded, &cloned)
	r.definition = cloned
	return r, nil
}

func validateExtensionResponse(raw json.RawMessage, typ string, id json.RawMessage, limit int) (map[string]any, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("response is not valid UTF-8")
	}
	if len(raw) > limit {
		return nil, errors.New("response too large")
	}
	head, ok := parseNapHead(raw)
	if !ok || head.collide || head.badID || head.id == nil || head.typ != typ+".result" || extensionIDKey(head.id) != extensionIDKey(id) {
		return nil, errors.New("invalid response correlation")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 {
		return nil, errors.New("invalid response envelope")
	}
	out := map[string]any{}
	if value, ok := fields["result"]; ok {
		out["result"] = value
		return out, nil
	}
	value, ok := fields["error"]
	if !ok {
		return nil, errors.New("missing result or error")
	}
	var errorFields map[string]json.RawMessage
	if json.Unmarshal(value, &errorFields) != nil || errorFields["code"] == nil || errorFields["message"] == nil || string(errorFields["code"]) == "null" {
		return nil, errors.New("missing public error fields")
	}
	var e ExtensionError
	if json.Unmarshal(value, &e) != nil || int64(e.Code) > 9007199254740991 || int64(e.Code) < -9007199254740991 || e.Message == "" || len(e.Message) > 1024 || len(e.Reason) > 128 {
		return nil, errors.New("invalid public error")
	}
	out["error"] = e
	return out, nil
}
func (ci *Instance) napRouteFor(typ string) *napRoute {
	if ci.builtin != nil {
		return ci.builtin.routes[typ]
	}
	return napRoutes[typ]
}
func extensionFailure(reason string) ExtensionError {
	code := -32000
	switch reason {
	case napErrInvalid:
		code = -32001
	case napErrTooLarge:
		code = -32002
	case napErrRateLimited:
		code = -32003
	case "timeout":
		code = -32004
	}
	return ExtensionError{Code: code, Message: "Custom request failed", Reason: reason}
}

// OpenBuiltin launches native-approved bytes using the stock hardened window.
// The caller owns the handler for the window lifetime. Close/reset cancels
// request contexts. No remote identity, signed manifest or trial is created.
func OpenBuiltin(ctx context.Context, d BuiltinDefinition, handler ExtensionHandler) (*Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := prepareBuiltin(d, handler)
	if err != nil {
		return nil, err
	}
	n := Napp{ID: "builtin:" + d.Key, Name: d.Name, Format: "napplet", InitialSize: &NappInitialSize{Width: d.Width, Height: d.Height}}
	ci := &Instance{instance: randomID(), storageInstance: randomID(), number: int(windowSerial.Add(1)), napp: n, builtin: r, nap: newNapSession(), subs: map[int]context.CancelFunc{}, actions: map[string]int{}, changed: make(chan struct{}), dispatches: map[int]chan WireMsg{}, gone: make(chan struct{})}
	theme, vars := Theme()
	w, h := n.WindowSize()
	spec := WindowSpec{Instance: ci.instance, Number: ci.number, NappID: n.ID, Name: n.Name, Format: "napplet", Theme: theme, ThemeVars: vars, Width: w, Height: h}
	addInstance(ci)
	var transport Transport
	if attached, ok := ctx.Value(attachedLaunchKey{}).(*attachedLaunch); ok {
		attached.window = &AttachedWindow{spec.NappID, spec.Name, spec.Instance, w, h, spec.Format, theme, vars}
		transport = attached.transport
		if binder, ok := transport.(interface{ BindWindow(string) }); ok {
			binder.BindWindow(ci.instance)
		}
	} else if contextual, ok := host.(ContextWindowHost); ok {
		transport, err = contextual.OpenWindowContext(ctx, spec)
	} else {
		transport, err = host.OpenWindow(spec)
	}
	if err != nil {
		WindowClosed(ci.instance)
		return nil, err
	}
	ci.attach(transport)
	notifyState()
	return ci, nil
}
func (ci *Instance) builtinStorageFile(scope string) string {
	key := ci.builtin.definition.Key
	if scope == "instance" {
		key += "\x00" + ci.storageInstance
	}
	return filepath.Join(dataDir, "builtin-storage", keyFileName(key))
}
func (ci *Instance) builtinBoot() (any, error) {
	d := ci.builtin.definition
	doc, err := webview.NappletSrcdocWithPreludes([]byte(d.HTML), d.Domains, ci.builtin.preludes)
	if err != nil {
		return nil, err
	}
	failures := map[string]any{}
	for typ, route := range ci.builtin.routes {
		if route.extension {
			failures[typ] = map[string]any{"maxBytes": route.maxRaw}
		}
	}
	return map[string]any{"srcdoc": doc, "title": d.Name, "extensions": failures}, nil
}

// BuiltinAttachedResult returns the stock child launch descriptor.
func BuiltinAttachedResult(ctx context.Context, instance string) ServiceLaunchResult {
	return serviceLaunchResult(ctx, "", instance)
}

func (ci *Instance) serviceAddress() string {
	if ci.builtin != nil {
		return ""
	}
	return ci.napp.Address()
}

// JSON-equivalent IDs identify the same pending promise, including escaped
// strings and differently formatted finite numeric values.
func extensionIDKey(raw json.RawMessage) string {
	var id any
	if json.Unmarshal(raw, &id) != nil {
		return string(raw)
	}
	canonical, err := json.Marshal(id)
	if err != nil {
		return string(raw)
	}
	return string(canonical)
}
