---
phase: 08-runtime-and-signer-integration
fixed_at: 2026-10-07T04:04:57Z
review_path: .planning/phases/08-runtime-and-signer-integration/08-REVIEW.md
iteration: 3
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 8: Code Review Fix Report (Iteration 3)

**Fixed at:** 2026-10-07T04:04:57Z  
**Source review:** `.planning/phases/08-runtime-and-signer-integration/08-REVIEW.md`  
**Iteration:** 3

**Summary:**
- Findings in scope: 1
- Fixed: 1
- Skipped: 0

## Fixed Issues

### CR-04: JSON escaping still makes a valid signer journal exceed its bound

**Files modified:** `backend/daemon/credentials_linux.go`, `backend/daemon/credentials_linux_test.go`  
**Commit:** `0b4d609`  
**Status:** fixed: requires human verification  
**Applied fix:** Encode the private journal with JSON HTML escaping disabled, so a canonical relay's `&` characters retain their accepted input size. Keep the journal bound at the 4 KiB credential plus the 1 MiB config file limit and field overhead. A regression loads a declarative relay with 180,000 ampersands, switches to `none`, and verifies persistence after restart.

## Previous Iteration Fixes

The earlier CR-04 attempt (`ba63783`) increased the journal bound and covered a 4,108-byte case. This iteration closes the remaining HTML expansion case.

### CR-01: Bunker switch is rejected before connection

**Files modified:** `backend/auth_service.go`, `backend/daemon/daemon_linux.go`, `backend/daemon/rpc_linux_test.go`  
**Commit:** `6789cca`  
**Status:** fixed: requires human verification  
**Applied fix:** Derive the configured relay from the bunker URL. A socket regression uses a valid bunker URL and a successful stubbed handshake.

### CR-02: Failed signer switch permanently erases the previous credential

**Files modified:** `backend/auth_service.go`, `backend/auth_service_test.go`, `backend/daemon/credentials_linux.go`, `backend/daemon/credentials_linux_test.go`, `backend/daemon/daemon_linux.go`  
**Commit:** `817df89`  
**Status:** fixed: requires human verification  
**Applied fix:** Keep the prior credential while validating or connecting the replacement. Commit the credential and mode with a private transition record, and recover interrupted or failed writes. Regression tests verify failed replacements and interrupted writes restore the previous signer after restart.

### CR-03: Failed pairing can save a new signer while retaining the old mode

**Files modified:** `backend/auth_service.go`, `backend/auth_nostrconnect.go`, `backend/daemon/daemon_linux.go`, `backend/daemon/credentials_linux_test.go`  
**Commit:** `ade8b32`  
**Status:** fixed: requires human verification  
**Applied fix:** Route pairing completion through the same recoverable credential and mode commit. Re-read durable records during rollback. Regression tests inject override failures before and after the mode write and verify restart restores the prior signer.

## Verification

Verification ran in the isolated review-fix worktree before fast-forwarding the phase branch. The ampersand-heavy CR-04 regression and `go test ./daemon -count=1` passed. A parallel `go test ./...` run hit the existing five-second timeout in `TestRPCInstallValidationAndFixedErrors`; `go test -p 1 ./...` passed across all backend packages. `git diff --check` passed before the commit. Desktop and Android tests were not run because these fixes affect the backend signer path.

---

_Fixed: 2026-10-07T04:04:57Z_  
_Fixer: the agent (gsd-code-fixer)_  
_Iteration: 3_
