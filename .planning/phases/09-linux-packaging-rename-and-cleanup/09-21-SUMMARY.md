---
phase: 09-linux-packaging-rename-and-cleanup
plan: 21
subsystem: cleanup
tags: [android, gradle, retirement, d-09]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 05
    provides: the Kotlin sources are gone, so android/ was a source-less Gradle shell
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: Linux service bundle and install helper as the supported install path
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 04
    provides: service-mode native entries
provides:
  - "The android/ tree is gone from the repository: no Gradle scripts, manifest, res/ or wrapper remain"
affects: [09-22 gomobile binding and Android CI removal, 09-09 justfile and CI rework, 09-10 docs rewrite]

actuals:
  tokens: 4460    # chars/4 over the realized diff (17836 chars, 449 deleted lines plus the binary wrapper jar)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  patterns: []

key-files:
  created: []
  modified:
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md
  deleted:
    - android/app/build.gradle.kts
    - android/app/src/main/AndroidManifest.xml
    - android/app/src/main/res/mipmap-anydpi-v26/ic_launcher.xml
    - android/app/src/main/res/values/colors.xml
    - android/app/src/main/res/values/strings.xml
    - android/build.gradle.kts
    - android/gradle.properties
    - android/gradle/wrapper/gradle-wrapper.jar
    - android/gradle/wrapper/gradle-wrapper.properties
    - android/gradlew
    - android/gradlew.bat
    - android/settings.gradle.kts

key-decisions:
  - "Only the twelve enumerated android/ paths were deleted. They were the only files still tracked under android/, so the directory is gone"
  - "The justfile aar/apk/install recipes, the .gitignore android entries and the README, nix comment and CONFORMANCE references were logged in deferred-items.md and left alone. No Phase 9 plan lists them, and the aar recipe binds backend/mobile, which 09-22 owns"

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "Retired Android build and resources paths are absent per D-09"
    requirement: CLNP-01
    verification:
      - kind: inspection
        ref: "git ls-files android/ prints nothing; git show --stat 915d3fb shows 12 files, 449 deletions"
        status: pass
    human_judgment: false
  - id: D2
    description: "Supported backend tests pass after removal"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd backend && go test -count=1 ./..."
        status: pass
    human_judgment: false

metrics:
  duration: 1 min
  completed: 2026-10-07
---

# Phase 9 Plan 21: Retire Android Gradle Build and Resources Summary

**Removed the rest of the Android app: the root and app Gradle scripts, `gradle.properties`, the Gradle wrapper (jar, properties, `gradlew`, `gradlew.bat`), `AndroidManifest.xml` and the three `res/` files. That's 12 files and 449 lines. Nothing is tracked under `android/` anymore.**

## What changed

- `915d3fb retire android gradle build and resources.` deletes exactly the twelve paths the plan lists. They were the only files still tracked under `android/`, so the directory itself is gone from the working tree, and no untracked files were left behind.
- No Go code, `backend/go.mod` or Linux child source changed. `golang.org/x/mobile` and `backend/mobile` stay for 09-22.

## Verification

- `cd backend && go test ./...`: every package ok, from cache. That's expected, because none of the deleted files is a Go build input.
- `cd backend && go test -count=1 ./...` (uncached, run first-hand): every package ok. `verdana/backend` 7.830s, `cmd/kwakore-daemon` 16.985s, `daemon` 7.928s, `linuxhost` 4.248s, `webview` 3.735s, `mobile` 0.005s, the rest ok, `eventdb` has no test files. Neither known flake showed up.
- `git ls-files android/` prints nothing, and `ls android` reports no such directory.
- The post-commit deletion check found 12 deletions, all of them intended.

## Deviations from Plan

None. The plan ran exactly as written, and no command was denied.

## Deferred

I logged these in `deferred-items.md` as the 09-21 entry. They are outside this plan's files and are not listed by any Phase 9 plan:

- `justfile:36-46`: the `aar`, `apk` and `install` recipes. `apk` and `install` now `cd android` into a missing directory. `aar` still binds `./mobile`, so the cleanest fix is to drop all three recipes together with `backend/mobile` in 09-22, or in 09-09, which also edits the justfile.
- `.gitignore:9-14`: six stale `android/...` ignore entries.
- `README.md:364`, the comment at `nix/package.nix:43`, and `spec/CONFORMANCE.md` rows `5D-8` / `5D-NG-android` still mention `android/`.

## Known Stubs

None.

## Next

Ready for 09-22 (gomobile bindings, Android CI and unsupported installer paths).

## Self-Check: PASSED

- The 12 files are absent from HEAD (`git ls-files android/` is empty).
- Commit 915d3fb exists.
