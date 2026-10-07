---
phase: 09-linux-packaging-rename-and-cleanup
plan: 05
subsystem: cleanup
tags: [android, kotlin, retirement, d-09]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 04
    provides: service-mode native entries, so Linux no longer depends on any other platform's launcher path
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: Linux service bundle and install helper as the supported install path
provides:
  - "The ten Android Kotlin application sources under android/app/src/main/java/com/verdana/app/ are gone"
affects: [09-21 Android Gradle build and resources removal, 09-22 gomobile binding and Android CI removal, 09-10 docs rewrite]

actuals:
  tokens: 34360   # chars/4 over the ten deleted files (3465 lines)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  patterns: []

key-files:
  created: []
  modified: []
  deleted:
    - android/app/src/main/java/com/verdana/app/Amber.kt
    - android/app/src/main/java/com/verdana/app/MainActivity.kt
    - android/app/src/main/java/com/verdana/app/Models.kt
    - android/app/src/main/java/com/verdana/app/NappActivity.kt
    - android/app/src/main/java/com/verdana/app/NappWebView.kt
    - android/app/src/main/java/com/verdana/app/Screens.kt
    - android/app/src/main/java/com/verdana/app/SettingsActivity.kt
    - android/app/src/main/java/com/verdana/app/Theme.kt
    - android/app/src/main/java/com/verdana/app/VerdanaApplication.kt
    - android/app/src/main/java/com/verdana/app/VerdanaHost.kt

key-decisions:
  - "Only the ten enumerated Kotlin sources were deleted. android/ Gradle files, the manifest, res/, the wrapper, backend/mobile and golang.org/x/mobile in backend/go.mod stay for 09-21 and 09-22, so the android/ tree is a source-less build shell until then"
  - "Stale doc references to the Kotlin tree (.claude/CLAUDE.md, AGENTS.md) were logged for the 09-10 docs rewrite and not edited here"

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "Retired Android Kotlin app sources are absent per D-09"
    requirement: CLNP-01
    verification:
      - kind: inspection
        ref: "git show --stat fa26166 (10 files, 3465 deletions, all under android/app/src/main/java/com/verdana/app/)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The backend still compiles and its tests pass after the deletion"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd backend && go test ./... (run by the user, see Verification)"
        status: pass
    human_judgment: true

metrics:
  duration: ~10min (mostly waiting on permission decisions)
  completed: 2026-10-07
---

# Phase 9 Plan 05: Retire Android Kotlin Sources Summary

**Removed the ten Android Kotlin application sources: activities, Compose screens, the WebView host, the Amber signer bridge and the gomobile `VerdanaHost` adapter (3465 lines). The backend doesn't depend on them.**

## What changed

- `fa26166 retire android kotlin app sources.` deletes every file in `android/app/src/main/java/com/verdana/app/`. No other file changed in that commit.
- Before deleting, I searched the tracked files outside `android/` and `.planning/`. Nothing in Go code, the justfile, CI or Nix references these files. The only references are in docs (see Deferred).

## Verification

- `cd backend && go test ./...`: **every package ok, reported by the user.** Two of my own attempts to run this command were denied by the auto-mode permission classifier. With the ten deletions staged, the user ran it and the coordinator relayed the result: `verdana/backend` ok 8.725s, `cmd/kwakore-daemon` ok 16.799s, `daemon` ok 7.796s, the other packages ok (cached), and `eventdb` has no test files. Neither known flake (TestRPCInstallValidationAndFixedErrors, TestNapDeliversDMsAsSigned) showed up. I didn't see this output first-hand.
- The commit's deletion check matches the plan: exactly 10 deletions, all of them intended.

## Deviations from Plan

None in scope. The deletion and the verification were each blocked for a while by permission denials. The user approved the deletion through /permissions and ran the verification themselves.

## Auth / permission gates

- **`git rm` of the ten files:** denied in the first run ("Irreversible Local Destruction"). After the user approved it, it succeeded in a later attempt.
- **`go test ./...`:** denied twice. The user ran it instead.

## Deferred

- I added an entry to `deferred-items.md` for 09-10: `.claude/CLAUDE.md:27,96,168,180` and `AGENTS.md:9` still describe the deleted Kotlin tree.
- `android/` still holds `build.gradle.kts`, `AndroidManifest.xml`, `res/` and the Gradle wrapper, but no sources. Plan 09-21 removes them. `backend/mobile` and `golang.org/x/mobile` belong to 09-22.

## Known Stubs

None.

## Self-Check: PASSED

- The 10 files are absent from HEAD (`git ls-files android/app/src/main/java` is empty).
- Commit fa26166 exists.
