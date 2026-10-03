package backend

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The NAP route table (D-01). A request type runs only through a declared
// route, and the declaration says two things the handler cannot forget:
//
//   - the gate: what consent the type needs before it may touch anything the
//     user cares about. Open types need none and say why (the reason names the
//     requirement or phase that owns their real consent). Session and PerCall
//     types name the permission they ask for, once per session or on every
//     call. Dynamic types decide per payload (a data: fetch needs nothing, an
//     https: one needs PermFetch) and name the permissions they may ask for.
//   - the failure shape: how the pristine shim settles the request when it
//     fails. Most types fail as <type>.result with an error, but several do
//     not (relay.publish answers ok:false, identity.getPublicKey never carries
//     an error, intent.invoke nests its result, inc.channel.list is dropped by
//     the shim unless it has a channels list), and a wrong shape leaves the
//     napplet waiting, some of them forever (relay.* has no shim timeout).
//
// The handlers themselves are still registered by each nap_*.go file's init
// through handleNap. handleNap joins every handler with its declared spec in
// napRouteSpecs and panics when there is none, when it is invalid, or when a
// type is registered twice, so a type cannot reach napDispatch undeclared.

// napGateKind is what kind of consent a route needs. The zero value is
// rejected at registration: every route says what it needs.
type napGateKind uint8

const (
	napGateUnset napGateKind = iota
	napGateOpen
	napGateSession
	napGatePerCall
	napGateDynamic
)

func (k napGateKind) String() string {
	switch k {
	case napGateOpen:
		return "open"
	case napGateSession:
		return "session"
	case napGatePerCall:
		return "perCall"
	case napGateDynamic:
		return "dynamic"
	}
	return "unset"
}

// napGate is a route's consent declaration.
type napGate struct {
	kind napGateKind
	// perm is what a Session or PerCall route asks for
	perm Permission
	// perms are the only permissions a Dynamic route may ask for
	perms []Permission
	// reason says why an Open route needs no consent, or how a Dynamic one
	// decides; it names the requirement or phase that owns the question
	reason string
}

// openGate declares a type that needs no consent, and why.
func openGate(reason string) napGate { return napGate{kind: napGateOpen, reason: reason} }

// sessionGate declares a type whose permission is asked (or checked) once per
// session.
func sessionGate(perm Permission) napGate { return napGate{kind: napGateSession, perm: perm} }

// perCallGate declares a type whose permission is asked on every call
// (unless a rule answers it).
func perCallGate(perm Permission) napGate { return napGate{kind: napGatePerCall, perm: perm} }

// dynamicGate declares a type whose consent depends on its payload, with the
// permissions it may ask for.
func dynamicGate(reason string, perms ...Permission) napGate {
	return napGate{kind: napGateDynamic, perms: perms, reason: reason}
}

func (g napGate) String() string {
	switch g.kind {
	case napGateSession, napGatePerCall:
		return g.kind.String() + ":" + string(g.perm)
	case napGateDynamic:
		perms := make([]string, len(g.perms))
		for i, p := range g.perms {
			perms[i] = string(p)
		}
		return g.kind.String() + ":" + strings.Join(perms, ",")
	}
	return g.kind.String()
}

// Generic failure codes (D-07). A route maps them to its spec's own codes
// where the spec names one (napFailShape.codes); otherwise they go out as is.
const (
	napErrInternal    = "internal-error"
	napErrDenied      = "user-denied"
	napErrRateLimited = "rate-limited"
	napErrTooLarge    = "too-large"
	napErrInvalid     = "invalid-request"
)

// napFailKind is the envelope a route fails with. The zero value is rejected
// at registration.
type napFailKind uint8

const (
	failUnset napFailKind = iota
	// failNone: never answered (fire-and-forget types, and types whose id
	// names another request)
	failNone
	// failErr: <type>.result with error, plus the static fields
	failErr
	// failOkFalse: <type>.result with ok:false and error, plus the static fields
	failOkFalse
	// failOkFalseCode: <type>.result with ok:false, code and error
	failOkFalseCode
	// failTypedErr: <type>.error with error
	failTypedErr
	// failLink: <type>.result with status:"denied" and error
	failLink
	// failIntent: <type>.result with a nested result {ok:false, archetype,
	// action, handled:false, error}
	failIntent
	// failDefault: <type>.result with the static fields only, never an error
	failDefault
	// failSchemaError: config.schemaError with code and error
	failSchemaError
	// failGranted: notify.permission.result with granted:false
	failGranted
	// failLifecycle: the subscription's closed push ({closed} with subId and
	// reason), sent only when the request carries a string subId
	failLifecycle
)

// String is the kind's JSON name, shared with the host page's table.
func (k napFailKind) String() string {
	switch k {
	case failNone:
		return "none"
	case failErr:
		return "err"
	case failOkFalse:
		return "okFalse"
	case failOkFalseCode:
		return "okFalseCode"
	case failTypedErr:
		return "typedErr"
	case failLink:
		return "link"
	case failIntent:
		return "intent"
	case failDefault:
		return "default"
	case failSchemaError:
		return "schemaError"
	case failGranted:
		return "granted"
	case failLifecycle:
		return "lifecycle"
	}
	return "unset"
}

// napFailShape is how a route fails.
type napFailShape struct {
	kind napFailKind
	// fields are static fields every failure carries (the defaults the spec
	// wants beside an error, or instead of one); copied per failure
	fields map[string]any
	// codes maps a generic code to the spec's own for this type
	codes map[string]string
	// closed is the push a lifecycle route fails with
	closed string
}

func failShape(kind napFailKind) napFailShape { return napFailShape{kind: kind} }

func lifecycleShape(closed string) napFailShape {
	return napFailShape{kind: failLifecycle, closed: closed}
}

func (f napFailShape) withFields(fields map[string]any) napFailShape {
	f.fields = fields
	return f
}

func (f napFailShape) withCodes(codes map[string]string) napFailShape {
	f.codes = codes
	return f
}

// code maps a generic code through the route's spec codes.
func (f napFailShape) code(generic string) string {
	if c, ok := f.codes[generic]; ok {
		return c
	}
	return generic
}

// napRoute is one request type: its handler, its gate and its failure shape.
type napRoute struct {
	h    napHandler
	gate napGate
	fail napFailShape
}

// autoFails says whether a request the handler left unanswered is failed for
// it. Reply-less types never are, and neither are lifecycle ones: their
// subscription runs on after the handler returns and ends with its own push.
func (r *napRoute) autoFails() bool {
	return r.fail.kind != failNone && r.fail.kind != failLifecycle
}

func (r napRoute) String() string { return "gate=" + r.gate.String() + " fail=" + r.fail.kind.String() }

// the shared gates of the table below
var (
	napGateStorage    = openGate("own isolated storage; keying KEY-01..04 Phase 5")
	napGateConfigSub  = openGate("config subscription; MISC-02 Phase 8")
	napGateNotifyOwn  = openGate("own notifications; MISC-01 Phase 8")
	napGateNip19      = openGate("pure NIP-19 encoding")
	napGateRelayRead  = openGate("public relay reads; RELY-01..06 Phase 6")
	napGateOutboxRead = openGate("public outbox reads; RELY-03..05 Phase 6")
	napGateIdentity   = openGate("NAP-IDENTITY read; read consent MISC-03 Phase 8")
	napGateIntentFind = openGate("intent discovery; INTN-02 Phase 6")
	napGateIncOwn     = openGate("own INC endpoints; INTN-03 Phase 6")
	napGateUploadOwn  = openGate("upload capability and own status")
	napGateMediaOwn   = openGate("own media sessions")
	napGateFetch      = dynamicGate("data: and nostr: are local or relay reads; https: needs PermFetch; blossom consent RES-02 Phase 7", PermFetch)
)

// napRouteSpecs declares every NAP request type the launcher handles: 67
// shim request types plus media.command, which goes both ways. Handlers are
// nil here; handleNap fills them in. TestNapRouteTableGolden pins it.
var napRouteSpecs = map[string]napRoute{
	"theme.get": {gate: openGate("launcher theme, read-only (NAP-THEME)"), fail: failShape(failDefault).withFields(map[string]any{
		"theme": map[string]any{"colors": map[string]any{"background": "#ffffff", "text": "#111111", "primary": "#111111"}},
	})},

	"storage.get":    {gate: napGateStorage, fail: failShape(failErr)},
	"storage.set":    {gate: napGateStorage, fail: failShape(failErr).withCodes(map[string]string{napErrTooLarge: "quota exceeded"})},
	"storage.remove": {gate: napGateStorage, fail: failShape(failErr)},
	"storage.keys":   {gate: napGateStorage, fail: failShape(failErr)},

	"link.open": {gate: perCallGate(PermOpenLink), fail: failShape(failLink)},

	"config.registerSchema": {gate: openGate("schema declaration; reply shapes MISC-02 Phase 8"), fail: failShape(failOkFalseCode)},
	"config.get":            {gate: openGate("config read; reply shape MISC-02 Phase 8 (A22)"), fail: failShape(failSchemaError)},
	"config.subscribe":      {gate: napGateConfigSub, fail: failShape(failNone)},
	"config.unsubscribe":    {gate: napGateConfigSub, fail: failShape(failNone)},
	"config.openSettings":   {gate: openGate("opens the launcher settings window; focus rule MISC-02 Phase 8"), fail: failShape(failNone)},

	// notify.send only checks the session's grant; it never prompts
	"notify.send": {gate: sessionGate(PermNotify), fail: failShape(failErr).withCodes(map[string]string{
		napErrDenied: "permission denied", napErrRateLimited: "rate limited",
	})},
	"notify.permission.request": {gate: sessionGate(PermNotify), fail: failShape(failGranted)},
	"notify.dismiss":            {gate: napGateNotifyOwn, fail: failShape(failNone)},
	"notify.badge":              {gate: napGateNotifyOwn, fail: failShape(failNone)},
	"notify.channel.register":   {gate: napGateNotifyOwn, fail: failShape(failNone)},

	"common.encodeNip19": {gate: napGateNip19, fail: failShape(failOkFalse)},
	"common.decodeNip19": {gate: napGateNip19, fail: failShape(failOkFalse)},
	"common.getProfile":  {gate: openGate("public profile read"), fail: failShape(failOkFalse).withFields(map[string]any{"pubkey": ""})},
	"common.follows":     {gate: openGate("user's public follow list; consent MISC-03 Phase 8"), fail: failShape(failOkFalse).withFields(map[string]any{"pubkeys": []any{}})},
	"common.follow":      {gate: perCallGate(PermPublish), fail: failShape(failOkFalse)},
	"common.unfollow":    {gate: perCallGate(PermPublish), fail: failShape(failOkFalse)},
	"common.react":       {gate: perCallGate(PermPublish), fail: failShape(failOkFalse)},
	"common.report":      {gate: perCallGate(PermPublish), fail: failShape(failOkFalse)},

	// relay.closed reasons carry NIP-01's machine-readable prefixes
	"relay.subscribe": {gate: napGateRelayRead, fail: lifecycleShape("relay.closed").withCodes(map[string]string{
		napErrInternal:    "error: internal-error",
		napErrInvalid:     "invalid: invalid-request",
		napErrTooLarge:    "invalid: too-large",
		napErrRateLimited: "rate-limited: rate-limited",
		napErrDenied:      "blocked: user-denied",
	})},
	"relay.close":            {gate: openGate("own subscription; relay.closed push decided in RELY-06 Phase 6 (D-21)"), fail: failShape(failNone)},
	"relay.query":            {gate: napGateRelayRead, fail: failShape(failErr).withFields(map[string]any{"events": []any{}})},
	"relay.publish":          {gate: perCallGate(PermPublish), fail: failShape(failOkFalse)},
	"relay.publishEncrypted": {gate: perCallGate(PermPublish), fail: failShape(failOkFalse)},

	"outbox.getEvent":      {gate: napGateOutboxRead, fail: failShape(failErr)},
	"outbox.resolveRelays": {gate: napGateOutboxRead, fail: failShape(failErr)},
	"outbox.query":         {gate: napGateOutboxRead, fail: failShape(failErr).withFields(map[string]any{"events": []any{}})},
	"outbox.subscribe":     {gate: napGateOutboxRead, fail: lifecycleShape("outbox.closed")},
	"outbox.close":         {gate: napGateOutboxRead, fail: lifecycleShape("outbox.closed")},
	"outbox.publish":       {gate: perCallGate(PermPublish), fail: failShape(failOkFalse).withCodes(map[string]string{napErrDenied: "publish denied"})},

	// NAP-IDENTITY: getPublicKey MUST always succeed, so it fails with the
	// signed-out answer and no error
	"identity.getPublicKey": {gate: napGateIdentity, fail: failShape(failDefault).withFields(map[string]any{"pubkey": ""})},
	"identity.getRelays":    {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"relays": map[string]any{}})},
	"identity.getProfile":   {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"profile": nil})},
	"identity.getFollows":   {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"pubkeys": []any{}})},
	"identity.getMutes":     {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"pubkeys": []any{}})},
	"identity.getBlocked":   {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"pubkeys": []any{}})},
	"identity.getZaps":      {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"zaps": []any{}})},
	"identity.getBadges":    {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"badges": []any{}})},
	"identity.getList":      {gate: napGateIdentity, fail: failShape(failErr).withFields(map[string]any{"entries": []any{}})},

	"intent.invoke": {gate: openGate("PermDispatch routing; handler authorization INTN-01 Phase 6"), fail: failShape(failIntent).withCodes(map[string]string{
		napErrDenied: "user cancelled", napErrInternal: "invoke failed",
	})},
	"intent.available": {gate: napGateIntentFind, fail: failShape(failErr)},
	"intent.handlers":  {gate: napGateIntentFind, fail: failShape(failErr)},

	"inc.emit":              {gate: openGate("INC broadcast; consent INTN-03 Phase 6"), fail: failShape(failNone)},
	"inc.subscribe":         {gate: openGate("INC topic subscription; INTN-03 Phase 6"), fail: failShape(failErr)},
	"inc.unsubscribe":       {gate: napGateIncOwn, fail: failShape(failNone)},
	"inc.channel.emit":      {gate: napGateIncOwn, fail: failShape(failNone)},
	"inc.channel.broadcast": {gate: napGateIncOwn, fail: failShape(failNone)},
	"inc.channel.close":     {gate: napGateIncOwn, fail: failShape(failNone)},
	"inc.channel.open":      {gate: openGate("INC channel consent INTN-03 Phase 6"), fail: failShape(failErr)},
	// the shim drops an inc.channel.list answer without a channels list
	"inc.channel.list": {gate: openGate("own INC channels; INTN-03 Phase 6"), fail: failShape(failDefault).withFields(map[string]any{"channels": []any{}})},

	"upload.info":   {gate: napGateUploadOwn, fail: failShape(failErr)},
	"upload.status": {gate: napGateUploadOwn, fail: failShape(failErr)},
	"upload.upload": {gate: perCallGate(PermUpload), fail: failShape(failErr).withCodes(map[string]string{
		napErrDenied: "policy denied", napErrTooLarge: "file too large",
	})},

	"media.session.create": {
		gate: dynamicGate("napplet-owned sessions are bookkeeping; shell-owned playback needs PermMedia; MDIA-01..03 Phase 7", PermMedia),
		fail: failShape(failErr).withCodes(map[string]string{napErrDenied: "source blocked"}),
	},
	"media.session.update":  {gate: napGateMediaOwn, fail: failShape(failNone)},
	"media.session.destroy": {gate: napGateMediaOwn, fail: failShape(failNone)},
	"media.state":           {gate: napGateMediaOwn, fail: failShape(failNone)},
	"media.capabilities":    {gate: napGateMediaOwn, fail: failShape(failNone)},
	"media.command":         {gate: napGateMediaOwn, fail: failShape(failNone)},

	"resource.info": {gate: openGate("resource capability"), fail: failShape(failTypedErr).withCodes(map[string]string{napErrRateLimited: "quota-exceeded"})},
	"resource.bytes": {gate: napGateFetch, fail: failShape(failTypedErr).withCodes(map[string]string{
		napErrRateLimited: "quota-exceeded", napErrDenied: "blocked-by-policy",
	})},
	"resource.bytesMany": {gate: napGateFetch, fail: failShape(failTypedErr).withCodes(map[string]string{
		napErrRateLimited: "quota-exceeded", napErrDenied: "blocked-by-policy",
	})},
	// its id names the request it cancels, so it is never answered
	"resource.cancel": {gate: openGate("cancels the napplet's own request; its id names another request"), fail: failShape(failNone)},
}

// napRoutes are the registered routes by type: napRouteSpecs joined with the
// handlers each nap_*.go init registers.
var napRoutes = map[string]*napRoute{}

// handleNap registers handlers; each nap_*.go file does it in its init. A
// handler whose type has no valid declared route, or a type registered twice,
// panics: the launcher does not start with an undeclared NAP type.
func handleNap(types map[string]napHandler) {
	registerNapRoutes(napRoutes, napRouteSpecs, types)
}

// registerNapRoutes joins handlers with their specs into dst.
func registerNapRoutes(dst map[string]*napRoute, specs map[string]napRoute, types map[string]napHandler) {
	if len(types) == 0 {
		panic("handleNap called with no NAP handlers")
	}
	for _, typ := range slices.Sorted(maps.Keys(types)) {
		h := types[typ]
		if h == nil {
			panic("NAP handler " + typ + " is nil")
		}
		if _, dup := dst[typ]; dup {
			panic("duplicate NAP handler for " + typ)
		}
		spec, ok := specs[typ]
		if !ok {
			panic("NAP handler " + typ + " has no declared route")
		}
		if err := validateRoute(typ, spec); err != nil {
			panic(err.Error())
		}
		spec.h = h
		dst[typ] = &spec
	}
}

// validateRoute reports what is missing from a route's declaration.
func validateRoute(typ string, r napRoute) error {
	g := r.gate
	switch g.kind {
	case napGateOpen:
		if strings.TrimSpace(g.reason) == "" {
			return fmt.Errorf("NAP route %s: an open gate needs a reason", typ)
		}
	case napGateSession, napGatePerCall:
		if g.perm == "" {
			return fmt.Errorf("NAP route %s: a %s gate needs a permission", typ, g.kind)
		}
	case napGateDynamic:
		if strings.TrimSpace(g.reason) == "" {
			return fmt.Errorf("NAP route %s: a dynamic gate needs a reason", typ)
		}
		if len(g.perms) == 0 {
			return fmt.Errorf("NAP route %s: a dynamic gate needs the permissions it may ask for", typ)
		}
		if slices.Contains(g.perms, "") {
			return fmt.Errorf("NAP route %s: a dynamic gate has an empty permission", typ)
		}
	default:
		return fmt.Errorf("NAP route %s has no gate", typ)
	}
	switch r.fail.kind {
	case failUnset:
		return fmt.Errorf("NAP route %s has no failure shape", typ)
	case failLifecycle:
		if r.fail.closed == "" {
			return fmt.Errorf("NAP route %s: a lifecycle failure needs its closed type", typ)
		}
	default:
		if r.fail.kind > failLifecycle {
			return fmt.Errorf("NAP route %s has an unknown failure shape", typ)
		}
	}
	return nil
}

// failWith answers a request that failed with a generic code (napErr*), in
// its route's shape. Types that are never answered, and requests without an
// id where the answer needs one, get nothing.
func (c *napCall) failWith(code string) {
	r := c.route
	if r == nil {
		r = napRoutes[c.Type]
	}
	shape := napFailShape{kind: failErr}
	if r != nil {
		shape = r.fail
	}
	code = shape.code(code)

	fields := napCloneFields(shape.fields)
	switch shape.kind {
	case failNone, failUnset:
		return
	case failLifecycle:
		var head struct {
			SubID *string `json:"subId"`
		}
		if json.Unmarshal(c.raw, &head) != nil || head.SubID == nil {
			return
		}
		c.replyAs(shape.closed, map[string]any{"subId": *head.SubID, "reason": code})
		return
	}
	if len(c.ID) == 0 {
		return
	}
	switch shape.kind {
	case failErr:
		fields["error"] = code
		c.reply(fields)
	case failOkFalse:
		fields["ok"] = false
		fields["error"] = code
		c.reply(fields)
	case failOkFalseCode:
		fields["ok"] = false
		fields["code"] = code
		fields["error"] = code
		c.reply(fields)
	case failTypedErr:
		fields["error"] = code
		c.replyAs(c.Type+".error", fields)
	case failLink:
		fields["status"] = "denied"
		fields["error"] = code
		c.reply(fields)
	case failIntent:
		archetype, action := napIntentHead(c.raw)
		fields["result"] = map[string]any{
			"ok": false, "archetype": archetype, "action": action, "handled": false, "error": code,
		}
		c.reply(fields)
	case failDefault:
		c.reply(fields)
	case failSchemaError:
		fields["code"] = code
		fields["error"] = code
		c.replyAs("config.schemaError", fields)
	case failGranted:
		fields["granted"] = false
		c.replyAs("notify.permission.result", fields)
	}
}

// napIntentHead reads what an intent.invoke failure has to echo: the request's
// archetype when it is a string, and its action when it is a non-empty string
// (else "open", the default action).
func napIntentHead(raw json.RawMessage) (archetype, action string) {
	var env struct {
		Request map[string]json.RawMessage `json:"request"`
	}
	action = "open"
	if json.Unmarshal(raw, &env) != nil {
		return "", action
	}
	_ = json.Unmarshal(env.Request["archetype"], &archetype)
	var a string
	if json.Unmarshal(env.Request["action"], &a) == nil && a != "" {
		action = a
	}
	return archetype, action
}

// napCloneFields deep-copies a shape's static fields, so no failure shares a
// map or slice with the table (or with another failure).
func napCloneFields(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		out[k] = napCloneValue(v)
	}
	return out
}

func napCloneValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		return napCloneFields(v)
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = napCloneValue(e)
		}
		return out
	}
	return v
}

// napFailShapeTable is the route table's failure shapes in a JSON-ready form:
// {type: {kind, fields?, codes?, closed?}}. The host page keeps the same
// table for the requests it refuses itself, and a test holds the two equal.
func napFailShapeTable() map[string]any {
	out := make(map[string]any, len(napRoutes))
	for typ, r := range napRoutes {
		entry := map[string]any{"kind": r.fail.kind.String()}
		if len(r.fail.fields) > 0 {
			entry["fields"] = napCloneFields(r.fail.fields)
		}
		if len(r.fail.codes) > 0 {
			entry["codes"] = maps.Clone(r.fail.codes)
		}
		if r.fail.kind == failLifecycle {
			entry["closed"] = r.fail.closed
		}
		out[typ] = entry
	}
	return out
}
