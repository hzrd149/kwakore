---
status: testing
phase: 01-containment-fix-and-canonical-shim-baseline
source: [01-VERIFICATION.md]
started: 2026-10-03T01:56:26Z
updated: 2026-10-03T01:56:26Z
---

## Current Test

number: 1
name: D-13 `just run` smoke list (steps 1-9 in 01-05-SUMMARY.md)
expected: |
  In a real WebKitGTK frame the probe napplet reports NappletShimPrelude undefined, exactly 14 domains,
  notify.controls arriving on load, and config returning "hi". A launcher intent reaches the probe once with
  sender "launcher". An INC ping between two windows reaches only the other window. Notify and resource work
  after consent. noris and hosted-nowhere-opener keep working with no handshake. A dev reload causes no focus
  loss, no theme flash and no duplicate intent. After uninstall, only 64-hex napp directories from this build remain.
awaiting: user response

## Tests

### 1. D-13 `just run` smoke list (steps 1-9 in 01-05-SUMMARY.md)
expected: Probe napplet passes all smoke steps in a live desktop build (rebuild the child first; `just run` does this).
result: [pending]

### 2. SPEC-05 live trigger of the `aar` CI job
expected: The `aar` job runs on a real GitHub PR touching backend/**, does not run on an unrelated PR, and fails when backend/mobile is broken.
result: [pending]

### 3. Flagged prohibitions (judgment)
expected: Domains never come from napplet tags (nappletBoot passes only napDomains); no sweep of legacy napps/{raw-id} dirs; no Conflicts reading weakens a NIP-5D MUST.
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps
