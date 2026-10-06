---
status: testing
phase: 07-unix-socket-and-cli
source: [07-VERIFICATION.md]
started: 2026-10-06T22:34:09Z
updated: 2026-10-06T22:34:09Z
---

## Current Test

number: 1
name: Live relay discovery
expected: |
  A refresh against a reachable relay containing a known napplet returns that
  napplet with complete:true and a fetched_at timestamp, as valid CLI JSON.
awaiting: user response

## Tests

### 1. Live relay discovery
expected: Start a live daemon; run `kwakore discover --refresh --query <known napplet>` and `kwakore installed`. The refresh result contains the known napplet with complete:true and a fetched_at timestamp; CLI output is valid JSON.
result: pending

### 2. Live install, update and uninstall
expected: In a disposable user profile, install a known napplet, update it when a newer manifest exists, then uninstall it with `--yes`. Each command returns final JSON outcome; installed-list reflects the committed version or removal; failed external downloads preserve the previous version.
result: pending

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps

None recorded.
