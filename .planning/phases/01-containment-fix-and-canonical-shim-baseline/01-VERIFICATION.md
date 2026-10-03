---
phase: 01-containment-fix-and-canonical-shim-baseline
verified: 2026-10-03T01:54:15Z
verified_at_commit: 5dcc49e
status: human_needed
score: 53/56 must-haves verified (5/5 roadmap success criteria, 48/51 plan truths; 3 plan truths are human-only by design)
behavior_unverified: 0
overrides_applied: 0
deferred:
  - truth: "A napplet that reloads its own frame gets a fresh session (WR-05 remainder; NIP-5D 5D-3 / NIP-5D-reload rows open)"
    addressed_in: "Phase 4"
    evidence: "Phase 4 SC1: 'When a napplet reloads or navigates its own frame, its old session is torn down (stale subscriptions stop) and the new document gets a fresh window.napplet with the CSP intact' (SBOX-01)"
  - truth: "Storage/config file names cannot collide across d values (CONFORMANCE CF-2, WR-07 code part)"
    addressed_in: "Phase 5"
    evidence: "Phase 5 SC1: '... no two d values map to the same file' (KEY-04)"
flagged_prohibitions:
  - statement: "MUST NOT derive the injected domain set from napplet-declared tags (requires, R/O): install({domains}) receives only the launcher's napDomains policy (01-04, verification: test)"
    disposition: unverified
    flagged: true
    llm_judge_verdict: "holds by inspection (non-authoritative): nappletBoot (backend/nap.go:532) is the only production caller of buildSrcdoc and passes the package var napDomains; no per-napp value reaches it"
    gap_in_enforcement: "No test drives nap.boot with a napplet whose manifest declares requires/R/O tags and asserts install({domains}) == napDomains. TestSrcdocLeavesOnlyWindowNapplet and TestProbeNappletFolderLoads call buildSrcdoc(…, napDomains) directly, bypassing nappletBoot"
    note: "unverified-prohibition — human review recommended"
  - statement: "MUST NOT automatically delete, sweep or migrate existing napps/{raw-id} directories (01-01, verification: judgment)"
    disposition: judgment
    flagged: true
    llm_judge_verdict: "holds (non-authoritative): no os.ReadDir / directory enumeration exists in non-test backend code; RemoveAll is called only on nappBaseDir results (registry_install.go:63, :92)"
    note: "judgment-tier — human resolution requested at the end-of-phase checkpoint"
  - statement: "MUST NOT record a Conflicts reading that relaxes a NIP-5D security MUST (01-05, verification: judgment)"
    disposition: judgment
    flagged: true
    llm_judge_verdict: "holds (non-authoritative): A2 and A3 keep 'never sign or publish napplet ciphertext'; A18 follows NIP-5D presence detection; A23 (user decision) broadcasts inc.emit per NAP-INC and does not touch a NIP-5D MUST"
    note: "judgment-tier — human resolution requested at the end-of-phase checkpoint"
human_verification:
  - test: "D-13 smoke step 1 — under `just run` (fresh child build), load the dev folder backend/testdata/probe-napplet and open 'Verdana probe'"
    expected: "PASS 'NappletShimPrelude is undefined' in the real WebKitGTK frame; PASS listing exactly the 14 napDomains; a `controls` line (not the 3 s FAIL), proving `load` fires after top-level scripts (DEC-2); a config.get PASS with greeting 'hi'"
    why_human: "Frame scope and load ordering are proven only in a node vm stand-in (TestSrcdocLeavesOnlyWindowNapplet, napplet_host_test.go); a real webview engine is needed"
  - test: "D-13 smoke step 2 — from the tray user menu open your profile, pick 'Verdana probe' if asked"
    expected: "Exactly one `intent` inc.event line with sender `launcher` and your pubkey in the payload (handler napplet inc.on firing)"
    why_human: "End-to-end intent routing through the chooser and a live webview"
  - test: "D-13 smoke step 3 — open two probe windows, click 'emit ping' in one"
    expected: "Only the other window logs `ping from <sender>`; the emitter does not hear itself"
    why_human: "Cross-window INC through two child processes"
  - test: "D-13 smoke steps 4-5 — click 'notify' and approve; click 'fetch resource' and approve"
    expected: "An OS notification titled 'Verdana probe'; the probe logs an image type/size and shows the image"
    why_human: "OS notifications, consent prompts, real network"
  - test: "D-13 smoke steps 6-7 — install and launch noris and hosted-nowhere-opener; open a profile through hosted-nowhere-opener"
    expected: "noris loads content with no 'timed out' or uncaught shim errors in devtools (WEBVIEW_DEBUG); the probe logs an intent whose sender is hosted-nowhere-opener's d"
    why_human: "Existing real napplets (intent, config, notify, resource, INC) keep working with no shell.ready/shell.init handshake — roadmap SC3's second half; needs real relays"
  - test: "D-13 smoke step 8 — dev-reload the probe, repeat step 2"
    expected: "Log restarts; exactly one new intent line (no duplicate from the old session); the fresh iframe causes no focus loss and no theme flash; notify.controls arrives on load"
    why_human: "Fresh-iframe focus/theme side effects and session replacement are visual/real-engine behaviors"
  - test: "D-13 smoke step 9 — uninstall noris and inspect {dataDir}/napps"
    expected: "Entries from this build all have 64-hex names; nothing outside napps/ was touched"
    why_human: "On-disk check in the real data directory (automated equivalent passes in TestHostileDTagStaysInsideDataDir)"
  - test: "SPEC-05 — open a real GitHub pull request touching backend/** (and one touching only unrelated paths)"
    expected: "The android workflow's `aar` job starts for the backend PR and runs gomobile bind; it does not start for the unrelated PR; a deliberately broken backend/mobile change fails the job"
    why_human: "Trigger behavior and the gomobile/NDK bind can only be observed on GitHub Actions; no Android SDK/NDK exists locally, so only the GOOS=android cross-compile proxy and the workflow structure were checked (01-03 truth tagged verification: backstop)"
  - test: "Resolve the three flagged prohibitions listed in flagged_prohibitions"
    expected: "Accept the non-authoritative verdicts, or ask for a nap.boot-level test for the domains-from-tags prohibition"
    why_human: "Judgment-tier items need explicit human resolution; the test-tier item has no wired test on the production path"
---

# Phase 1: Containment Fix and Canonical Shim Baseline Verification Report

**Phase Goal:** No napp or napplet `d` tag can write or delete outside the data directory, and the conformance audit starts from a reproducible baseline: upstream `@napplet/shim` 0.30.0 vendored byte-identical, pinned spec texts committed, an audit checklist skeleton with a Conflicts section, and CI that catches Android breakage
**Verified:** 2026-10-03T01:54:15Z (HEAD `5dcc49e`, after review/fix iteration 3)
**Status:** human_needed
**Re-verification:** No (initial verification)

All automated evidence passes. Several claims were checked against upstream directly instead of only through the repo's own self-referential hash tests:

- The vendored prelude is byte-identical to the npm 0.30.0 tarball. `cmp` against a fresh download from registry.npmjs.org found no difference, and the tarball sha512 matches npm's published integrity.
- The conformance fixture's 237 envelope specs equal `ENVELOPE_SPECS` exported by the npm `@napplet/conformance@0.17.0` dist.
- All 18 pinned snapshot bodies hash to their `body_sha256`. That hash equals the sha256 of `raw.githubusercontent.com/{repo}/{commit}/{path}` for every one of them.

What remains is the end-of-phase real-webview smoke, the live GitHub PR trigger for the Android job, and three flagged prohibitions.

## Goal Achievement

### Roadmap Success Criteria

| # | Success criterion | Status | Evidence |
|---|-------------------|--------|----------|
| 1 | Hostile `d` (`..`, `../../..`, `a/b`, `/../x`) for napp 35130, NIP-5D and WEB-NAPPLET: install, launch, update, uninstall and failed install read, write and remove only inside the data dir. `d` stays unchanged in identity, storage keys and wire. Regression tests cover each case | ✓ VERIFIED | `nappBaseDir`/`nappBaseDirIn` (backend/backend.go:136-153) hashes the id, checks `Rel`+`IsLocal`, and refuses a non-absolute dataDir. `nappAssetPath` (:160-176) is the shared asset rule. Every FS caller goes through them: registry_install.go:52/63/89-92/267, registry_updates.go:143, napp.go:191-192, nap.go:556, window_instances.go:503. No other `"napps"` join exists, and exported `NappBaseDir` is gone. `TestHostileDTagStaysInsideDataDir` runs 12/12 subtests (3 shapes × 4 d) across all 5 operations. It asserts a dataDir sentinel, a parent-dir sentinel and state.json survive; that only 64-hex entries appear under napps/; that `n.D==d` and `n.ID` are raw; that `spec.NappID` is raw; and that the storage file stays under storage/. The adjacency, empty and ordering edges are covered in `TestNappBaseDirIsHashedAndContained` and `TestNappAssetPathStaysInsideBase` |
| 2 | `go test` fails on a 1-byte difference from npm `@napplet/shim` 0.30.0, and `ShimVersion` and the README state that version and sha256 | ✓ VERIFIED | `TestShimPreludeIsPristineUpstream` (backend/webview/shim_test.go) compares the sha256 of the embedded bytes to `ShimSHA256` and requires the README to contain `**0.30.0**` and the hash. File sha256 is `25d6bb0e…0753`, 136157 bytes, no trailing newline. **Independently verified:** `cmp` against `package/dist/prelude.global.js` from the npm 0.30.0 tarball reports IDENTICAL, and the tarball sha512 equals npm's `dist.integrity`. `.gitattributes` marks the file `-text`, and desktop CI runs `go test ./...` in backend/ |
| 3 | `window.napplet` holds only granted domains; `NappletShimPrelude` is unreachable and cannot install domains; existing napplets work without the shell.ready/shell.init handshake; every dropped patch has a checklist row | ✓ VERIFIED (automated); real-webview confirmation in Human Verification | `buildSrcdoc` (nap.go:612-630) wraps the pristine prelude and `install` in `(function(){…})()`. `TestSrcdocLeavesOnlyWindowNapplet` (node vm) shows the only added global is `napplet`, `typeof NappletShimPrelude` is `undefined`, and keys equal napDomains. Its control run proves the probe can detect a leak. Sessions start only via the host-page `nap.start` rpc (`napStart`, nap.go:449). A frame-sent `shell.ready` is dropped (`TestNapFrameShellReadyIsIgnored`), and `shell.init` is never pushed. Go wire-shape tests for all 14 domains pass with `nap.start`. CONFORMANCE.md has rows P1-P7 |
| 4 | A test built from the offline `@napplet/conformance` 0.17.0 fixture fails when the shim can send a request type with neither a handler nor an explicit N/A entry | ✓ VERIFIED | `TestNAPHandlersCoverReferenceEnvelopes` passes, along with its 8 self-mutation subtests: missing handler, missing bidirectional handler, shim drift, version drift, empty fixture, stray handler, N/A domain offered, unknown domain. **Independently verified:** the fixture's `envelopes` equal the sorted `ENVELOPE_SPECS` from the npm `@napplet/conformance@0.17.0` dist (237 = 237, JSON-equal) |
| 5 | Every pinned spec is committed at its SHA. Conflicts records the four required readings. A PR that breaks the Android AAR fails CI | ✓ VERIFIED (structure); live PR trigger in Human Verification | 18 snapshots under spec/pinned/ (17 pins + the NAP-RESOURCE 9511232f tolerance) with commits matching SPEC-PINS.md. `TestPinnedSpecSnapshotsMatchTheirHashes` passes. **Independently verified:** all 18 match upstream raw text at their commit. CONFORMANCE.md Conflicts has A18 (NAP-SHELL vs NIP-5D presence), A1 (WEB-NAPPLET legacy 35129 vs NIP-5D), A2 (NAP-RELAY decrypt vs NIP-5D cleartext) and A3 (NAP-OUTBOX kind-1059), each quoting both sides, plus A4-A17 and A19-A23. In .github/workflows/android.yml, the `aar` job runs on `pull_request` with paths `backend/**`, `android/**` and the workflow file. It runs `gomobile bind -target=android -androidapi 26 ./mobile` with XMOBILE_VERSION equal to backend/go.mod, and has no `needs`. `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes locally |

**Score:** 5/5 roadmap SCs verified. For plan truths, 48/51 are verified and 3 are human-only by design: the 01-03 PR-trigger backstop and the two 01-05 `just run` smoke truths. 0 truths are present-but-behavior-unverified.

### Plan must_haves (current code, after review iterations 1-3)

| Plan | Truths | Result | Notes |
|------|--------|--------|-------|
| 01-01 CRIT-01 | 8 | 8 ✓ | All verified by the four containment tests, which pass. Uninstall with a nappBaseDir error skips RemoveAll and still forgets the napp (registry_install.go:89-100). IconBlob falls back to the hash-verified download |
| 01-02 shim, intents, oracle | 10 | 10 ✓ | `intent.deliver` is no longer pushed anywhere. `dispatchToNapplet` (window_instances.go:1116) now loops inline on `s.established && s.topics[name]` and the `ci.changed` signal closed by `registerAction` (from `napIncSubscribe`), not the planned `waitForHandler` helper. The intent of the key link holds. It pushes to the resolved `ci` only, and the reserved `launcherSender` is the sender for launcher-fired intents. Tests: `TestIntentDeliveryToNapplet`, `…ReachesOnlyTheHandler`, `…WaitsForTheReceivingSession`, `…TimesOutWithoutSubscriber` (errNoHandler) |
| 01-03 pins, Android CI | 11 | 10 ✓, 1 human | The 11th truth (path filter vs real trigger) is tagged `verification: backstop`, so it is routed to human. aar/apk `if` conditions are mutually exclusive. The concurrency group is keyed on workflow, event and ref, with cancel-in-progress only on PRs |
| 01-04 nap.start, scope | 8 | 8 ✓ | One intentional deviation. The truth says notify.controls is pushed "once per session". The code now pushes on **every** load of the current frame (`napLoaded`, nap.go:466-487; `TestNapLoadedPushesControlsOnEveryLoad`), so a self-reloaded document also gets controls. This is recorded as DEC-2 and P5 in CONFORMANCE.md. Without a self-reload, one load still means one push. The plan artifact `func (ci *Instance) napStart() error` is now `napStart() (int, error)`, because review added the session gen that `__nap_push(gen, json)` uses. MAX_PENDING=256 with terminal refusal, and lifecycle calls bypass the bound (`TestNappletHostBoundsPendingEnvelopes`, `…LifecycleBypassesPendingBound`). desktop.yml sets `VERDANA_REQUIRE_NODE: "1"` |
| 01-05 CONFORMANCE skeleton | 14 | 12 ✓, 2 human | Sections are ordered per pin, and the Conflicts row set is now A1-A23 (A23 was added by user decision), with P1-P7 and DEC-1..3. `TestConformanceChecklistSkeleton` passes. Every `Test*` and symbol cited in CONFORMANCE.md exists. Truths 13-14 (probe and real napplets under `just run`) are the D-13 smoke list |

### Deferred Items

| # | Item | Addressed In | Evidence |
|---|------|-------------|----------|
| 1 | Frame self-reload keeps its session, subscriptions, grants and actions (WR-05 remainder; CONFORMANCE `5D-3`, `NIP-5D-reload` open) | Phase 4 | SBOX-01 / SC1 "old session is torn down … new document gets a fresh `window.napplet`" |
| 2 | `safeFileName` storage/config filename collisions across `d` values (CF-2) | Phase 5 | KEY-04 / SC1 "no two `d` values map to the same file" |

Accepted by the user, not gaps: CR-01, where legacy `napps/{raw-id}` dirs are orphaned on upgrade (D-04; no sweep code exists). A23, where napplet `inc.emit` on intent convention topics broadcasts per NAP-INC.

### Required Artifacts

| Artifact | Status | Details |
|----------|--------|---------|
| `backend/backend.go` nappBaseDir / nappAssetPath | ✓ VERIFIED | Signatures as planned. Six production callers, all stop on error |
| `backend/containment_test.go` | ✓ VERIFIED | 457 lines, 4 tests, 12-case matrix, parent-dir sentinel |
| `backend/webview/shim/prelude.global.js` | ✓ VERIFIED | Byte-identical to the npm 0.30.0 dist (independent cmp) |
| `backend/webview/embed.go` ShimVersion/ShimSHA256 | ✓ VERIFIED | `0.30.0` / `25d6bb0e…0753` |
| `.gitattributes` | ✓ VERIFIED | Prelude and `spec/pinned/*@*.md` are `-text` |
| `backend/testdata/napplet-conformance-0.17.0-envelopes.json` | ✓ VERIFIED | Equals the npm ENVELOPE_SPECS |
| `backend/nap_conformance_test.go` | ✓ VERIFIED | Oracle with self-mutation subtests |
| `spec/pinned/*` + README | ✓ VERIFIED | 18 files, all equal upstream |
| `backend/spec_pinned_test.go` | ✓ VERIFIED | Reads `../spec/pinned`, checks hashes and README order |
| `.github/workflows/android.yml` | ✓ VERIFIED (structure) | aar job on PR, apk on dispatch/tag |
| `backend/nap.go` napStart/napLoaded/buildSrcdoc | ✓ VERIFIED | `napStart` returns `(int, error)`, an intentional post-review signature |
| `backend/webview/napplet-host.js` | ✓ VERIFIED | nap.start before a fresh frame, bootSerial, MAX_PENDING, gen-tagged `__nap_push`, nap.loaded on load |
| `backend/nap_scope_test.go`, `backend/webview/napplet_host_test.go` | ✓ VERIFIED | Node-backed, mandatory in CI |
| `spec/CONFORMANCE.md`, `backend/spec_conformance_test.go` | ✓ VERIFIED | 239 lines; structural guard passes |
| `backend/testdata/probe-napplet/`, `backend/dev_probe_test.go` | ✓ VERIFIED | The probe's scripted parts run in node |

### Key Link Verification

The gsd `verify.key-links` tool reported false negatives because of double-escaped YAML regexes, an EISDIR on a directory target, and cross-language targets. Each link was checked by hand:

| From | To | Status | Evidence |
|------|----|--------|----------|
| registry_install.go InstallNapp/Uninstall | nappBaseDir | WIRED | :52 `base, err := nappBaseDir(n.ID)`; :89 |
| registry_install.go fetchNappAsset | nappAssetPath | WIRED | :267, before `downloadBlob` |
| napp.go IconBlob | nappBaseDir + nappAssetPath | WIRED | :191-192, falls back to the hash-verified download |
| window_instances.go launchWithDocument | nappBaseDir | WIRED | :503 `appDir, err := nappBaseDir(id)` |
| webview/shim_test.go | embed.go ShimSHA256 | WIRED | |
| window_instances.go dispatchToNapplet | nap_inc.go napIncSubscribe | WIRED (inline, not via `waitForHandler`) | `s.topics[r.Topic]=true` + `registerAction` closes `ci.changed` |
| nap_conformance_test.go | webview.ShimVersion | WIRED | Shim-drift check |
| spec_pinned_test.go | spec/pinned | WIRED | `pinnedSpecDir = "../spec/pinned"` |
| android.yml aar | backend/mobile | WIRED | `gomobile bind … ./mobile` |
| napplet-host.js boot | nap.go napStart | WIRED | `enqueue(() => rpc("nap.start"), true)` → napRPC case `nap.start` |
| napplet-host.js iframe load | nap.go napLoaded | WIRED | `rpc("nap.loaded")` → napRPC case |
| nap.go buildSrcdoc | embed.go ShimPrelude | WIRED | `(function(){` + prelude + install + `})()` |
| desktop.yml | nap_scope_test.go | WIRED | `VERDANA_REQUIRE_NODE: "1"` on the backend test step |
| spec_conformance_test.go | spec_pinned_test.go | WIRED | Reuses `pinnedSnapshots` |
| probe metadata.json roles | dev.go readDevFolder | WIRED | dev.go:57 `Roles []string json:"roles"`, :161 |

### Behavioral Spot-Checks (gate commands, run by the verifier)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Format + vet + full backend suite, node mandatory | `cd backend && gofmt -l . ; go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` | gofmt empty, vet OK, all packages ok | ✓ PASS |
| Race detector | `cd backend && go test -race -count=1 .` | ok 4.99s | ✓ PASS |
| Android cross-compile proxy | `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` | exit 0 | ✓ PASS |
| Desktop CI repro | `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` | all ok | ✓ PASS |
| CRIT-01 matrix | `go test -run TestHostileDTagStaysInsideDataDir -v .` | 12/12 subtests PASS | ✓ PASS |
| CR-03 regression (iter-3 fix) | `-run 'TestNapRelayResubscribeKeepsLiveEntry\|TestNapOutboxResubscribeKeepsLiveEntry'` | PASS | ✓ PASS |
| Shell handshake gone / intent routing / A23 | `-run 'TestNapFrameShellReadyIsIgnored\|TestIntentDelivery\|TestLauncherSender\|TestIncEmitBroadcasts'` | PASS | ✓ PASS |
| Host page ordering/bounds (node) | `go test -run TestNappletHost -v ./webview` | 5/5 PASS | ✓ PASS |
| Shim bytes vs npm | `curl …/shim-0.30.0.tgz; cmp package/dist/prelude.global.js backend/webview/shim/prelude.global.js` | IDENTICAL; sha512 = npm integrity | ✓ PASS |
| Fixture vs npm conformance 0.17.0 | node import of `ENVELOPE_SPECS`, compared sorted to fixture `envelopes` | 237/237, equal | ✓ PASS |
| Snapshots vs upstream | sha256 of `raw.githubusercontent.com/{repo}/{commit}/{path}` vs `body_sha256` | 18/18 MATCH | ✓ PASS |
| AAR bind | `gomobile bind -target=android ./mobile` | no Android SDK/NDK locally | ? SKIP (human) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` exist, and none are declared by the plans. The "probe napplet" (`backend/testdata/probe-napplet`) is a dev-folder smoke fixture for `just run`, not a shell probe. Its loadability is covered by `TestProbeNappletFolderLoads` (PASS), and its live run is in Human Verification.

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
|-------------|------------|--------|----------|
| CRIT-01 | 01-01 | ✓ SATISFIED | SC1 evidence |
| SPEC-01 | 01-03 | ✓ SATISFIED | 18 snapshots equal upstream; hash/order test |
| SPEC-03 | 01-05 | ✓ SATISFIED | Conflicts A1-A23 incl. the four required readings, quoted |
| SPEC-05 | 01-03 | ✓ SATISFIED (structure) / ? live trigger NEEDS HUMAN | android.yml aar job on pull_request |
| SHIM-01 | 01-02 | ✓ SATISFIED | Byte-identical to npm, hash test, README |
| SHIM-02 | 01-02, 01-04, 01-05 | ✓ SATISFIED | P1-P7 rows; P2/P3/P5 reimplemented in Go/JS with tests; P1/P4/P6/P7 owned by later phases |
| SHIM-03 | 01-04, 01-05 | ✓ SATISFIED (automated) / ? real napplets NEEDS HUMAN | nap.start session start, shell.ready ignored, domain wire tests |
| SHIM-04 | 01-04 | ✓ SATISFIED (node vm) / ? real frame NEEDS HUMAN | TestSrcdocLeavesOnlyWindowNapplet |
| SHIM-05 | 01-02 | ✓ SATISFIED | Coverage oracle + npm-equal fixture |

All 9 phase requirement IDs are claimed by at least one plan. No orphans: REQUIREMENTS.md maps no other ID to Phase 1, and SPEC-02/SPEC-04 belong to Phase 8.

### Prohibitions

| Plan | Prohibition | Tier | Disposition |
|------|-------------|------|-------------|
| 01-01 | Do not normalize `d` | test | ✓ enforced: matrix asserts `n.D==d`, raw id in state/spec/storage key |
| 01-01 | No sweep/migration of `napps/{raw-id}` | judgment | flagged, non-authoritative pass (no directory enumeration in backend non-test code) |
| 01-02 | Never modify prelude bytes | test | ✓ enforced: sha256 test, plus independent npm cmp |
| 01-02 | Launcher-routed intent only to the resolved handler | test | ✓ enforced: TestIntentDeliveryReachesOnlyTheHandler. Napplet-originated `inc.emit` broadcasts are a separate path, accepted under A23 |
| 01-03 | Never edit a snapshot body | test | ✓ enforced: body hash test; all equal upstream |
| 01-04 | No frame message changes a session | test | ✓ enforced: TestNapFrameShellReadyIsIgnored, TestNapStartDropsEnvelopesFromThePreviousDocument |
| 01-04 | Domains never derived from napplet tags | test | ⚠️ **flagged / unverified**: code holds by inspection, but no test exercises nap.boot with tagged manifests |
| 01-05 | No silent drop of a former patch | test | ✓ enforced: skeleton test requires P1-P7 |
| 01-05 | No Conflicts reading relaxes a NIP-5D MUST | judgment | flagged, non-authoritative pass |
| 01-05 | No `fixed` row without code + test citation | test | ✓ partially enforced. The test checks only a non-empty Code cell. Manual scan: every fixed row cites a test except A19, which cites `nap_conformance_test.go` `bidirectionalOut` (the enforcing test file) with no `Test*` name. Info |

### Anti-Patterns Found

Debt-marker scan (`TBD|FIXME|XXX`, `TODO|HACK|PLACEHOLDER`, placeholder text) over the 34 non-vendored files changed since `6fdcbdd^`: **none**.

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| backend/testdata/probe-napplet/index.html | 72 | Comment says "pushed once per session on frame load"; the code now pushes on every load (DEC-2) | ℹ️ Info | Stale comment only |
| spec/CONFORMANCE.md | A19 row | Fixed row cites a test file/table, not a `Test*` name | ℹ️ Info | Weak citation |
| backend/nap.go | 342 | `nap.reset` rpc case has no caller (review IN-01) | ℹ️ Info | Dead lifecycle entry, reachable only from the trusted host page |
| backend/backend.go | 65-68 | `Start` does not make `DataDir` absolute, so a relative dir makes every install fail closed (IN-04) | ℹ️ Info | Fails safe; GUIs pass absolute dirs |
| backend/nap_inc.go | 78-83 | Address-form senders can be imitated by a crafted `d` (IN-06) | ℹ️ Info | Owned by Phase 6 INTN-01/03 (A5) |

The remaining review Info items (IN-02, IN-03, IN-05, IN-07, IN-08) are carried over and not goal-blocking. The known flake `TestNapDeliversDMsAsSigned` (deferred-items.md) passed in every run here.

### Human Verification Required

1. **Probe frame scope, domains, controls, config (D-13 step 1).** Under `just run`, load the dev folder `backend/testdata/probe-napplet` and open "Verdana probe". Expected:
   - `NappletShimPrelude` is undefined.
   - Exactly the 14 domains are present.
   - A `controls` line appears.
   - config.get returns "hi".
2. **Launcher intent (step 2).** Open your profile from the tray. Expected: exactly one `intent` line with sender `launcher`.
3. **INC between two windows (step 3).** Expected: only the other window logs the ping.
4. **Notify and resource (steps 4-5).** Expected: an OS notification appears, and the image is fetched and shown after consent.
5. **Existing napplets (steps 6-7).** Run noris and hosted-nowhere-opener. Expected: content loads, no timeouts or shim errors, and the intent reaches the probe with the opener's `d` as sender.
6. **Dev reload (step 8).** Expected:
   - The log restarts.
   - Exactly one new intent line appears.
   - There is no focus loss and no theme flash.
   - Controls are re-pushed.
7. **On-disk containment (step 9).** Expected: only 64-hex entries from this build under `napps/`.
8. **SPEC-05 live trigger.** Open a GitHub PR touching `backend/**` and one touching only unrelated paths. Expected: `aar` runs only for the first, and a broken `backend/mobile` fails it.
9. **Flagged prohibitions.** Resolve the domains-from-tags test-tier item, either by accepting it or by adding a nap.boot-level test, and the two judgment-tier items.

### Gaps Summary

No blocking gaps. All five roadmap success criteria hold in the current code (post review iteration 3), and all nine requirement IDs are satisfied. Three load-bearing claims (shim bytes, conformance fixture, pinned spec texts) were confirmed against the upstream sources themselves, not only through the repo's self-referential hashes.

Status is `human_needed` because:
- The runtime claims about a real webview (frame scope, load ordering, fresh-iframe focus/theme, live intent/INC/notify/resource with existing napplets) can only be confirmed with the D-13 `just run` smoke.
- The Android PR trigger can only be observed on GitHub.
- One test-tier prohibition (domains never derived from napplet tags) holds by inspection but has no test on the `nap.boot` path, and two judgment-tier prohibitions need explicit human resolution.

Two items are deferred with clear owners: WR-05 frame self-reload goes to Phase 4 SBOX-01, and CF-2 filename collisions go to Phase 5 KEY-04.

---

_Verified: 2026-10-03T01:54:15Z_
_Verifier: Claude (gsd-verifier)_
