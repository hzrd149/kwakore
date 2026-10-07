---
status: testing
phase: 09-linux-packaging-rename-and-cleanup
source: [09-VERIFICATION.md]
started: 2026-10-07T09:33:42Z
updated: 2026-10-07T09:33:42Z
---

## Current Test

number: 1
name: Desktop menu launch with no display session shows the session_unavailable error (LNXS-03)
expected: |
  With DISPLAY and WAYLAND_DISPLAY unset in the systemd user manager, launching an installed
  napplet's entry from a real desktop menu opens a terminal (Terminal=true) that visibly shows
  {"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}
awaiting: user response

## Tests

### 1. Desktop menu launch with no display session shows the session_unavailable error (LNXS-03)
expected: The terminal opened by the entry visibly shows the fixed session_unavailable JSON error and the launch exits non-zero.
result: [pending]

### 2. Real NixOS system: activation, reload and stable desktop entries through rebuild + GC (WR-02)
expected: kwakore.socket activates per configured user, systemctl --user reload logs "configuration reloaded", and desktop entries keep launching via /run/current-system/sw/bin/kwakore after nixos-rebuild, reboot and nix-collect-garbage.
result: [pending]

### 3. First real run of .github/workflows/linux.yml
expected: All lanes pass on GitHub Actions (backend, child, graphical under xvfb, nix, service under ci-user-manager.sh, bundle amd64+arm64, installed --full, identity); WebKit tests show PASS, not SKIP.
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps
