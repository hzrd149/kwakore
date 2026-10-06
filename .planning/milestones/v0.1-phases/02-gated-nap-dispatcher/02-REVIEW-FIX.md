---
phase: 02-gated-nap-dispatcher
fixed_at: 2026-10-03T00:00:00Z
review_path: .planning/phases/02-gated-nap-dispatcher/02-REVIEW.md
iteration: 2
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 2: Code Review Fix Report

**Fixed at:** 2026-10-03
**Source review:** .planning/phases/02-gated-nap-dispatcher/02-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 1 (WR-01; fix scope critical_warning, so the 11 Info findings are out of scope)
- Fixed: 1
- Skipped: 0

## Fixed Issues

### WR-01: `newPromptID` is not positive on 32-bit targets, and its test does not compile there

**Files modified:** `backend/window_prompt.go`, `backend/window_instances_test.go`, `backend/eventdb/lmdb.go`, `backend/eventdb/boltdb.go`
**Commit:** 2e6277f
**Applied fix:**
- `backend/window_prompt.go`: added `const promptIDMask = uint64(1<<53-1) & uint64(math.MaxInt)`. `newPromptID` masks the random uint64 with it before converting to `int`, so ids are positive and below 2^53 on every architecture (below 2^31 where `int` is 32-bit). The doc comment now states the 32-bit bound, and that ownership (CR-01), not guessability, is what protects a prompt.
- `backend/window_instances_test.go`: the bound checks compare as `int64(a) >= 1<<53`, so the constant no longer overflows `int` on 32-bit. A guard asserts that `promptIDMask` fits both `math.MaxInt` and 2^53. A loop draws 1000 ids and checks each is positive and in range, which catches a regression to truncation (before the fix, about half the draws were negative on 32-bit).
- `backend/eventdb/lmdb.go` / `boltdb.go`: the lmdb store needs cgo, so its build constraint now includes `&& cgo`, and boltdb gets `|| !cgo`. This matches the package doc ("lmdb where its cgo build works, boltdb everywhere else"). Without it, `GOOS=linux GOARCH=386 CGO_ENABLED=0` could not build the backend package (`lmdb-go: build constraints exclude all Go files`), so the requested 32-bit test run was impossible. cgo builds (desktop on linux amd64, the default) still use lmdb.

## Verification

All gates ran in the main checkout, `/home/user/Projects/verdana` on branch `master` (no worktree):
- `cd backend && GOOS=linux GOARCH=arm CGO_ENABLED=0 go vet .`: pass (this failed before the fix with `1 << 53 ... overflows int`)
- `GOOS=linux GOARCH=386 CGO_ENABLED=0 go test -count=1 -run 'Prompt' .`: pass. Ran natively, and `-v` confirms `TestPromptAnswerOnlyFromOwner` runs and passes.
- `GOOS=linux GOARCH=386 CGO_ENABLED=0 go vet ./...`: pass
- `go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`: pass
- `go test -race -count=2 .`: pass
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` and `GOOS=android GOARCH=arm CGO_ENABLED=0 go build ./...`: pass
- `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`: pass

---

_Fixed: 2026-10-03_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
