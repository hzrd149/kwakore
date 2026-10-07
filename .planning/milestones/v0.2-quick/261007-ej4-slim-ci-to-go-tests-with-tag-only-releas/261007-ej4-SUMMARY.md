---
phase: quick-261007-ej4
plan: 01
subsystem: infra
tags: [ci, github-actions, release, smoke, systemd, webkitgtk, nix]

requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    provides: "the 9-job linux.yml (09-09), the CI user-manager helper and the --full installed-artifact lane (09-11)"
provides:
  - "linux.yml with four jobs: backend and child on every push and pull request; bundle (amd64, arm64) and release only on v* tags"
  - "TestCIWorkflowContract + ciWorkflowViolations, with a mutation table, pinning that shape inside the backend Go suite"
  - "smoke-linux-service.sh as a local-only developer tool (the throwaway-manager branch removed)"
  - "AGENTS.md / README.md local commands for every lane CI no longer runs"
  - "Phase 9 records: 3 VERIFICATION overrides (62/64), UAT Test 3 retargeted, gap G-09-3 resolved"
affects: [phase-09-verification, milestone-v0.2-audit, release]

actuals:
  tokens: 21000
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Workflow contract test: strip comment lines, split jobs by two-space headers, check required/forbidden tokens, and prove the checker against mutated copies of the real file"

key-files:
  created: []
  modified:
    - .github/workflows/linux.yml
    - backend/daemon/rpc_linux_test.go
    - desktop/child/webkit_test.go
    - scripts/smoke-linux-service.sh
    - AGENTS.md
    - README.md
    - .claude/CLAUDE.md
    - spec/CONFORMANCE.md
    - .planning/phases/09-linux-packaging-rename-and-cleanup/09-VERIFICATION.md
    - .planning/phases/09-linux-packaging-rename-and-cleanup/09-UAT.md
  deleted:
    - scripts/ci-user-manager.sh

key-decisions:
  - "CI runs only the Go lanes (backend, child) on every push and pull request; bundle and release run only on v* tags; the graphical, nix, user service and installed artifact lanes are removed (user decision, quick task 261007-ej4)"
  - "TestRPCRealChildCIContract is rewritten as TestCIWorkflowContract instead of deleted: it still guards the node gate, the release needs/tag gate, the pinned publish action, and the absence of half-wired real-engine lanes"
  - "The smoke contract check exempts only the bundle job's marker-count reads (want=$(grep -c ... scripts/smoke-linux-service.sh)), which name the script without running it and had to stay byte-identical"

patterns-established:
  - "A green child job carries a ::notice listing the real-engine tests it did not run"

requirements-completed: [CLNP-01, CLNP-02, NAME-01]

coverage:
  - id: D1
    description: "linux.yml has exactly backend, child, bundle and release; release needs [backend, child, bundle]; bundle and release carry the tag condition; backend and child have none"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "backend/daemon/rpc_linux_test.go#TestCIWorkflowContract"
        status: pass
      - kind: other
        ref: "python3 yaml.safe_load(.github/workflows/linux.yml) jobs == ['backend','bundle','child','release']"
        status: pass
    human_judgment: false
  - id: D2
    description: "The slimmed workflow runs green on GitHub (master push) and publishes the exact three assets on a v* tag"
    requirement: CLNP-02
    verification: []
    human_judgment: true
    rationale: "Needs a GitHub runner and a push; tracked as 09-UAT Test 3"
  - id: D3
    description: "CI helper deleted, smoke script local-only with the drop-in session switch, all scripts pass bash -n, both identity scans pass"
    requirement: NAME-01
    verification:
      - kind: other
        ref: "bash -n scripts/*.sh; bash scripts/check-product-identity.sh (full and --runtime-only)"
        status: pass
    human_judgment: false

duration: 16min
completed: 2026-10-07
status: complete
---

# Quick Task 261007-ej4: Slim CI to Go Tests with Tag-Only Release Summary

**linux.yml is down from 9 jobs to 4. The Go lanes (backend, child) run on every push and pull request, and the bundle matrix and release run only on v* tags. A rewritten, mutation-tested TestCIWorkflowContract pins that shape, the CI user-manager helper is deleted, and the Phase 9 records show the scope change as a deliberate user decision.**

## Performance

- **Duration:** about 16 min
- **Completed:** 2026-10-07
- **Tasks:** 3/3
- **Files:** 10 modified, 1 deleted

## The scope change

CI failed three runs in a row: 37638803603 (b1f6602), 37642157485 (f4f5957) and 37642631093 (dc78dbf). `gh run view` confirms that in all three, backend, identity, child, graphical, nix and both bundle jobs passed. Only the systemd and sudo lanes failed (user service and installed artifact), so none of the failures were in Kwakore itself. The repo owner, hzrd149, then decided:

> "we dont need to test the full linux integrations in the CI, probably just the go tests"

and chose **"Keep tag-only release"**. This task implements exactly that.

## Accomplishments

- **linux.yml:**
  - **backend** now also runs `bash -n` on `scripts/*.sh` and the full identity scan, folded in from the old identity job.
  - **child** is unchanged apart from a new `real-engine tests are local-only` step. It lists the display-backed TestWebKit tests and emits one `::notice` saying they and TestRPCRealChildGraphical were not run.
  - **bundle** is byte-identical apart from a job-level tag `if:`.
  - **release** now needs `[backend, child, bundle]`. Its manifest gate and the pinned softprops SHA are unchanged (checked with a diff against HEAD).
  - The `on:` block is unchanged.
- **TestCIWorkflowContract** replaces TestRPCRealChildCIContract. `ciWorkflowViolations` strips comment lines, splits the file into jobs, and checks:
  - the required tokens in backend and child;
  - no job-level `if:` on backend or child;
  - the tag condition on bundle and release;
  - the release `needs:` and the pinned SHA;
  - the `pull_request` and `v*` triggers;
  - the forbidden tokens (real-engine gates, `xvfb`, `sudo`, `install-nix-action`, `systemctl`);
  - smoke invocations other than `--bundle-only`.

  Ten mutation subtests cover cases (a) to (i) and the comment-only case, and each asserts that its mutation actually changed the text.
- **TestRPCRealChildGraphical** has a new doc comment and skip message, and the webkit rig comment is updated. Both now say the tests are local-only.
- **`scripts/ci-user-manager.sh` is deleted.** In `smoke-linux-service.sh`, `isolated_manager`, `session_vars`, `manager_saved`, `save_manager_session`, `restore_manager_session` and the `install_cleanup` call are gone. `set_session` now always uses the runtime drop-in. The header and the `run_full` messages no longer mention CI, and a new top paragraph says the stages are local tools (CI runs only `--bundle-only` in the tagged build).
- **AGENTS.md:**
  - The lane list is rewritten, and bundle-check is described "as the tagged release build does".
  - Its test-command line now says it "reproduces the child CI lane".
  - The smoke stages are described as local tools ("Run it before tagging a release").
  - New bullets: "Local-only real-engine tests" and "Local Nix checks".
- **README.md:** bundle-check wording, a new paragraph on the local real-engine tests, and a rewritten smoke sentence.
- **.claude/CLAUDE.md:** only the three codebase bullets changed (lines 46, 68 and 103). The GSD-managed sections are untouched.
- **CONFORMANCE 5D-NG-webkitgtk:** gh confirmed the graphical job succeeded in run 37638803603, so the row cites that one CI pass. It then says CI no longer runs the smoke, which now runs locally. The code cell cites "local real-engine run (AGENTS.md, Local-only real-engine tests)".
- **09-VERIFICATION.md:**
  - 3 frontmatter overrides with `accepted_by: hzrd149`, `overrides_applied: 3` and `score: 62/64`.
  - human_verification item 3 rewritten.
  - Rows 09-09 #3, 09-11 #4 and the CI key link now read PASSED (override). The 09-09 #1 evidence, the two artifact rows, the score sentence and human section #3 are updated.
  - `status: human_needed` is kept.
- **09-UAT.md:**
  - `status: partial`.
  - Test 3 is retargeted to the slimmed workflow and is `[pending]`.
  - Summary counts are 3 total / 0 issues / 3 pending.
  - G-09-3 is resolved by quick-261007-ej4, and its history fields are kept.

## Superseded must-haves

| Source | Must-have | Handling |
|---|---|---|
| 09-09 truth | "CI builds and tests only supported Linux backend, child, Nix and service paths" | **Narrowed.** It stays VERIFIED, with a note that the jobs are now backend, child, bundle and release and that Nix and service paths are checked locally. No override needed. |
| 09-09 truth | "An isolated per-user manager can run the installed-artifact smoke in CI" | **VERIFICATION override #1**, PASSED (override) |
| 09-09 key link | "CI builds the real child before graphical launch tests" | **Narrowed:** the child job still builds `child/napplet`, but the graphical launch tests are local. 09-VERIFICATION.md has no row for this link, so no override was added. |
| 09-11 truth | "The Nix package/module and generic release pass their own install/evaluation checks and Linux CI" | **VERIFICATION override #2**, PASSED (override) |
| 09-11 key link | "CI invokes the same installed-artifact script used for local acceptance" | **VERIFICATION override #3**, PASSED (override) |

## Why TestCIWorkflowContract was kept and rewritten

The old string list only pinned what the graphical job contained, which is now deleted, so it could not simply stay. Deleting it would have dropped four guards that still matter:

1. **The backend node gate.** Without it, the security-relevant host-page and frame-scope tests would silently skip in CI.
2. **The release ordering.** The release must not be able to run ahead of the Go lanes or the bundles (needs list plus tag gate).
3. **The pinned publish action.** This is the only step with `contents: write`.
4. **No half-wired real-engine gate.** The old job needed PASS-line checks to stop silent skips.

It also pins the child job's "not run in CI" notice. Comment lines are ignored, so the workflow and docs can still name the local gates.

## Local-only coverage (exact commands)

- **WebKit real-engine tests**, in `desktop/` after `just webview-libs`, with a live DISPLAY or WAYLAND_DISPLAY, or under `xvfb-run -a`:

  ```
  KWAKORE_WEBKIT_SMOKE=1 go test ./child -run '^TestWebKit' -count=1 -v
  ```

  `NO_AT_BRIDGE=1` is optional.
- **Real daemon child**, in `backend/` after building `desktop/child/napplet`, under a display or `xvfb-run -a`:

  ```
  KWAKORE_REQUIRE_GRAPHICS=1 KWAKORE_WINDOW_BIN=$PWD/../desktop/child/napplet KWAKORE_WEBVIEW_LIB=$PWD/../desktop/internal/webviewlib/lib/linux_$(go env GOARCH)/libwebview.so go test -v ./daemon -run '^TestRPCRealChildGraphical$' -count=1
  ```
- **User-service smokes:** `bash scripts/smoke-linux-service.sh --activation-only`, `--install-only`, and `--full` (optionally `--archive FILE --sha256sums FILE` for a downloaded release).
- **Nix:**
  - `nix eval --impure --json --file nix/module-test.nix` prints `true`
  - `nix build -L --no-link .#packages.x86_64-linux.kwakore`
  - `nix flake check -L`

## Task Commits

1. **Task 1 (tracer, TDD):**
   - RED: `984a8a2` test: pin the slimmed ci workflow with TestCIWorkflowContract (261007-ej4)
   - GREEN: `2db21a9` ci: slim linux.yml to the go lanes with a tag-only release (261007-ej4)
2. **Task 2:** `8000f93` remove the ci user-manager helper and make the smoke local-only (261007-ej4)
3. **Task 3:** `67c2f4f` docs: record the slimmed ci scope in context, conformance and phase 9 (261007-ej4)

## Verification

| Check | Result |
|---|---|
| `gofmt -l` on both Go files | clean |
| `go vet ./...` (backend), `go vet ./...` (desktop) | clean |
| `go test -run '^TestCIWorkflowContract' -v ./daemon` | PASS, 10/10 mutation subtests |
| YAML jobs / needs / if assertions | `['backend', 'bundle', 'child', 'release']`, OK |
| Bundle and release blocks vs HEAD | only the added `if:` and the new `needs:` differ |
| `KWAKORE_REQUIRE_NODE=1 go test -count=1 ./...` (backend) | **all packages ok except `daemon`: `TestRPCInstallRelayHints` fails here with an i/o timeout. This predates the task (see Deferred Issues).** |
| desktop: `go generate`, build `child/napplet`, `go vet`, `go test -count=1 ./...` | PASS |
| `go test -run TestConformance .` | PASS |
| Identity scans (full / runtime) | PASS, 37 and 32 reviewed, 0 unreviewed |
| `bash -n scripts/*.sh` | PASS |
| `git status --porcelain --untracked-files=all -- backend desktop scripts .github` | empty |
| `go mod tidy -diff` (both modules) | clean |
| Task 2 and Task 3 automated verify blocks | PASS |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The "smoke line must contain --bundle-only" check would have rejected the byte-identical bundle job**
- **Found during:** Task 1, RED run
- **Issue:** The bundle job's marker-count lines, `want=$(grep -c "pass \"$marker:" scripts/smoke-linux-service.sh)`, name the script without `--bundle-only`. The plan requires those steps to stay unchanged.
- **Fix:** The checker exempts exactly that read-only form, via the regex `^\s*[A-Za-z_]+=\$\(grep -c [^;&|]+ scripts/smoke-linux-service\.sh\)\s*$`, which allows no `;`, `&` or `|`. Every other line naming the script still needs `--bundle-only`.
- **Files modified:** backend/daemon/rpc_linux_test.go
- **Commit:** 984a8a2

**2. [Accuracy] 09-VERIFICATION score sentence**
- The plan said the 2 remaining UNCERTAIN truths wait on "human items 1 and 2". In fact both (SC3 and 09-03's headless terminal surface) wait on human item 1, and item 2 confirms SC2's real-NixOS runtime. The sentence says that.

**3. [Detail] Child notice excludes TestWebKitHardeningSymbolsResolve**
- That test needs no display and runs in the child test step, so the notice lists only the display-backed TestWebKit tests. A comment above the step explains this.

### Tracer gate
Task 1 is `type="tracer"`. Its `<verify>` was re-run after the GREEN commit and passed end to end. The plan is `autonomous: true` and the orchestrator asked for all tasks, so execution continued to the expansion tasks without an interactive checkpoint.

## Deferred Issues

- **`TestRPCInstallRelayHints` (backend/daemon) times out locally.**
  - Error: `read unix ...daemon.sock: i/o timeout` at the valid-hints install, which tries relay lookups for `wss://r0.example` and similar hosts.
  - It fails the same way at base commit e783a99, run in a throwaway worktree, so this task did not cause it.
  - CI's backend job passed in all three runs that include 261007-cth, so it looks specific to this sandbox's DNS or network behavior. Out of scope; not fixed.
- **The optional live `--activation-only` smoke was not run.** It registers runtime units with the user's real systemd manager. The code changed in the smoke (`set_session`, `install_cleanup`) is only on the `--install-only` and `--full` paths. `bash -n` passes.

## Threat model

- **T-01, T-02, T-03 are mitigated.** The release needs all three lanes and the tag gate, the publish SHA stays pinned, and the manifest and bundle checks are byte-identical. TestCIWorkflowContract enforces the first two.
- **T-05 and T-06 are mitigated.** No sudo, systemctl or Nix action remains, and the node gate is pinned.
- **T-04 is accepted (user decision).** It is offset by the child notice, the AGENTS.md and README commands, and the CONFORMANCE row. Residual risk: a WebKit-only sandbox regression can ship unless the local run is done before tagging.

## Known Stubs

None.

## Follow-ups

1. Push master and watch the slimmed run (09-UAT Test 3). Expect backend and child green, with bundle and release skipped on a non-tag push.
2. Before the first `v*` tag, run the local real-engine tests and `smoke-linux-service.sh --full`.
3. The Phase 4 residue "first green CI xvfb smoke" in STATE.md is now moot. gh confirmed run 37638803603's graphical job as the one observed CI pass, and the graphical job also passed in 37642157485 and 37642631093.
4. The STATE.md decision lines for 09-09 and 09-11 that describe the CI user manager and the installed CI job are now historical. The orchestrator can note that they were superseded by 261007-ej4.

## Self-Check: PASSED

- FOUND: .github/workflows/linux.yml (4 jobs), backend/daemon/rpc_linux_test.go (TestCIWorkflowContract), scripts/smoke-linux-service.sh, AGENTS.md, README.md, .claude/CLAUDE.md, spec/CONFORMANCE.md, 09-VERIFICATION.md, 09-UAT.md
- ABSENT as intended: scripts/ci-user-manager.sh
- FOUND commits: 984a8a2, 2db21a9, 8000f93, 67c2f4f
