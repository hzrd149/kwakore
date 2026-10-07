---
phase: 09
slug: linux-packaging-rename-and-cleanup
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-10-07
---

# Phase 09 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Linux user session to daemon | Client and inherited file descriptor must retain private same-user control. | see plan threat models |
| Installed artifact to user filesystem | Build and installer may write executables and unit files for this user. | see plan threat models |
| Untrusted napplet metadata to desktop shell | Names and addresses are data, never executable syntax. | see plan threat models |
| Daemon to child | Retained executable still supplies sandbox and host operations. | see plan threat models |
| Go module identifier to dependent source | Import strings must point to the same retained implementation. | see plan threat models |
| Daemon to child environment | Window metadata crosses process boundary. | see plan threat models |
| Child to host page | Bound bridge names must match exactly to preserve capability checks. | see plan threat models |
| Nix store to user service | Declarative public settings enter daemon configuration. | see plan threat models |
| User manager to daemon | Nix rendered units pass an inherited private listener. | see plan threat models |
| CI archive to local installer | Downloaded binaries must match published checksums. | see plan threat models |
| Documentation to operator | Commands change user unit state and signer credentials. | see plan threat models |
| Release archive to installed binaries | Same bytes must pass checksums and run under user manager. | see plan threat models |
| Desktop shell to CLI to socket | Entry token carries untrusted address data into canonical RPC validation. | see plan threat models |
| Runtime identity to child/client | A string rename may cross an active protocol or bridge boundary. | see plan threat models |
| Retired platform to backend | Shared backend security code remains in use on Linux. | see plan threat models |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-09-01-1 | Spoofing | inherited descriptor | high | mitigate | Validate PID, descriptor count, Unix socket path, UID and mode before serving. | closed |
| T-09-01-2 | Elevation of privilege | socket directory | high | mitigate | Assert 0700 directory and 0600 socket in unit and runtime tests. | closed |
| T-09-02-1 | Tampering | archive install | high | mitigate | Verify named archive SHA256SUMS, reject unsafe members, stage and atomically replace. | closed |
| T-09-02-2 | Denial of service | concurrent install | medium | mitigate | Owner-only lock serializes helper and smoke asserts repeated installs. | closed |
| T-09-03-1 | Tampering | desktop entry text | high | mitigate | Hash filenames, sanitize display fields, encode canonical address token and validate Exec. | closed |
| T-09-03-2 | Information disclosure | headless launch error | low | mitigate | Only fixed JSON error data reaches terminal; no raw internal path. | closed |
| T-09-04-01 | Tampering | entry reconciliation | high | mitigate | Rebuild from committed canonical records and remove only owned namespace files. | closed |
| T-09-04-02 | Denial of service | concurrent mutation | medium | mitigate | Serialized latest-state reconciliation with concurrent regression test. | closed |
| T-09-05-01 | Denial of service | Android binding removal | medium | mitigate | Keep shared backend packages and run full backend suite after deletion. | closed |
| T-09-05-02 | Tampering | unsupported installer removal | low | mitigate | Delete only enumerated tracked installer source; leave user files untouched. | closed |
| T-09-06-01 | Tampering | Go module graph | medium | mitigate | Run both module test suites and inspect module list for retired import paths. | closed |
| T-09-06-02 | Denial of service | child build | high | mitigate | Compile retained child and run the prior graphical integration test after rename. | closed |
| T-09-07-01 | Denial of service | child environment | high | mitigate | Rename host and child readers atomically and run real launch test. | closed |
| T-09-07-02 | Elevation of privilege | host-page bridge | high | mitigate | Keep existing token checks and test exact paired bridge names. | closed |
| T-09-08-01 | Information disclosure | Nix settings | high | mitigate | Reject secret-like keys and emit only validated public serviceconfig fields. | closed |
| T-09-08-02 | Elevation of privilege | user socket | high | mitigate | Assert ConditionUser plus 0700 directory/0600 inode and identical path to generic unit. | closed |
| T-09-08-03 | Tampering | patched child | medium | mitigate | Retain interpreter, RUNPATH, ldd and WebKit install checks. | closed |
| T-09-09-01 | Tampering | release bundle | high | mitigate | Generate per-asset SHA256SUMS and enforce before extraction. | closed |
| T-09-09-02 | Denial of service | CI user manager | medium | mitigate | Isolate runtime/home and clean up only created resources. | closed |
| T-09-10-01 | Information disclosure | signer examples | high | mitigate | Show protected stdin/file input and public-only output; no secret argument or Nix store secret. | closed |
| T-09-10-02 | Spoofing | socket docs | medium | mitigate | Preserve UID and owner-only endpoint requirements in client reference. | closed |
| T-09-11-01 | Tampering | release archive | high | mitigate | Smoke uses checksummed actual release layout; no source-tree bypass. | closed |
| T-09-11-02 | Elevation of privilege | socket and entry | high | mitigate | Assert owner modes, peer contract and canonical token path during installed launch. | closed |
| T-09-11-03 | Information disclosure | signer/logs | medium | mitigate | Inspect journal for fixed public errors; keep fixture secret material in isolated private directory. | closed |
| T-09-12-01 | Denial of service | Linux child retention | high | mitigate | Check child imports and require retained child tests after deletion. | closed |
| T-09-13-01 | Tampering | Linux child retention | high | mitigate | Check child imports and require retained child tests after deletion. | closed |
| T-09-14-01 | Denial of service | Linux child retention | high | mitigate | Check child imports and require retained child tests after deletion. | closed |
| T-09-15-01 | Denial of service | Linux child retention | high | mitigate | Check child imports and require retained child tests after deletion. | closed |
| T-09-16-01 | Elevation of privilege | runtime identity | medium | mitigate | Update both call sites of active contracts in one task and run named tests. | closed |
| T-09-17-01 | Tampering | runtime identity | medium | mitigate | Update both call sites of active contracts in one task and run named tests. | closed |
| T-09-18-01 | Information disclosure | runtime identity | medium | mitigate | Update both call sites of active contracts in one task and run named tests. | closed |
| T-09-19-01 | Denial of service | Child retention | high | mitigate | Require retained child tests after deletion. | closed |
| T-09-20-01 | Denial of service | Child retention | high | mitigate | Require retained child tests after deletion. | closed |
| T-09-21-01 | Denial of service | Backend retention | medium | mitigate | Require backend tests after deletion. | closed |
| T-09-22-01 | Denial of service | Backend retention | medium | mitigate | Require backend tests after deletion. | closed |
| T-09-23-01 | Tampering | registry address | medium | mitigate | Preserve canonical address values and run backend tests. | closed |
| T-09-24-01 | Tampering | identity scanner | medium | mitigate | Use file-specific allowlist and test runtime scan. | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

Per-threat evidence (file:line) is in the 2026-10-07 gsd-security-auditor verdict, summarized in the audit trail below. Every mitigation was verified present in the implementation; none is counted closed on the strength of the deleted scripts/ci-user-manager.sh.

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-09-01 | T-09-06-02, T-09-07-01, T-09-07-02, T-09-08-02, T-09-11-01, T-09-11-02, T-09-11-03 | Quick task 261007-ej4 removed the graphical, nix, user service and installed-artifact CI lanes ("we dont need to test the full linux integrations in the CI, probably just the go tests"; "Keep tag-only release"). These mitigations still exist and are verified, but now run only locally: real-engine forged-binding and launch tests, the Nix module eval, and `scripts/smoke-linux-service.sh --full`. Published release archives are checked in CI only by the manifest and checksum gate. Compensating control: before each `v*` tag, run `smoke-linux-service.sh --full` (optionally `--archive`/`--sha256sums` against the published asset) plus the local real-engine and Nix commands in AGENTS.md. | hzrd149 | 2026-10-07 |

---

## Residual Notes (non-blocking, not in the register)

- IN-02 (09-REVIEW): `LISTEN_PIDFDID` is not cleared, and `LISTEN_*`/fd 3 are cleaned only after `daemon.Open`. Validation still precedes serving, so T-09-01-1 holds.
- IN-01 (09-REVIEW): the sticky-bit exemption in `checkProgram` also covers the program's own directory; no exploit was found.
- A NUL or control character in a napplet `d` tag fails closed at exec; daemon-side rejection is a deferred follow-up from quick task 261007-cth (the CLI path already rejects it).
- The post-phase `napplet.install` `relays` param is registered in 261007-cth's own model (T-q261007-05) and spot-checked present: `backend/daemon/rpc_linux.go:429-441`, `backend/registry_address.go:145` (`napExplicitRelay`).

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-10-07 | 37 | 37 | 0 | gsd-security-auditor (ASVS L1, block_on high): backend and desktop suites, both identity scans, Nix module eval, child import graph; display-backed tests and `--full` smoke not re-run (verified by presence and wiring at L1) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-10-07
