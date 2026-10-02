# Stack Research: napplet shim upgrade and reference-implementation delta

**Domain:** Nostr napplet runtime (NIP-5D host), with a vendored `@napplet/shim` prelude and Go NAP handlers
**Researched:** 2026-10-02
**Scope:** This file covers only the napplet reference stack (`@napplet/shim`, `@napplet/nap`, `@napplet/core`, `@napplet/conformance`). Verdana's own stack is in `.planning/codebase/STACK.md` and is not repeated here.
**Overall confidence:** HIGH. Every version, hash and wire claim below comes from the pinned napplet/web checkout (`956135b`), the npm registry tarballs, byte-level diffs against Verdana's vendored file, or the pinned naps checkouts. None of it comes from training data.

> **Confidence labels.** The GSD `classify-confidence` seam rates web providers LOW. These findings rest on primary artifacts instead: git objects at pinned SHAs, npm tarballs with checked integrity, and diffs I ran myself. They are labelled HIGH where an artifact was checked directly and MEDIUM where they depend on reading Verdana code that I did not test.

---

## TL;DR (what the roadmap needs to know)

1. **The functional upgrade to 0.30.0 is already in the tree, but nobody can reproduce it.** `backend/webview/shim/prelude.global.js` is the **0.29.2** build (its bundler chunk names are 0.29.2's) with hand patches applied. Those patches include the one protocol change between 0.29.2 and 0.30.0 (NAP-RESOURCE server hints). Commit `686b4d4` ("record latest napplet shim version") changed only `ShimVersion` and `README.md`, not the file. `ShimVersion = "0.30.0+verdana.2"` is therefore *behaviourally* true and *provenance-wise* false. `NAPPLETS.md` still says "0.29.2, copied unmodified", which is wrong on both counts. (HIGH)
2. **The README hash is stale.** `shim/README.md` says `6d98d7ba…`, but the committed file hashes to `8e9c3f7b41e7d84520f885b48b577e7a29ce00b3f96ea52f989fcebaf2136c56`. The drift came in with `df767df`. No test checks the hash. (HIGH)
3. **0.29.2 → 0.30.0 contains exactly one protocol change:** NAP-RESOURCE `resource.bytes` gains optional `servers`, `resource.bytesMany` changes from `urls: string[]` to `requests: {url, servers?}[]`, and `ResourceInfo.maxServers` is added. Verdana's Go handler (`backend/nap_resource.go`) already accepts both shapes and advertises `maxServers: 8`. **The upgrade forces no further host-side change.** (HIGH)
4. **The real issue is pin divergence, not the upgrade.** The 0.30.0 server-hint wire shape comes from napplet/naps branch `nap-resource` @ `9511232`. The pinned NAP-RESOURCE (PR #80 @ `fa6bcc6`, branch `nub-resource`) still says `urls`. The reference implementation also **does not implement** pinned NAP-SHELL (`shell.ready`/`shell.init`) or pinned NAP-INTENT PR #91 (`intent.deliver`/`onDelivery`). Verdana carries those as shim patches. (HIGH)
5. **`@napplet/conformance` 0.17.0 checks napplets, not shells.** It boots a napplet against a *mock* reference shell in Playwright Chromium. It cannot test Verdana's runtime. Run against Verdana's own injected traffic, it would **fail**: it rejects `shell.ready` (`unknown-domain`) and `intent.deliver` (`unknown-type`), which I verified by running it. **Recommendation: use part of it, as an offline oracle only.** Do not adopt the CLI or the boot harness. (HIGH)

---

## Recommended Stack

### Core reference packages (pin these exact versions)

| Package | Version | Pin | Purpose for Verdana | Why |
|---|---|---|---|---|
| `@napplet/shim` | **0.30.0** | napplet/web `956135bfc41a2cff5e45d6c68d9f9a4d68c50531`; npm integrity `sha512-aGkHuT6NI28rC1x8LXDKnF7XTv8RVf1WieItCJqKag14jQqxUOIuDUqFru1uzE5BIj6TnuqO3LRwvQzxuw7BwQ==`; pristine `dist/prelude.global.js` sha256 `25d6bb0e737e698e0c499e35c1ccd590ef6f87f7a025554d3af41bc6d5890753` (136157 bytes) | Base of the vendored prelude that is inlined into every napplet srcdoc | npm `latest` (published 2026-08-26). Source at `956135b` is identical to the published 0.30.0 for shim/nap/core/conformance: the only commits after `de1cb7e` (Version Packages #207) touch skills/CLI/boilerplate. |
| `@napplet/nap` | **0.32.0** (bundled inside the shim) | same SHA | Per-domain `src/<domain>/shim.ts` and `types.ts`: the reference wire shapes | Secondary oracle for handler shapes. Pinned specs stay primary (see the divergence table). |
| `@napplet/core` | **0.32.0** (bundled) | same SHA | `NAP_DOMAINS`, envelope types | `NAP_DOMAINS` has 23 domains and **no `shell`**, which matters for conformance. |
| `@napplet/conformance` | **0.17.0** | same SHA; npm published 2026-08-26 | Offline oracle: `ENVELOPE_SPECS` (237 types, 114 napplet→shell), `validateManifestEvent`, `reference-responses.ts` | Use partially. Do not make it a runtime or CI dependency. |
| `@napplet/conformance-cli` | 0.2.19 | same SHA | **Not used** | Playwright/Chromium harness that tests *napplets* against a mock shell. Wrong direction for a host audit. |

Previous version, for history: `@napplet/shim` 0.29.2 (npm 2026-08-02, napplet/web `dc1d241`), pristine `prelude.global.js` sha256 `d3539080c553841d0387511cd8c902f15da8122075e0343380ff82a7beab898f`.

### Supporting tools (no JS toolchain enters the build)

| Tool | Purpose | Notes |
|---|---|---|
| `curl` + `tar` + `sha256sum` | Fetch `https://registry.npmjs.org/@napplet/shim/-/shim-0.30.0.tgz`, extract `package/dist/prelude.global.js`, check its hash | Keeps CLAUDE.md's "no JS toolchain" rule. A `just shim` recipe can wrap these steps. |
| `patch` / `git apply` | Apply a committed `backend/webview/shim/verdana.patch` on top of the pristine file | I checked this works: a 77-hunk, 866-line unified diff over pristine 0.30.0 reproduces the current vendored behaviour byte-for-byte (after normalising three bundler comment lines). |
| `node` (optional, test-gated) | Already used by `backend/webview/shim_test.go`, which skips when node is absent | Keep the same gate for any new JS-executing test. Never require node for `go build`. |
| `npm i @napplet/conformance@0.17.0` in a **scratch dir** (one-off) | Regenerate the committed JSON fixture of `ENVELOPE_SPECS` | Run by hand when re-pinning, never in CI. Commit only the JSON output to `backend/testdata/`. |

---

## Q1. What changed between shim 0.29.2 and 0.30.0 (nap up to 0.32.0), and what Verdana must change

### 1a. Upstream changes, 0.29.2 → 0.30.0 / nap 0.31.2 → 0.32.0

Source: `git diff dc1d241 956135b -- packages/{shim,nap,core,conformance}` (34 files) and `diff` of the two npm `prelude.global.js` files (49 lines). Only one protocol commit sits in that range: **`19e0029` feat(resource)!: adopt Blossom server hints (#206)**, which implements naps `nap-resource` @ `9511232`.

| Change | Wire before (0.29.2) | Wire after (0.30.0) | Verdana file | Status in Verdana | Confidence |
|---|---|---|---|---|---|
| `resource.bytes` optional advisory Blossom hints | `{type,id,url}` | `{type,id,url,servers?: string[]}` | `backend/nap_resource.go` (`napResourceBytes`, `fetchBlossomResource`, `blossomServers`) | Already decoded and used | HIGH |
| `resource.bytesMany` payload renamed and restructured (**BREAKING**) | `{type,id,urls: string[]}` | `{type,id,requests: [{url, servers?}]}` | `backend/nap_resource.go` (`napResourceBytesMany`) | Accepts both `urls` and `requests` (concatenated) | HIGH |
| `ResourceInfo.maxServers` (optional) | absent | `maxServers?: uint` | `backend/nap_resource.go` (`resourceMaxServers = 8`, `resource.info.result`) | Already advertised | HIGH |
| Napplet-side `bytesMany([])` error text | `urls must be non-empty` | `requests must be non-empty` | none (shim-internal) | n/a | HIGH |
| Conformance validator: `invalid-resource-request` code; `requests` required | n/a | n/a | none (napplet-side tool) | n/a | HIGH |

Changes in the 0.29.x line (0.29.0 `fs` domain, 0.29.1 refactor, 0.29.2 → nap 0.31.2 intent convention fix `b3f0007`) are **already in the 0.29.2 base**. Verdana does not expose `fs`. Envelope framing, the `install({domains})` activation, the `event.source === window.parent` guard, the `DOMAIN_ROUTERS` set and the error-code vocabulary did **not** change between 0.29.2 and 0.30.0.

**Host-side changes forced by the upgrade itself: none.** `backend/webview/napplet-host.js` never interprets a message beyond its failure-fallback switch (which already covers `resource.bytesMany`). `backend/nap.go` `buildSrcdoc` and the activation script stay as they are.

### 1b. Reference implementation vs pinned specs (affects the audit, not the upgrade)

`@napplet/shim` 0.30.0 does not match the pinned specs everywhere. Each Verdana shim patch exists to close one of these gaps. Each gap is a host obligation the audit has to verify in Go.

| Area | Pinned spec (SPEC-PINS.md) | Reference 0.30.0 / nap 0.32.0 | Verdana today | Files | Roadmap action | Conf. |
|---|---|---|---|---|---|---|
| **NAP-SHELL handshake** | naps master `a040914`: mandatory `window.napplet.shell` (`supports`, `services`, `ready`, `onReady`); `shell.ready` (no payload) → exactly one `shell.init {capabilities, services}`; duplicate ready is idempotent; no capability calls are serviced before the handshake | **Absent.** `shell.test.ts` asserts `installed.shell` is `undefined`; `NAP_DOMAINS` has no `shell`; no `shell.*` router | Shim patch adds `createShellGlobal` and a `["shell.", handleShellMessage]` router; activation script posts `shell.ready`; Go `napReady` sends `shell.init` once and drops calls before `established` | `backend/webview/shim/prelude.global.js`, `backend/nap.go` (`napDispatch`, `napReady`, `buildSrcdoc`) | Keep the patch. Audit the Go side against NAP-SHELL MUSTs. Note: Go also pushes `notify.controls` in the same batch as `shell.init`. | HIGH |
| **NAP-INTENT** | PR #91 @ `a718915`: `invoke(uri, options?)` with URI normalisation, `onDelivery`, `intent.deliver`, result `{ok, archetype?, action?, convention?, handler?, error?}` | Merged-master model: `invoke(request)`, no `onDelivery`/`intent.deliver`, result needs `ok, archetype, action, handled` | Shim patch implements the PR #91 API (`normalizeConventionUri` path, `deliveryHandlers`, `pendingDeliveries`, new result validator) | shim, `backend/nap_intent.go` | Keep the patch. Audit `nap_intent.go` against PR #91, not against nap/src/intent. | HIGH |
| **NAP-RESOURCE wire** | PR #80 @ `fa6bcc6` (branch `nub-resource`, 2026-07-03): `bytesMany {id, urls}`; no `servers`, no `maxServers` | `nap-resource` @ `9511232` (2026-08-22): `requests`, `servers`, `maxServers`, ordered fallback tiers, origin-only HTTPS hints, dedupe and cap, 404/410 = definitive miss, hints excluded from the cache key, mismatch → `decode-failed` | Implements the `9511232` shape and also accepts PR #80 `urls` | `backend/nap_resource.go` | **Re-pin NAP-RESOURCE to `napplet/naps@9511232f69313aa7953d110e35d32cc28d506f66` (branch `nap-resource`)** and record it in SPEC-PINS.md. Every napplet built with current tooling emits `requests`, so auditing against `urls` would flag the only shape real napplets send. After re-pinning, drop the Go `urls` path (strict reading; the shim patch already maps legacy string entries to `{url}` on the napplet side). | HIGH (divergence) / MEDIUM (re-pin choice) |
| Resource server-hint MUSTs (consequence of re-pin) | `9511232`: discard invalid entries (non-HTTPS, credentials, path, query, fragment); dedupe; **consider only the first N** (cap); all definitive misses → `not-found`; any inconclusive → `network-error` | n/a (runtime policy) | `fetchBlossomResource` **rejects** more than 8 hints with `too-large` instead of truncating; `blossomServers` silently turns `https://user@host/path?q` into `https://host` instead of discarding it; the final error is "last attempt's error", not the not-found/network-error rule | `backend/nap_resource.go` | Audit items. Fix with regression tests. | MEDIUM |
| `relay.publish` failure envelope | PR #2 @ `0be8abc`: only `relay.publish.result {id, ok:false, error}` | Shim accepts `relay.publish.error` *and* `.result` | Go emits `relay.publish.error` on an invalid event; `napplet-host.js` fallback also emits `relay.publish.error` | `backend/nap_relay.go:386`, `backend/webview/napplet-host.js` (fallback switch) | Switch to `relay.publish.result {ok:false}`. Works with the shim either way. | HIGH |
| `config.get` failure | PR #14 @ `448013e`: `config.schemaError {error, code}` has **no `id`**; `config.get` is answered only by `config.values {id, values}` | No correlation; `config.get` waits forever on error | Shim patch: a `config.schemaError` carrying a pending get's `id` rejects that get; Go (`nap_config.go:78`) and the `napplet-host.js` fallback send `schemaError` with an `id` | shim, `backend/nap_config.go`, `napplet-host.js` | Record as a Verdana extension (an extra field the spec does not forbid) or replace it. Decide during the audit. | MEDIUM |
| Request deadlines | Specs: "every request gets one terminal result or error" (e.g. NAP-RESOURCE) | 30 s (storage 5 s) shim-side timeouts | Shim patch removes default deadlines (`napRequestTimer`); the host must answer every request, including on panic (`c.fail`) and after teardown | shim, `backend/nap.go` (`napDispatch` recover), every `nap_*.go` | Audit: one terminal reply per correlated request, on every path. | HIGH |
| `notify.controls` late subscriber | PR #11 @ `e14f5c9` | `onControls` registered after the push gets nothing | Shim patch replays the last controls | shim | Napplet-side only. No host change. | HIGH |
| NAP-INC empty query / nameless param | naps master | Accepted | Shim patch rejects them | shim | Napplet-side only. | HIGH |

**Rule for the audit:** pinned spec first, `nap/src/<domain>/{shim,types}.ts` @ `956135b` second. `NAPPLETS.md` step 3 ("Re-check the handlers against `nap/src/*/shim.ts`") is wrong for `shell`, `intent` and `resource`, and needs rewording.

I diffed the message-type names of every implemented domain between pinned spec wire tables and `nap/src` (`scratchpad/cmp.py`). The wire tables match the reference for `relay, identity, storage, theme, link, common, inc, media, notify, config, outbox, upload`. The apparent mismatches in media (`media.createSession` …), notify (`notify.registerChannel` …) and config come from the specs' *API* tables, which name methods in the "Wire" column, while their *Wire Protocol* tables use the reference names (`media.session.create`, `notify.channel.register`, `notify.permission.request`). That is an internal inconsistency in the drafts. Record it as an ambiguity and conform to the Wire Protocol table. (HIGH)

---

## Q2. `@napplet/conformance` 0.17.0: what it is and whether Verdana can use it

**What it is.** A framework-agnostic engine that answers "does this **napplet** conform?". It contains:
- `validateEnvelope(msg)`: validates a *napplet-emitted* envelope against `ENVELOPE_SPECS` (237 `domain.action` types with direction and required fields; a drift test keeps it in lockstep with `@napplet/nap` source).
- `validateManifestEvent(event)`: NIP-5D manifest checks (kinds 5129/15129/35129, `d` tag rules, hashed `/index.html` path tag, bare known `requires`).
- `createReferenceShell` / `attachReferenceShell`: a **mock** shell with canned `RESPONDERS`.
- `bootAndCollect`: loads the napplet in a `sandbox="allow-scripts"` iframe with the harness's *own* minimal `runtimePrelude` (not `@napplet/shim`), records envelopes, and runs a no-domains degraded pass.
- A check catalog of 9 checks: `manifest/*` ×3, `boot/*` ×4, `wire/envelope-well-formed`, `degrade/domain-absence`, plus a `lifecycle/clean-teardown` warning. Reporters: pretty, JSON, JUnit.

**How it is run.** Through `napplet-conformance` (`@napplet/conformance-cli` 0.2.19). It serves a built napplet on loopback, launches **Playwright headless Chromium** (`npx playwright install chromium`), attaches the reference shell, and exits 0/1/2. Alternatively, `apps/conformance` is a standalone web runtime, or the engine can be imported into a custom harness.

**Can Verdana run it against its runtime?** No, not in any useful way. (HIGH)
- **Wrong subject.** Every check grades the napplet. The shell under test is always the reference mock. No mode accepts an external shell adapter.
- **Wrong engine.** Verdana's runtime is WebKitGTK (desktop child) and Android WebView, with Go behind `nap.msg`. The harness uses Chromium and its own JS shell.
- **It contradicts the pinned specs.** I installed 0.17.0 and fed it Verdana's traffic. `validateEnvelope({type:"shell.ready"})` returns `unknown-domain`, `validateEnvelope({type:"intent.deliver"})` returns `unknown-type`, and `bytesMany` with `urls` returns `missing-field requests`. A napplet running under Verdana's injected prelude would fail `wire/envelope-well-formed` because of Verdana's *spec-correct* `shell.ready`.

**Recommendation: use it partially, as an offline oracle. Do not run it.** (HIGH)
1. **Envelope inventory fixture.** Generate `backend/testdata/napplet-conformance-0.17.0-envelopes.json` from `ENVELOPE_SPECS` with a one-off script in a scratch dir, and commit it with the version and SHA in the filename or a header. Add a Go test (`TestNAPHandlersCoverReferenceEnvelopes`) that asserts every `dir:"out"` type of every domain in `napDomains` either has an entry in `napHandlers` or appears in an explicit, commented N/A list. Assert the reverse direction too: every type Go pushes or replies with is a known `in` type, plus an explicit allowlist for spec-only types (`shell.init`, `intent.deliver`). This catches handler gaps and renames mechanically and adds no JS dependency.
2. **Manifest cases.** Port `validators/manifest.test.ts` cases into Go table tests for `backend/napplet_nip5d.go`. Keep NIP-5D @ `24711d9` authoritative: it requires full aggregate-hash recomputation, which the conformance validator does not do.
3. **Response shapes.** Use `shell/reference-responses.ts` only as a hint when writing Go golden tests. Its responses are canned and may lag the drafts.
4. **Not used:** `conformance-cli`, `bootAndCollect`, Playwright, `apps/conformance`. Real end-to-end runtime testing (a probe napplet in WebKitGTK) is out of proportion for this milestone. The existing Go dispatcher tests (`backend/nap_test.go`) are the right level.

---

## Q3. How Verdana vendors the shim today, and the exact upgrade procedure

### Today

| Item | Location | State |
|---|---|---|
| Prelude | `backend/webview/shim/prelude.global.js` (141278 bytes), embedded via `//go:embed` in `backend/webview/embed.go`, inlined by `buildSrcdoc` (`backend/nap.go`) with `</script` escaped, activated with `globalThis.NappletShimPrelude.install({"domains":napDomains})` then `window.parent.postMessage({type:"shell.ready"},"*")` | 0.29.2 build plus 6 patch groups (timeouts, notify controls replay, INC query validation, config get rejection, resource legacy-string normalisation, PR #91 intent, NAP-SHELL). Patches are inline, so there is no patch file. Some are marked `// verdana:`, others are not. |
| Version | `ShimVersion = "0.30.0+verdana.2"` (`embed.go:47`) | **Not referenced anywhere else.** Informational only. |
| Provenance doc | `backend/webview/shim/README.md` | Says 0.30.0 plus patch list; **sha256 is stale** (`6d98d7ba…` vs actual `8e9c3f7b…`) |
| User doc | `NAPPLETS.md` "Updating the shim" and "How a napplet runs" | **Stale:** says "0.29.2, copied unmodified". The domain table also lists `notify`/`config` as unimplemented, though `nap_notify.go` and `nap_config.go` exist. |
| Test | `backend/webview/shim_test.go` `TestShimProvidesShellCapabilityDiscovery` | String checks plus a node-gated behavioural check of the shell patch. No hash check. |
| Consumers | Desktop child host and Android (through the gomobile AAR) both embed the same file | Android picks up the change on the next `just apk`. |

### Exact upgrade/rebase procedure (prescriptive)

The goal is a reproducible vendored file: pristine upstream + committed patch = embedded bytes, enforced by tests.

1. **Fetch the pristine file and check it**
   ```bash
   curl -sL https://registry.npmjs.org/@napplet/shim/-/shim-0.30.0.tgz -o /tmp/shim.tgz
   # optional: compare against the npm dist.integrity sha512 recorded above
   tar xzf /tmp/shim.tgz -C /tmp package/dist/prelude.global.js
   sha256sum /tmp/package/dist/prelude.global.js   # must be 25d6bb0e…0753
   ```
2. **Create `backend/webview/shim/verdana.patch` once.** Take `diff -u` of pristine 0.30.0 against the current vendored file, after rewriting the three `// ../nap/dist/chunk-*.js` comment lines to their 0.30.0 names (`6RQMV3K3`, `5XY2AH5L`, `XKRW6Q4Q`). Split it, or annotate each hunk group, by the spec pin it implements: NAP-SHELL `a040914`, NAP-INTENT PR #91 `a718915`, no-deadline, NAP-NOTIFY, NAP-INC, NAP-CONFIG, NAP-RESOURCE legacy strings. Mark every patched region with `// verdana:`.
3. **Apply it:** `cp pristine prelude.global.js && patch prelude.global.js < verdana.patch`. Commit pristine hash, patched hash and patch together.
4. **Record provenance:**
   - `embed.go`: `ShimVersion = "0.30.0+verdana.3"`. Add `ShimUpstreamSHA256 = "25d6bb0e…"` and `ShimSHA256 = "<patched>"`.
   - `shim/README.md`: upstream version, napplet/web commit `956135b`, npm integrity, both hashes, patch list mapped to spec SHAs.
5. **Add tests** (backend, Go standard `testing`):
   - `TestShimPreludeHash`: sha256 of the embedded `shimPrelude` equals `ShimSHA256`. This would have caught the `df767df` drift.
   - Extend `shim_test.go` (node-gated) to cover the intent `onDelivery` buffering and the `resource.bytesMany` envelope shape (`requests`).
   - `TestNAPHandlersCoverReferenceEnvelopes` against the conformance fixture (Q2 step 1).
6. **Optional `just shim` recipe** wrapping steps 1–3 (curl, tar, sha256sum, patch only; no npm).
7. **Update the docs:** `NAPPLETS.md` "Updating the shim" (procedure above; oracle order is pinned spec first, `nap/src` @ `956135b` second) and the "How a napplet runs" version string. Update `.planning/research/SPEC-PINS.md` (NAP-RESOURCE re-pin; note that the vendored file is now pristine-plus-patch).
8. **Verify:** `cd backend && go test ./...`, then `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`. Smoke-test one napplet per patched domain (shell, intent, resource, notify, config) with `just run`, and run `just apk` to confirm Android still builds.

Doing this first fits the "upgrade shim before auditing" decision. The rebase is mechanical, about half a day, and makes every later audit diff legible.

---

## Alternatives Considered

| Recommended | Alternative | When to use the alternative |
|---|---|---|
| Pristine npm dist + committed `verdana.patch` | Keep editing `prelude.global.js` in place | Never. In-place edits are how the hash drift and the false "0.29.2 unmodified" doc happened. |
| Pristine npm dist | Build the shim from napplet/web source with pnpm/tsup | Only if a future upstream fix is merged but not yet published. It needs pnpm and a workspace install, which breaks the no-toolchain rule. |
| Patch NAP-SHELL and PR #91 intent into the shim | Wait for napplet/web to implement them | When napplet/web ships them. As of `956135b` no open PR does (open PRs: #205, #187, #116, #111, #79). Re-check at each re-pin. |
| Conformance as a committed JSON oracle | Run `napplet-conformance` in CI | If Verdana later ships first-party napplets, use the CLI on *those napplets* (with `--no-degraded` as needed), never as a host test. |
| Re-pin NAP-RESOURCE to `nap-resource@9511232` | Keep PR #80 `fa6bcc6` and conform to `urls` | If the maintainers move server hints into PR #80 under a different shape. Check `gh pr view 80 -R napplet/naps` before the audit phase. |

## What NOT to Use

| Avoid | Why | Use instead |
|---|---|---|
| `@napplet/conformance-cli` / Playwright as a Verdana test | Tests napplets against a mock shell in Chromium. It cannot exercise Go handlers or WebKitGTK, and it rejects spec-required `shell.ready`/`intent.deliver` | Go dispatcher tests in `backend/nap_test.go` plus the envelope fixture |
| `@napplet/shim` `renderNappletRuntimePreludeScript` / `normalizePreludeDomains` semantics as the boot contract | Upstream has no shell handshake. Its `install` filters by `NAP_DOMAINS`, which has no `shell` | Verdana's `buildSrcdoc` (CSP → prelude → install → `shell.ready`) |
| `nap/src/intent`, `nap/src/shell`, `nap/src/resource` @ `956135b` as the oracle for those domains | They diverge from the pinned PR #91, NAP-SHELL master and (after re-pin) `9511232` | The pinned spec text |
| `ShimVersion` as proof of provenance | Nothing checks it | `ShimSHA256` plus a hash test |

## Version Compatibility

| Package | Compatible with | Notes |
|---|---|---|
| `@napplet/shim@0.30.0` | `@napplet/nap@0.32.0`, `@napplet/core@0.32.0` (bundled) | One coordinated release, `de1cb7e` (2026-08-26) |
| `@napplet/conformance@0.17.0` | `@napplet/nap@0.32.0`, `@napplet/core@0.32.0` | Its `ENVELOPE_SPECS` matches shim 0.30.0's wire output: `requests`, not `urls` |
| Shim 0.30.0 `resource.bytesMany` | Verdana `nap_resource.go` | Already accepts `requests` and legacy `urls` |
| Shim 0.30.0 (unpatched) | Verdana Go host | **Incompatible.** Without the NAP-SHELL patch the shim never routes `shell.init`, so `window.napplet.shell` is absent and `supports()` cannot be answered, violating NAP-SHELL's "mandatory". The patches are required, not cosmetic. |

## Sources

- napplet/web @ `956135bfc41a2cff5e45d6c68d9f9a4d68c50531` (scratchpad checkout): `packages/{shim,nap,core,conformance,conformance-cli}/package.json` and `CHANGELOG.md`; `packages/shim/src/{runtime,prelude}.ts`, `shell.test.ts`; `packages/conformance/src/**` (HIGH)
- napplet/web history fetched to depth 80: `dc1d241` (0.29.2 release), `19e0029` (server hints, PR #206 body cites naps `9511232`), `de1cb7e`/`956135b` (version packages) (HIGH)
- npm registry: `@napplet/shim` 0.29.2/0.30.0 tarballs (byte diff: 49 lines, resource only), dist-tags and publish times for shim/nap/core/conformance/conformance-cli (HIGH)
- `npm i @napplet/conformance@0.17.0` in scratch, live `validateEnvelope` calls on `shell.ready`, `intent.deliver` and `bytesMany{urls}` (HIGH)
- napplet/naps: pinned checkouts (master `a040914`, PR #91 `a718915`, PR #80 `fa6bcc6`, PR #2, #10, #11, #14, #32); `gh api` for branch `nap-resource` @ `9511232` (2026-08-22) and its comparison with `fa6bcc6` (diverged, 31 ahead / 11 behind) (HIGH)
- nostr-protocol/nips PR #2303 @ `24711d9` `5D.md` (injection, sandbox, CSP clauses) (HIGH)
- Verdana: `backend/webview/{embed.go,shim/README.md,shim/prelude.global.js,shim_test.go,napplet-host.js}`, `backend/nap.go`, `backend/nap_resource.go`, `backend/nap_relay.go`, `backend/nap_config.go`, `NAPPLETS.md`; `git log`/`git show` of `686b4d4`, `1df181c`, `5bd9857`, `df767df` (HIGH for file state, MEDIUM for behavioural readings of Go code that I did not run)
