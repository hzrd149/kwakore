;(() => {
  // The napplet host page: the launcher-owned main frame of a napplet's
  // window. The napplet itself runs below it in a sandboxed srcdoc iframe
  // (allow-scripts only, so an opaque origin, and the NIP-5D CSP inside), and
  // this page is the only thing it can talk to. Its whole job is to carry NAP
  // envelopes between that iframe and the Go host:
  //
  //   napplet --postMessage--> here --rpc("nap.msg")--> Go
  //   napplet <--postMessage-- here <--rpc result / __nap_push(...)-- Go
  //
  // Go owns NAP routing and subscriptions. Immediate gamepad
  // focus-loss neutralization runs here. Nothing here is reachable from the
  // napplet: a sandboxed frame without allow-same-origin cannot touch this
  // window's globals, only post messages to it.

  // init scripts may be injected into child frames too (WebView2 does that);
  // none of this belongs anywhere but the top frame
  if (window !== window.top) return

  // ── talking to the host ─────────────────────────────────────────
  // Desktop: __kwakoreNappletRPC, a wrapper the child process defines in the
  // top frame that adds this window's secret token to every call. Android: the
  // __kwakoreHost web message channel, which only this page's origin gets.
  const rpc = (() => {
    const decode = value => {
      const val = typeof value === "string" ? JSON.parse(value) : value
      if (val && val.__bridge_error) throw new Error(val.__bridge_error)
      return val
    }
    const encode = params => (params !== undefined ? JSON.stringify(params) : "null")

    if (typeof window.__kwakoreNappletRPC === "function") {
      const bound = window.__kwakoreNappletRPC
      return (method, params) => bound(method, encode(params)).then(decode)
    }

    const port = window.__kwakoreHost
    if (!port) return () => Promise.reject(new Error("no napplet host to talk to"))

    const pending = new Map()
    let rpcSerial = 0
    port.onmessage = event => {
      let msg
      try {
        msg = JSON.parse(typeof event.data === "string" ? event.data : "")
      } catch {
        return
      }
      const waiter = msg && pending.get(msg.id)
      if (!waiter) return
      pending.delete(msg.id)
      if (msg.error) {
        // Go answers an rpc it refused for its size with the bare NAP code
        // (HandleWireMessage), so the request fails as too-large
        const err = new Error(msg.error)
        if (msg.error === "too-large") err.napCode = "too-large"
        waiter.reject(err)
        return
      }
      try {
        waiter.resolve(msg.result === undefined ? null : decode(msg.result))
      } catch (err) {
        waiter.reject(err)
      }
    }
    return (method, params) =>
      new Promise((resolve, reject) => {
        const id = ++rpcSerial
        pending.set(id, { resolve, reject })
        port.postMessage(JSON.stringify({ t: "rpc", id, method, params: encode(params) }))
      })
  })()

  // Ordinary NAP envelopes stay small. NAP-UPLOAD may carry up to 16 MiB of
  // raw bytes; base64 plus its JSON envelope needs a larger transport bound.
  // Both count UTF-8 bytes of the envelope's JSON, as Go's route caps do.
  // MAX_WIRE_BYTES bounds what any envelope takes on its way to Go
  // (wireBytes): Go's line cap (MaxInboundWireMsg, 25 MiB) less room for the
  // rpc's own wrapping, so an envelope this page accepts never closes the
  // window for its size.
  const MAX_ENVELOPE = 1024 * 1024
  const MAX_UPLOAD_BYTES = 16 * 1024 * 1024
  const MAX_UPLOAD_ENVELOPE = 24 * 1024 * 1024
  const MAX_WIRE_BYTES = 24 * 1024 * 1024

  // napError is a failure the host page answers itself, tagged with the
  // generic code (D-07) Go would have failed the request with
  const napError = (message, napCode) => Object.assign(new Error(message), { napCode })

  const bytesToBase64 = bytes => {
    let out = ""
    const chunk = 0x8000
    for (let i = 0; i < bytes.length; i += chunk) {
      out += String.fromCharCode(...bytes.subarray(i, i + chunk))
    }
    return btoa(out)
  }

  // postMessage gives this launcher-owned page real Blob/ArrayBuffer values,
  // but the native RPC carrier is JSON. Encode them here so napplets still use
  // NAP-UPLOAD's structured-clone API and never have to base64 their own data.
  const dehydrate = async (value, seen = new WeakSet()) => {
    if (value instanceof Blob) {
      if (value.size > MAX_UPLOAD_BYTES) throw napError("upload is too large", "too-large")
      const bytes = new Uint8Array(await value.arrayBuffer())
      return { __blob: { b64: bytesToBase64(bytes), mime: value.type || "" } }
    }
    if (value instanceof ArrayBuffer) {
      if (value.byteLength > MAX_UPLOAD_BYTES) throw napError("upload is too large", "too-large")
      return { __blob: { b64: bytesToBase64(new Uint8Array(value)), mime: "" } }
    }
    if (ArrayBuffer.isView(value)) {
      if (value.byteLength > MAX_UPLOAD_BYTES) throw napError("upload is too large", "too-large")
      const bytes = new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
      return { __blob: { b64: bytesToBase64(bytes), mime: "" } }
    }
    if (Array.isArray(value)) {
      if (seen.has(value)) throw napError("cyclic NAP envelope", "invalid-request")
      seen.add(value)
      const out = []
      for (const item of value) out.push(await dehydrate(item, seen))
      seen.delete(value)
      return out
    }
    if (value && typeof value === "object") {
      if (seen.has(value)) throw napError("cyclic NAP envelope", "invalid-request")
      seen.add(value)
      const out = {}
      for (const key of Object.keys(value)) out[key] = await dehydrate(value[key], seen)
      seen.delete(value)
      return out
    }
    return value
  }

  let frame = null
  // session is the gen nap.start answered for the current frame. Go tags
  // every push with the session it was made for, and checks that session
  // before it sends, but a push that passed the check can still land after
  // nap.start answered for the next one: only pushes for this session reach
  // the frame.
  let session = null

  // ── Go -> napplet ───────────────────────────────────────────────
  // Envelopes come back as plain JSON. A resource result carries its bytes as
  // {__blob:{b64, mime}}; the napplet expects a real Blob, so it is rebuilt
  // here, the one place that can make one.
  const revive = value => {
    if (Array.isArray(value)) return value.map(revive)
    if (value && typeof value === "object") {
      const b = value.__blob
      if (b && typeof b.b64 === "string") {
        const bin = atob(b.b64)
        const bytes = new Uint8Array(bin.length)
        for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
        return new Blob([bytes], { type: typeof b.mime === "string" ? b.mime : "" })
      }
      const out = {}
      for (const k of Object.keys(value)) out[k] = revive(value[k])
      return out
    }
    return value
  }

  const deliver = envelopes => {
    if (!frame || !frame.contentWindow || envelopes == null) return
    const list = Array.isArray(envelopes) ? envelopes : [envelopes]
    for (const env of list) {
      if (!env || typeof env.type !== "string") continue
      if (env.type === "__kwakore.gamepad") {
        gamepadControl(env)
        continue
      }
      if (env.type === "gamepad.state") {
        if (!gamepadSubscribed) continue
        gamepadState = env
        // A daemon push may already be in flight when this window loses
        // focus. Never let it restore live values in an inactive frame.
        gamepadDeliver(env)
        continue
      }
      // the napplet's origin is opaque, so "*" is the only target that reaches
      // it; the message is addressed by contentWindow, not by origin
      frame.contentWindow.postMessage(revive(env), "*")
    }
  }

  // unsolicited messages: relay events, inc events, theme/identity changes,
  // and the answers to the napplet's requests
  window.__nap_push = (gen, json) => {
    if (session === null || gen !== session) return
    try {
      deliver(typeof json === "string" ? JSON.parse(json) : json)
    } catch (err) {
      console.error("[napplet-host] bad push", err)
    }
  }

  // ── trusted controller focus ───────────────────────────────────
  // The daemon reads devices for all windows. Host pages report focus and
  // mask incoming snapshots immediately on blur. Focus updates bypass the
  // napplet's bounded outbound lane so floods cannot delay neutralization.
  let gamepadSubscribed = false
  let gamepadFocusTimer = null
  let gamepadFocus = false
  let gamepadFocusSeq = 0
  let gamepadState = null
  let gamepadLastView = null

  const gamepadFocused = () => !!frame &&
    document.visibilityState === "visible" && document.hasFocus() &&
    document.activeElement === frame && !document.getElementById("__kwakore_prompt")

  const neutralGamepadState = state => ({
    ...state, focused: false,
    pads: state.pads.map(pad => pad && ({
      ...pad, timestamp: 0, axes: pad.axes.map(() => 0),
      buttons: pad.buttons.map(() => ({ value: 0, pressed: false, touched: false })),
    })),
  })

  const gamepadDeliver = state => {
    if (!frame) return
    const view = gamepadFocused() ? state : neutralGamepadState(state)
    const encoded = JSON.stringify(view)
    // Late live pushes must not expose input timing through repeated neutral
    // onChange callbacks while a blur RPC is still reaching the daemon.
    if (encoded === gamepadLastView) return
    gamepadLastView = encoded
    frame.contentWindow.postMessage(view, "*")
  }

  const gamepadCheckFocus = (force = false) => {
    if (!gamepadSubscribed || session === null) return
    const focused = gamepadFocused()
    if (!force && focused === gamepadFocus) return
    gamepadFocus = focused
    // Clear locally before any RPC. This also covers prompt overlays and
    // hidden windows whose animation frames are suspended by the engine.
    if (!focused && gamepadState && frame) {
      gamepadDeliver(neutralGamepadState(gamepadState))
    }
    rpc("nap.gamepad", { gen: session, focused, focusSeq: ++gamepadFocusSeq }).catch(() => {})
  }

  const gamepadStop = () => {
    gamepadSubscribed = false
    clearInterval(gamepadFocusTimer)
    gamepadFocusTimer = null
    gamepadState = null
    gamepadLastView = null
    gamepadFocus = false
  }

  const gamepadControl = env => {
    if (!env.subscribed) { gamepadStop(); return }
    const first = !gamepadSubscribed
    gamepadSubscribed = true
    gamepadLastView = null // each accepted subscribe receives its snapshot
    if (first) {
      gamepadCheckFocus(true)
      gamepadFocusTimer = setInterval(gamepadCheckFocus, 50)
    }
  }

  window.addEventListener("blur", () => gamepadCheckFocus())
  window.addEventListener("focus", () => gamepadCheckFocus())
  window.addEventListener("focusin", () => gamepadCheckFocus())
  window.addEventListener("focusout", () => gamepadCheckFocus())
  document.addEventListener("visibilitychange", () => gamepadCheckFocus())
  // The trusted native prompt renderer calls this after changing its overlay.
  window.__nap_gamepad_focus_changed = () => gamepadCheckFocus()

  // ── napplet -> Go ───────────────────────────────────────────────
  // One ordered lane to Go, bounded so a napplet cannot queue without limit.
  // MAX_PENDING matches the 256-slot per-session queue in Go's napEnqueue;
  // past it every envelope that carries an id is refused at once. The bound
  // is the napplet's: this page's own lifecycle calls (nap.start, nap.loaded,
  // nap.reset) are trusted, keep their place in the lane and never count
  // against it, so a flooding napplet cannot make a boot fail, lose its
  // controls push or keep its replaced session alive.
  const MAX_PENDING = 256
  let outbound = Promise.resolve()
  let pending = 0
  const enqueue = (task, trusted = false) => {
    if (!trusted) {
      if (pending >= MAX_PENDING) return Promise.reject(napError("too many pending NAP envelopes", "rate-limited"))
      pending++
    }
    const run = outbound.then(task)
    outbound = run
      .catch(() => {})
      .finally(() => {
        if (!trusted) pending--
      })
    return run
  }

  // The document-start marker (D-18): the launcher's preamble in every
  // srcdoc (webview.NappletSrcdoc, DocumentMarker in Go, the same literal)
  // posts it once, after window.napplet is installed and before any napplet
  // script runs. Same-source messages keep their order, so a reloaded
  // document's marker arrives ahead of all of its envelopes, even when that
  // document holds back its load event. markers counts them for the current
  // frame (boot() resets it), apart from its loads: the first marker may come
  // after the first load. The second one, like a second load, means the
  // document was replaced, and whichever comes first rebuilds; replaced()
  // ignores a frame that is no longer current, so the other changes nothing.
  // A marker is consumed here, never forwarded to Go or answered; a napplet
  // that forges one only gets itself rebuilt, under the reload cap.
  const DOCUMENT_MARKER = "__kwakore.document"
  let markers = 0
  let gamepadPolicyChecked = false

  window.addEventListener("message", event => {
    // sender binding: only this window's own napplet frame, never anyone else
    if (!frame || event.source !== frame.contentWindow) return
    const data = event.data
    if (!data || typeof data !== "object" || typeof data.type !== "string") return
    if (data.type === DOCUMENT_MARKER) {
      if (++markers >= 2) replaced(frame)
      return
    }
    if (data.type === "__kwakore.gamepad.policy") {
      if (gamepadPolicyChecked) return
      gamepadPolicyChecked = true
      if (data.denied !== true) {
        // The first report precedes all untrusted scripts in this srcdoc.
        // Refuse this engine rather than advertise controller isolation it
        // does not enforce. Later forged reports cannot reopen the frame.
        const old = frame
        gamepadStop()
        frame = null
        session = null
        ++bootSerial
        old.remove()
        enqueue(() => rpc("nap.reset"), true).catch(() => {})
        showBootError(new Error("This web engine cannot isolate controller input"))
      }
      return
    }
    // the frame this envelope came from: Go's reply or a refusal is its
    // answer and goes to it only, never to a document that replaced it in
    // the meantime
    const from = frame
    // one at a time: the desktop binding runs every call on its own thread,
    // so two calls in flight can reach Go in either order, and NAP needs the
    // order kept: nap.start goes ahead of a new document's first envelope,
    // and a subscribe goes ahead of its close. Go only queues the envelope
    // before answering, so the wait is short.
    enqueue(async () => {
      const encoded = await dehydrate(data)
      let json
      try {
        json = JSON.stringify(encoded)
      } catch (err) {
        throw napError((err && err.message) || "unencodable NAP envelope", "invalid-request")
      }
      if (typeof json !== "string") throw napError("unencodable NAP envelope", "invalid-request")
      const limit = data.type === "upload.upload" ? MAX_UPLOAD_ENVELOPE : MAX_ENVELOPE
      if (utf8Length(json) > limit || wireBytes(json) > MAX_WIRE_BYTES) {
        throw napError("NAP envelope is too large", "too-large")
      }
      return rpc("nap.msg", json)
    }).then(envelopes => {
      if (frame === from) deliver(envelopes)
    }, err => {
      console.error("[napplet-host]", err)
      if (frame === from) refuse(data, err)
    })
  })

  // ── failure shapes ──────────────────────────────────────────────
  // One entry per NAP request type, mirroring Go's route table
  // (backend/nap_route.go, exported by napFailShapeTable): the envelope a
  // request of that type fails with. Go's table is the source of truth; this
  // copy exists only because this page has no build step, and
  // TestHostFailShapesMatchGoRoutes keeps the two equal. Strict JSON between
  // the markers: double quotes, no trailing commas, no comments inside.
  const FAIL_SHAPES = /* nap-fail-shapes:begin */ {
    "common.decodeNip19": { "kind": "okFalse" },
    "common.encodeNip19": { "kind": "okFalse" },
    "common.follow": { "kind": "okFalse" },
    "common.follows": { "fields": { "pubkeys": [] }, "kind": "okFalse" },
    "common.getProfile": { "fields": { "pubkey": "" }, "kind": "okFalse" },
    "common.react": { "kind": "okFalse" },
    "common.report": { "kind": "okFalse" },
    "common.unfollow": { "kind": "okFalse" },
    "config.get": { "kind": "schemaError" },
    "config.openSettings": { "kind": "none" },
    "config.registerSchema": { "kind": "okFalseCode" },
    "config.subscribe": { "kind": "none" },
    "config.unsubscribe": { "kind": "none" },
    "gamepad.subscribe": { "kind": "none" },
    "gamepad.unsubscribe": { "kind": "none" },
    "identity.getBadges": { "fields": { "badges": [] }, "kind": "err" },
    "identity.getBlocked": { "fields": { "pubkeys": [] }, "kind": "err" },
    "identity.getFollows": { "fields": { "pubkeys": [] }, "kind": "err" },
    "identity.getList": { "fields": { "entries": [] }, "kind": "err" },
    "identity.getMutes": { "fields": { "pubkeys": [] }, "kind": "err" },
    "identity.getProfile": { "fields": { "profile": null }, "kind": "err" },
    "identity.getPublicKey": { "fields": { "pubkey": "" }, "kind": "default" },
    "identity.getRelays": { "fields": { "relays": {} }, "kind": "err" },
    "identity.getZaps": { "fields": { "zaps": [] }, "kind": "err" },
    "inc.channel.broadcast": { "kind": "none" },
    "inc.channel.close": { "kind": "none" },
    "inc.channel.emit": { "kind": "none" },
    "inc.channel.list": { "fields": { "channels": [] }, "kind": "default" },
    "inc.channel.open": { "kind": "err" },
    "inc.emit": { "kind": "none" },
    "inc.subscribe": { "kind": "err" },
    "inc.unsubscribe": { "kind": "none" },
    "intent.available": { "kind": "err" },
    "intent.handlers": { "kind": "err" },
    "catalog.get": { "kind": "err" },
    "intent.invoke": { "codes": { "internal-error": "invoke failed", "user-denied": "user cancelled" }, "kind": "intent" },
    "link.open": { "kind": "link" },
    "media.capabilities": { "kind": "none" },
    "media.command": { "kind": "none" },
    "media.session.create": { "codes": { "user-denied": "source blocked" }, "kind": "err" },
    "media.session.destroy": { "kind": "none" },
    "media.session.update": { "kind": "none" },
    "media.state": { "kind": "none" },
    "notify.badge": { "kind": "none" },
    "notify.channel.register": { "kind": "none" },
    "notify.dismiss": { "kind": "none" },
    "notify.permission.request": { "kind": "granted" },
    "notify.send": { "codes": { "rate-limited": "rate limited", "user-denied": "permission denied" }, "kind": "err" },
    "outbox.close": { "closed": "outbox.closed", "kind": "lifecycle" },
    "outbox.getEvent": { "kind": "err" },
    "outbox.publish": { "codes": { "user-denied": "publish denied" }, "kind": "okFalse" },
    "outbox.query": { "fields": { "events": [] }, "kind": "err" },
    "outbox.resolveRelays": { "kind": "err" },
    "outbox.subscribe": { "closed": "outbox.closed", "kind": "lifecycle" },
    "relay.close": { "kind": "none" },
    "relay.publish": { "kind": "okFalse" },
    "relay.publishEncrypted": { "kind": "okFalse" },
    "relay.query": { "fields": { "events": [] }, "kind": "err" },
    "relay.subscribe": {
      "closed": "relay.closed",
      "codes": {
        "internal-error": "error: internal-error",
        "invalid-request": "invalid: invalid-request",
        "rate-limited": "rate-limited: rate-limited",
        "too-large": "invalid: too-large",
        "user-denied": "blocked: user-denied"
      },
      "kind": "lifecycle"
    },
    "resource.bytes": { "codes": { "rate-limited": "quota-exceeded", "user-denied": "blocked-by-policy" }, "kind": "typedErr" },
    "resource.bytesMany": { "codes": { "rate-limited": "quota-exceeded", "user-denied": "blocked-by-policy" }, "kind": "typedErr" },
    "resource.cancel": { "kind": "none" },
    "resource.info": { "codes": { "rate-limited": "quota-exceeded" }, "kind": "typedErr" },
    "storage.get": { "kind": "err" },
    "storage.keys": { "kind": "err" },
    "storage.remove": { "kind": "err" },
    "storage.set": { "codes": { "too-large": "quota exceeded" }, "kind": "err" },
    "theme.get": { "fields": { "theme": { "colors": { "background": "#ffffff", "primary": "#111111", "text": "#111111" } } }, "kind": "default" },
    "upload.info": { "kind": "err" },
    "upload.status": { "kind": "err" },
    "upload.upload": { "codes": { "too-large": "file too large", "user-denied": "policy denied" }, "kind": "err" }
  } /* nap-fail-shapes:end */

  // the generic failure codes Go uses (D-07); an entry's codes map them to
  // its spec's own
  const NAP_CODES = ["internal-error", "user-denied", "rate-limited", "too-large", "invalid-request"]
  const MAX_ID_BYTES = 128
  const own = (obj, key) => obj != null && Object.prototype.hasOwnProperty.call(obj, key)
  const utf8Length = s => new TextEncoder().encode(s).length

  // wireBytes is how many bytes an envelope's JSON takes on its way to Go.
  // The rpc carries it as a JSON string (encode escapes " and \), and the
  // desktop child writes that string into its JSON line once more, which
  // escapes " and \ again and HTML-escapes <, > and & (and U+2028/U+2029)
  // as \uXXXX. Android's carrier escapes less, so this bounds both.
  const WIRE_EXTRA = { '"': 1, "\\": 1, "<": 5, ">": 5, "&": 5, "\u2028": 3, "\u2029": 3 }
  const wireBytes = json => {
    const quoted = JSON.stringify(json)
    let extra = 0
    for (const m of quoted.matchAll(/["\\<>&\u2028\u2029]/g)) extra += WIRE_EXTRA[m[0]]
    return utf8Length(quoted) + extra
  }

  // napCodeOf is the generic code a failure carries; anything untagged is a
  // failed rpc
  const napCodeOf = err => (err && NAP_CODES.includes(err.napCode) ? err.napCode : "internal-error")

  // the same id rule as Go's napValidID (D-11): a string of at most 128
  // UTF-8 bytes, or a finite number whose token is at most 128 characters
  const validId = id =>
    (typeof id === "string" && utf8Length(id) <= MAX_ID_BYTES) ||
    (typeof id === "number" && Number.isFinite(id) && String(id).length <= MAX_ID_BYTES)

  // a subscription id, as Go's napValidSubID: a non-empty string of at most
  // 128 UTF-8 bytes
  const validSubId = subId => typeof subId === "string" && subId !== "" && utf8Length(subId) <= MAX_ID_BYTES

  // failureFor builds the envelope Go's napCall.failWith would send for data
  // failing with code, or null when Go would send nothing: an unknown type
  // (NIP-5D), a reply-less type (its id may name another request), an id Go
  // would not echo, or a request without the correlator its answer needs (A16).
  const failureFor = (data, code) => {
    const entry = own(FAIL_SHAPES, data.type) ? FAIL_SHAPES[data.type] : null
    if (!entry || entry.kind === "none") return null
    // Go drops a request whose id it would not echo, whatever its type
    if (data.id !== undefined && !validId(data.id)) return null
    const mapped = own(entry.codes, code) ? entry.codes[code] : code

    if (entry.kind === "lifecycle") {
      if (!validSubId(data.subId)) return null
      const closed = { type: entry.closed, subId: data.subId, reason: mapped }
      if (data.id !== undefined) closed.id = data.id
      return closed
    }
    if (data.id === undefined) return null

    const env = entry.fields ? JSON.parse(JSON.stringify(entry.fields)) : {}
    const result = data.type + ".result"
    switch (entry.kind) {
      case "err":
        Object.assign(env, { type: result, error: mapped })
        break
      case "okFalse":
        Object.assign(env, { type: result, ok: false, error: mapped })
        break
      case "okFalseCode":
        Object.assign(env, { type: result, ok: false, code: mapped, error: mapped })
        break
      case "typedErr":
        Object.assign(env, { type: data.type + ".error", error: mapped })
        break
      case "link":
        Object.assign(env, { type: result, status: "denied", error: mapped })
        break
      case "intent": {
        const req = data.request && typeof data.request === "object" && !Array.isArray(data.request) ? data.request : {}
        const archetype = typeof req.archetype === "string" ? req.archetype : ""
        const action = typeof req.action === "string" && req.action !== "" ? req.action : "open"
        Object.assign(env, { type: result, result: { ok: false, archetype, action, handled: false, error: mapped } })
        break
      }
      case "default":
        env.type = result
        break
      case "schemaError":
        Object.assign(env, { type: "config.schemaError", code: mapped, error: mapped })
        break
      case "granted":
        Object.assign(env, { type: "notify.permission.result", granted: false })
        break
      default:
        return null
    }
    env.id = data.id
    return env
  }

  // refuse answers a request that never reached Go (too large, unencodable,
  // too many pending, or the rpc failed), in the same shape Go would have
  // used: it mirrors napCall.failWith. Without an answer the napplet would
  // wait out the shim's own timeout, and relay.* has none.
  const refuse = (data, err) => {
    const env = failureFor(data, napCodeOf(err))
    if (env) deliver(env)
  }

  // ── the launcher's own hooks ────────────────────────────────────
  // the theme: the host page paints itself in it (the napplet gets
  // theme.changed from Go, through its own NAP domain)
  const applyTheme = (theme, vars) => {
    if (typeof vars === "string") {
      try {
        vars = vars ? JSON.parse(vars) : null
      } catch {
        vars = null
      }
    }
    window.__nappTheme = { name: theme, vars: vars || {} }
    const root = document.documentElement
    if (!root) return
    if (theme) root.style.colorScheme = theme === "dark" ? "dark" : "light"
    if (vars && vars.surface) root.style.background = vars.surface
    if (vars && typeof vars === "object") {
      for (const k of ["surface-alt", "border", "text"]) {
        if (typeof vars[k] === "string") root.style.setProperty("--" + k, vars[k])
      }
    }
  }
  window.__bridge_theme_change = applyTheme

  // napplets get intents and inc events as NAP pushes, never as napp actions;
  // a stray one must still be answered so nobody waits on it
  window.__bridge_dispatch_action = id => {
    rpc("napp.dispatchResult", { id, result: null }).catch(() => {})
  }

  // ── the chrome ──────────────────────────────────────────────────
  // ── the frame ───────────────────────────────────────────────────
  // A session starts here, in this trusted page, never in the frame: nothing
  // the napplet posts can start, reset or replay one. nap.start rides the same
  // ordered lane as the envelopes, and the frame is created only once Go has
  // acknowledged it, so the napplet's first envelope lands in the new session.
  // Every session gets a fresh iframe element, and the previous one is
  // dropped first: whatever its document still posts fails the sender check
  // instead of reaching the new session. bootSerial makes an older boot that
  // is still in flight give up, so overlapping boots leave one frame.
  //
  // One frame holds exactly one document: the srcdoc boot() gave it. Its
  // first load only triggers the notify.controls push (nap.loaded) and starts
  // nothing. Any later load of the same frame means the document was
  // replaced: a reload, a navigation the CSP let through, one it blocked
  // while the old document lives on (WebKitGTK still reports that load),
  // about:blank, about:srcdoc, or document.open followed by close. Whatever
  // an engine still lets through is caught here, every kind the same way:
  // the frame goes, its session ends, and a fresh frame boots with a fresh
  // session (replaced, below). A second document-start marker
  // (DOCUMENT_MARKER, above) does the same, and closes the window before a
  // reloaded document's load, in which its envelopes would otherwise reach
  // the old session.
  //
  // Not caught: a document the napplet makes itself without a URL load (the
  // result of a javascript: URL, or document.open with no close). It is not
  // built from the srcdoc, so it posts no marker; its envelopes before its
  // own load reach the live session, one made before the first load is
  // taken for the boot, and an unclosed one never loads. It stays under the
  // inherited policy and sandbox, and no signal this page gets can tell it
  // apart (spec/CONFORMANCE.md NIP-5D-reload-residual).
  const showBootError = err => {
    document.body.textContent = "This napplet could not be started: " + ((err && err.message) || err)
  }

  let bootSerial = 0
  const boot = async () => {
    const serial = ++bootSerial
    let doc
    try {
      doc = await rpc("nap.boot")
    } catch (err) {
      if (serial === bootSerial) showBootError(err)
      return
    }
    if (serial !== bootSerial) return
    if (!doc || typeof doc.srcdoc !== "string") return

    gamepadStop()
    if (frame) frame.remove()
    frame = null
    session = null

    let started
    try {
      started = await enqueue(() => rpc("nap.start"), true)
    } catch (err) {
      if (serial === bootSerial) showBootError(err)
      return
    }
    if (serial !== bootSerial) return
    if (!started || typeof started.gen !== "number") {
      showBootError(new Error("the launcher started no session"))
      return
    }
    session = started.gen

    const f = document.createElement("iframe")
    // allow-scripts and nothing else: never allow-same-origin
    f.setAttribute("sandbox", "allow-scripts")
    // Controllers belong to the trusted runtime. CSP cannot deny this API.
    f.setAttribute("allow", "gamepad 'none'")
    f.setAttribute("referrerpolicy", "no-referrer")
    f.setAttribute("title", typeof doc.title === "string" ? doc.title : "napplet")
    f.style.cssText = "position:fixed;inset:0;width:100%;height:100%;border:0;margin:0;padding:0;display:block"
    // loads are counted per frame, here in the closure that made it: the
    // first is the boot, any later one a replaced document. srcdoc is set
    // before the frame is appended, so the boot is exactly one load.
    let loads = 0
    f.addEventListener("load", () => {
      if (frame !== f) return
      if (++loads === 1) enqueue(() => rpc("nap.loaded"), true).catch(() => {})
      else replaced(f)
    })
    f.srcdoc = doc.srcdoc
    frame = f
    markers = 0
    gamepadPolicyChecked = false
    document.body.appendChild(f)
  }

  // ── replaced documents ──────────────────────────────────────────
  // replaced(f) ends a frame whose document was replaced. Everything runs
  // synchronously in the load or marker handler: with frame null, whatever
  // the replacing document (or a still-running original, after a blocked
  // navigation) posts fails the sender check, and with session null, every
  // push for the old session is dropped. nap.reset then ends the old
  // session in Go on the trusted lane, so the session ends even when no new
  // one follows: its subscriptions, prompts, fetches, uploads, inc topics
  // and grants go with it, and Go drops anything still queued for it. The
  // fresh boot waits for that, so its nap.boot and nap.start come after the
  // teardown, and a boot already in flight gives up (bootSerial). A frame
  // that is no longer current is ignored, so one replacement rebuilds once.
  //
  // A napplet that keeps replacing its document (a reload loop, or one that
  // rewrites itself with document.open and close, which is rebuilt and
  // counted like any other replacement) is stopped: at most REBUILD_LIMIT rebuilds within
  // REBUILD_WINDOW_MS, and the next replacement still ends its session but
  // boots nothing and says why in the window. rebuilds and halted live in
  // this closure, out of the frame's reach; only the launcher's dev reload
  // (__nap_reload) or a new window clears them. The initial boot and dev
  // reloads are never counted. Time is performance.now(), which only moves
  // forward: the wall clock can step with NTP or by hand, and a step back
  // would halt early, a step forward would miss a loop.
  const REBUILD_LIMIT = 3
  const REBUILD_WINDOW_MS = 10 * 1000
  const HALTED = "This napplet keeps reloading itself and was stopped."
  let rebuilds = []
  let halted = false

  // resetSession ends the current session in Go on the trusted lane. A
  // failure is logged and passed on, never swallowed: it means the session
  // may still hold its subscriptions and grants in Go.
  const resetSession = () =>
    enqueue(() => rpc("nap.reset"), true).catch(err => {
      console.error("[napplet-host] nap.reset failed", err)
      throw err
    })

  const replaced = f => {
    if (frame !== f) return
    f.remove()
    gamepadStop()
    frame = null
    session = null
    const serial = ++bootSerial
    const reset = resetSession()

    const now = performance.now()
    rebuilds = rebuilds.filter(t => now - t < REBUILD_WINDOW_MS)
    if (halted || rebuilds.length >= REBUILD_LIMIT) {
      // the serial bump above already made any boot in flight give up
      halted = true
      document.body.textContent = HALTED
      // No nap.start follows here to end the old session in Go, so a
      // failed reset is tried once more, unless a dev reload booted in the
      // meantime (its nap.start ends the session, and a reset queued after
      // it would end the new one). If that fails too, the window says so.
      reset
        .catch(() => {
          if (halted && serial === bootSerial) return resetSession()
        })
        .catch(() => {
          if (halted && serial === bootSerial) {
            document.body.textContent = HALTED + " Its session could not be ended; close this window."
          }
        })
      return
    }
    rebuilds.push(now)
    // a failed reset was logged; the fresh boot's nap.start ends the old
    // session in Go all the same
    reset
      .catch(() => {})
      .then(() => {
        // a dev reload in the meantime booted already
        if (serial === bootSerial) boot()
      })
  }

  const start = () => {
    if (window.__nappTheme) applyTheme(window.__nappTheme.name, window.__nappTheme.vars)
    boot()
  }

  // a dev reload: Go has new bytes for us. It is the launcher's, never the
  // napplet's, so it starts a halted napplet again with a clean history.
  window.__nap_reload = () => {
    if (halted) document.body.textContent = ""
    halted = false
    rebuilds = []
    boot()
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start, { once: true })
  else start()
})()
