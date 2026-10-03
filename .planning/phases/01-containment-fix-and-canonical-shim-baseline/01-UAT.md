---
status: complete
phase: 01-containment-fix-and-canonical-shim-baseline
source: [01-VERIFICATION.md]
started: 2026-10-03T01:56:26Z
updated: 2026-10-03T13:56:16Z
---

## Current Test

[testing complete]

## Tests

### 1. D-13 `just run` smoke list (steps 1-9 in 01-05-SUMMARY.md)
expected: Probe napplet passes all smoke steps in a live desktop build (rebuild the child first; `just run` does this).
result: pass

### 2. SPEC-05 live trigger of the `aar` CI job
expected: The `aar` job runs on a real GitHub PR touching backend/**, does not run on an unrelated PR, and fails when backend/mobile is broken.
result: pass

### 3. Flagged prohibitions (judgment)
expected: Domains never come from napplet tags (nappletBoot passes only napDomains); no sweep of legacy napps/{raw-id} dirs; no Conflicts reading weakens a NIP-5D MUST.
result: pass

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
