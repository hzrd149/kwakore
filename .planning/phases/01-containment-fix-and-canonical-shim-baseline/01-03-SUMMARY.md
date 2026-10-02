---
phase: 01-containment-fix-and-canonical-shim-baseline
plan: 03
subsystem: testing
tags: [spec-pins, conformance, sha256, github-actions, gomobile, android]

requires:
  - phase: 01-containment-fix-and-canonical-shim-baseline
    provides: "01-01 CRIT-01 fix (ships before any audit work)"
provides:
  - "spec/pinned/: 17 pinned spec texts plus the NAP-RESOURCE 9511232f tolerance text, byte-exact with front matter and body_sha256"
  - "spec/pinned/README.md index in SPEC-PINS order with re-verify and re-pin instructions"
  - "backend/spec_pinned_test.go: pinnedSnapshot type, pinnedSnapshots list (SPEC-PINS order, reused by 01-05), TestPinnedSpecSnapshotsMatchTheirHashes"
  - "android workflow aar job: read-only gomobile AAR bind on path-filtered pull requests; apk job on dispatch and v* tags"
affects: [01-05, spec/CONFORMANCE.md, android-ci, all later audit phases]

actuals:
  tokens: 67700
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Spec snapshots are generated from git objects (git show >> file), never hand-edited; a re-pin is a new file at a new SHA"
    - "Byte-exact vendored text is marked -text in .gitattributes and guarded by a sha256 test"

key-files:
  created:
    - spec/pinned/README.md
    - spec/pinned/*@*.md (18 snapshots)
    - backend/spec_pinned_test.go
    - .planning/phases/01-containment-fix-and-canonical-shim-baseline/deferred-items.md
  modified:
    - .github/workflows/android.yml
    - .gitattributes

key-decisions:
  - "Spec snapshots are byte-exact git blobs with front matter (spec, role, repo, ref, commit, path, fetched, body_sha256); .gitattributes marks spec/pinned/*@*.md -text"
  - "NAP-RESOURCE@9511232f.md is a tolerance (role: tolerance), a separate file from the PR #80 pin, never merged"
  - "Android PRs run only a read-only gomobile AAR bind (aar job, plain pull_request, contents: read, no secrets); the APK build runs on workflow_dispatch and v* tags"

patterns-established:
  - "pinnedSnapshots in backend/spec_pinned_test.go is the canonical SPEC-PINS order; CONFORMANCE.md sections and the README follow it"

requirements-completed: [SPEC-01, SPEC-05]

coverage:
  - id: D1
    description: "All 17 pinned spec texts plus the NAP-RESOURCE 9511232f tolerance text committed byte-exact under spec/pinned with verifiable front matter"
    requirement: SPEC-01
    verification:
      - kind: unit
        ref: "backend/spec_pinned_test.go#TestPinnedSpecSnapshotsMatchTheirHashes"
        status: pass
      - kind: other
        ref: "python3 glob/sha256 structural check of spec/pinned/*@*.md (Task 1 verify)"
        status: pass
      - kind: other
        ref: "git -C ~/Projects/naps show 9511232f69313aa7953d110e35d32cc28d506f66:naps/NAP-RESOURCE.md | sha256sum"
        status: pass
    human_judgment: false
  - id: D2
    description: "spec/pinned/README.md lists the snapshots in SPEC-PINS order, enforced by the test"
    requirement: SPEC-01
    verification:
      - kind: unit
        ref: "backend/spec_pinned_test.go#TestPinnedSpecSnapshotsMatchTheirHashes"
        status: pass
    human_judgment: false
  - id: D3
    description: "Pull requests touching backend/**, android/** or the workflow run a read-only gomobile AAR bind; APK build only on dispatch and v* tags"
    requirement: SPEC-05
    verification:
      - kind: other
        ref: "python3/pyyaml structural check of .github/workflows/android.yml (Task 2 verify)"
        status: pass
      - kind: other
        ref: "cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./..."
        status: pass
    human_judgment: true
    rationale: "Path-filter trigger behavior and the real gomobile bind can only be observed on GitHub; first pull request touching backend/ should show the aar job running and passing"

duration: 4min
completed: 2026-10-02
status: complete
---

# Phase 01 Plan 03: Pinned Spec Snapshots and Android PR Bind Summary

**18 byte-exact spec snapshots (17 pins + the NAP-RESOURCE server-hint tolerance) under spec/pinned, guarded by a sha256/front-matter/order test, plus a read-only gomobile AAR bind job on Android-relevant pull requests**

## Performance

- **Duration:** 4 min
- **Started:** 2026-10-02T23:32:48Z
- **Completed:** 2026-10-02T23:36:11Z
- **Tasks:** 2
- **Files modified:** 23 (18 snapshots, README, test, .gitattributes, deferred-items.md, android.yml)

## Accomplishments

- Every spec text in `.planning/research/SPEC-PINS.md` is committed at its SHA as `spec/pinned/{spec}@{sha8}.md`. The bodies are the raw `git show {commit}:{path}` bytes, and each file's front matter records its source and `body_sha256`. The generator checked each hash against the plan's table before writing and stopped on any missing object.
- `TestPinnedSpecSnapshotsMatchTheirHashes` checks that the files on disk exactly match the `pinnedSnapshots` list, that the front matter is complete and well formed, that each file name equals `spec@commit[:8].md`, that each commit and role matches the list, that no body is empty, that each body hashes to its `body_sha256`, and that the README lists the files in SPEC-PINS order. A mutation check (appending one byte to a body) made the test fail.
- `.github/workflows/android.yml` now has an `aar` job that runs on plain `pull_request`, is path-filtered to `backend/**`, `android/**` and the workflow file, and has `contents: read`, no secrets and no `needs`. It binds `./mobile` with `gomobile bind -target=android -androidapi 26`. The `apk` job is limited to `workflow_dispatch` and `v*` tags, and its steps are unchanged. Concurrency cancels in-flight runs only for pull requests.

## Task Commits

1. **Task 1: Pinned spec snapshots with hash test (tracer)**: `5d67384` (feat)
2. **Task 2: Android AAR bind on pull requests; APK on dispatch and tags**: `a46843b` (ci)

**Plan metadata:** recorded in the docs commit that follows this SUMMARY

## Files Created/Modified

- `spec/pinned/*@*.md` (18 files): pinned spec texts with front matter
- `spec/pinned/README.md`: index in SPEC-PINS order, file format, re-verify steps, the tolerance note and the re-pin rule
- `backend/spec_pinned_test.go`: `pinnedSnapshot`, `pinnedSnapshots`, `splitPinnedSnapshot`, `TestPinnedSpecSnapshotsMatchTheirHashes`
- `.gitattributes`: `spec/pinned/*@*.md -text`
- `.github/workflows/android.yml`: triggers, concurrency, new `aar` job, `apk` gated off pull requests
- `.planning/phases/01-containment-fix-and-canonical-shim-baseline/deferred-items.md`: flaky-test note

## Decisions Made

- Snapshots are marked `-text` in `.gitattributes`, matching how the vendored shim is handled, so a Windows checkout with autocrlf can never change the bytes and fail the hash test.
- The test reuses the package's existing `hex64` regexp (from `backend/napplet.go`) for `body_sha256`, and adds `pinnedCommitHex` for 40-hex commits.
- The tracer feedback gate was handled by re-running Task 1's automated verify end to end (it passed) and then continuing. The reason: the project sets `human_verify_mode: end-of-phase` and `mode: yolo`, and the tracer's verify is fully automated, so a mid-flight human halt would add nothing.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Marked snapshots -text in .gitattributes**
- **Found during:** Task 1
- **Issue:** Snapshot bodies must keep the git blob's exact bytes. Without a `-text` attribute, a checkout with `core.autocrlf` could rewrite their line endings and break `body_sha256` (the encoding probe).
- **Fix:** Added `spec/pinned/*@*.md -text` with a comment, the same way the shim prelude is handled.
- **Files modified:** `.gitattributes`
- **Verification:** `git check-attr text spec/pinned/NIP-5D@24711d9c.md` prints `unset`
- **Committed in:** `5d67384`

**2. [Rule 3 - Blocking] hex64 redeclared in package backend**
- **Found during:** Task 1 (`go vet`)
- **Issue:** The test declared `hex64`, which `backend/napplet.go` already declares.
- **Fix:** Reused the existing `hex64` and named the commit regexp `pinnedCommitHex`.
- **Files modified:** `backend/spec_pinned_test.go`
- **Verification:** `go vet ./...` is clean and the test passes
- **Committed in:** `5d67384`

---

**Total deviations:** 2 auto-fixed (1 missing critical, 1 blocking)
**Impact on plan:** Both were small and needed for correctness. No scope creep.

## Issues Encountered

- `TestNapDeliversDMsAsSigned` (`backend/nap_outbox_test.go:352`) failed once in a full `go test ./...` run because its 3s wait expired. It then passed 5/5 times in isolation and in 3/3 full-suite reruns. It looks like an existing timing flake and is unrelated to this plan. It is logged in `deferred-items.md`.
- `actionlint` is not installed locally, so the workflow was validated only with the pyyaml structural check. The real trigger, path-filter and bind behavior will show on the first GitHub pull request (a backstop truth).

## Threat Surface

- T-01-11 is mitigated: the `aar` job uses the plain `pull_request` trigger with `permissions: contents: read`, references no secrets and has no upload or release steps. The structural verify asserts all of this.
- T-01-13 is mitigated: `body_sha256` plus `TestPinnedSpecSnapshotsMatchTheirHashes` (which runs in desktop CI's backend tests), and the README's re-pin rule.
- No new surface beyond the threat model.

## Residual Risk

The pull request job binds only the AAR (D-17), so Kotlin compile breakage from a `backend/mobile` API change still surfaces only on dispatch and tag APK builds. Plan 01-05 records this in `spec/CONFORMANCE.md`.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- Plan 01-05 can reuse `pinnedSnapshots` for the CONFORMANCE.md section order and cite `spec/pinned/*` files.
- Do not make the `aar` check required in branch protection without converting it to an always-running job that no-ops via `if:` (01-RESEARCH Pitfall 9).

---
*Phase: 01-containment-fix-and-canonical-shim-baseline*
*Completed: 2026-10-02*

## Self-Check: PASSED

- FOUND: spec/pinned/README.md, 18 spec/pinned/*@*.md files, backend/spec_pinned_test.go, .github/workflows/android.yml
- FOUND: commits 5d67384, a46843b
- Acceptance criteria re-run: 18 snapshots; the NAP-RESOURCE@9511232f hash matches `git show` and its role is tolerance; 5 files at a040914b; the hash test passes; `go test ./...` passes in backend; desktop tests pass; the pyyaml check prints ok; the android cross-build passes
