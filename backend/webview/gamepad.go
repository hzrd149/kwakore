package webview

// gamepadPrelude implements draft NAP-GAMEPAD independently of the pristine
// upstream shim. It is installed after the document marker so the host always
// sees that marker before this binding's eager subscription.
const gamepadPrelude = `
;(() => {
  let state = { available: true, focused: false, pads: [] }
  const changes = new Set()
  const navigatorObject = typeof navigator === "object" ? navigator : null
  const navigatorProto = navigatorObject && Object.getPrototypeOf(navigatorObject)
  const descriptor = navigatorProto && Object.getOwnPropertyDescriptor(navigatorProto, "getGamepads")
  const native = descriptor && descriptor.value
  let replaceNative = false
  if (typeof native === "function") {
    try { native.call(navigatorObject) }
    catch (err) { replaceNative = err && err.name === "SecurityError" }
  }
  // This runs before any napplet script. Same-source postMessage ordering
  // makes the first policy report evidence from the trusted preamble; a
  // later report forged by the napplet cannot override it.
  parent.postMessage({ type: "__kwakore.gamepad.policy", denied: replaceNative ||
    !navigatorObject || typeof navigatorObject.getGamepads !== "function" }, "*")
  const own = (proto, getters) => {
    const object = Object.create(proto)
    for (const key of Object.keys(getters)) Object.defineProperty(object, key, {
      get: getters[key], enumerable: true,
    })
    return object
  }
  const records = new WeakMap()
  const updatePad = (pad, data, connected = true) => {
    const record = records.get(pad)
    record.data = { ...data, connected, timestamp: connected ? data.timestamp : 0,
      axes: Object.freeze(data.axes.map(value => connected ? value : 0)),
      buttons: data.buttons.map(button => connected ? button : { value: 0, pressed: false, touched: false }),
    }
    const before = record.buttons || []
    record.buttons = Object.freeze(record.data.buttons.map((_, index) => before[index] || own(
      typeof GamepadButton === "function" ? GamepadButton.prototype : Object.prototype, {
        value: () => record.data.buttons[index]?.value || 0,
        pressed: () => !!record.data.buttons[index]?.pressed,
        touched: () => !!record.data.buttons[index]?.touched,
      },
    )))
  }
  const makePad = (data, index) => {
    const record = {}
    const pad = own(typeof Gamepad === "function" ? Gamepad.prototype : Object.prototype, {
      index: () => index, id: () => record.data.id, mapping: () => record.data.mapping,
      connected: () => record.data.connected, timestamp: () => record.data.timestamp,
      axes: () => record.data.axes, buttons: () => record.buttons, vibrationActuator: () => null,
    })
    records.set(pad, record)
    updatePad(pad, data)
    return pad
  }
  const emit = (type, pad) => {
    if (!replaceNative) return
    // GamepadEvent's constructor requires an engine-created Gamepad. A real
    // Event retains the engine's dispatch machinery, with the native event
    // prototype and an own gamepad property for the shell-created snapshot.
    const event = new Event(type)
    if (typeof GamepadEvent === "function") Object.setPrototypeOf(event, GamepadEvent.prototype)
    Object.defineProperty(event, "gamepad", { value: pad, enumerable: true })
    window.dispatchEvent(event)
  }
  window.addEventListener("message", event => {
    const data = event.data
    if (event.source !== parent || !data || data.type !== "gamepad.state" ||
        typeof data.available !== "boolean" || typeof data.focused !== "boolean" || !Array.isArray(data.pads)) return
    const before = state.pads
    // Native game libraries often keep the pad from gamepadconnected. Keep
    // that object (and its buttons) current so focus loss clears cached reads
    // as well as later navigator.getGamepads() calls.
    const pads = data.available ? data.pads.map((pad, index) => {
      if (!pad) return null
      const old = before[index]
      if (old && old.id === pad.id && old.mapping === pad.mapping) {
        updatePad(old, pad)
        return old
      }
      return makePad(pad, index)
    }) : []
    state = { available: data.available, focused: data.available && data.focused, pads }
    if (!data.available) state.reason = data.reason === "blocked" ? "blocked" : "unavailable"
    for (let index = 0; index < Math.max(before.length, pads.length); index++) {
      const old = before[index], next = pads[index]
      if (old && old !== next) {
        updatePad(old, records.get(old).data, false)
        emit("gamepaddisconnected", old)
      }
      if (next && old !== next) emit("gamepadconnected", next)
    }
    for (const handler of [...changes]) {
      try { handler({ ...state, pads: pads.slice() }) } catch (err) { console.error("[napplet-gamepad]", err) }
    }
  })
  window.napplet.gamepad = Object.freeze({
    getGamepads() { return state.pads.slice() },
    get available() { return state.available },
    get focused() { return state.focused },
    onChange(handler) {
      if (typeof handler !== "function") throw new TypeError("gamepad.onChange requires a function")
      // Each Subscription owns one registration, even for the same handler.
      const listener = value => handler(value)
      changes.add(listener)
      return { close() { changes.delete(listener) } }
    },
  })
  if (replaceNative) {
    const replacement = {
      getGamepads() {
        // The denied native operation still performs its own receiver check.
        // Objects merely inheriting Navigator.prototype must remain invalid.
        try { native.call(this) } catch (err) { if (!err || err.name !== "SecurityError") throw err }
        if (state.reason === "blocked") throw new DOMException("Controller access blocked by host policy", "SecurityError")
        return state.pads.slice()
      },
    }.getGamepads
    Object.defineProperty(navigatorProto, "getGamepads", { ...descriptor, value: replacement })
  }
  parent.postMessage({ type: "gamepad.subscribe" }, "*")
})();`
