# Phase 1: Containment Fix and Canonical Shim Baseline - Research

**Researched:** 2026-10-02
**Domain:** Go filesystem containment, vendored JS shim provenance, napplet session lifecycle (Go + host page), spec snapshotting, GitHub Actions
**Confidence:** HIGH (nearly every claim was checked against a primary artifact: the repo at `40adf21`, npm tarballs with checked integrity, or the pinned git objects in `~/Projects/{naps,nips,napplet}`)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Napp directory naming (CRIT-01)
- **D-01:** Install directory names are `hex(sha256(id))` under `<dataDir>/napps/`. The raw id and `d` are never path segments. `d` itself is never normalized: identity, storage keys and wire messages keep the raw value.
- **D-02:** Phase 1 hashes **today's id string** (`<pk16>~<d>` for napps, `napplet~<pk16>~<d>` for napplets). Phase 5 (KEY-03/04) is free to change what goes into the hash. No migration is needed because nothing is deployed.
- **D-03:** **One choke point.** `nappBaseDir` (`backend/backend.go:106`) becomes the single place that builds a napp directory. It returns a validated path or an error, checked with `filepath.Rel` containment against `<dataDir>/napps`. Every caller goes through it: install, failed-install `RemoveAll`, uninstall, update, launch, the srcdoc `index.html` read and asset paths. Asset sub-paths (`backend/napp.go:190`) get their own `filepath.Rel` containment check against the base. — **Reversibility:** costly — the signature changes ripple through 6+ call sites, and the exported `NappBaseDir` is used by GUIs.
- **D-04:** Every install path uses the hashed name, dev folder napps included. Old `napps/<raw-id>` dirs on dev machines are simply orphaned. There is **no** startup sweep or `RemoveAll` of non-hash dirs.
- **D-05:** Regression tests cover `d` = `..`, `../../..`, `a/b`, `/../x` for napps and both napplet shapes, across install, launch, update, uninstall and failed install. Every case must stay inside the data dir, and `d` must be unchanged in the id, storage keys and wire.

#### Session start without the NAP-SHELL handshake (SHIM-03, SHIM-04)
- **D-06:** The **host page** (`backend/webview/napplet-host.js`, trusted, outside the frame) tells Go that a session starts. The frame cannot forge or replay that signal. `shell.ready` from the frame is no longer meaningful, and the current `napDispatch` `shell.ready` branch (`backend/nap.go:344`) is replaced.
- **D-07:** The session starts **when the host page creates the iframe or assigns srcdoc**, before any napplet code runs. The host page queues frame envelopes until Go acknowledges the new session, so no early request is lost. The iframe `load` event is kept only for detecting unexpected loads later (Phase 4, SBOX-01, builds on this hook).
- **D-08:** **Drop `shell.init` entirely.** Domains are conveyed only by which objects `install({domains})` places on `window.napplet` (NIP-5D presence detection). The NAP-SHELL "every runtime MUST implement" conflict is recorded in the checklist Conflicts section. The `notify.controls` push that was bundled with `shell.init` must still reach the napplet at session start. The planner decides the trigger.
- **D-09:** `buildSrcdoc` (`backend/nap.go` ~line 483-512) inlines the **unmodified** prelude bytes inside a function scope, `(function(){ <prelude> ; NappletShimPrelude.install({domains}) })()`. The vendored file stays byte-identical, and the hash test is on the file. A regression test asserts that nothing but `window.napplet` survives in the frame (`typeof NappletShimPrelude === "undefined"`, and no extra domains can be installed). The researcher should still confirm that the prelude has no other top-level globals and doesn't rely on top-level `var` semantics.

#### Dropped shim patches (SHIM-02)
- **D-10:** **Accept upstream's 30s per-request shim timeout** (`REQUEST_TIMEOUT_MS = 30_000` in `napplet/web` `packages/nap/src/*/shim.ts`). Go still answers late, and the shim ignores the late reply. A permission prompt still open after the napplet's request has timed out gets cancelled and treated as dismissed, so the user never approves an action whose result the napplet has given up on. This is recorded in the checklist.
- **D-11:** The prompt cancellation in D-10 is **implemented in Phase 2** inside the bounded prompt queue (DISP-04). Phase 1 only records the decision and the checklist row.
- **D-12:** Phase 1 adds **one checklist row per dropped former-patch behavior**: no-deadline requests, PR #91 intent delivery, the NAP-SHELL global/`shell.init`, INC query validation, NOTIFY control replay, and resource legacy-string `bytesMany`. The Go/host replacements land in their domain phases: INC query validation in Go (Phase 6), the resource legacy-string/server-hint shape accepted in Go (Phase 7), notify control replay (Phase 8). Phase 1 fixes only breakage that would be immediately visible in existing napplets.
- **D-13:** Verification for "existing napplets keep working" (success criterion 3) uses Go wire-shape tests, **plus** a short manual smoke list of real napplets launched under `just run` before the phase closes. The researcher or planner picks candidate napplets that use intent, config, notify, resource and INC (e.g. from `~/Projects/napplet-soy`, `~/Projects/napplet-portal`).

#### Spec snapshots, checklist, and CI (SPEC-01, SPEC-03, SPEC-05)
- **D-14:** A new top-level **`spec/`** dir. Snapshots go in `spec/pinned/<spec>@<sha8>.md` and the checklist in `spec/CONFORMANCE.md`. It is a lasting, public project artifact, not under `.planning/`. — **Reversibility:** reversible
- **D-15:** Snapshots contain the **spec .md text only**, with a front-matter header (repo, ref, full commit SHA, fetch date). PR discussion is not archived. The `@napplet/conformance` 0.17.0 `ENVELOPE_SPECS` fixture goes to `backend/testdata/` (per `.planning/research/STACK.md`), with its version and SHA recorded.
- **D-16:** The checklist uses **Markdown tables, one section per spec**, with the pinned SHA in the section header. Columns: ID | requirement quote | MUST/SHOULD | status (conforming/fixed/N/A/open) | reason | code ref. Phase 1 creates the skeleton: the sections with SHAs, the Conflicts section with the four required readings (NAP-SHELL vs NIP-5D presence detection; WEB-NAPPLET legacy 35129 vs NIP-5D; NAP-RELAY decrypt vs NIP-5D cleartext; NAP-OUTBOX kind-1059 example), the dropped-shim-patch rows and the timeout decision.
- **D-17:** Android CI on pull requests runs **only the gomobile AAR bind**, path-filtered to `backend/**`, `android/**` and the workflow file itself. The full APK build stays on `workflow_dispatch` and tags. It can be a separate job or workflow, at the planner's discretion.

### Claude's Discretion
- The exact host-page ↔ Go message name and shape for "new session" and "ack", and the bound on the pre-ack envelope queue.
- The trigger for the `notify.controls` push once `shell.init` is gone.
- The test layout for the shim hash test (`ShimVersion` and README must state exactly 0.30.0 and the sha256) and for `TestNAPHandlersCoverReferenceEnvelopes`, including its explicit N/A list.
- How the conformance fixture is regenerated (a one-off script in a scratch dir, never run in CI).

### Deferred Ideas (OUT OF SCOPE)
- Cancelling prompts that outlive the 30s shim timeout: decided here (D-10), implemented in Phase 2 with the bounded prompt queue.
- Go-side replacements for dropped shim patches: INC query validation in Phase 6, resource legacy-string/server-hint tolerance in Phase 7, notify control replay check in Phase 8.
- Final id scheme (full pubkey, distinct root namespace): Phase 5. The hash input may change then.
- Android napplet session-start behavior and runtime parity: out of scope (desktop first).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CRIT-01 | `d` with separators or `..` installs/launches/updates/uninstalls inside the data dir; raw `d` never a path segment; `d` not normalized; containment check on every napp dir | §Containment: exact escape targets for each test `d` (Pitfall 1), full caller inventory (7 sites, no GUI callers of `NappBaseDir`), `filepath.IsLocal`/`Rel` pattern, test rig (httptest blossom + `previewTestHost`) |
| SPEC-01 | Every pinned spec text snapshotted at its pinned SHA | §Spec snapshots: 18 files, exact `git show` sources, body sha256 per file, all SHAs verified present locally |
| SPEC-03 | Checklist Conflicts section with the four named readings | §Checklist skeleton: verbatim spec quotes for all four conflicts plus the chosen reading from PROJECT.md Key Decisions / FEATURES A1-A3 |
| SPEC-05 | Android CI on PRs | §CI: workflow shape, path-filter pitfall with required checks, permissions, local proxy check |
| SHIM-01 | Vendored prelude byte-identical to npm 0.30.0, sha256 test, `ShimVersion` + README state it | Verified sha256 `25d6bb0e…0753`, 136157 bytes, **no trailing newline**, npm sha512 integrity; fetch procedure; `.gitattributes` need |
| SHIM-02 | Six former patches dropped or reimplemented, each noted in the checklist | Byte diff of vendored file vs pristine 0.29.2 found **seven** behavior groups, not six (config.get schemaError correlation is missing from the README and D-12). Per-patch impact and replacement table |
| SHIM-03 | Presence detection; no `shell.ready`/`shell.init` dependency | Host-page `nap.start` design on the existing `outbound` chain + generation bump; intent delivery must move to an INC topic event (NAP-INTENT master) or handler napplets stop receiving intents |
| SHIM-04 | Only `window.napplet` survives injection | Prelude top level is exactly `"use strict"; var NappletShimPrelude = (() => {…})();` + a trailing `//# sourceMappingURL` line comment. Wrapper verified under node: only `napplet` is a new global; unwrapped form lets `install({domains:["keys","dm"]})` add domains |
| SHIM-05 | Test that every shim-sendable request type has a handler or N/A entry | Fixture generated and cross-checked against pinned source (237 types, 114 out). Go covers all 67 out types in `napDomains`; 9 whole domains are N/A; `media.command` is bidirectional and absent from the fixture's `out` set |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

Directives from `./CLAUDE.md` and `./.claude/CLAUDE.md` that plans must honor:

- **Go formatting:** `gofmt` (tabs), `MixedCaps`, short lowercase packages. Keep `go vet` clean.
- **File placement:** new backend code goes in the root package under the matching prefix (`nap_*.go`, `registry_*.go`, `window_*.go`, …), not a new subpackage. Platform code uses OS suffix files.
- **No JS toolchain:** plain JS in `backend/webview/`, two-space indent, no semicolons, IIFE `;(() => { … })()`. Do not add a bundler or npm build step. The shim is vendored byte-identical to upstream.
- **Errors:** wrap with `fmt.Errorf("…: %w", err)`, lowercase human-readable messages. NAP handlers return machine-readable codes; any new async NAP handler uses `c.async(...)`, never a bare `go`.
- **Logging:** zerolog chaining, lowercase messages, no trailing punctuation; tests use `zerolog.Nop()`.
- **Comments:** explain why and the contract, in prose; section dividers `// ─── name ───` (Go) and `// ── name ──` (JS).
- **Tests:** standard `testing`, `TestBehavior` names, beside the code; fixtures in `backend/testdata/`. Changes to parsing, permissions, storage, networking or napplet lifecycle need focused regression tests.
- **Verification before merge:** `cd backend && go test ./...` and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`.
- **Android:** shared backend changes must keep `just apk` building. `GOOS=android` implies the `linux` build tag. Add new host capabilities as optional interfaces.
- **Commits:** concise, imperative, mostly lowercase subjects; commit significant changes; never commit binaries/APKs/AARs/`desktop/dist/`.
- **Workflow:** file edits go through a GSD command (`/gsd-execute-phase` for this work).

## Summary

The containment bug is worse than the phase text suggests. With today's `nappBaseDir`, `d="../../.."` resolves `<dataDir>/napps/<pk16>~../../..` to **`<dataDir>` itself**, so a failed install or an uninstall runs `os.RemoveAll(dataDir)` and wipes `state.json`, the keys and the event store. `d="/../x"` lands in `<dataDir>/napps/x` (a sibling napp's namespace), `a/b` nests under napp `<pk16>~a` (so uninstalling `a` deletes it), and `/../../../../tmp/evil` leaves the data dir entirely [VERIFIED: `go run` of `filepath.Join` in scratch, output in Pitfall 1]. The fix is mechanical. Seven call sites use `nappBaseDir`, all inside `backend/`. The exported `NappBaseDir` has **no** callers in `desktop/`, `backend/mobile/` or Kotlin, so changing its signature (or deleting it) ripples nowhere outside the backend [VERIFIED: repo-wide grep]. GUIs get the directory only through `WindowSpec.Dir`.

The shim work hides one large behavioral dependency that D-12 does not list. The upstream 0.30.0 intent shim has **no** `intent.deliver`/`onDelivery`. Go's `dispatchToNapplet` (`window_instances.go:1089-1123`) currently pushes `intent.deliver` after `shell.ready`, so with the pristine shim every handler napplet silently stops receiving intents. Pinned NAP-INTENT master says delivery uses "the named convention's ordinary delivery mechanism — typically an INC topic event". Verdana delivered exactly that way before commit `bf29b5d`: wait until the target `inc.subscribe`s to the convention topic (`waitForHandler`, which still exists), then push `inc.event{topic, sender, payload}`. Restoring that is the Phase 1 fix that keeps intent napplets working. It also removes the intent runtime's dependency on any session-ready signal. The byte diff also turned up a **seventh** patch that neither the shim README nor D-12 names: a `config.schemaError` carrying a pending `config.get`'s `id` rejects that get. It needs its own checklist row.

The function-scope wrapper (D-09) is sound. The pristine prelude's top level is exactly two statements (`"use strict";` and `var NappletShimPrelude = (() => {…})();`), followed by a `//# sourceMappingURL=…` **line comment with no trailing newline**. The wrapper must therefore put a newline after the prelude, or the closing `})()` is commented out. Under node, the wrapped activation leaves exactly one new global (`napplet`) whose keys are exactly the 14 `napDomains`. The unwrapped form leaks `NappletShimPrelude`, and calling `install({domains:["keys","dm"]})` on it really does install extra domains [VERIFIED: node vm run in scratch].

**Primary recommendation:** Ship in this order: (1) CRIT-01 choke point + tests; (2) pristine shim + function-scope srcdoc + host-page `nap.start` session start (generation bump, fresh iframe per session) + INC-topic intent delivery + a `notify.controls` push on frame load; (3) the conformance-fixture coverage test; and in parallel (4) the `spec/` snapshots + `CONFORMANCE.md` skeleton and (5) the Android AAR-bind PR job.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Napp directory naming + containment | Backend (Go, `backend/backend.go`) | — | The backend is the only writer of `<dataDir>/napps`; GUIs receive `WindowSpec.Dir` and never compute paths |
| Asset sub-path containment | Backend (`registry_install.go`, `napp.go`) | — | Manifest paths are author input; checked where joined |
| Session start signal | Host page (`napplet-host.js`, trusted top frame) | Backend (`nap.go` generation model) | D-06: only the host page knows when it creates/assigns the frame; Go owns the session state and generation |
| `window.napplet` namespace scoping | Backend-built srcdoc (`buildSrcdoc`) | Browser (frame JS scope) | The preamble is assembled in Go; the scoping is a property of the JS it emits |
| Shim provenance | Backend webview package (`embed.go`, test) | Repo (`.gitattributes`) | Embedded bytes are what ship to desktop child and Android AAR alike |
| Intent delivery to handler napplets | Backend (`window_instances.go`, `nap_inc.go`) | Shim (INC listener) | NAP-INTENT master routes delivery through the INC topic mechanism |
| Handler coverage oracle | Backend tests (`backend/nap_*_test.go`) | `backend/testdata/` fixture | Needs unexported `napHandlers`/`napDomains` |
| Spec snapshots + checklist | Repo docs (`spec/`) | — | D-14 |
| Android breakage detection | CI (`.github/workflows/android.yml`) | — | D-17 |

## Standard Stack

### Core (no new Go modules are added in this phase)

| Library / artifact | Version | Purpose | Why standard |
|---------|---------|---------|--------------|
| Go stdlib `path/filepath` (`Rel`, `IsLocal`, `Join`) | Go 1.26 (go.mod `go 1.26.2`, local 1.26.7) | Containment checks | `filepath.IsLocal` (Go ≥1.20) is the stdlib's lexical "stays inside base" test and also rejects Windows reserved names (`NUL`) [VERIFIED: `go doc filepath.IsLocal`] |
| Go stdlib `crypto/sha256`, `encoding/hex` | stdlib | `hex(sha256(id))` dir names; shim hash test | Already imported in `registry_install.go` and `nap.go` |
| `@napplet/shim` `dist/prelude.global.js` | **0.30.0** | Vendored napplet runtime prelude | npm `latest`; sha256 `25d6bb0e737e698e0c499e35c1ccd590ef6f87f7a025554d3af41bc6d5890753`, 136157 bytes, tarball integrity `sha512-aGkHuT6NI28rC1x8LXDKnF7XTv8RVf1WieItCJqKag14jQqxUOIuDUqFru1uzE5BIj6TnuqO3LRwvQzxuw7BwQ==` [VERIFIED: downloaded tarball + `sha256sum` + `openssl dgst -sha512`, matching `npm view` and SPEC-PINS/STACK] |
| `@napplet/conformance` `ENVELOPE_SPECS` | **0.17.0** | Offline JSON oracle in `backend/testdata/` | Same release as shim 0.30.0; npm dist output equals `packages/conformance/src/validators/envelope-specs.ts` @ `956135b` (237 `(type, dir)` pairs identical) [VERIFIED: scratch install + regex cross-check]; tarball integrity `sha512-EycR0WoSGrMURYeH8frahrAgEU6AGIORXgJK+3jiPHwlVUDs9dlyKLgEVoH6vRlBkr0ptAZGtH4iV/NSNFCPfQ==` |
| `node` (test-time only, optional) | any modern; local v26.5.0, GitHub ubuntu-24.04 ships Node 22 | Run the SHIM-04 scope test | Same gate pattern as the existing `backend/webview/shim_test.go` (`exec.LookPath("node")` → `t.Skip`) [CITED: github.com/actions/runner-images Ubuntu2404-Readme] |
| `gomobile`/`gobind` | `golang.org/x/mobile v0.0.0-20260908204917-8b95e45f8d3e` | AAR bind in CI | Matches `backend/go.mod` line 13 and `android.yml` `XMOBILE_VERSION` [VERIFIED: both files] |

### Alternatives Considered

| Instead of | Could use | Tradeoff |
|------------|-----------|----------|
| `filepath.Rel` lexical containment (D-03, locked) | `os.Root` (`os.OpenRoot`, Go ≥1.24; `MkdirAll`/`WriteFile`/`RemoveAll`/`ReadFile` methods present in Go 1.26) | `os.Root` also resists symlink escapes. It is **defense-in-depth only**; D-03 locks `Rel`. Install never creates symlinks, so the lexical check is sufficient for this phase [VERIFIED: `go doc os.Root`] |
| Committed `.mjs` generator for the fixture | Commands documented in the test's doc comment / a testdata README | A committed `.mjs` reads like a JS toolchain; documenting the 4-line scratch procedure keeps the "no toolchain" rule |
| Path-filtered required check | Unfiltered AAR job | Path filtering is locked (D-17). See Pitfall 9 for the required-check interaction |

**Fetch (no npm needed):**
```bash
curl -sL https://registry.npmjs.org/@napplet/shim/-/shim-0.30.0.tgz -o "$SCRATCH/shim.tgz"
tar xzf "$SCRATCH/shim.tgz" -C "$SCRATCH" package/dist/prelude.global.js
sha256sum "$SCRATCH/package/dist/prelude.global.js"   # must print 25d6bb0e737e698e0c499e35c1ccd590ef6f87f7a025554d3af41bc6d5890753
cp "$SCRATCH/package/dist/prelude.global.js" backend/webview/shim/prelude.global.js
```

## Package Legitimacy Audit

No package is installed into the repo or the build. The shim bytes are vendored and hash-pinned, and the conformance package is used once in a scratch dir to produce JSON.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| `@napplet/shim` 0.30.0 | npm | published 2026-08-26 | 126/wk | `git+https://github.com/sandwichfarm/napplet.git` (npm metadata; the project's pin is `github.com/napplet/web`) | SUS (`low-downloads` only; `postinstall: null`) | Approved by locked decision (SPEC-PINS, D-09). Bytes are pinned by sha256 + sha512 and were cross-checked against the pinned source commit by prior research. A light `checkpoint:human-verify` that the printed sha256 equals `25d6bb0e…0753` before commit satisfies the gate |
| `@napplet/conformance` 0.17.0 | npm | published 2026-08-26 | 177/wk | same as above | SUS (`low-downloads` only; `postinstall: null`) | Scratch-only, `npm i --ignore-scripts`; output JSON equals the pinned source file. Same light checkpoint |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** `@napplet/shim`, `@napplet/conformance`. Both are flagged for low weekly downloads in a niche ecosystem, not for any code signal. Neither runs at install or build time.

## Architecture Patterns

### System Architecture Diagram

```
                     ┌─────────────────────────── napp/napplet event (relay) ───────────────────────────┐
                     │  d (raw, opaque) ─► id = "<pk16>~d" | "napplet~<pk16>~d"   (unchanged: wire, storage keys)
                     ▼
  Install/Update/Uninstall/Launch/Icon/Document read
                     │
                     ▼
        nappBaseDir(id) ──► name = hex(sha256(id)) ──► Join(<dataDir>/napps, name)
                     │            └─► Rel/IsLocal containment vs <dataDir>/napps ──fail──► error (no FS op)
                     ▼
        base dir ──► per-asset: Join(base, FromSlash(trim(path))) ─► IsLocal(rel) check ─► write/read
                     │
                     └──► WindowSpec.Dir ──► desktop child (VERDANA_NAPP_DIR) / Android (spec.dir)

  Napplet session (desktop child or Android WebView, same napplet-host.js)
  host page boot():
     rpc nap.boot ──► Go: verified HTML ─► buildSrcdoc:
                           <CSP meta><script>(function(){ PRISTINE PRELUDE \n;NappletShimPrelude.install({domains})\n})()</script></head>+napplet bytes
     drop old <iframe> (old document's in-flight posts now fail the sender check)
     outbound chain ─► rpc nap.start ─► Go: teardown(gen++) ; established=true ─► ack
     create fresh <iframe sandbox=allow-scripts srcdoc=…>  (+ load listener ─► outbound ─► rpc nap.loaded ─► Go pushes notify.controls once/gen)
  frame postMessage ─► host page (event.source === frame.contentWindow) ─► bounded outbound chain ─► rpc nap.msg
     ─► Go napEnqueue(gen captured) ─► napWorker ─► napDispatch: stale gen? drop : handler? handle : drop silently
  Go pushes ─► __nap_push ─► frame.contentWindow.postMessage ─► shim DOMAIN_ROUTERS (event.source === window.parent)

  Intent to a handler napplet:
     intent.invoke (caller) ─► runNappAction ─► resolve/launch target ─► Accept ─► caller gets result{ok,handled:true}
     ─► dispatchToNapplet: waitForHandler(convention) [target's inc.subscribe registered it] ─► push inc.event{topic:convention,sender,payload}
```

### Recommended file touch map

```
backend/backend.go              # nappBaseDir(id) (string, error); NappBaseDir changed or removed (no external callers)
backend/registry_install.go     # InstallNapp, Uninstall, fetchNappAsset (shared asset-path helper)
backend/registry_updates.go     # applyUpdate
backend/napp.go                 # IconBlob local read (+ asset containment)
backend/window_instances.go     # launchWithDocument; dispatchToNapplet → INC topic delivery
backend/nap.go                  # napDispatch (drop shell.ready branch), napStart/napLoaded, buildSrcdoc wrapper, fail() comment
backend/webview/shim/prelude.global.js   # pristine 0.30.0 bytes
backend/webview/shim/README.md  # rewritten: version, napplet/web commit, npm integrity, sha256, "no patches"
backend/webview/embed.go        # ShimVersion = "0.30.0", ShimSHA256 const
backend/webview/shim_test.go    # replace shell test with hash/version/README test
backend/webview/napplet-host.js # nap.start, fresh frame per session, bounded queue, nap.loaded, comments
backend/nap_conformance_test.go # TestNAPHandlersCoverReferenceEnvelopes (new)
backend/testdata/napplet-conformance-0.17.0-envelopes.json  # fixture (new)
backend/containment_test.go (or registry_install_test.go)   # CRIT-01 matrix (new)
.gitattributes                  # backend/webview/shim/prelude.global.js -text (new)
spec/pinned/*.md, spec/CONFORMANCE.md   # new
.github/workflows/android.yml   # PR AAR job + tags trigger
```

### Pattern 1: Single choke point for napp directories (CRIT-01)

**What:** `nappBaseDir` hashes the id and validates containment; every filesystem op on a napp dir goes through it, and nothing touches the FS on error.
**Example (sketch; the names are the planner's):**
```go
// nappBaseDir is the one place a napp's install directory is named. The id
// (and the author's d inside it) is opaque and never a path segment: the
// directory is the hex sha256 of the id, checked to sit inside <dataDir>/napps.
func nappBaseDir(id string) (string, error) {
	if !filepath.IsAbs(dataDir) {
		return "", errors.New("data directory is not set")
	}
	root := filepath.Join(dataDir, "napps")
	sum := sha256.Sum256([]byte(id))
	name := hex.EncodeToString(sum[:])
	dir := filepath.Join(root, name)
	if rel, err := filepath.Rel(root, dir); err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("napp directory escapes %s", root)
	}
	return dir, nil
}

// nappAssetPath joins a manifest path (author input) under base.
func nappAssetPath(base, manifestPath string) (string, error) {
	rel := filepath.FromSlash(strings.TrimPrefix(manifestPath, "/"))
	if rel == "" {
		rel = "index.html" // NIP-5D lets a napplet name its index "/"
	}
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%s: path escapes the napp directory", manifestPath)
	}
	return filepath.Join(base, rel), nil
}
```
Call sites to convert (all verified by grep; `[VERIFIED: grep -rn nappBaseDir]`): `registry_install.go:48` (install; the `RemoveAll(base)` at :55 must use the same validated `base`), `registry_install.go:79` (uninstall: on error log and skip `RemoveAll`, still forget state), `registry_updates.go:143` (update), `napp.go:190` (icon read: also add the asset check), `nap.go:458` (`nappletDocument`), `window_instances.go:502` (launch: return the error), plus tests `nap_test.go:1323,1326` and `preview_test.go:77`. `fetchNappAsset` (`registry_install.go:260-266`) already has a `Rel` check; replace it with the shared helper so the icon read and the writer use one rule.

The current code, quoted from source [VERIFIED: backend/backend.go:106-111]:
```go
func nappBaseDir(id string) string {
	return filepath.Join(dataDir, "napps", id)
}

// NappBaseDir is where a napp's files are unpacked.
func NappBaseDir(id string) string { return nappBaseDir(id) }
```
Id formation stays untouched [VERIFIED: backend/napplet.go:121-123 `return "napplet~" + pk.Hex()[:16] + "~" + d`; backend/registry_discovery.go:147 `ID:        evt.PubKey.Hex()[:16] + "~" + d,`; backend/napplet_nip5d.go:164,168 `n.ID = nappletID(evt.PubKey, n.D)` / `n.ID = nappletID(evt.PubKey, "") + "root"`].

There is precedent for hashing ids into names. `desktop/internal/osintegration/appshortcut.go:26-29` already does `sha256.Sum256([]byte(id))` for shortcut keys [VERIFIED: grep].

### Pattern 2: Host-page session start on the existing ordered channel (SHIM-03, D-06/D-07)

**What:** The host page already serializes every frame envelope through one promise chain (`outbound`, `napplet-host.js:177-188`) because the desktop binding runs rpcs on separate threads. The session start goes through **that same chain**, so it is ordered before every envelope of the new document. That chain is the D-07 queue; add a counter to bound it.

**Go side (recommended names: `nap.start`, `nap.loaded`, added to `napRPC`'s switch):**
- `nap.start`: under `s.mu`, `ci.napTeardownLocked("napplet restarted")` (this does `gen++`, cancels the ctx, clears topics/grants/subs and **unregisters INC topic actions** via `incForget`), then `s.established = true`, then `close(s.ready)` if anything still waits on it. Reply `{"ok": true}` (or the gen). Bumping the generation here is what keeps the D-07 guarantee: an envelope from the outgoing document that was enqueued before `nap.start` carries the old `gen` and is dropped as stale by `napDispatch` (`nap.go:349-357`).
- `napDispatch`: delete the `shell.ready` branch (`nap.go:344-347`) and `napReady`. A frame-sent `shell.ready` then falls through to "unknown type, dropped silently", which is NIP-5D behavior. It cannot start, reset or answer anything.
- `nap.loaded`: if `established` and the per-session flag `controlsSent` is false, set it and push `{"type":"notify.controls","controls":host.NotificationControls()}`. Reset the flag in `resetLocked`.
- Frame-forgery check: the frame can only `postMessage` the host page, which only ever forwards as `nap.msg`. `nap.start`/`nap.loaded` are reachable only from the trusted top frame's rpc binding (desktop: token-checked `__verdanaNappletRPC`, `desktop/child/napplet.go:46,73-79`; Android: page-origin message channel). Neither the child nor Android filters rpc method names, so the new rpcs need **no** desktop-child or Kotlin change [VERIFIED: desktop/child/napplet.go; android NappWebView.kt `applyMessage`].

**Host page side (sketch):**
```js
  // ── napplet -> Go ───────────────────────────────────────────────
  // One ordered lane to Go, bounded so a napplet cannot queue without limit.
  const MAX_PENDING = 256 // matches Go's per-session queue (napEnqueue)
  let outbound = Promise.resolve()
  let pending = 0
  const enqueue = task => {
    if (pending >= MAX_PENDING) return Promise.reject(new Error("too many pending NAP envelopes"))
    pending++
    const run = outbound.then(task)
    outbound = run.catch(() => {}).finally(() => { pending-- })
    return run
  }

  const boot = async () => {
    let doc
    try {
      doc = await rpc("nap.boot")
    } catch (err) { /* existing error text */ return }
    if (!doc || typeof doc.srcdoc !== "string") return
    // every session gets a fresh frame: anything the previous document still
    // posts fails the sender check instead of landing in the new session
    if (frame) frame.remove()
    frame = null
    try {
      await enqueue(() => rpc("nap.start"))
    } catch (err) { /* show "could not be started" and stop */ return }
    const f = document.createElement("iframe")
    // allow-scripts and nothing else: never allow-same-origin
    f.setAttribute("sandbox", "allow-scripts")
    /* referrerpolicy, title, style as today */
    f.addEventListener("load", () => {
      if (frame === f) enqueue(() => rpc("nap.loaded")).catch(() => {})
    })
    f.srcdoc = doc.srcdoc
    frame = f
    document.body.appendChild(f)
  }
```
The message listener keeps its sender check and routes through `enqueue(...)` instead of assigning `outbound` directly. When the enqueue is refused, it calls `refuse(data, err)` so requests still get a terminal reply.

**Why `notify.controls` on the frame `load` event:** NAP-NOTIFY PR #11 only says the shell "MAY send `notify.controls`" with no timing rule [CITED: spec NAP-NOTIFY @ e14f5c9 line 234]. The upstream shim keeps no last value. `onControls` callbacks get the push only if they are registered when it arrives (`controlsHandlers` set, no replay) [VERIFIED: napplet/web@956135b packages/nap/src/notify/shim.ts:53-54,133-136,331-334]. The frame `load` event fires after the napplet's synchronous scripts ran, so top-level `onControls(...)` registrations receive it. A push at `nap.start` time would reach no shim at all. This uses the `load` hook D-07 keeps, and it does not treat `load` as a session start.

**Alternative trigger (if the planner prefers no `nap.loaded` rpc):** push `notify.controls` once per session on the napplet's first `notify.*` envelope. It is simpler, but a napplet that only calls `onControls` would never get it.

### Pattern 3: Function-scoped prelude (SHIM-04, D-09)

Verified facts about the pristine file [VERIFIED: shim030/package/dist/prelude.global.js]:
- Line 1 is `"use strict";`, and line 2 is `var NappletShimPrelude = (() => {`. No other column-0 statements.
- The last line is `//# sourceMappingURL=prelude.global.js.map` with **no trailing newline** (last byte is `p`).
- The only global writes are `window.napplet = napplet;` (in `installNappletGlobal`) and `window.addEventListener("message", …)`. `globalThis` appears only inside the string template of `renderNappletRuntimePreludeCall`.
- It contains zero `</script`, zero `<!--`, and one `<script` (inside a JS string `<script>${…}<\/script>`), so the existing `strings.ReplaceAll(…, "</script", …)` is a no-op and the inlined bytes are exactly the file.
- Wrapping in `(function(){ … })()` makes the leading `"use strict"` the function's directive prologue. Strict semantics are unchanged, since the file was already strict as a classic script.

Exact emission (the `\n` before `;` is mandatory because of the trailing line comment):
```go
return "<!doctype html><html><head>" +
	`<meta http-equiv="Content-Security-Policy" content="` + nappletCSP + `">` +
	"<script>(function(){" + prelude + "\n;NappletShimPrelude.install(" + string(domainsJSON) + ")\n})()</script>" +
	"</head>" + doc, nil
```
The `window.parent.postMessage({type:"shell.ready"},"*")` is removed.

Node verification of this exact shape with all 14 `napDomains` and no `document` global: new globals `[ 'napplet' ]`, `typeof NappletShimPrelude` → `undefined`, `napplet` keys = `common,config,identity,inc,intent,link,media,notify,outbox,relay,resource,storage,theme,upload`, and nothing posted at install. The unwrapped form leaves `[ 'NappletShimPrelude', 'napplet' ]`, and a second `NappletShimPrelude.install({domains:["keys","dm"]})` proceeds into `installKeysShim` [VERIFIED: node vm run in scratch].

### Pattern 4: Intent delivery through the convention's INC topic (keeps intent napplets working)

`dispatchToNapplet` today [VERIFIED: backend/window_instances.go:1089-1123] waits on `ci.nap.ready`/`established` and pushes `{"type": "intent.deliver", "delivery": delivery}`. The pristine shim's `handleIntentMessage` routes only `intent.invoke.result`, `intent.available.result`, `intent.handlers.result` and `intent.changed` [VERIFIED: napplet/web@956135b packages/nap/src/intent/shim.ts:109-119], so `intent.deliver` is silently dropped.

NAP-INTENT @ `a040914` (the pin): "The shell delivers `payload` to the resolved handler using the named `convention`'s ordinary delivery mechanism — typically an INC topic event (e.g., `napplet:note/open`)…" and "The shell MUST deliver `payload` to the resolved handler only after that handler is ready to receive it." [CITED: naps@a040914 naps/NAP-INTENT.md lines 126, 207]

Restore the pre-`bf29b5d` delivery (`git show bf29b5d -- backend/instances.go` shows the old body). Wait with `ci.waitForHandler(waitCtx, req.name)` (20 s timeout; `window_instances.go:440`), then push `{"type":"inc.event","topic":req.name,"sender":req.sender,"payload":payload}`. Readiness is exact: `napIncSubscribe` calls `c.ci.registerAction(r.Topic, incActionIdx)` [VERIFIED: backend/nap_inc.go:134], and `incForget` unregisters topic actions on teardown, so a reset session must re-subscribe. The upstream INC shim requires `topic` and `sender` to be strings and forwards `payload` [VERIFIED: packages/nap/src/inc/shim.ts:334-341]. The caller side needs no change. Go already decodes `{request:{…}}` and returns `result:{ok, archetype, action, handled, …}`, which the upstream validator requires (`ok` boolean, `archetype`/`action` strings, `handled` boolean) [VERIFIED: backend/nap_intent.go:49-66; shim.ts:54-61].

### Anti-Patterns to Avoid
- **Sanitizing or normalizing `d`.** WEB-NAPPLET: "`d` is exact and case-sensitive. Clients MUST NOT normalize it." [CITED: hzrd149/naps@7ae5b19 WEB-NAPPLET.md:41]. Encode only at the filesystem boundary.
- **Checking containment after the fact.** The existing `fetchNappAsset` check validates `dest` against a `base` that has already escaped, so it cannot catch W-1.
- **Editing `prelude.global.js` in any editor.** The file has no trailing newline, and most editors add one, which breaks the hash. Only `cp` from the tarball.
- **Pushing `notify.controls` (or anything) at `nap.start` time.** The frame has no shim yet, so the message is lost.
- **Reusing the iframe element across sessions.** Old-document posts are attributed to the new session.
- **Keeping `TestShimProvidesShellCapabilityDiscovery`.** It asserts the dropped NAP-SHELL patch. Delete it.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| "Is this relative path inside base?" | String-prefix checks on `..` | `filepath.IsLocal(rel)` (+ `filepath.Rel`) | Handles `..`, absolute, empty, and Windows reserved names (`NUL`, `CON`) lexically |
| Directory names from ids | Escaping/percent-encoding of `d` | `hex(sha256(id))` (D-01) | Fixed-width alphabet `[0-9a-f]`; no collisions; same pattern as `appShortcutKey` |
| Shim provenance | A version string | sha256 test over the embedded bytes | `ShimVersion` was never checked, which is how the README hash drifted (`6d98d7ba…` vs actual `8e9c3f7b…`) |
| Handler coverage | A hand-maintained list of shim types | Committed `ENVELOPE_SPECS` JSON from `@napplet/conformance` 0.17.0 | It is generated from the same release and guarded by upstream's own drift test |
| Pre-ack queue | A new buffer object | The existing `outbound` promise chain plus a counter | Already the ordering mechanism; a second queue would reorder |
| Intent readiness | A new "handler ready" signal | `waitForHandler` + INC `registerAction` | Already wired; teardown already clears it |

## Runtime State Inventory

(This phase renames on-disk directories, so the inventory applies.)

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `<dataDir>/napps/<raw-id>/` on dev machines. `state.json` `InstalledNapps` stays keyed by raw id (unchanged). Storage (`storage/<safeFileName(id)>.json`) and config (`config/<safeFileName(id)>.json`) are keyed by id and already contained (the id always has a `<pk16>~` or `napplet~` prefix, and `safeFileName` maps separators to `_` then `filepath.Base`) | **None** (D-04: orphan old dirs, no sweep). Note for the PR/commit body: installed napps on dev machines must be reinstalled, because launch will say "not installed" until then. The `a/b` vs `a_b` storage/config collision is Phase 5 (CF-2/KEY-*) |
| Live service config | None. No external service stores napp ids or paths | None, verified by repo inspection |
| OS-registered state | Desktop app shortcuts (Linux `.desktop`, Windows `.lnk`, macOS `.app`) reference napp **ids**, and icons are keyed by `appShortcutKey(id)`. Neither embeds the install path | None. Ids are unchanged |
| Secrets/env vars | `VERDANA_NAPP_DIR` is passed per launch from `WindowSpec.Dir` (`desktop/childproc.go:42`) | None. It picks up the hashed path automatically |
| Build artifacts | `desktop/child/child` embeds `napplet-host.js` (`desktop/child/napplet.go:53`) and is itself embedded by the launcher. A stale child binary would never send `nap.start`, so every napplet would stay dead. The Android AAR embeds the same webview package | Rebuild the child (`just run` already does) and the AAR after the host-page change. Mention it in the smoke-test steps |

## Common Pitfalls

### Pitfall 1: `d="../../.."` wipes the data directory today
**What goes wrong:** `filepath.Join("/home/u/.local/share/verdana","napps","napplet~0123456789abcdef~../../..")` = `/home/u/.local/share/verdana`. `"…~/../x"` → `<dataDir>/napps/x`. `"…~a/b"` → nested under napp `<pk16>~a`. `"…~/../../../../tmp/evil"` → `/home/u/.local/tmp/evil` [VERIFIED: `go run` in scratch].
**Why it happens:** `filepath.Join` cleans `..` lexically across the id's own separators.
**How to avoid:** Pattern 1. Tests must assert that a sentinel file in `dataDir` (e.g. `state.json`) and a sentinel dir *outside* `dataDir` survive failed install and uninstall for every hostile `d`.
**Warning signs:** Any FS op whose path does not start with `<dataDir>/napps/<64 hex>`.

### Pitfall 2: The prelude's trailing `//# sourceMappingURL` comment swallows the wrapper
**What goes wrong:** `"(function(){" + prelude + ";NappletShimPrelude.install(…)})()"` turns the install call and the closing paren into part of the line comment. The script then fails to parse and no `window.napplet` exists.
**How to avoid:** Insert `"\n"` right after the prelude. The SHIM-04 node test catches it.

### Pitfall 3: Pristine shim has no `intent.deliver`, so handler napplets go silent
See Pattern 4. **Warning sign:** an "open with" accepts (`handled:true`) but the target napplet shows nothing.

### Pitfall 4: Old-document envelopes leaking into the new session
**What goes wrong:** On dev reload (`dev.go:270-271`: `napReset` then `__nap_reload`), the old document can keep posting until navigation commits. The `contentWindow` WindowProxy is the same object across srcdoc navigations, so those posts pass `event.source === frame.contentWindow`.
**How to avoid:** Bump `gen` in `nap.start` (stale drops) **and** create a fresh iframe per session (sender check fails). Note: CONTEXT's code insight says the host page "recreates the iframe on `__nap_reload`". It does not. `boot()` reuses the frame and reassigns `srcdoc` (`napplet-host.js:260-272`) [VERIFIED].

### Pitfall 5: The vendored file's bytes drift through git or editors
**What goes wrong:** The pristine file has no final newline. An editor, a "fix trailing newline" hook, or `core.autocrlf=true` on Windows changes the bytes, and the hash test fails.
**How to avoid:** Add a `.gitattributes` line `backend/webview/shim/prelude.global.js -text` (there is no `.gitattributes` today [VERIFIED: ls]). Copy only from the tarball.

### Pitfall 6: Upstream timeouts now apply, and storage's is 5 s
**What goes wrong:** All pristine request timeouts are `3e4`, except storage (`REQUEST_TIMEOUT_MS2 = 5e3`, "State request timed out") [VERIFIED: prelude lines 535-3713]. Prompts (relay.publish, common.follow/react, identity via a remote signer) can outlive 30 s. Storage requests sit in the same serialized Go queue as everything else.
**How to avoid:** Phase 1 records it (D-10). Phase 2 cancels stale prompts. Keep storage handlers non-blocking (they are today).

### Pitfall 7: Removing the handshake changes what a self-reloaded napplet sees
**What goes wrong:** Today a napplet that calls `location.reload()` re-posts `shell.ready`, Go ignores it, and the patched no-deadline shim hangs forever (PITFALLS #2). After Phase 1, the reloaded document's envelopes are served by the still-established old session. This is not a new privilege, since PITFALLS #2 already documents that a navigated frame is served, but it changes behavior.
**How to avoid:** Leave it to Phase 4 (SBOX-01, the unexpected-`load` hook). Add a CONFORMANCE.md row for NIP-5D "including … reloads" with status `open → Phase 4`.

### Pitfall 8: 63 test call sites use the `ready()` helper
**What goes wrong:** `ready(t, ci, rec, n)` posts `shell.ready` and waits for `shell.init` (`nap_test.go:163-167`). It is used 63 times across 8 test files [VERIFIED: grep -c].
**How to avoid:** Rewrite the helper once to call `napRPC(ci, "nap.start", "")`. Keep its signature so call sites don't change. Rewrite only the tests that assert handshake semantics: `TestBuildSrcdoc`, `TestNapSessionHandshake`, `TestNapDuplicateReadyIsIdempotent`, `TestIntentDeliveryToNapplet`, the accepted-delivery test around `nap_test.go:1011-1019`, and `TestOpenUserProfileDeliversToHandler`.

### Pitfall 9: A path-filtered workflow used as a required check blocks unrelated PRs
**What goes wrong:** "If a workflow is skipped due to path filtering… checks associated with that workflow will remain in a 'Pending' state. A pull request that requires those checks to be successful will be blocked from merging." [CITED: docs.github.com troubleshooting-required-status-checks]
**How to avoid:** Don't mark the path-filtered `android` job as a required status check. If it must be required, use an always-running job that no-ops via `if:` (a job skipped by a conditional reports Success).

### Pitfall 10: A Go-only Android proxy can miss cgo/NDK and Kotlin breakage
`GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` in `backend/` passes in about 7 s locally and catches most Go-level breakage [VERIFIED: ran it]. It skips cgo (lmdb is compiled for `android/amd64`/`386` in a real bind) and Kotlin. Use it as the per-task check; CI's `gomobile bind` is the gate. D-17 binds only the AAR, so Kotlin compile breakage from `backend/mobile` API changes is still not caught on PRs (residual risk; record it).

## Code Examples

### SHIM-01 hash test (backend/webview)
```go
// ShimVersion is the @napplet/shim release prelude.global.js is, byte for byte.
const ShimVersion = "0.30.0"

// ShimSHA256 is the sha256 of npm @napplet/shim@0.30.0 dist/prelude.global.js.
const ShimSHA256 = "25d6bb0e737e698e0c499e35c1ccd590ef6f87f7a025554d3af41bc6d5890753"

func TestShimPreludeIsPristineUpstream(t *testing.T) {
	sum := sha256.Sum256([]byte(shimPrelude))
	if got := hex.EncodeToString(sum[:]); got != ShimSHA256 {
		t.Fatalf("prelude.global.js sha256 = %s, want npm @napplet/shim@%s %s", got, ShimVersion, ShimSHA256)
	}
	readme, err := os.ReadFile("shim/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**" + ShimVersion + "**", ShimSHA256} {
		if !bytes.Contains(readme, []byte(want)) {
			t.Errorf("shim/README.md does not state %q", want)
		}
	}
}
```
(`go test` runs with the package dir as cwd, so `shim/README.md` resolves. The README phrasing is the planner's; the test must match it exactly.)

### SHIM-04 scope test (backend, node-gated)
Build `buildSrcdoc([]byte("<p>x</p>"), napDomains)`, extract the inline activation script (between the `<script>` that follows the CSP meta and its `</script>`), and run it with node using `vm.createContext` and a fake `window`/`parent`/`addEventListener` (the scratch harness above needed only `crypto`, `setTimeout`, `clearTimeout`, `console`). Assert:
- the set of new own-property names on the context is exactly `["napplet"]`
- `typeof NappletShimPrelude === "undefined"`
- sorted `Object.keys(napplet)` equals sorted `napDomains`
- `strings.Contains(srcdoc, webview.ShimPrelude())` (bytes inlined unmodified)

Gate on `exec.LookPath("node")` → `t.Skip`. Optional: fail instead of skipping when an env var such as `VERDANA_REQUIRE_NODE=1` is set in the `desktop.yml` test job, so CI cannot silently skip.

### SHIM-05 fixture generation (one-off, scratch dir, never CI)
```bash
cd "$SCRATCH" && npm init -y >/dev/null && npm i --ignore-scripts --no-audit --no-fund @napplet/conformance@0.17.0
cat > gen.mjs <<'EOF'
import { ENVELOPE_SPECS } from '@napplet/conformance'
const out = { package: '@napplet/conformance', version: '0.17.0', shim: '0.30.0',
  source: 'napplet/web@956135bfc41a2cff5e45d6c68d9f9a4d68c50531',
  envelopes: Object.fromEntries(Object.keys(ENVELOPE_SPECS).sort().map(k => [k, ENVELOPE_SPECS[k]])) }
process.stdout.write(JSON.stringify(out, null, 2) + '\n')
EOF
node gen.mjs > napplet-conformance-0.17.0-envelopes.json
```
Facts about the output [VERIFIED: generated in scratch]: 237 entries, 114 `dir:"out"`, 123 `dir:"in"`. Each entry is `{dir, fields?, forbiddenFields?}`.

### SHIM-05 coverage test shape (`backend/nap_conformance_test.go`)
- Load the fixture. Assert `fixture.shim == webview.ShimVersion`, which forces fixture regeneration on any shim upgrade.
- `napaDomains := {"ble","count","cvm","dm","fs","keys","lists","serial","webrtc"}`: whole domains N/A, with reason "domain not offered: absent from napDomains, so the shim never installs it". Assert none of them is in `napDomains`.
- `bidirectionalOut := {"media.command": "NAP-MEDIA @2b2d29e: for shell-owned sessions media.command is napplet -> shell"}`. The fixture marks it `in`, but the shim's `sendCommand` posts it [VERIFIED: prelude line 449; NAP-MEDIA @2b2d29e lines 158-159].
- `naTypes := map[string]string{}`: explicit type-level N/A (empty today).
- For every fixture type with `dir=="out"` (plus `bidirectionalOut`) whose domain is in `napDomains`: require `napHandlers[t] != nil || naTypes[t] != ""`.
- Honesty checks: every `naTypes` key has no handler; every `napHandlers` key is a fixture `out` type or in `bidirectionalOut`.

Today's gap computation [VERIFIED: computed in scratch]: all 67 `out` types of the 14 implemented domains have handlers. The only Go handler not in the fixture's `out` set is `media.command`. The 47 `out` types of the 9 unimplemented domains are domain-level N/A.

### Spec snapshot sources (SPEC-01)

All objects exist locally [VERIFIED: `git cat-file -t` / `git ls-tree` at each SHA]. Produce each body with `git -C <repo> show <sha>:<path>` and prepend front matter (`spec`, `repo`, `ref`, `commit` (full), `path`, `fetched`, and recommended `body_sha256` so anyone can re-verify).

| File (`spec/pinned/`) | Repo / ref | Commit | Path | body sha256 | lines |
|---|---|---|---|---|---|
| `NIP-5D@24711d9c.md` | nostr-protocol/nips `refs/pull/2303/head` (`~/Projects/nips`) | `24711d9c47bbdd07908bf1d52bf677d9cbc530f0` | `5D.md` | `3adea2e3db6d32807ed5832bd928c72ae02bbd26266994d8b8d6f26a158f41e2` | 144 |
| `WEB-NAPPLET@7ae5b19a.md` | hzrd149/naps `web-napplet-event` | `7ae5b19a9c32fbd4c881836f4d821c02630e2b4f` | `WEB-NAPPLET.md` | `5af565a1a78a76daeac567b67ef1013b3ac2ed8dec4a155b7e38c4f987cde451` | 250 |
| `NAP-SHELL@a040914b.md` | napplet/naps `master` | `a040914b4bbd3a5cd8a14b0f316a723c968ebfb2` | `naps/NAP-SHELL.md` | `a12ae633679b5fb3acff43e502e3749c640ee105ff211928b7eecb7d127590ec` | 168 |
| `NAP-IDENTITY@a040914b.md` | same | same | `naps/NAP-IDENTITY.md` | `f599c0b0cf68fb2bf4c2f6f08a3675d09cca1d53c620e7863957f3ab3dc45bce` | 253 |
| `NAP-INC@a040914b.md` | same | same | `naps/NAP-INC.md` | `352f27c8d0cb22da93a541b95b832d9d7f7f9840a19a98c863c2de2d0fbdccbe` | 392 |
| `NAP-INTENT@a040914b.md` | same | same | `naps/NAP-INTENT.md` | `d6a533ea9c132f0196057c177e87452a7c9edda452fea4b89368afc202b709f0` | 223 |
| `NAP-THEME@a040914b.md` | same | same | `naps/NAP-THEME.md` | `f87fc4afcd0cfdc9401540935421a9a876036384953efa8ceaf9b1ba607e9762` | 156 |
| `NAP-RELAY@0be8abce.md` | `refs/pull/2/head` | `0be8abce18beb46ca37bd4ddd042f58d30b4eedc` | `naps/NAP-RELAY.md` | `898a1f316b5750a368354fa58abdd9358bc6ce65efd11d33c3eb2e47646ef140` | 283 |
| `NAP-STORAGE@f71e84eb.md` | `refs/pull/3/head` | `f71e84ebca7474db260346cbfc2d88f41b4e421e` | `naps/NAP-STORAGE.md` | `0045b6e304b194e20e83b5a29431f81e66b0a45a28c112d11e8cadc07d32a850` | 144 |
| `NAP-MEDIA@2b2d29e9.md` | `refs/pull/10/head` | `2b2d29e90c30b994bf5035a65b57e5fe7f08a9a2` | `naps/NAP-MEDIA.md` | `4dab9c6c658860ad266aa74e47e30faf9b3b63f1c3bba4fe4d98d0c7f05c83bb` | 338 |
| `NAP-NOTIFY@e14f5c9d.md` | `refs/pull/11/head` | `e14f5c9d6a6dd2a69ccf79668c4a3c1e955e1ac9` | `naps/NAP-NOTIFY.md` | `892d338587627f71899a4594096cf08795faa5e9fa230af72c31083f12abc5a2` | 257 |
| `NAP-CONFIG@448013e6.md` | `refs/pull/14/head` | `448013e6d8cb8c75dce49576b3e7c0d46d960eac` | `naps/NAP-CONFIG.md` | `2d34af51d84edd4353ec142002d9ffe0a7a5477ce9b0fd71835273fd4034931b` | 339 |
| `NAP-OUTBOX@4589a8f9.md` | `refs/pull/32/head` | `4589a8f9a16d8aa29b3740e2b3b0cdca11e0976e` | `naps/NAP-OUTBOX.md` | `1761cd6515b125f2ab802343c19749d9da09863ce62a90a7a156714a0239d0f4` | 325 |
| `NAP-UPLOAD@a7cc1746.md` | `refs/pull/33/head` | `a7cc17463cbf5d9cb87884b31071bc4fc826034c` | `naps/NAP-UPLOAD.md` | `fa9ef6df22091f4ee874853b64834f10d5fe4547f92d41d90e1d15f66a1466e8` | 300 |
| `NAP-LINK@e2514335.md` | `refs/pull/53/head` | `e25143355f6d416bfce73b12ec814f1c795ec16a` | `naps/NAP-LINK.md` | `e65cb78b0d30aff82c370a3b21840282649dec16ed984d99dc10e1b800d9ba1d` | 108 |
| `NAP-COMMON@de603e20.md` | `refs/pull/67/head` | `de603e205a9b498f252be9a5e8e6825c4648df39` | `naps/NAP-COMMON.md` | `0e65f63eaf74483fcacb069117c5239063db0af1073ce92b27fd5076be706e75` | 244 |
| `NAP-RESOURCE@fa6bcc69.md` | `refs/pull/80/head` | `fa6bcc6935aa19e7b70ab2a2c721dafca77c78e1` | `naps/NAP-RESOURCE.md` | `109f7f9107b6c1548a50faf0b7c4ad59dedee53d05249a574d5636017b7f446d` | 225 |
| `NAP-RESOURCE@9511232f.md` (recorded tolerance, not a pin) | branch `nap-resource` | `9511232f69313aa7953d110e35d32cc28d506f66` | `naps/NAP-RESOURCE.md` | `acac746006fe29d3bdd4e214a5aa0069a99624c5c1f9402cd905d220e4cb50bd` | 328 |

If a checkout is missing, `git fetch --depth 1 https://github.com/napplet/naps <ref-or-sha>` (per SPEC-PINS.md) then `git show FETCH_HEAD:<path>`.

### CONFORMANCE.md skeleton content (SPEC-03, SHIM-02, D-16)

**Per-spec sections** in SPEC-PINS order. Header example: `## NAP-SHELL @ a040914b4bbd3a5cd8a14b0f316a723c968ebfb2`, table header `| ID | Requirement | Level | Status | Reason | Code |`, rows filled in later phases.

**Conflicts section (the four required readings).** Every quote is verbatim from the pinned text:

| ID | Specs | Conflict (verbatim) | Chosen reading | Source of decision |
|---|---|---|---|---|
| CF-1 | NAP-SHELL @a040914 vs NIP-5D @24711d9 | NAP-SHELL: "**Required:** Mandatory — every conformant runtime MUST implement NAP-SHELL." vs NIP-5D: "The namespace MUST contain only the NAP domain objects the shell exposes to that napplet. Presence of a domain object means that domain is available to the napplet." | Follow NIP-5D presence detection and the canonical upstream shim (no `window.napplet.shell`, no `shell.ready`/`shell.init`). The session is started by the trusted host page. A frame `shell.ready` is an unknown type, silently ignored. NAP-SHELL rows are marked N/A (conflict CF-1) | SPEC-PINS Decisions; PROJECT Key Decisions; D-06/D-08 |
| CF-2 | WEB-NAPPLET @7ae5b19 vs NIP-5D @24711d9 | WEB-NAPPLET: "A runtime MUST reject a kind `35129` event containing a `path`, `requires`, or `C` tag. It MUST NOT reinterpret or partially load those legacy shapes." vs NIP-5D: "The manifest MUST include a `path` tag per file" and "The manifest declares required capabilities with `requires` tags" (kind `35129`) | Support both shapes. A 35129 with `path` tags is read under NIP-5D rules. Otherwise WEB-NAPPLET applies, including rejection of `requires`/`C` without `path` (`backend/napplet.go` `nappletFromEvent`). WEB-NAPPLET's legacy clause is scoped to events without a valid NIP-5D shape | PROJECT "Support both manifest shapes"; FEATURES A1/W-2 |
| CF-3 | NAP-RELAY @0be8abc vs NIP-5D @24711d9 | NAP-RELAY: "The shell MUST decrypt incoming encrypted events (NIP-04/NIP-44) before delivering them to the napplet via `relay.event`. Napplets receive plaintext content." vs NIP-5D Security 7: "Napplets produce cleartext only. Shells MUST NOT sign or broadcast events containing ciphertext received from a napplet." | Both apply in their own direction. Inbound events addressed to the user are decrypted for the napplet (behind consent; the id/sig no longer verify, recorded). Napplet-supplied ciphertext is never signed. Implemented in the relay domain phase | PROJECT "Decrypt events addressed to the user for napplets; never sign napplet ciphertext"; FEATURES A2/R-1 |
| CF-4 | NAP-OUTBOX @4589a8f vs NIP-5D @24711d9 | NAP-OUTBOX example: `"type": "outbox.publish", … "event": { "kind": 1059, "content": "...", …}` vs NIP-5D Security 7 (above) | NIP-5D wins. The example is non-normative. Plain `outbox.publish` rejects napplet ciphertext (best-effort detection: NIP-04/NIP-44-shaped content, encrypted kinds), implemented in the outbox domain phase | PROJECT decision above; FEATURES A3/O-1 |

Worth seeding as `open` rows too (all found during this research): **CF-5** fixture direction vs NAP-MEDIA bidirectional `media.command`; **CF-6** NAP-RESOURCE pin `fa6bcc6` (`urls`) vs the shim 0.30.0 server-hint shape from `9511232` (accepted as a tolerance); **CF-7** NAP-MEDIA/NOTIFY API tables name methods in their "Wire" column while the Wire Protocol tables use other names (conform to the Wire Protocol table; STACK.md); **CF-8** NAP-CONFIG `config.get` before any schema: `config.schemaError` carries no `id`, so a pending get cannot be answered by spec (see P7 below).

**Dropped shim patches section (SHIM-02).** The rows are taken from the byte diff of the vendored file against pristine 0.29.2 (`diff -u` → 78 hunks) [VERIFIED: scratch diff]:

| ID | Former patch behavior | Upstream 0.30.0 behavior | Impact after dropping | Replacement / owner |
|---|---|---|---|---|
| P1 | No shim deadlines (`napRequestTimer`, every `REQUEST_TIMEOUT_MS*` = `void 0`; `common.follow/unfollow/react` with no timeout) | 30 s per request; storage 5 s | Long prompts or remote signers time out napplet-side; Go's late reply is ignored | D-10 accepted. Prompt cancel in Phase 2 (DISP-04) |
| P2 | NAP-INTENT PR #91: `invoke(uri, options)` URI normalization, `open("napplet:…")`, `onDelivery`, `intent.deliver` buffering, relaxed result validator | Master API: `invoke(request)`, no delivery API; result requires `ok, archetype, action, handled` | Handler napplets would get nothing (Pitfall 3); PR #91-style `invoke("napplet:…")` calls fail with an error | **Phase 1:** Go delivers via `inc.event` on the convention topic after `inc.subscribe` (NAP-INTENT master) |
| P3 | NAP-SHELL global (`window.napplet.shell`, `supports/services/ready/onReady`), `["shell.", handleShellMessage]` router, activation `shell.ready` | None (`NAP_DOMAINS` has no `shell`) | No handshake | **Phase 1:** host-page `nap.start`; CF-1 |
| P4 | INC/convention URI query validation (reject empty query, nameless params) | Accepted | Napplet-side leniency | Go-side validation, Phase 6 |
| P5 | NOTIFY `onControls` replays the last `notify.controls` | No replay | Late subscribers miss controls | **Phase 1:** push on frame `load`, once per session. Replay check Phase 8 |
| P6 | RESOURCE `bytesMany` maps legacy string entries to `{url}` | Strings are passed through as `requests: ["…"]` | Go's decode of `requests []struct{URL,Servers}` fails → `resource.bytesMany.error invalid-request` (terminal, not a hang) [VERIFIED: backend/nap_resource.go:178-188] | Go tolerance, Phase 7 |
| P7 | **CONFIG: a `config.schemaError` carrying a pending `config.get`'s `id` rejects that get** (marked `// verdana:` in the old file; missing from the shim README and from D-12) | `schemaError` goes only to `onSchemaError`; the pending get waits | `config.get` before any schema (Go replies `config.schemaError` with `id`, `nap_config.go:78`) now rejects only after the 30 s timeout | Decide in the config domain phase (CF-8). Add the row now |

**Timeout decision row:** D-10/D-11 verbatim, plus a pointer to DISP-04.

**Other rows to add now:** NIP-5D "including … reloads" injection (`open → Phase 4`, Pitfall 7); NIP-5D Security 5 / Transport namespace scoping, status `fixed` (Phase 1, SHIM-04 test).

### Android CI (SPEC-05, D-17)

```yaml
on:
  pull_request:
    paths: ["backend/**", "android/**", ".github/workflows/android.yml"]
  push:
    tags: ["v*"]
  workflow_dispatch:

jobs:
  aar:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-24.04
    permissions:
      contents: read
    steps: # checkout, setup-go (backend/go.mod), setup-java 17, setup-android, sdkmanager platform + ndk;$NDK_VERSION,
           # go install gomobile/gobind @$XMOBILE_VERSION, gomobile init, gomobile bind -target=android -androidapi 26 ./mobile
  apk:
    if: github.event_name != 'pull_request'
    permissions:
      contents: write
    # existing job unchanged
```
The current file has only `workflow_dispatch`, and its comment says to restore the triggers. D-17 also wants tags, so add `push: tags`. Recent runs of this workflow took about 6 min including Gradle [VERIFIED: `gh run list --workflow android.yml`]. Use `pull_request`, never `pull_request_target`, and give the PR job read-only permissions. The ubuntu-24.04 image already ships an Android SDK and JDK 17 [CITED: runner-images Ubuntu2404-Readme], but the pinned `NDK_VERSION` 27.2.12479018 still has to be installed by `sdkmanager`.

## State of the Art

| Old approach | Current approach | When changed | Impact |
|---|---|---|---|
| NAP-SHELL `shell.ready`/`shell.init` handshake | NIP-5D presence detection (`window.napplet.<domain>` present means available) | napplet/web #96; shim has no shell domain at 0.30.0 | Session start becomes the host's job |
| NAP-INTENT PR #91 `intent.deliver` | Master: delivery via the convention's mechanism (INC topic event) | Shim 0.28.0 removed delivery (PITFALLS #6) | Go must deliver as `inc.event` |
| `resource.bytesMany {urls}` | `{requests:[{url,servers?}]}` + `maxServers` | Shim 0.30.0 (`19e0029`) | Go already accepts both |
| Vendored patched shim, unverified | Byte-identical vendoring + hash test | This phase | Upgrades become a hash bump + fixture regen |

**Deprecated/outdated:** `ShimVersion = "0.30.0+verdana.2"` and the README sha `6d98d7ba…` (actual file `8e9c3f7b…`) [VERIFIED: backend/webview/embed.go:47; `sha256sum`]. `NAPPLETS.md` shim text is stale until Phase 8.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `gomobile bind` for Android needs a JDK and an SDK platform (`android.jar`) in addition to the NDK, so the AAR job keeps `setup-java` and `sdkmanager "platforms;android-35"` | CI | The job fails at bind. Low risk: keeping the existing steps is safe either way |
| A2 | The iframe `load` event reliably fires after the napplet's synchronous scripts on WebKitGTK, WebView2 and Android WebView for srcdoc frames | Pattern 2 (notify.controls trigger) | A napplet's top-level `onControls` misses the push. Mitigation: the alternative trigger (first `notify.*` envelope) |
| A3 | Replacing the iframe element per session has no side effects on desktop/Android webviews (focus, theme) | Pattern 2 | Minor UI glitch on dev reload only. Easy to revert to srcdoc reassignment, relying on the generation bump alone |
| A4 | Real napplets in the wild register intent handlers with `inc.on("napplet:<archetype>/<action>")` (master model), not `intent.onDelivery` | Pattern 4 | No `onDelivery` usage exists in `~/Projects/napplet-soy` or `~/Projects/napplet-portal` [VERIFIED: grep]. Napplets written against Verdana's patched API would lose delivery either way, since the pristine shim has no `onDelivery` |
| A5 | No PR to master is currently required to pass a status check named for the android workflow | Pitfall 9 | If one is, path filtering will block unrelated PRs |

## Open Questions

1. **Which real napplets go on the D-13 smoke list for config and notify?**
   - What we know: `backend/testdata/nip5d-napplets.jsonl` has `noris` (identity, inc, outbox, relay, resource, theme; author `bbb5dda0e1556797…`) and `hosted-nowhere-opener` (intent caller; author `42d617d410c079de…`). napplet-soy's fixture `tests/fixtures/interoperability/foreign.html` uses `config.registerSchema`/`subscribe`, and its asset helper uses `napplet.resource.bytes('blossom:sha256:…')`. No local napplet uses notify or acts as an intent handler.
   - Recommendation: smoke `noris`, `hosted-nowhere-opener` plus any installed profile/note handler napplet. Add a throwaway dev-folder probe napplet (loaded with `DevLoadFolder`) that exercises `config.registerSchema/get/subscribe`, `notify.onControls/send`, `inc.on("napplet:profile/open")` as an intent handler, and `resource.bytes`. Ask the user for real config/notify napplets if they want those instead.
2. **Should INC-topic intent delivery count as "Phase 1 fixes immediately visible breakage" (D-12)?**
   - Recommendation: yes. Without it every handler napplet silently breaks, and it is the pinned NAP-INTENT master behavior. Flag it in the plan so the user sees it.
3. **Snapshot the NAP-RESOURCE `9511232` tolerance text too?** Recommendation: yes (row 18 above). CF-6 cites it.
4. **Keep or delete exported `NappBaseDir`?** It has no callers outside `backend`, and gomobile binds only `backend/mobile`. Recommendation: delete it, or change it to `(string, error)`. D-03's "used by GUIs" note is inaccurate today.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | all | ✓ | 1.26.7 (go.mod 1.26.2) | — |
| node | SHIM-04 test (gated), fixture regen | ✓ | v26.5.0 | Test skips locally; CI ubuntu image has Node 22 |
| npm | one-off fixture regen in scratch | ✓ | — | — |
| curl / tar / sha256sum | shim fetch + verify | ✓ | — | — |
| git checkouts `~/Projects/{naps,nips,napplet}` with all pinned SHAs | SPEC-01 snapshots, source reads | ✓ | all 18 objects present | `git fetch --depth 1 <repo> <sha>` |
| gh (authenticated) | checking CI runs | ✓ | — | — |
| just | `just run` smoke | ✓ | — | run the recipe commands directly |
| gomobile / gobind | local `just apk`/`just aar` | ✗ | — | CI `aar` job; local proxy `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` (passes, about 7 s) |
| Android SDK at `/opt/android-sdk` | local AAR/APK | ✗ | — | CI only |
| WebKitGTK desktop session | D-13 manual smoke under `just run` | presumably ✓ (dev machine) | — | — |

**Missing dependencies with no fallback:** none that block execution.
**Missing with fallback:** local gomobile/SDK → CI AAR job plus the cross-compile proxy.

## Security Domain

(`security_enforcement: true`, ASVS level 1)

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | yes (napplet session lifecycle) | Session start only from the trusted host page; generation bump drops cross-session envelopes; fresh iframe per session |
| V4 Access Control | yes | `window.napplet` limited to granted domains; injection global unreachable (SHIM-04) |
| V5 Input Validation | yes | `d` treated as opaque; manifest paths checked with `filepath.IsLocal` |
| V6 Cryptography | no new crypto | sha256 only for naming and integrity (stdlib) |
| V12 Files and Resources | yes (core) | User-controlled names never reach the filesystem (hash); containment check before every write, read and `RemoveAll` |
| V14 Configuration / dependencies | yes | Third-party JS pinned by sha256 + npm sha512; `.gitattributes -text`; CI PR job with read-only token |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal via `d` (CWE-22), recursive delete of the data dir (CWE-73) | Tampering / DoS | `hex(sha256(id))` + Rel/IsLocal containment; no FS op on error |
| Manifest asset path escape | Tampering / Info disclosure (icon read) | Shared `nappAssetPath` helper used by writer and icon reader |
| Napplet calls the leaked `NappletShimPrelude.install` to add domains | Elevation of privilege | Function-scoped prelude (Pattern 3) |
| Frame forges session start/replay | Spoofing | Start is a host-page rpc; frame `shell.ready` is ignored |
| Old document's envelopes served in the new session | Spoofing / Info disclosure | `gen++` on start + fresh iframe |
| Vendored JS silently modified | Tampering (supply chain) | Hash test against the npm artifact |
| Unbounded pre-ack queue | DoS | Bound in the host page (refuse beyond the cap) on top of Go's 256-slot queue |
| PR workflow token abuse | Elevation | `pull_request` trigger, `contents: read` |

## Sources

### Primary (HIGH confidence)
- Verdana repo @ `40adf21`: `backend/{backend.go, nap.go, nap_inc.go, nap_intent.go, nap_config.go, nap_resource.go, nap_basic.go, napp.go, napplet.go, napplet_nip5d.go, registry_install.go, registry_updates.go, registry_discovery.go, window_instances.go, window_storage.go, bridge.go, dev.go}`, `backend/napconfig/store.go`, `backend/webview/{embed.go, napplet-host.js, shim_test.go, shim/README.md, shim/prelude.global.js}`, tests `nap_test.go`, `preview_test.go`, `napplet_test.go`, `desktop/child/napplet.go`, `desktop/childproc.go`, `desktop/internal/osintegration/*`, Android `NappWebView.kt`, `.github/workflows/{android,desktop}.yml`, `justfile`; `git show bf29b5d`
- npm tarballs `@napplet/shim` 0.30.0 and 0.29.2 (hash-checked); scratch install of `@napplet/conformance` 0.17.0 (`--ignore-scripts`)
- napplet/web @ `956135b` (`git archive`): `packages/nap/src/{intent,inc,notify,config}/shim.ts`, `packages/conformance/src/validators/envelope-specs.ts`
- Pinned spec texts via `git show` at every SHA in SPEC-PINS.md (quotes above)
- `go doc filepath.IsLocal`, `go doc os.Root` (Go 1.26.7)
- Executed checks: `filepath.Join` escape demo, node vm wrapper test, fixture-vs-handler gap computation, Android cross-compile proxy, `go test -count=1 ./...` baseline (all pass, about 3 s)

### Secondary (MEDIUM)
- `.planning/research/{SPEC-PINS,STACK,FEATURES,PITFALLS,SUMMARY}.md`, `.planning/PROJECT.md` (decisions; re-verified where cited)

### Tertiary (LOW per the seam's web tier; official docs)
- [GitHub Docs: troubleshooting required status checks](https://docs.github.com/en/enterprise-server@3.15/pull-requests/collaborating-with-pull-requests/collaborating-on-repositories-with-code-quality-features/troubleshooting-required-status-checks): path-filtered required checks stay Pending
- [actions/runner-images Ubuntu 24.04 readme](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md): Node 22, JDK 17 default, Android SDK/NDK preinstalled

## Metadata

**Confidence breakdown:**
- Containment (CRIT-01): HIGH. Escape targets were executed, all call sites enumerated, test rig patterns exist.
- Shim provenance and scoping (SHIM-01/04): HIGH. Hashes and the wrapper were verified by execution.
- Session/notify/intent design (SHIM-03): MEDIUM-HIGH. Code paths are verified; webview `load` timing and iframe replacement are assumptions A2/A3.
- Fixture test (SHIM-05): HIGH. Fixture generated and gap computed.
- Specs/checklist (SPEC-01/03): HIGH. Every SHA and quote came from git objects.
- CI (SPEC-05): MEDIUM. Workflow shape follows the existing file; gomobile prerequisites are A1.

**Research date:** 2026-10-02
**Valid until:** 2026-11-01 (re-check the npm `latest` for `@napplet/shim` and draft PR heads before re-pinning)
