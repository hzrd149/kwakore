# Phase 2: Gated NAP Dispatcher - Pattern Map

**Mapped:** 2026-10-03
**Files analyzed:** 22 (new + modified)
**Analogs found:** 21 / 22

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/nap_route.go` (new) | registry/config | request-response (dispatch) | `backend/nap.go:197-214` (`napHandlers`, `handleNap`) | exact (replaces it) |
| `backend/nap_sink.go` (new) | service (gated sinks) | request-response | `backend/nap.go:636-657` (`sessionGrant`), `window_prompt.go:233-265` (`askApproval`) | role-match |
| `backend/nap_limits.go` (new) | config/constants + limiter | event-driven (rate) | `backend/nap_notify.go:97-106`, `nap_config.go:20,118-122` | role-match |
| `backend/nap_envelope.go` (optional new) | utility | transform | `backend/nap.go:356-389` (`napEnqueue` head parse) | exact |
| `backend/nap.go` (mod) | dispatcher | request-response | itself (`napEnqueue`/`napWorker`/`napDispatch`/`fail`/`async`) | self |
| `backend/nap_*.go` (all 12 domain files, mod) | handlers | request-response | `backend/nap_basic.go:16-27` init registration | self |
| `backend/window_prompt.go` (mod) | service (prompt queue) | event-driven | itself (`askApproval`, `enqueuePrompt`, `promptTimeout`) | self |
| `backend/window_instances.go` (mod: `HandleWireMessage` cap, Instance ctx) | transport entry | request-response | itself :307-314 | self |
| `backend/cache.go` (mod) | utility | — | itself (`mustNewCache`) | self |
| `backend/bridge_lists.go:710`, `registry_updates.go:231` (mod) | cache callers | — | — | self |
| `backend/webview/napplet-host.js` (mod) | host page shim | request-response | itself :219-243 `refuse()` | self |
| `desktop/internal/wireline/wireline.go` (new) | utility (bounded reader) | streaming / file-I/O | `desktop/internal/instancelock/` (package layout + `doc.go`) | partial (layout only) |
| `desktop/childproc.go` (mod) | transport | streaming | itself :128-150 `readChild` | self |
| `desktop/child/main.go` (mod) | transport | streaming | itself :277-282 `reader` | self |
| `backend/go.mod`, `desktop/go.mod` (mod) | config | — | existing require blocks | self |
| `backend/nap_route_test.go` (new) | test (golden, deny short-circuit) | — | `backend/nap_test.go:335-353` + `nap_conformance_test.go:138` | role-match |
| `backend/nap_guard_test.go` (new) | test (AST) | — | none in repo | no analog |
| `backend/nap_limits_test.go` (new) | test | — | `backend/nap_notify_test.go` (rate-limited asserts) | role-match |
| `backend/nap_failshape_test.go` (new) | test (Go vs JS table) | file-I/O | `backend/webview/shim_test.go:15` (reads embedded asset) | partial |
| `backend/webview/napplet_host_test.go` (mod) | test (node-backed) | — | itself :314 `TestNappletHostBoundsPendingEnvelopes` | exact |
| `desktop/internal/wireline/wireline_test.go` (new) | test | — | `desktop/internal/instancelock/lock_unix_test.go` | role-match |
| `backend/nap_conformance_test.go` (mod) | test | — | itself :138 `conformanceProblems(..., handlers map[string]napHandler, ...)` -> read `napRoutes` | self |

## Pattern Assignments

### `backend/nap_route.go` (registry, request-response)

**Analog:** `backend/nap.go:197-214` — copy the doc comment about sync handlers holding `dispatchMu.RLock` and the duplicate-panic registration:
```go
type napHandler func(c *napCall)

var napHandlers = map[string]napHandler{}

// handleNap registers handlers; each nap_*.go file does it in its init.
func handleNap(types map[string]napHandler) {
	for t, h := range types {
		if _, dup := napHandlers[t]; dup {
			panic("duplicate NAP handler for " + t)
		}
		napHandlers[t] = h
	}
}
```
New: `handleNap(map[string]napRoute{...})`; add panics for missing gate and empty Open/Dynamic reason in the same loop. Keep the per-file `init()` style (`nap_basic.go:16-27`):
```go
func init() {
	handleNap(map[string]napHandler{
		"theme.get": napThemeGet,
		"storage.get":    napStorageGet,
		...
		"link.open": napLinkOpen,
	})
}
```
Fail-shape table replaces the `fail()` switch at `nap.go:245-259` (the shapes to preserve or fix: `config.get` -> `config.schemaError{code}`, `notify.permission.request` -> `notify.permission.result{granted:false}`, `resource.bytes(Many)` -> `.error`; `relay.publish` moves from `.error` to `.result` per R-2).

---

### `backend/nap.go` (dispatcher) — changes in place

- `napEnqueue` (:356-389): head parse to replace with case-sensitive scan + `foldKey` collision check + id check + size caps; `s.queue = make(chan napCall, 256)` -> `chan *napCall`; blocking send below becomes non-blocking with `rate-limited`:
```go
	select {
	case s.queue <- call:
	case <-ci.gone:
	}
```
- `napDispatch(c napCall)` (:407-439) -> `napDispatch(c *napCall)`; insert gate + deny short-circuit (D-04) between the `napHandlers[c.Type]` lookup and the `beforeHandler` hook; keep the recover block:
```go
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Str("type", c.Type).Msg("NAP handler panicked")
			c.fail()
		}
	}()
```
- `c.async` (:263-273) is the template for `safeGo` (D-08) and for the post-handler auto-fail check of the `answered` flag (D-06):
```go
func (c *napCall) async(fn func(ctx context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("type", c.Type).Msg("NAP handler panicked")
				c.fail()
			}
		}()
		fn(c.ctx)
	}()
}
```
- `c.reply`/`c.replyAs` (:232-240) are where the `answered` CompareAndSwap goes; second reply -> `log.Warn()`.
- `sessionGrant` (:636-657) moves to `nap_sink.go`, takes ctx, stops recording `false` on dismissal (Research Pattern 9). Test hooks (`beforeHandler`, `pumpHook` :117-126) are the precedent for per-sink atomic counters.

---

### `backend/nap_sink.go` (gated sinks)

**Analog:** `sessionGrant` (above) + `askApproval` (`window_prompt.go:233-265`): rule lookup first, then prompt, log with structured fields:
```go
	key := RuleKey{Napp: nappID, Permission: perm}
	if rule, ok := lookupRule(key); ok {
		log.Info().Str("napp", name).Str("ask", title).
			Str("rule", string(rule.Decision)).Msg("approval answered by the rules")
		return rule.Decision.granted()
	}
```
Call sites to move behind wrappers (from RESEARCH Pattern 2): `nap_basic.go:241,245`; `nap_relay.go:527,539,541,548,554`; `nap_upload.go:79-127,227`; `nap_notify.go:64,66,117`; `nap_resource.go:286,292`; `nap_media.go:169,176`. Move `napUploadAuth`/`napUploadToServer` var decls here.

---

### `backend/nap_limits.go` (constants + limiter)

**Analog:** existing ad-hoc limit in `nap_notify.go:97-106` (to be folded; note spec code `"rate limited"` with a space is kept for notify):
```go
	now := time.Now()
	s.notifyTimes = recentNotifications(s.notifyTimes, now)
	s.urgentNotifyTimes = recentNotifications(s.urgentNotifyTimes, now)
	if len(s.notifyTimes) >= 20 || (req.Priority == "urgent" && len(s.urgentNotifyTimes) >= 3) {
		s.mu.Unlock()
		c.reply(map[string]any{"error": "rate limited"})
		return
	}
```
Other constants to gather: `napMaxSubs` (nap_relay.go:41), `configOpenSettingsEvery` (nap_config.go:20), `nappletStorageQuota` (nap_basic.go:74), `napUploadMaxBytes` (nap_upload.go:27), `mediaMaxSessions` (nap_media.go:37), `nip19MaxLen` (nap_common.go:547), `resourceMaxBytes/URLs` (nap_resource.go:41-42). Limiters live on `napSession` but must NOT be reset in `resetLocked` (precedent: `configOpenedAt`, nap.go:107-111).

---

### `backend/window_prompt.go` (prompt queue)

Self-modify `askApproval` signature to take ctx; `enqueuePrompt(p)` returns bool for the per-window 3 / global 32 cap; `p.wait()` selects on ctx. Launcher prompts (`Instance == ""`, registry_install.go:193, registry_address.go:352) exempt. Bridge callers needing ctx: bridge.go:115,152,375,660,676,843; `askActionHandler` window_instances.go:897.

---

### `backend/cache.go`

Current (convert to `newCache(...) (*ristretto.Cache[K,V], error)`; assign in `Start`, nil on error — ristretto methods are nil-safe):
```go
func mustNewCache[K ristretto.Key, V any](size int) *ristretto.Cache[K, V] {
	c, err := ristretto.NewCache(&ristretto.Config[K, V]{
		NumCounters: int64(size * 10),
		MaxCost:     int64(size),
		BufferItems: 64,
	})
	if err != nil {
		panic(err)
	}
	return c
}
```

---

### `backend/webview/napplet-host.js` `refuse()` (:219-243)

Current switch to replace with a strict-JSON `FAIL_SHAPES` block between comment markers (Go test reads it). Keep style: no semicolons, two-space indent, inside the IIFE.
```js
  const refuse = (data, err) => {
    if (typeof data.id !== "string" && typeof data.id !== "number") return
    const error = (err && err.message) || "request failed"
    const reply = { id: data.id }
    switch (data.type) {
      case "config.get": ...
      case "notify.permission.request": ...
      case "resource.bytes":
      case "resource.bytesMany":
      case "relay.publish":
        Object.assign(reply, { type: data.type + ".error", ok: false, error })
        break
      default:
        Object.assign(reply, { type: data.type + ".result", ok: false, error })
    }
    deliver(reply)
  }
```

---

### `desktop/childproc.go` `readChild` (:128-150) and `desktop/child/main.go` `reader` (:277-282)

Both use unbounded `json.NewDecoder`; swap for `wireline` reader (24 MiB child->parent; 128 MiB parent->child per D-17). Overlong in `readChild` -> `ct.cmd.Process.Kill()` + `log.Error()`, then fall through to the existing `WindowClosed`/`ct.cmd.Wait()` tail:
```go
func readChild(ct *childTransport, stdout io.ReadCloser) {
	dec := json.NewDecoder(stdout)
	for {
		var m backend.WireMsg
		if err := dec.Decode(&m); err != nil {
			log.Debug().Str("instance", ct.instance).Err(err).Msg("child stdout ended")
			break
		}
		...
	}
	if ct.settings { backend.SettingsClosed(ct.instance) } else { backend.WindowClosed(ct.instance) }
	ct.cmd.Wait()
```

### `desktop/internal/wireline/` (new package)

Layout analog: `desktop/internal/instancelock/` (`doc.go` + impl + `_test.go`). No existing bounded reader; use `bufio.Reader.ReadSlice`/`Scanner.Buffer(max+1)` per RESEARCH Pattern 5 / Pitfall 5.

---

### Tests

**Rig** (`backend/nap_test.go`): `setupNapTest(t)`, `openNapplet(t, d)` (:144-163), `post(t, ci, env)` (:166-176), `ready(t, ci, rec, n)` (:182-187), `rec.wait(t, type, n)`. Test-route registration pattern to adapt to `napRoutes` (:335-353):
```go
func TestNapPanickingHandlerStillReplies(t *testing.T) {
	setupNapTest(t)
	napHandlers["test.boom"] = func(*napCall) { panic("boom") }
	t.Cleanup(func() { delete(napHandlers, "test.boom") })
	ci, rec := openNapplet(t, "boom")
	ready(t, ci, rec, 1)
	post(t, ci, map[string]any{"type": "test.boom", "id": "b1"})
	if got := rec.wait(t, "test.boom.result", 1); got["id"] != "b1" || got["error"] == nil {
		t.Fatalf("sync panic: %v", got)
	}
}
```
- `nap_conformance_test.go:138` `conformanceProblems(fx, handlers map[string]napHandler, ...)` -> pass route map.
- Node-backed host-page tests: `backend/webview/napplet_host_test.go` (node via `exec.Command(node, "-")` :123, `VERDANA_REQUIRE_NODE` :20-27, model on `TestNappletHostBoundsPendingEnvelopes` :314).
- Existing asserts that change: `nap_identity_test.go:140` `"internal error"` -> `internal-error`; `nap_notify_test.go:109,206` keep spec codes.

## Shared Patterns

### Async / panic recovery
**Source:** `backend/nap.go:263-273` (`c.async`). Apply to: all 5 bare `go` sites (nap_config.go:134, nap_relay.go:261, nap_resource.go:222, nap_inc.go:296, nap_identity.go:431) via `safeGo`.

### Error replies
Machine codes, lowercase; spec codes verbatim (see RESEARCH Pitfall 9); generic hyphenated set `internal-error`, `user-denied`, `rate-limited`, `too-large`, `invalid-request`. Reply via `c.reply`/`c.replyAs` only.

### Logging
`log.Warn().Str(...).Msg("lowercase no punctuation")`; use zerolog `BurstSampler` for flood-path drop logs (Pitfall 8). Tests use `zerolog.Nop()`.

### Locking
Lock order per nap.go:52-63; never hold `dispatchMu`/`s.mu` while prompting; prompts only inside `c.async`.

## No Analog Found

| File | Role | Reason |
|---|---|---|
| `backend/nap_guard_test.go` | AST test | No go/ast tests exist; use RESEARCH Pattern 6 sketch |
| `desktop/internal/wireline/wireline.go` (logic) | bounded reader | No bounded line reader exists; RESEARCH Pattern 5 |
| `foldKey` | utility | Copy stdlib `encoding/json/fold.go` (RESEARCH Code Examples) |

## Metadata

**Analog search scope:** `backend/nap*.go`, `backend/window_prompt.go`, `backend/cache.go`, `backend/webview/`, `desktop/childproc.go`, `desktop/child/main.go`, `desktop/internal/`
**Files scanned:** ~20
**Pattern extraction date:** 2026-10-03
