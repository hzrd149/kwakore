---
phase: 01-containment-fix-and-canonical-shim-baseline
reviewed: 2026-10-03T00:02:42Z
depth: standard
files_reviewed: 30
files_reviewed_list:
  - .gitattributes
  - .github/workflows/android.yml
  - .github/workflows/desktop.yml
  - backend/backend.go
  - backend/containment_test.go
  - backend/dev_probe_test.go
  - backend/nap.go
  - backend/nap_config.go
  - backend/nap_conformance_test.go
  - backend/nap_intent.go
  - backend/nap_notify_test.go
  - backend/nap_outbox_test.go
  - backend/nap_scope_test.go
  - backend/nap_test.go
  - backend/napp.go
  - backend/preview_test.go
  - backend/registry_install.go
  - backend/registry_updates.go
  - backend/spec_conformance_test.go
  - backend/spec_pinned_test.go
  - backend/testdata/probe-napplet/index.html
  - backend/testdata/probe-napplet/metadata.json
  - backend/webview/embed.go
  - backend/webview/napplet-host.js
  - backend/webview/napplet_host_test.go
  - backend/webview/shim/README.md
  - backend/webview/shim_test.go
  - backend/window_instances.go
  - spec/CONFORMANCE.md
  - spec/pinned/README.md
findings:
  critical: 2
  warning: 7
  info: 5
  total: 14
status: issues_found
---

# Phase 1: Code Review Report

**Reviewed:** 2026-10-03T00:02:42Z
**Depth:** standard
**Files Reviewed:** 30
**Status:** issues_found

## Summary

The review covered the phase diff `6fdcbdd^..HEAD`, focusing on CRIT-01 path containment (`nappBaseDir`, `nappAssetPath` and their callers), the start and teardown of host-page sessions (`nap.start` / `nap.loaded` in `backend/nap.go`, `boot`/`enqueue` in `napplet-host.js`), the function-scoped prelude in `buildSrcdoc`, and intent delivery over INC (`dispatchToNapplet`). Upstream content guarded by hashes was skipped as instructed.

What holds up:
- **Containment.** The 64-hex directory name is fixed-width and cannot escape, and `nappAssetPath` rejects every escape shape tested. The shapes tried were `..`, absolute paths, `//`, `/.`, `a/..` and Windows volume or colon names via `filepath.IsLocal`.
- **Prelude scope.** It now leaves only `window.napplet` behind.
- **Host-page ordering.** Old frame removed, then `nap.start`, then the new frame. This is correct for the cases the node harness exercises.

Backend tests pass (`go test ./...`), and so does `go vet`.

The findings fall into three groups:

1. **Intent delivery moved onto the INC topic namespace.** Since this phase, a launcher-routed intent and an ordinary peer `inc.emit` reach the handler as the same envelope type on the same topic. The sender attestation that should tell them apart is a d tag the author chooses (`"launcher"`), or empty for root napplets.
2. **The generation guard and frame replacement leave races.**
   - A push or intent that passed the gen check can still land in the replacement document. The check in `napPushGen` and the send are not atomic, and the host page cannot filter by session.
   - Intent readiness is checked against `ci.actions` without any session generation.
3. **The hashed layout orphans every existing install with no fallback.** This was decided on purpose (D-02/D-04, "nothing is deployed"). But a `v0.0.0` tag and a public `scripts/install.sh` that pulls `releases/latest` both exist.

The new containment tests also add data races under `-race`.

## Critical Issues

### CR-01: Switching to hashed install dirs breaks every existing install with no fallback or migration

**File:** `backend/backend.go:131-143`, `backend/window_instances.go:502-530`, `backend/nap.go:491-498`, `backend/registry_install.go:84-89`
**Issue:** `nappBaseDir` now returns `{dataDir}/napps/{sha256(id)}`. Before, it was `{dataDir}/napps/{id}`. `state.InstalledNapps` still lists every napp installed before the upgrade, so the launcher keeps showing them as installed. But:
- `launchWithDocument` fails with `napp X is not installed`, or `napplet X is not installed` for napplets.
- `nappletDocument` fails the same way.
- `IconBlob` falls back to the network for every icon, every time `syncAppShortcuts` runs.
- `Uninstall` deletes only the (empty or missing) hashed directory, so the real files under `napps/{raw-id}` leak forever.

D-02 justifies this with "no migration is needed because nothing is deployed". That premise does not hold: the repo has a `v0.0.0` tag, CI attaches the APK to `v*` releases, and `scripts/install.sh:121` installs from `releases/latest/download`. Every upgrading user gets a launcher whose whole installed list fails to start, and the error message is misleading.
**Fix:** Two options; either one removes the breakage without sweeping unknown directories.
- Do a one-time, contained rename on load: for each id in `state.InstalledNapps`, the legacy path qualifies only if `filepath.IsLocal(id)` holds and it is a single path element (no separator, not `.`/`..`). Then rename `napps/{id}` to `napps/{hash}` when the hashed directory does not exist yet.
- Or, at minimum, treat "installed in state but hashed dir missing" as "needs reinstall": call `go Install(n)` and show that instead of "not installed".

```go
// in loadState / refreshInstalled, once:
for id := range state.InstalledNapps {
	if strings.ContainsAny(id, `/\`) || !filepath.IsLocal(id) {
		continue // hostile legacy ids are left alone, as D-04 wants
	}
	legacy := filepath.Join(dataDir, "napps", id)
	if dst, err := nappBaseDir(id); err == nil {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			_ = os.Rename(legacy, dst)
		}
	}
}
```

### CR-02: Launcher-routed intents can be forged by any napplet (the "launcher" sender is a d tag anyone can use)

**File:** `backend/window_instances.go:805-811, 1120`, `backend/nap_inc.go:96-118`
**Issue:** This phase changed intent delivery from `intent.deliver`, a type only Go could produce, to a plain `inc.event` on the convention topic. Any napplet can produce that same envelope with `inc.emit` on `napplet:<archetype>/<action>`. `incPublish` then delivers it to every napplet subscribed to that topic. That reaches exactly the handler napplets, because their readiness signal is that very subscription. The handler therefore cannot tell an intent the launcher resolved (rule-checked and user-approved) from a peer broadcast, except by `sender`. `sender` cannot be trusted either:
- For launcher-originated intents (`OpenUserProfile`, the tray, `runNappAction` with a nil caller), `sender` is the literal `"launcher"`.
- d tags are author-chosen and unvalidated. Any author can publish a napplet with `d = "launcher"`. Its `inc.emit("napplet:profile/open", …)` then reaches every profile handler as `{type:"inc.event", topic:"napplet:profile/open", sender:"launcher"}`. That is byte for byte what the launcher sends.

NAP-INC requires `sender` to be "runtime-attested", and "launcher" is not a dTag at all. This is a sender-attestation forgery that the phase introduced, and it bypasses the `PermDispatch` rules and the handler chooser entirely.
**Fix:**
- Make the launcher sender a value no d tag can take, or refuse such d tags at parse time.
- Stop accepting peer `inc.emit` on reserved intent convention topics, so only the launcher's resolved delivery uses them.

```go
// nap_inc.go napIncEmit
if _, _, ok := conventionParts(r.Topic); ok {
	return // napplet:<archetype>/<action> is delivered only by intent resolution
}
```

Also record a CONFORMANCE conflict row for the reserved topic, and use `incSender(caller)` for napplet callers (see WR-03).

## Warnings

### WR-01: `napPushGen` checks the generation, then sends without holding the lock, so a stale session's push can reach the replacement frame

**File:** `backend/nap.go:255-275`, `backend/webview/napplet-host.js:146-164`
**Issue:**
- `napPushGen` reads `gen == ci.nap.gen && established` under `nap.mu`, then releases the lock, marshals, and calls `ci.eval(...)`. A goroutine from session N can pass the check, then get preempted. `napStart` then runs, its response goes out, and the host page creates the new frame. Only after that does the session-N eval get written.
- The host page's `__nap_push` delivers to whatever `frame` is current, and it has no idea which session a push belongs to. So relay events, resource bytes and identity answers from the old document's requests reach the new document.
- This breaks the D-07 claim in `napStart`'s comment ("a late answer for the old session is dropped by napPushGen").
- The same holds on Android, where `SendToWindow` and RPC replies are not ordered with respect to each other.

**Fix:**
- Have `nap.start` return the new `gen`, keep it in `napplet-host.js`, and tag every push: `window.__nap_push(gen, json)`. The host page drops any push whose gen is not current.
- Alternatively, hold `nap.mu` across `ci.send` if the transport's send is a non-blocking enqueue.

### WR-02: Intent readiness is not tied to a session, so intents get lost while the dispatch reports success

**File:** `backend/window_instances.go:1106-1124`
**Issue:** `dispatchToNapplet` waits on `ci.waitForHandler(req.name)`, which looks only at `ci.actions`, then pushes with `ci.napPush`, which uses whatever the current gen is. Two cases lose the payload while the function returns `nil, nil`, and the caller has already received `ok:true, handled:true`:
- `nap.start` (a dev reload, or a host-page reload) can run between the wait and the push. The event then goes to a new document that has not subscribed yet, and the shim drops it.
- The napplet reloads its own frame. The session persists, and so do `ci.actions` and `s.topics` (see WR-05). The handler counts as "ready" at once, and the event reaches a document whose scripts have not called `inc.on`.

**Fix:** Do the readiness check and the push against one generation. Capture `gen` while holding `nap.mu` together with `s.topics[req.name]`, push with `napPushGen(gen, ev)`, and wait again if the session changed:

```go
for {
	if _, ok := ci.waitForHandler(waitCtx, req.name); !ok { return nil, errNoHandler… }
	s := ci.nap
	s.mu.Lock()
	gen, live := s.gen, s.established && s.topics[req.name]
	s.mu.Unlock()
	if live && ci.napPushGenOK(gen, ev) { break } // napPushGen variant that reports delivery
}
```

### WR-03: An intent from a root napplet carries an empty `sender`

**File:** `backend/window_instances.go:810`, `backend/window_instances.go:1120`
**Issue:** `req.sender = caller.napp.D`. A root napplet (kind 15129) has `D == ""`, so the handler gets `inc.event` with `sender: ""`. NAP-INC makes `sender` required. Verdana's own INC path already handles this case: `incSender` falls back to `Address()`. Now that intents travel as INC events, the two paths name the same caller differently.
**Fix:** `req.sender = incSender(caller)` when `caller.nap != nil`, and keep the d tag (or address) for napp callers.

### WR-04: Lifecycle RPCs share the napplet's `MAX_PENDING` budget, so a flooding napplet can make `nap.start` fail

**File:** `backend/webview/napplet-host.js:170-183, 296-301, 311`
**Issue:**
- `nap.start` and `nap.loaded` go through the same `enqueue` that rejects once `pending >= 256`.
- If the frame has 256 envelopes in flight when a boot happens (a dev reload, or the Android WebView recreating the host page), `enqueue(() => rpc("nap.start"))` rejects at once. `showBootError` then wipes the body, and nothing retries: the window stays dead until the user reopens it.
- `nap.loaded` is dropped silently the same way, so `notify.controls` is never sent for that session.

The bound exists to limit the napplet. The host page's own lifecycle calls should not count against it.
**Fix:** Give `enqueue` a `trusted` flag that skips the bound (it still chains on `outbound` for ordering), and use it for `nap.start` and `nap.loaded`.

### WR-05: A frame that reloads or navigates itself keeps the live session and its intent topics

**File:** `backend/webview/napplet-host.js:273-275, 310-312`, `backend/nap.go:413-427`
**Issue:** The frame's `contentWindow` stays the same WindowProxy across navigations, so a document the napplet navigates to still passes `event.source === frame.contentWindow`, keeps the session, its grants, `s.topics` and `ci.actions`. That holds even for an `https:` page with no CSP and full network access. On desktop the host page's `navigate-to 'self'` CSP does nothing, and Android allows sub-frame navigation (`NappWebView.kt:102`). This is tracked as 5D-3 / NIP-5D-reload for Phase 4. It is listed here because it interacts with code this phase added:
- `nap.loaded` fires again on the new document, but `controlsSent` suppresses the push, so a reloaded document never gets `notify.controls`.
- WR-02's intent readiness counts as satisfied for a document that never subscribed.

**Fix:** The Phase 4 plan stands. Until then, have the `load` listener tell Go about every load after the first (`nap.loaded` with a counter), so Go can at least clear `controlsSent` and the topic registrations.

### WR-06: The new containment tests swap globals that background goroutines read, causing data races

**File:** `backend/containment_test.go:255-261, 391-395`, `backend/nap_test.go:120` (via `newContainmentRig`)
**Issue:** `go test -race -run 'NappBaseDirIsHashed|HostileDTagStaysInside' .` fails with three `DATA RACE` reports:
- `InstallNapp`/`Uninstall` call `refreshInstalled`, which starts `go syncAppShortcuts()`. That goroutine reads `host` (`app_shortcuts.go:51`) and, through `IconBlob`, reads `dataDir`.
- At the same time, the test assigns `host = h` (lines 393/395), and the next subtest's `setupNapTest` and `TestNappBaseDirIsHashedAndContained` assign `dataDir`.

CI does not run `-race` today, but these tests can see the wrong data directory or host. A leftover shortcut sync from a previous subtest can also read the next subtest's state.
**Fix:**
- Wait for the shortcut sync in the rig, for example by taking `appShortcutSyncMu` in a `t.Cleanup` before restoring globals.
- Or inject the host through `previewTestHost` before any install, instead of swapping it mid-test.
- In `TestNappBaseDirIsHashedAndContained`, call a pure helper that takes `dataDir` as an argument rather than mutating the global.

### WR-07: CRIT-01 and W-1 claim one choke point, but storage and config filenames still normalize the id and can collide

**File:** `backend/window_storage.go:35-55` (used by the test at `backend/containment_test.go:410-414`), `backend/napconfig/store.go:53, 233-249`, `spec/CONFORMANCE.md:47, 125`
**Issue:** `safeFileName` maps every character outside `[A-Za-z0-9._~-]` to `_`. So `pk~a/b`, `pk~a_b` and `pk~a b` share one localStorage file and one config file. On case-insensitive filesystems (the macOS and Windows defaults), `pk~App` and `pk~app` collide as well. The scope is the same author only, because of the pk16 prefix.

The W-1 row in CONFORMANCE.md is marked "fixed" and says `d` "stays untouched in the id, state, storage keys", and the `nappBaseDir` comment calls it "the one place". But the persisted per-napp files are named by a lossy mapping. 01-RESEARCH defers this to Phase 5 CF-2, and the checklist does not say so.
**Fix:**
- Name the storage and config files `hex(sha256(id)).json`, with the same rule as `nappBaseDir`.
- Or narrow the W-1/CRIT-01 rows and add an open row owned by Phase 5 for the storage/config collision.

## Info

### IN-01: The `nap.reset` RPC has no caller left

**File:** `backend/nap.go:296-298`, `backend/nap.go:429-438`
**Issue:** `napplet-host.js` no longer calls `nap.reset`; `DevReload` calls `ci.napReset()` directly. The RPC case is dead, but it is still a lifecycle entry point that the host page can reach.
**Fix:** Remove the `case "nap.reset"` branch, and the mentions of it in the comments at lines 60-62 and 280-283.

### IN-02: The `</script` escape in `buildSrcdoc` is case-sensitive

**File:** `backend/nap.go:557-559`
**Issue:** HTML ends a script element at `</script` matched case-insensitively. The guard only rewrites lowercase. Today's prelude is pinned by hash, so there is no live bug, but the guard does not hold up for "a future one", which is the case its comment says it protects against.
**Fix:** Use a case-insensitive replace, e.g. `regexp.MustCompile("(?i)</script").ReplaceAllString(p, `<\/script`)`, or fail the build if the prelude contains `</script` in any case.

### IN-03: Pinned-snapshot integrity is self-referential, and the README hash column is never checked

**File:** `backend/spec_pinned_test.go:100-157`
**Issue:** `body_sha256` lives in the same file it verifies, so an edit that updates both passes. The README's `body sha256` column is never compared against the front matter either.
**Fix:**
- Also compare each README row's hash with the front-matter value.
- Optionally record the upstream git blob SHA-1 and check `sha1("blob <len>\x00" + body)`, which pins the text to upstream rather than to itself.

### IN-04: `Start` does not make `DataDir` absolute, so a relative path silently disables every install

**File:** `backend/backend.go:65-68`, `backend/backend.go:131-134`
**Issue:** `nappBaseDir` refuses a non-absolute `dataDir`, but `Start` accepts one without complaint. A GUI or CLI that passes a relative path gets a backend that starts but fails every install, launch and update.
**Fix:** In `Start`, run `dataDir, err = filepath.Abs(opts.DataDir)` and return an error if that fails.

### IN-05: The "nothing was pushed" assertions use fixed sleeps

**File:** `backend/nap_test.go` (TestNapSessionStartsFromHostPage, TestNapFrameShellReadyIsIgnored, TestIntentDeliveryToNapplet, TestIntentDeliveryReachesOnlyTheHandler)
**Issue:** Negative checks after `time.Sleep(50 * time.Millisecond)` pass trivially if the worker is slow, so a regression could slip through unnoticed. They cannot fail spuriously.
**Fix:** Before asserting absence, post a sync envelope (as `TestNapFrameShellReadyIsIgnored/after nap.start` already does with `storage.keys`) and wait for its answer.

---

_Reviewed: 2026-10-03T00:02:42Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
