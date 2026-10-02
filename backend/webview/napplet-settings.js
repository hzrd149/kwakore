;(() => {
  // A napp's settings window: the launcher-owned page that renders a
  // napplet's NAP-CONFIG schema as a form, and lists what the user let the
  // napp do. Nothing of the napplet runs here. The schema is data: only the
  // keywords the backend already checked are read, every string goes in as
  // text, never as HTML.
  //
  // The page sends back the settings the user set or touched, nothing else:
  // a setting left alone keeps following the napplet's default. Secrets are
  // never sent down; the page only learns which ones are set.

  if (window !== window.top) return

  // ── talking to the host ─────────────────────────────────────────
  // Desktop: __verdanaSettingsRPC, the child's token-carrying wrapper.
  // Android: the __verdanaHost web message channel.
  const rpc = (() => {
    const decode = value => {
      const val = typeof value === "string" ? JSON.parse(value) : value
      if (val && val.__bridge_error) throw new Error(val.__bridge_error)
      return val
    }
    const encode = params => (params !== undefined ? JSON.stringify(params) : "null")

    if (typeof window.__verdanaSettingsRPC === "function") {
      const bound = window.__verdanaSettingsRPC
      return (method, params) => bound(method, encode(params)).then(decode)
    }

    const port = window.__verdanaHost
    if (!port) return () => Promise.reject(new Error("no host to talk to"))

    const pending = new Map()
    let serial = 0
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
        waiter.reject(new Error(msg.error))
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
        const id = ++serial
        pending.set(id, { resolve, reject })
        port.postMessage(JSON.stringify({ t: "rpc", id, method, params: encode(params) }))
      })
  })()

  // ── theme ───────────────────────────────────────────────────────
  const applyTheme = (theme, vars) => {
    if (typeof vars === "string") {
      try {
        vars = vars ? JSON.parse(vars) : null
      } catch {
        vars = null
      }
    }
    const root = document.documentElement
    if (!root) return
    if (theme) root.style.colorScheme = theme === "dark" ? "dark" : "light"
    if (vars && typeof vars === "object") {
      for (const [k, v] of Object.entries(vars)) {
        if (/^[a-z0-9-]+$/.test(k) && typeof v === "string") root.style.setProperty("--" + k, v)
      }
    }
  }
  window.__bridge_theme_change = applyTheme

  // ── helpers ─────────────────────────────────────────────────────
  const el = (tag, attrs, ...children) => {
    const node = document.createElement(tag)
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v === undefined || v === null || v === false) continue
      if (k === "class") node.className = v
      else if (k.startsWith("on")) node.addEventListener(k.slice(2), v)
      else node.setAttribute(k, v === true ? "" : String(v))
    }
    for (const c of children) {
      if (c === undefined || c === null || c === false) continue
      node.append(c instanceof Node ? c : String(c))
    }
    return node
  }
  const str = v => (typeof v === "string" ? v : undefined)
  const isObj = v => v && typeof v === "object" && !Array.isArray(v)
  const join = (path, key) => (path ? path + "." + key : key)

  // properties in display order: x-napplet-order first, ascending, then
  // the rest; ties and the rest by key
  const ordered = props =>
    Object.entries(props || {})
      .filter(([, s]) => isObj(s))
      .sort(([ka, a], [kb, b]) => {
        const oa = typeof a["x-napplet-order"] === "number" && a["x-napplet-order"] >= 0 ? a["x-napplet-order"] : null
        const ob = typeof b["x-napplet-order"] === "number" && b["x-napplet-order"] >= 0 ? b["x-napplet-order"] : null
        if (oa !== null && ob !== null && oa !== ob) return oa - ob
        if (oa !== null && ob === null) return -1
        if (oa === null && ob !== null) return 1
        return ka < kb ? -1 : ka > kb ? 1 : 0
      })

  // ── state ───────────────────────────────────────────────────────
  let data = null // the last settings.load
  let fields = [] // { path, read(), touched() }
  let pendingSection = ""
  let saveTimer = 0
  let saveInFlight = false
  let queuedSave = null
  let renderVersion = 0

  // Save without rerendering so the active control keeps its focus. If a
  // value changes while a save is in flight, the newest state is saved next.
  const queueSave = (request, delay = 350) => {
    queuedSave = request
    clearTimeout(saveTimer)
    status("Saving…")
    saveTimer = setTimeout(flushSave, delay)
  }

  const flushSave = async () => {
    clearTimeout(saveTimer)
    saveTimer = 0
    if (saveInFlight || !queuedSave) return
    const request = queuedSave
    const version = renderVersion
    queuedSave = null
    saveInFlight = true
    try {
      const next = await rpc(request.method, request.params())
      if (version === renderVersion) data = next
      status("Saved")
    } catch (err) {
      const message = (err && err.message) || String(err)
      status(message, true)
      markInvalid(message)
    } finally {
      saveInFlight = false
      if (queuedSave) flushSave()
    }
  }

  const saveNapp = (delay = 350) => queueSave({ method: "settings.save", params: () => ({ values: collect() }) }, delay)

  const describe = (schema, wrap) => {
    const text = str(schema.description) || str(schema.markdownDescription)
    if (text) wrap.append(el("div", { class: "hint" }, text))
    if (str(schema.deprecationMessage)) wrap.append(el("div", { class: "deprecated" }, schema.deprecationMessage))
  }

  // field builds the editor for one leaf and registers how to read it back.
  // read() returns undefined to leave the setting unset, null to clear a
  // secret, or the value.
  const field = (key, schema, path, value, required) => {
    const title = str(schema.title) || key
    const wrap = el("div", { class: "field", "data-path": path })
    const id = "f-" + path.replace(/[^a-zA-Z0-9_-]/g, "_")
    let touched = false
    const touch = () => {
      touched = true
      wrap.classList.remove("invalid")
      saveNapp()
    }
    let read

    const secret = schema.type === "string" && schema["x-napplet-secret"] === true
    if (Array.isArray(schema.enum)) {
      const select = el("select", { id, onchange: () => (touch(), hint()) })
      const blank = !required && value === undefined
      if (blank) select.append(el("option", { value: "" }, "—"))
      schema.enum.forEach((v, i) => {
        const opt = el("option", { value: String(i) }, typeof v === "string" ? v : JSON.stringify(v))
        if (JSON.stringify(v) === JSON.stringify(value)) opt.selected = true
        select.append(opt)
      })
      const descs = Array.isArray(schema.enumDescriptions) ? schema.enumDescriptions : []
      const descEl = el("div", { class: "hint" })
      const hint = () => {
        const d = descs[Number(select.value)]
        descEl.textContent = typeof d === "string" ? d : ""
      }
      hint()
      wrap.append(el("label", { for: id }, title), select, descEl)
      read = () => (select.value === "" ? undefined : schema.enum[Number(select.value)])
    } else if (schema.type === "boolean") {
      wrap.classList.add("check")
      const box = el("input", { type: "checkbox", id, onchange: touch })
      box.checked = value === true
      wrap.append(el("label", { for: id }, box, title))
      read = () => box.checked
    } else if (schema.type === "number" || schema.type === "integer") {
      const input = el("input", {
        type: "number",
        id,
        min: typeof schema.minimum === "number" ? schema.minimum : undefined,
        max: typeof schema.maximum === "number" ? schema.maximum : undefined,
        step: schema.type === "integer" ? 1 : "any",
        oninput: touch,
      })
      if (typeof value === "number") input.value = String(value)
      wrap.append(el("label", { for: id }, title), input)
      read = () => (input.value.trim() === "" ? undefined : Number(input.value))
    } else if (schema.type === "string") {
      // format is only a hint: never a widget that refuses what it does not like
      const formats = { color: "color", date: "date" }
      const type = secret ? "password" : formats[schema.format] || "text"
      const isSet = secret && data.secrets.includes(path)
      let cleared = false
      const input = el("input", {
        type,
        id,
        autocomplete: secret ? "off" : undefined,
        minlength: schema.minLength,
        maxlength: schema.maxLength,
        placeholder: secret ? (isSet ? "set (type to replace)" : "not set") : undefined,
        oninput: () => {
          touch()
          cleared = false
        },
      })
      if (!secret && typeof value === "string") input.value = value
      wrap.append(el("label", { for: id }, title))
      if (secret && isSet) {
        const clear = el(
          "button",
          {
            type: "button",
            class: "danger",
            onclick: () => {
              touch()
              cleared = true
              input.value = ""
              input.placeholder = "will be cleared"
            },
          },
          "Clear",
        )
        wrap.append(el("div", { class: "row" }, input, clear))
      } else {
        wrap.append(input)
      }
      read = () => {
        if (secret) {
          if (cleared) return null
          return input.value === "" ? undefined : input.value
        }
        return input.value === "" ? undefined : input.value
      }
    } else if (schema.type === "array" && isObj(schema.items)) {
      const items = schema.items
      const list = el("div")
      const rows = []
      const addRow = v => {
        const input =
          items.type === "boolean"
            ? el("input", { type: "checkbox", onchange: touch })
            : el("input", {
                type: items.type === "number" || items.type === "integer" ? "number" : "text",
                step: items.type === "integer" ? 1 : undefined,
                oninput: touch,
              })
        if (items.type === "boolean") input.checked = v === true
        else if (v !== undefined) input.value = String(v)
        const row = el("div", { class: "row" }, input)
        const entry = { input, row }
        row.append(
          el(
            "button",
            {
              type: "button",
              onclick: () => {
                touch()
                rows.splice(rows.indexOf(entry), 1)
                row.remove()
              },
            },
            "Remove",
          ),
        )
        rows.push(entry)
        list.append(row)
      }
      ;(Array.isArray(value) ? value : []).forEach(addRow)
      wrap.append(
        el("div", { class: "label" }, title),
        list,
        el("button", { type: "button", onclick: () => (touch(), addRow(undefined)) }, "Add"),
      )
      read = () =>
        rows.map(({ input }) => {
          if (items.type === "boolean") return input.checked
          if (items.type === "number" || items.type === "integer") return Number(input.value)
          return input.value
        })
    } else {
      return null
    }
    describe(schema, wrap)
    fields.push({ path, read, touched: () => touched, wrap })
    return wrap
  }

  // object renders a nested object's properties into a container
  const object = (schema, path, values, into) => {
    const required = Array.isArray(schema.required) ? schema.required : []
    for (const [key, sub] of ordered(schema.properties)) {
      const p = join(path, key)
      const v = isObj(values) ? values[key] : undefined
      if (sub.type === "object") {
        const set = el("fieldset", { "data-path": p }, el("legend", {}, str(sub.title) || key))
        describe(sub, set)
        object(sub, p, v, set)
        into.append(set)
        continue
      }
      const f = field(key, sub, p, v, required.includes(key))
      if (f) into.append(f)
    }
  }

  // collect builds the values to save: what was set before or touched now
  const collect = () => {
    const out = {}
    const set = new Set(data.set)
    for (const f of fields) {
      if (!f.touched() && !set.has(f.path)) continue
      const v = f.read()
      if (v === undefined) continue
      const parts = f.path.split(".")
      let at = out
      for (const part of parts.slice(0, -1)) at = at[part] = isObj(at[part]) ? at[part] : {}
      at[parts[parts.length - 1]] = v
    }
    return out
  }

  const status = (text, error) => {
    const s = document.getElementById("status")
    if (!s) return
    s.textContent = text || ""
    s.className = "status" + (error ? " error" : "")
  }

  const markInvalid = message => {
    // the backend names the setting first: "a.b is not a valid value"
    const path = String(message).split(" ")[0]
    for (const f of fields) if (f.path === path) f.wrap.classList.add("invalid")
  }

  const permissionsView = () => {
    const box = el("section", { "data-section": "__permissions" }, el("h2", {}, "Permissions"))
    if (!data.permissions.length) {
      box.append(el("p", { class: "muted" }, "Nothing remembered for this napp."))
      return box
    }
    const byPerm = new Map()
    for (const r of data.permissions) {
      if (!byPerm.has(r.permission)) byPerm.set(r.permission, [])
      byPerm.get(r.permission).push(r)
    }
    for (const [perm, rules] of byPerm) {
      const detail = el("div", {}, el("strong", {}, perm))
      for (const r of rules) {
        detail.append(el("div", { class: "hint" }, (r.subject ? r.subject + ": " : "") + r.decision + (r.target ? " → " + r.target : "")))
      }
      box.append(
        el(
          "div",
          { class: "perm" },
          detail,
          el("button", { type: "button", onclick: () => run("settings.forgetPermission", { permission: perm }, "Forgotten") }, "Forget"),
        ),
      )
    }
    box.append(
      el(
        "div",
        { class: "actions" },
        el("button", { type: "button", class: "danger", onclick: () => run("settings.forgetPermission", { permission: "" }, "Forgotten") }, "Forget all"),
      ),
    )
    return box
  }

  // the napp's own page: its NAP-CONFIG form
  const nappPage = () => {
    const out = []
    let schema = null
    if (data.schema) {
      try {
        schema = typeof data.schema === "string" ? JSON.parse(data.schema) : data.schema
      } catch {
        schema = null
      }
    }
    if (!schema || !isObj(schema.properties)) {
      out.push(el("p", { class: "muted" }, "This napp has no settings of its own."))
    } else {
      // top-level properties grouped by x-napplet-section, in order of
      // first appearance; the unsectioned ones first, under no heading
      const groups = new Map([["", []]])
      for (const [key, sub] of ordered(schema.properties)) {
        const sec = str(sub["x-napplet-section"]) || ""
        if (!groups.has(sec)) groups.set(sec, [])
        groups.get(sec).push(key)
      }
      const form = el("form", { onsubmit: e => (e.preventDefault(), flushSave()) })
      for (const [sec, keys] of groups) {
        if (!keys.length) continue
        const box = el("section", { "data-section": sec })
        if (sec) box.append(el("h2", {}, sec))
        const props = {}
        for (const k of keys) props[k] = schema.properties[k]
        object({ properties: props, required: schema.required }, "", data.values, box)
        form.append(box)
      }
      form.append(
        el(
          "div",
          { class: "actions" },
          el("span", { id: "status", class: "status" }),
          el("button", { type: "button", onclick: () => run("settings.reset", undefined, "Defaults restored") }, "Reset to defaults"),
        ),
      )
      out.push(form)
    }
    return out
  }

  // listEditor edits a list of urls; read() is what it holds now
  const listEditor = (title, hint, values, placeholder, changed) => {
    const list = el("div")
    const inputs = []
    const add = v => {
      const input = el("input", { type: "text", inputmode: "url", placeholder, spellcheck: "false", oninput: changed })
      if (v) input.value = v
      const row = el("div", { class: "row" }, input)
      row.append(
        el(
          "button",
          {
            type: "button",
            onclick: () => {
              inputs.splice(inputs.indexOf(input), 1)
              row.remove()
              changed(0)
            },
          },
          "Remove",
        ),
      )
      inputs.push(input)
      list.append(row)
    }
    ;(values || []).forEach(add)
    const box = el(
      "section",
      {},
      el("h2", {}, title),
      el("div", { class: "hint" }, hint),
      el("div", { style: "margin-top:8px" }, list),
      el("button", { type: "button", onclick: () => (add(""), changed(0)) }, "Add"),
    )
    return { box, read: () => inputs.map(i => i.value.trim()).filter(Boolean) }
  }

  const loginFromSettings = async (input, button) => {
    const value = input.value.trim()
    if (!value) {
      status("Enter an nsec, bunker URL, or NIP-05 address.", true)
      return
    }
    button.disabled = true
    status("Signing in…")
    const previous = (data.launcher || {}).pubkey || ""
    try {
      await rpc("settings.login", { input: value })
      let sawLoading = false
      for (let attempt = 0; attempt < 130; attempt++) {
        await new Promise(resolve => setTimeout(resolve, 500))
        const next = await rpc("settings.load")
        const account = next.launcher || {}
        sawLoading = sawLoading || account.phase === "loading"
        const finished = account.phase !== "loading" && (sawLoading || account.pubkey !== previous || account.loginErr || attempt >= 3)
        if (!finished) continue
        data = next
        render()
        status(account.loginErr || (account.loggedIn ? "Signed in" : "Sign-in failed"), !!account.loginErr || !account.loggedIn)
        return
      }
      status("Sign-in is taking longer than expected.", true)
    } catch (err) {
      status((err && err.message) || String(err), true)
    } finally {
      button.disabled = false
    }
  }

  const accountPage = () => {
    const l = data.launcher || {}
    const section = el("section", {})
    if (l.loggedIn) {
      const identity = el("div", { class: "account-identity" }, el("strong", {}, l.profileName || "Nostr account"))
      if (l.pubkey) identity.append(el("div", { class: "account-key" }, l.pubkey))
      const card = el("div", { class: "account-card" })
      if (l.profilePicture) card.append(el("img", { class: "account-avatar", src: l.profilePicture, alt: "" }))
      card.append(identity)
      section.append(card)
    } else {
      section.append(el("p", { class: "muted" }, "You are not signed in."))
    }
    const input = el("input", {
      type: "password",
      autocomplete: "off",
      placeholder: "nsec1…, bunker://…, or name@example.com",
    })
    const login = el("button", { type: "button" }, l.loggedIn ? "Switch account" : "Sign in")
    login.addEventListener("click", () => loginFromSettings(input, login))
    input.addEventListener("keydown", event => {
      if (event.key === "Enter") {
        event.preventDefault()
        login.click()
      }
    })
    section.append(
      el("h2", {}, l.loggedIn ? "Use another account" : "Sign in"),
      el("div", { class: "hint" }, "Use an nsec, a bunker signer URL, or a NIP-05 address."),
      el("div", { class: "row", style: "margin-top:8px" }, input, login),
    )
    if (l.loggedIn) {
      section.append(
        el("h2", {}, "Sign out"),
        el("div", { class: "hint" }, "Signing out closes open napps and napplets."),
        el("button", {
          type: "button",
          class: "danger",
          onclick: () => {
            if (window.confirm("Sign out and close all open apps?")) run("settings.logout", undefined, "Signed out")
          },
        }, "Sign out"),
      )
    }
    section.append(el("div", { class: "actions" }, el("span", { id: "status", class: "status" })))
    return [section]
  }

  // Verdana's settings are separate pages. Each page sends only the fields it
  // owns; settings.saveLauncher treats omitted fields as unchanged.
  const verdanaPage = page => {
    const l = data.launcher || {}
    let readLauncher = () => ({})
    const changed = delay =>
      queueSave({ method: "settings.saveLauncher", params: () => readLauncher() }, typeof delay === "number" ? delay : 350)
    let content

    if (page === "general") {
      const themeMode = el(
        "select",
        { name: "themeMode", onchange: () => changed(0) },
        el("option", { value: "system" }, "System"),
        el("option", { value: "light" }, "Light"),
        el("option", { value: "dark" }, "Dark"),
      )
      themeMode.value = l.themeMode || "system"
      content = el(
        "section",
        {},
        el("h2", {}, "Appearance"),
        el("div", { class: "hint" }, "System follows your desktop appearance and updates open napps and napplets automatically."),
        el("label", { class: "field" }, el("span", {}, "Theme"), themeMode),
      )
      let autostart = null
      if (l.autostartSupported) {
        autostart = el("input", { type: "checkbox", name: "autostart", onchange: () => changed(0) })
        autostart.checked = !!l.autostart
        content.append(
          el("div", { class: "field check" }, el("label", {}, autostart, el("span", {}, "Launch at login", el("span", { class: "hint" }, "Start Verdana in the background.")))),
        )
      }
      const appShortcuts = el("input", { type: "checkbox", name: "appShortcuts" })
      appShortcuts.checked = !!l.appShortcuts
      const shortcutNaming = el(
        "select",
        { name: "appShortcutNaming", onchange: () => changed(0) },
        el("option", { value: "plain" }, "App name"),
        el("option", { value: "hosted" }, "App name — Verdana"),
      )
      shortcutNaming.value = l.appShortcutNaming || "plain"
      shortcutNaming.disabled = !appShortcuts.checked
      appShortcuts.onchange = () => {
        shortcutNaming.disabled = !appShortcuts.checked
        changed(0)
      }
      if (l.appShortcutsSupported) {
        content.append(
          el("div", { class: "field check" }, el("label", {}, appShortcuts, el("span", {}, "Show installed apps in the system launcher", el("span", { class: "hint" }, "Keep native entries synchronized for every installed napp and napplet.")))),
          el("label", { class: "field" }, el("span", {}, "Launcher names"), shortcutNaming),
        )
      }
      let gnomeSearch = null
      if (l.gnomeSearchSupported) {
        gnomeSearch = el("input", { type: "checkbox", name: "gnomeSearch", onchange: () => changed(0) })
        gnomeSearch.checked = !!l.gnomeSearch
        content.append(
          el("div", { class: "field check" }, el("label", {}, gnomeSearch, el("span", {}, "Show napplets in GNOME search", el("span", { class: "hint" }, "Install and maintain GNOME Shell and D-Bus integration files. A new GNOME session may be required after changing this.")))),
        )
      }
      readLauncher = () => ({
        themeMode: themeMode.value,
        ...(autostart ? { autostart: autostart.checked } : {}),
        ...(l.appShortcutsSupported ? { appShortcuts: appShortcuts.checked, appShortcutNaming: shortcutNaming.value } : {}),
        ...(gnomeSearch ? { gnomeSearch: gnomeSearch.checked } : {}),
      })
    } else if (page === "discovery") {
      const relays = listEditor(
        "Relays",
        "Napps and napplets are discovered on these relays, and on your own when enabled below.",
        l.relays,
        "wss://relay.example.com",
        changed,
      )
      const discoverOnUserRelays = el("input", { type: "checkbox", name: "discoverOnUserRelays", onchange: () => changed(0) })
      discoverOnUserRelays.checked = !!l.discoverOnUserRelays
      const userRelays = l.userRelays || []
      let userStatus = "Log in to load your relays."
      if (l.loggedIn && !l.userRelaysLoadedAt) userStatus = "Loading your relay list…"
      else if (l.loggedIn && !userRelays.length) userStatus = "No relay list (NIP-65) found for your account."
      else if (l.loggedIn) userStatus = "From your relay list (NIP-65), loaded " + new Date(l.userRelaysLoadedAt * 1000).toLocaleString() + "."
      relays.box.append(
        el("div", { class: "field check" }, el("label", {}, discoverOnUserRelays, el("span", {}, "Also discover on my relays", el("span", { class: "hint" }, "Ask the write (outbox) relays from your relay list too.")))),
        el("h3", {}, "Your relays"),
        el("div", { class: "hint" }, userStatus),
        el("ul", { class: "user-relays" }, ...userRelays.map(r => el("li", {}, el("span", {}, r.url), ...(r.read ? [el("span", { class: "relay-tag" }, "read")] : []), ...(r.write ? [el("span", { class: "relay-tag" }, "write")] : [])))),
      )
      content = relays.box
      readLauncher = () => ({ relays: relays.read(), discoverOnUserRelays: discoverOnUserRelays.checked })
    } else {
      const servers = listEditor(
        "Blossom servers",
        "Napp and napplet files are fetched from these servers first, before the ones a napp or its author names. Every file is checked against its hash, wherever it comes from.",
        l.blossomServers,
        "https://blossom.example.com",
        changed,
      )
      content = servers.box
      readLauncher = () => ({ blossomServers: servers.read() })
    }

    const form = el(
      "form",
      { onsubmit: e => (e.preventDefault(), flushSave()) },
      content,
      el("div", { class: "actions" }, el("span", { id: "status", class: "status" })),
    )
    return [form]
  }

  let tab = ""
  const selectTab = async id => {
    clearTimeout(saveTimer)
    saveTimer = 0
    if (queuedSave) await flushSave()
    while (saveInFlight || queuedSave) {
      await new Promise(resolve => setTimeout(resolve, 25))
      if (!saveInFlight && queuedSave) await flushSave()
    }
    tab = id
    render()
  }

  const pages = () => [
    ...(data.napp
      ? [
          { id: "napp", group: data.name || "App", label: "App settings", title: data.name || "App", description: "Settings provided by this app." },
          { id: "permissions", group: data.name || "App", label: "Permissions", title: "Permissions", description: "Review decisions Verdana remembers for this app." },
        ]
      : []),
    { id: "account", group: "Verdana", label: "Account", title: "Account", description: "Manage the Nostr identity Verdana uses." },
    { id: "general", group: "Verdana", label: "General", title: "General", description: "Appearance and operating system integration." },
    { id: "discovery", group: "Verdana", label: "Discovery", title: "Discovery", description: "Choose where Verdana discovers napps and napplets." },
    { id: "downloads", group: "Verdana", label: "Downloads", title: "Downloads", description: "Choose where Verdana fetches app files." },
  ]

  const render = () => {
    renderVersion++
    clearTimeout(saveTimer)
    saveTimer = 0
    queuedSave = null
    const app = document.getElementById("app")
    fields = []
    if (!data.napp && (!tab || tab === "napp" || tab === "permissions")) tab = "general"
    else if (!tab) tab = "napp"
    document.title = (data.name || "Napp") + " — Settings"
    const available = pages()
    const current = available.find(page => page.id === tab) || available[0]
    tab = current.id
    const nav = el("nav", { class: "settings-nav", role: "tablist", "aria-label": "Settings sections" })
    let group = ""
    for (const page of available) {
      if (page.group !== group) {
        group = page.group
        nav.append(el("div", { class: "nav-group" }, group))
      }
      nav.append(
        el(
          "button",
          {
            type: "button",
            role: "tab",
            "aria-selected": tab === page.id ? "true" : "false",
            class: "nav-tab" + (tab === page.id ? " active" : ""),
            onclick: () => selectTab(page.id),
          },
          page.label,
        ),
      )
    }
    const body = tab === "napp" ? nappPage() : tab === "permissions" ? [permissionsView()] : tab === "account" ? accountPage() : verdanaPage(tab)
    const content = el(
      "div",
      { class: "settings-content", role: "tabpanel" },
      el("h2", { class: "page-title" }, current.title),
      el("div", { class: "muted page-description" }, current.description),
      ...body,
    )
    app.replaceChildren(
      el("header", { class: "settings-header" }, el("h1", {}, "Settings"), el("div", { class: "muted" }, data.napp ? data.name || "Napp" : "Verdana")),
      el("div", { class: "settings-shell" }, nav, content),
    )

    const sec = pendingSection || data.section
    pendingSection = ""
    data.section = ""
    if (sec && tab === "napp") showSection(sec)
  }

  const showSection = name => {
    for (const box of document.querySelectorAll("section[data-section]")) {
      if (box.getAttribute("data-section") !== name) continue
      box.scrollIntoView({ block: "start", behavior: "smooth" })
      box.classList.add("highlight")
      setTimeout(() => box.classList.remove("highlight"), 1500)
    }
  }

  let busy = false
  const run = async (method, params, done) => {
    if (busy) return
    clearTimeout(saveTimer)
    saveTimer = 0
    queuedSave = null
    if (saveInFlight) {
      setTimeout(() => run(method, params, done), 25)
      return
    }
    busy = true
    try {
      data = await rpc(method, params)
      render()
      status(done)
    } catch (err) {
      status((err && err.message) || String(err), true)
      markInvalid((err && err.message) || err)
    } finally {
      busy = false
    }
  }

  const load = async () => {
    try {
      data = await rpc("settings.load")
      render()
    } catch (err) {
      document.getElementById("app").textContent = "Could not load settings: " + ((err && err.message) || err)
    }
  }

  // the backend's hooks: the schema changed under us, or the napplet asked
  // for a section while this window was already open
  window.__settings_reload = load
  window.__settings_section = name => {
    if (!data) {
      pendingSection = name
      return
    }
    if (data.napp && tab !== "napp") {
      tab = "napp"
      pendingSection = name
      render()
      return
    }
    showSection(name)
  }

  const start = () => {
    if (window.__nappTheme) applyTheme(window.__nappTheme.name, window.__nappTheme.vars)
    load()
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start, { once: true })
  else start()
})()
