# Phase 1: Containment Fix and Canonical Shim Baseline - Context

**Gathered:** 2026-10-02
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 1 delivers:
- **CRIT-01, shipped as the first plan.** No napp (35130) or napplet (NIP-5D or WEB-NAPPLET shape) `d` tag can read, write or remove outside the data directory.
- **Shim.** `@napplet/shim` 0.30.0 is vendored byte-identical, with a hash test (SHIM-01). The launcher stops depending on the NAP-SHELL handshake (SHIM-03). `NappletShimPrelude` is scoped out of the frame (SHIM-04). A test checks shim request types against an offline `@napplet/conformance` 0.17.0 envelope fixture (SHIM-05). Every dropped former-patch behavior is accounted for (SHIM-02).
- **Specs.** Every pinned spec text is committed at its SHA (SPEC-01). An audit checklist skeleton is created, with its Conflicts section (SPEC-03).
- **CI.** Android AAR breakage fails CI on pull requests (SPEC-05).

Not in this phase:
- Checklist close-out (SPEC-02) and the `NAPPLETS.md` rewrite (SPEC-04). Both are Phase 8.
- The dispatcher (Phase 2).
- Frame reload/navigation hardening (Phase 4).
- Id/storage rekeying (Phase 5).

**Desktop is the target.** Android only has to keep compiling (`just apk` / the AAR bind). Android napplet runtime behavior is not a Phase 1 concern.

</domain>

<decisions>
## Implementation Decisions

### Napp directory naming (CRIT-01)
- **D-01:** Install directory names are `hex(sha256(id))` under `<dataDir>/napps/`. The raw id and `d` are never path segments. `d` itself is never normalized: identity, storage keys and wire messages keep the raw value.
- **D-02:** Phase 1 hashes **today's id string** (`<pk16>~<d>` for napps, `napplet~<pk16>~<d>` for napplets). Phase 5 (KEY-03/04) is free to change what goes into the hash. No migration is needed because nothing is deployed.
- **D-03:** **One choke point.** `nappBaseDir` (`backend/backend.go:106`) becomes the single place that builds a napp directory. It returns a validated path or an error, checked with `filepath.Rel` containment against `<dataDir>/napps`. Every caller goes through it: install, failed-install `RemoveAll`, uninstall, update, launch, the srcdoc `index.html` read and asset paths. Asset sub-paths (`backend/napp.go:190`) get their own `filepath.Rel` containment check against the base. — **Reversibility:** costly — the signature changes ripple through 6+ call sites, and the exported `NappBaseDir` is used by GUIs.
- **D-04:** Every install path uses the hashed name, dev folder napps included. Old `napps/<raw-id>` dirs on dev machines are simply orphaned. There is **no** startup sweep or `RemoveAll` of non-hash dirs.
- **D-05:** Regression tests cover `d` = `..`, `../../..`, `a/b`, `/../x` for napps and both napplet shapes, across install, launch, update, uninstall and failed install. Every case must stay inside the data dir, and `d` must be unchanged in the id, storage keys and wire.

### Session start without the NAP-SHELL handshake (SHIM-03, SHIM-04)
- **D-06:** The **host page** (`backend/webview/napplet-host.js`, trusted, outside the frame) tells Go that a session starts. The frame cannot forge or replay that signal. `shell.ready` from the frame is no longer meaningful, and the current `napDispatch` `shell.ready` branch (`backend/nap.go:344`) is replaced.
- **D-07:** The session starts **when the host page creates the iframe or assigns srcdoc**, before any napplet code runs. The host page queues frame envelopes until Go acknowledges the new session, so no early request is lost. The iframe `load` event is kept only for detecting unexpected loads later (Phase 4, SBOX-01, builds on this hook).
- **D-08:** **Drop `shell.init` entirely.** Domains are conveyed only by which objects `install({domains})` places on `window.napplet` (NIP-5D presence detection). The NAP-SHELL "every runtime MUST implement" conflict is recorded in the checklist Conflicts section. The `notify.controls` push that was bundled with `shell.init` must still reach the napplet at session start. The planner decides the trigger.
- **D-09:** `buildSrcdoc` (`backend/nap.go` ~line 483-512) inlines the **unmodified** prelude bytes inside a function scope, `(function(){ <prelude> ; NappletShimPrelude.install({domains}) })()`. The vendored file stays byte-identical, and the hash test is on the file. A regression test asserts that nothing but `window.napplet` survives in the frame (`typeof NappletShimPrelude === "undefined"`, and no extra domains can be installed). The researcher should still confirm that the prelude has no other top-level globals and doesn't rely on top-level `var` semantics.

### Dropped shim patches (SHIM-02)
- **D-10:** **Accept upstream's 30s per-request shim timeout** (`REQUEST_TIMEOUT_MS = 30_000` in `napplet/web` `packages/nap/src/*/shim.ts`). Go still answers late, and the shim ignores the late reply. A permission prompt still open after the napplet's request has timed out gets cancelled and treated as dismissed, so the user never approves an action whose result the napplet has given up on. This is recorded in the checklist.
- **D-11:** The prompt cancellation in D-10 is **implemented in Phase 2** inside the bounded prompt queue (DISP-04). Phase 1 only records the decision and the checklist row.
- **D-12:** Phase 1 adds **one checklist row per dropped former-patch behavior**: no-deadline requests, PR #91 intent delivery, the NAP-SHELL global/`shell.init`, INC query validation, NOTIFY control replay, and resource legacy-string `bytesMany`. The Go/host replacements land in their domain phases: INC query validation in Go (Phase 6), the resource legacy-string/server-hint shape accepted in Go (Phase 7), notify control replay (Phase 8). Phase 1 fixes only breakage that would be immediately visible in existing napplets.
- **D-13:** Verification for "existing napplets keep working" (success criterion 3) uses Go wire-shape tests, **plus** a short manual smoke list of real napplets launched under `just run` before the phase closes. The researcher or planner picks candidate napplets that use intent, config, notify, resource and INC (e.g. from `~/Projects/napplet-soy`, `~/Projects/napplet-portal`).

### Spec snapshots, checklist, and CI (SPEC-01, SPEC-03, SPEC-05)
- **D-14:** A new top-level **`spec/`** dir. Snapshots go in `spec/pinned/<spec>@<sha8>.md` and the checklist in `spec/CONFORMANCE.md`. It is a lasting, public project artifact, not under `.planning/`. — **Reversibility:** reversible
- **D-15:** Snapshots contain the **spec .md text only**, with a front-matter header (repo, ref, full commit SHA, fetch date). PR discussion is not archived. The `@napplet/conformance` 0.17.0 `ENVELOPE_SPECS` fixture goes to `backend/testdata/` (per `.planning/research/STACK.md`), with its version and SHA recorded.
- **D-16:** The checklist uses **Markdown tables, one section per spec**, with the pinned SHA in the section header. Columns: ID | requirement quote | MUST/SHOULD | status (conforming/fixed/N/A/open) | reason | code ref. Phase 1 creates the skeleton: the sections with SHAs, the Conflicts section with the four required readings (NAP-SHELL vs NIP-5D presence detection; WEB-NAPPLET legacy 35129 vs NIP-5D; NAP-RELAY decrypt vs NIP-5D cleartext; NAP-OUTBOX kind-1059 example), the dropped-shim-patch rows and the timeout decision.
- **D-17:** Android CI on pull requests runs **only the gomobile AAR bind**, path-filtered to `backend/**`, `android/**` and the workflow file itself. The full APK build stays on `workflow_dispatch` and tags. It can be a separate job or workflow, at the planner's discretion.

### Claude's Discretion
- The exact host-page ↔ Go message name and shape for "new session" and "ack", and the bound on the pre-ack envelope queue.
- The trigger for the `notify.controls` push once `shell.init` is gone.
- The test layout for the shim hash test (`ShimVersion` and README must state exactly 0.30.0 and the sha256) and for `TestNAPHandlersCoverReferenceEnvelopes`, including its explicit N/A list.
- How the conformance fixture is regenerated (a one-off script in a scratch dir, never run in CI).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Pins and decisions
- `.planning/research/SPEC-PINS.md` — every pinned spec SHA, and the decisions on the canonical shim, NAP-SHELL, NAP-INTENT, NAP-RESOURCE and ciphertext (these override SUMMARY.md where they conflict)
- `.planning/REQUIREMENTS.md` — CRIT-01, SPEC-01/03/05, SHIM-01..05
- `.planning/ROADMAP.md` §Phase 1 — goal and the five success criteria

### Research
- `.planning/research/FEATURES.md` — gap W-1 (headline 1 and the table row: affected call sites), 5D-1 (prelude global leak)
- `.planning/research/STACK.md` — shim 0.29.2→0.30.0 diff notes, the list of six patch groups, the conformance fixture plan (`TestNAPHandlersCoverReferenceEnvelopes`), and why the conformance CLI/Playwright is not used
- `.planning/research/PITFALLS.md` — shim patch drift, reload/session pitfalls, and the "MUST respond to every request" hang risk
- `.planning/research/SUMMARY.md` — Phase A ordering rationale

### Specs (local checkouts; snapshot at pinned SHAs)
- `~/Projects/nips` — NIP-5D (`5D.md`) at PR #2303 head `24711d9`
- `~/Projects/naps` — NAP-* specs at the SHAs in SPEC-PINS.md; WEB-NAPPLET from `hzrd149/naps` `web-napplet-event` @ `7ae5b19`
- `~/Projects/napplet` — upstream `napplet/web` (shim source, `packages/nap/src/*/shim.ts`, `packages/shim/src/runtime-guard.ts`). The pinned commit is `956135b`, but the local checkout HEAD differs, so check out the pin before reading.

### Existing code docs
- `backend/webview/shim/README.md` — the current (stale) patch list and sha256. To be rewritten for pristine 0.30.0
- `NAPPLETS.md` — will be stale until Phase 8. Don't treat it as truth

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `backend/backend.go:106` `nappBaseDir` / exported `NappBaseDir`: the choke point to harden
- `backend/napplet.go:121` `nappletID`, `backend/registry_discovery.go:143` `nappFromNappEvent`: where ids (with raw `d`) are formed. Leave them unchanged
- `safeNappletPath` (NIP-5D `path` validation): existing per-asset path check that the base containment check complements
- `backend/webview/embed.go`: `//go:embed` of the prelude and `ShimVersion`

### Established Patterns
- Session lifecycle in `backend/nap.go`: `napWorker`/`napDispatch`, `napReady` (established flag, `s.gen` generation, `close(s.ready)`), `napReset`/`napTeardownLocked`. The new host-signalled start replaces `napReady`'s trigger but should keep the generation model, so stale calls from an old document are dropped
- `backend/window_instances.go:1091`: the intent runtime waits for `shell.ready`, so it must be retargeted to the new session-ready signal
- `napplet-host.js` already enforces envelope size limits and preserves order (`MAX_ENVELOPE`, around lines 175-181) and recreates the iframe on `__nap_reload` (around line 261-281)
- Tests: Go `testing`, beside the code (`backend/nap_test.go`, `backend/preview_test.go` already touch `nappBaseDir`), fixtures in `backend/testdata/`

### Integration Points
- `nappBaseDir` callers: `backend/registry_install.go:48,79`, `backend/registry_updates.go:143`, `backend/napp.go:190`, `backend/nap.go:458`, `backend/window_instances.go:502`, plus the desktop/mobile callers of `NappBaseDir`
- CI: `.github/workflows/android.yml` (currently `workflow_dispatch` only), `.github/workflows/desktop.yml`

</code_context>

<specifics>
## Specific Ideas

- The user is explicit that **desktop is the target, not Android**. Android checks stay minimal: keep it compiling.
- Hashing for directory names is preferred over readable names. Debuggability is not a priority here.

</specifics>

<deferred>
## Deferred Ideas

- Cancelling prompts that outlive the 30s shim timeout: decided here (D-10), implemented in Phase 2 with the bounded prompt queue.
- Go-side replacements for dropped shim patches: INC query validation in Phase 6, resource legacy-string/server-hint tolerance in Phase 7, notify control replay check in Phase 8.
- Final id scheme (full pubkey, distinct root namespace): Phase 5. The hash input may change then.
- Android napplet session-start behavior and runtime parity: out of scope (desktop first).

</deferred>

---

*Phase: 01-containment-fix-and-canonical-shim-baseline*
*Context gathered: 2026-10-02*
