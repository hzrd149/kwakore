---
status: partial
phase: 09-linux-packaging-rename-and-cleanup
source: [09-VERIFICATION.md]
started: 2026-10-07T09:33:42Z
updated: 2026-10-07T15:43:24Z
---

## Current Test

[testing complete]

## Tests

### 1. Desktop menu launch with no display session shows the session_unavailable error (LNXS-03)
expected: The terminal opened by the entry visibly shows the fixed session_unavailable JSON error and the launch exits non-zero.
result: [pending]

### 2. Real NixOS system: activation, reload and stable desktop entries through rebuild + GC (WR-02)
expected: kwakore.socket activates per configured user, systemctl --user reload logs "configuration reloaded", and desktop entries keep launching via /run/current-system/sw/bin/kwakore after nixos-rebuild, reboot and nix-collect-garbage.
result: [pending]

### 3. Run of the slimmed .github/workflows/linux.yml
expected: The slimmed linux.yml passes on master: backend and child are green on every push and pull request. bundle (amd64, arm64) and release run only on v* tags. The graphical, nix, user service and installed artifact lanes were removed by quick task 261007-ej4.
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps

- gap_id: G-09-3
  truth: "The first real run of .github/workflows/linux.yml passes every lane, including user service and installed artifact under scripts/ci-user-manager.sh"
  status: resolved
  resolved_by: quick-261007-ej4
  resolved_at: 2026-10-07
  resolution: "The user service and installed artifact lanes and the CI user-manager helper (scripts/ci-user-manager.sh) were removed from CI by user decision (\"we dont need to test the full linux integrations in the CI, probably just the go tests\"; \"Keep tag-only release\"); the smoke stages stay local developer tools."
  reason: "Run 37638803603: ci-user-manager.sh exits 1 with 'user@kwakore-ci-XXXXXX.service is already loaded' before running any smoke stage"
  severity: blocker
  test: 3
  root_cause: "scripts/ci-user-manager.sh:124 rejects the unit when LoadState=loaded, but user@kwakore-ci-X.service is an instance of the template user@.service, so systemd always reports LoadState=loaded (FragmentPath=/usr/lib/systemd/system/user@.service) before the helper writes its own unit. Confirmed locally: systemctl show user@kwakore-ci-zzz123.service -> LoadState=loaded, ActiveState=inactive."
  artifacts:
    - path: "scripts/ci-user-manager.sh"
      issue: "pre-flight guard tests LoadState instead of ActiveState"
  missing:
    - "Guard on ActiveState (must be inactive) plus the existing unit-file existence check"
    - "Re-run linux.yml to confirm the smoke stages under the isolated manager (they have never run in CI)"
  debug_session: ""

