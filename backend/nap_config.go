package backend

import (
	"encoding/json"

	"verdana/backend/napconfig"
)

// NAP-CONFIG: a napplet declares its settings as a JSON Schema, the launcher
// renders them in the napp's settings window, and the napplet reads what the
// user chose. The launcher is the only writer; nothing here takes a value
// from the napplet. Schema and values live in the napconfig package, keyed by
// nappletScope: the napplet's address and artifact hash, the same scope
// NAP-STORAGE keys by. An update is a new artifact hash and so a fresh scope,
// starting from the schema's defaults (NAP-CONFIG keys values on (dTag,
// aggregateHash)). A napplet without a valid hash has no scope and every
// config request fails internal-error; nothing falls back to the address.
//
// config.values payloads may carry secrets: they are never logged.

func init() {
	handleNap(map[string]napHandler{
		"config.registerSchema": napConfigRegisterSchema,
		"config.get":            napConfigGet,
		"config.subscribe":      napConfigSubscribe,
		"config.unsubscribe":    napConfigUnsubscribe,
		"config.openSettings":   napConfigOpenSettings,
	})
}

func configSchemaErrorEnv(code, msg string) map[string]any {
	return map[string]any{"type": "config.schemaError", "code": code, "error": msg}
}

// configScope is the NAP-CONFIG scope of the calling napplet. Without one
// the call fails internal-error in its route's shape: a napplet always has a
// hash once installed or tried, so a missing one is a launcher bug, not
// something to paper over with a shared key.
func (c *napCall) configScope() (string, bool) {
	scope, err := nappletScope(c.ci.napp)
	if err != nil {
		log.Error().Err(err).Str("napplet", c.ci.napp.ID).Str("type", c.Type).Msg("napplet config has no scope")
		c.failWith(napErrInternal)
		return "", false
	}
	return scope, true
}

func napConfigRegisterSchema(c *napCall) {
	var r struct {
		Schema  json.RawMessage `json:"schema"`
		Version *float64        `json:"version"`
	}
	if err := c.decode(&r); err != nil {
		c.reply(map[string]any{"ok": false, "code": napconfig.CodeInvalidSchema, "error": napErrInvalid})
		return
	}
	var version *uint64
	if r.Version != nil {
		if *r.Version < 0 || *r.Version != float64(uint64(*r.Version)) {
			c.reply(map[string]any{"ok": false, "code": napconfig.CodeInvalidSchema, "error": "version must be a non-negative integer"})
			return
		}
		v := uint64(*r.Version)
		version = &v
	}
	scope, ok := c.configScope()
	if !ok {
		return
	}
	napp := c.ci.napp
	changed, cerr := napconfig.Register(scope, r.Schema, version)
	if cerr != nil {
		log.Info().Str("napplet", napp.ID).Str("code", cerr.Code).Str("error", cerr.Msg).Msg("napplet config schema rejected")
		c.reply(map[string]any{"ok": false, "code": cerr.Code, "error": cerr.Msg})
		// onSchemaError listeners hear about it too: the result is what
		// the registerSchema promise settles on
		c.ci.napPushGen(c.gen, configSchemaErrorEnv(cerr.Code, cerr.Msg))
		return
	}
	c.reply(map[string]any{"ok": true})
	if changed {
		// subscribers see the values the new schema resolves to; this
		// window's own included, after the result its registerSchema
		// was waiting for
		pushConfigValues(scope)
		settingsChanged(scope)
	}
}

func napConfigGet(c *napCall) {
	scope, ok := c.configScope()
	if !ok {
		return
	}
	values, ok := napconfig.Values(scope)
	if !ok {
		// Go still sends the request's id, but the pristine shim routes
		// config.schemaError only to onSchemaError, so a get made before
		// any schema settles only by the shim's request timeout. NAP-CONFIG
		// gives schemaError no id, so the spec has no way to settle it
		// either; spec/CONFORMANCE.md P7/A22, decided in Phase 8 (MISC-02).
		c.replyAs("config.schemaError", map[string]any{"code": napconfig.CodeNoSchema, "error": "no schema has been registered"})
		return
	}
	c.replyAs("config.values", map[string]any{"values": values})
}

func napConfigSubscribe(c *napCall) {
	scope, ok := c.configScope()
	if !ok {
		return
	}
	s := c.ci.nap
	s.mu.Lock()
	s.configSubscribed = true
	s.mu.Unlock()
	values, ok := napconfig.Values(scope)
	if !ok {
		// the subscription stands: the first values arrive once a schema
		// is registered
		c.ci.napPushGen(c.gen, configSchemaErrorEnv(napconfig.CodeNoSchema, "no schema has been registered"))
		return
	}
	c.ci.napPushGen(c.gen, map[string]any{"type": "config.values", "values": values})
}

func napConfigUnsubscribe(c *napCall) {
	s := c.ci.nap
	s.mu.Lock()
	s.configSubscribed = false
	s.mu.Unlock()
}

func napConfigOpenSettings(c *napCall) {
	var r struct {
		Section string `json:"section"`
	}
	_ = c.decode(&r)
	// one settings window per 2 s per window (nap_limits.go's
	// limitOpenSettings), across reloads too, so a napplet cannot keep
	// throwing its settings window in the user's face. An extra call is
	// ignored silently: the type is reply-less.
	if !c.ci.nap.limits.allow(limitOpenSettings, 1) {
		return
	}

	section := r.Section
	if section != "" {
		// an undeclared section is ignored silently: the window opens
		// at the top, and the napplet learns nothing either way
		scope, err := nappletScope(c.ci.napp)
		if err != nil {
			section = ""
		} else if sch, _ := napconfig.Snapshot(scope); sch == nil || !sch.Sections[section] {
			section = ""
		}
	}
	napp := c.ci.napp
	// config.openSettings is reply-less: a panic here has nobody to answer.
	// The window opens on this napplet's own scope, whatever version is
	// installed.
	safeGo(nil, "open settings", func() {
		if err := openSettingsFor(napp, section); err != nil {
			log.Warn().Err(err).Str("napplet", napp.ID).Msg("could not open napplet settings")
		}
	})
}

// pushConfigValues gives every subscribed window of a scope its values.
// Windows are matched by scope, not napp id: an old-version window and a
// new-version window share an id, and neither may hear the other's values.
func pushConfigValues(scope string) {
	values, ok := napconfig.Values(scope)
	if !ok {
		return
	}
	env := map[string]any{"type": "config.values", "values": values}
	for _, ci := range allInstances() {
		if ci.nap == nil || !ci.napp.IsNapplet() {
			continue
		}
		if s, err := nappletScope(ci.napp); err != nil || s != scope {
			continue
		}
		ci.nap.mu.Lock()
		sub := ci.nap.configSubscribed
		ci.nap.mu.Unlock()
		if sub {
			ci.napPush(env)
		}
	}
}
