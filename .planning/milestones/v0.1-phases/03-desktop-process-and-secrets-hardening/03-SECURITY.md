---
phase: 3
slug: desktop-process-and-secrets-hardening
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-10-04
---

# Phase 3 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified against HEAD `b4ac31b`. T-03-37, the only threat left open below the threshold, was then fixed in `be7ac43`. Full per-threat evidence (file:line, test names): gsd-security-auditor verdict of 2026-10-04.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Other local users/processes → per-user cache dir | Child executable and libwebview extracted and executed from disk | Executable code, shared libraries |
| Other local users → single-instance channel | Unix socket / named pipe between launcher instances | Commands, shortcut tokens |
| Launcher → OS keyring | Login secrets moved out of `state.json` | NIP-46 client key, login (nsec / bunker URL) |
| Disk → launcher state | `state.json` and other files rewritten on every save | Settings, file-mode secrets |
| Napp / host → OS link opener | URLs handed to `xdg-open` / `open` / ShellExecute | Untrusted URLs |
| Pull request → CI | New Windows test job | Workflow token |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-03-01 | Tampering | saveState / state.json | high | mitigate | `fileutil.WriteFileAtomic` (temp, fsync, rename, dir fsync); TestWriteFileAtomicInterruptedSaveKeepsOld | closed |
| T-03-02 | Repudiation | loadState on unparsable file | high | mitigate | Renamed to `.corrupt-<unix>` without clobbering; a failed rename blocks saves and raises a notice; TestLoadStateCorruptIsKeptAside | closed |
| T-03-03 | Info disclosure | data dir and corrupt copy | medium | mitigate | Data dir 0700, symlink refused (`fileutil.TightenDir`); TestDataDirIsPrivate | closed |
| T-03-04 | Tampering | WriteFileNew destination | low | mitigate | `os.Link` / `O_EXCL`, never clobbers; TestWriteFileNewNeverClobbers | closed |
| T-03-05 | DoS | fsync cost on installs | low | accept | See AR-01 | closed |
| T-03-06 | EoP | OpenLink file:/custom scheme | high | mitigate | `netguard.ExternalLink` in desktop, mobile and bridge hosts; TestExternalLink, TestOpenLinkRefusesNonHTTP | closed |
| T-03-07 | Tampering | argv injection / whitespace | medium | mitigate | Control/space chars rejected, http(s) only, normalized `u.String()` | closed |
| T-03-08 | Spoofing | userinfo links | medium | mitigate | `u.User != nil` rejected | closed |
| T-03-09 | DoS | zombie opener processes | low | mitigate | `startCommand` reaps with `Wait` | closed |
| T-03-10 | Tampering | saveFile overwrite race | low | mitigate | `WriteFileNew` with free-name loop; TestSaveFileRaceMovesToNextName | closed |
| T-03-11 | Tampering | approved NAP link refused later | low | accept | See AR-02 | closed |
| T-03-12 | EoP | child extraction in shared /tmp | high | mitigate | Per-user `UserCacheDir/Verdana/child`, base and version dirs verified (plain, owned, 0700) on every spawn; TestEnsureRefusesSymlinkedDir | closed |
| T-03-13 | Tampering | reused child file | high | mitigate | Full sha256 re-hash with `os.SameFile` before every spawn; TestPrepareChildRunsTheHashedChild | closed |
| T-03-14 | EoP | ./child/child CWD fallback | high | mitigate | Fallbacks only in `embed_dev.go` (`dev` tag); prod fails closed | closed |
| T-03-15 | Tampering | symlink at final name or dir | medium | mitigate | Lstat + reparse-point refusal, rename replaces links; TestEnsureVersionRefusesSymlinks | closed |
| T-03-16 | DoS | dir owned by another user | low | accept | See AR-03 | closed |
| T-03-17 | Repudiation | silent fail-closed | medium | mitigate | Error log, `ErrWindowProgramUnavailable`, child-unavailable notice, manager raised | closed |
| T-03-18 | EoP | shared /tmp/webview dlopen | high | mitigate | `go-webview/embedded` dropped; libwebview extracted via `childbin`; `WEBVIEW_PATH` last env entry; TestPrepareChildPassesWebviewPath | closed |
| T-03-19 | EoP | Windows DLL search via PATH | high | mitigate | `webview.dll` next to `child-<sha>.exe`; no PATH/SetDllDirectory changes | closed |
| T-03-20 | Tampering | bare-name dlopen fallback | medium | mitigate | `child/libcheck.go` requires absolute dir + regular file before `webview.New`; TestWebviewLibraryRequiresAbsoluteDir | closed |
| T-03-21 | Tampering | copies drifting from pinned module | medium | mitigate | `sync_test.go` compares copies with the pinned module | closed |
| T-03-22 | DoS | Windows rename over loaded DLL | low | accept | See AR-04 | closed |
| T-03-23 | Spoofing | other user connecting to listener | high | mitigate | 0700 dir + 0600 socket + peer uid check; owner-only pipe DACL + client SID check; TestAcceptRefusesOtherUID, TestPipeDACLIsOwnerOnly | closed |
| T-03-24 | Info disclosure | channel squatting for shortcut token | high | mitigate | Dial verifies listener uid / pipe server SID before writing; TestDialRefusesOtherUIDListener, TestPipeDialRefusesForeignServer | closed |
| T-03-25 | Tampering | malformed/legacy commands | medium | mitigate | v2 only, known commands, token ≤16 KiB; legacy path removed | closed |
| T-03-26 | DoS | oversized/slow requests | medium | mitigate | 64 KiB LimitReader, 5 s deadline | closed |
| T-03-27 | DoS | IPC dir pre-created by another user | low | accept | See AR-05 | closed |
| T-03-28 | Info disclosure | world-readable launcher.port | medium | mitigate | TCP instance code removed, stale port file deleted; TestInstanceListenerRemovesPortFile | closed |
| T-03-SC-a | Tampering (supply chain) | go-winio v0.6.2 | high | mitigate | Pinned in go.mod/go.sum; `go mod verify` | closed |
| T-03-29 | Tampering | Windows-only security code untested | medium | mitigate | `test-windows` CI job (windows-2022) | closed |
| T-03-30 | EoP | new CI job permissions | low | accept | See AR-06 | closed |
| T-03-31 | DoS | flaky node tests on Windows | low | accept | See AR-07 | closed |
| T-03-32 | Info disclosure | client_key/login in state.json | high | mitigate | Set → Get → compare → mark keyring → atomic save without secrets; TestSecretsMigratesFileToKeyring (residue AR-11) | closed |
| T-03-33 | Tampering | overwriting keyring pairing after corrupt/missing state | high | mitigate | Get before any write; found item adopted; `clientKey()` sole generator, gated | closed |
| T-03-34 | DoS | Unavailable treated as NotFound | high | mitigate | Distinct sentinels; unavailable keeps file copy or shows failed screen | closed |
| T-03-35 | DoS | blocking keyring call freezes UI | high | mitigate | Load off the UI goroutine, short `ls.mu` holds, KeyringWait after 1 s | closed |
| T-03-36 | Repudiation | silent plaintext fallback | medium | mitigate | Keyring-fallback notice on every fallback path (residue IN-01 in AR-12) | closed |
| T-03-37 | Info disclosure | secrets in logs or notices | medium | mitigate | Audit found that a malformed stored client key and a malformed nsec/hex login could be logged verbatim through library error text. Fixed in `be7ac43`: fixed log/error messages (`errUnreadableClientKey`, `keyInputError`); TestSecretsMalformedFileClientKeyNeverLogged, TestSecretsMalformedKeyringClientKeyNeverLogged, TestLoginMalformedKeyNeverLogged | closed |
| T-03-38 | EoP | stale keyring login resumed after logout | high | mitigate | `LogoutPending`; never resumed, item deleted when reachable | closed |
| T-03-39 | Tampering | auto client-key regen in failed state | high | mitigate | `freshKeyOK` only via user-initiated `LoginWithoutKeyring` | closed |
| T-03-40 | DoS | duplicate unlock prompts on retry | low | mitigate | Retry joins in-flight load | closed |
| T-03-41 | Info disclosure | LogoutPending leaking secrets | low | accept | See AR-08 | closed |
| T-03-42 | DoS | Secret Service prompt with no timeout | high | mitigate | Single worker, 120 s / 3 s timeouts, probe on a private bus | closed |
| T-03-43 | Tampering | late Set after newer Set | medium | mitigate | Ordered worker; reads after a write don't join older reads | closed |
| T-03-44 | DoS | dismissed prompt read as not found | high | mitigate | Only `keyring.ErrNotFound` maps to NotFound; TestErrorMapping | closed |
| T-03-45 | Info disclosure | same-user macOS security CLI read | medium | accept | See AR-09 | closed |
| T-03-46 | Info disclosure | secret values in adapter errors/logs | medium | mitigate | Values redacted from provider messages; TestErrorsNeverCarryTheValue | closed |
| T-03-47 | Info disclosure | credential roaming on Windows | low | mitigate | wincred `PersistLocalMachine` | closed |
| T-03-SC-b | Tampering (supply chain) | go-keyring v0.2.8, wincred v1.2.3 | high | mitigate | Desktop module only; no keyring/dbus/winio/wincred in `./mobile` deps | closed |
| T-03-48 | Repudiation | fallback or fail-closed unseen | medium | mitigate | Notice stack on login and main screens; manager raised on KeyringWait and fail-closed | closed |
| T-03-49 | DoS | UI freeze during keyring unlock | high | mitigate | All backend actions from the frame run off the UI goroutine | closed |
| T-03-50 | Info disclosure | raw errors/hashes in UI | low | mitigate | Fixed notice copy only | closed |
| T-03-51 | Info disclosure | corrupt path on clipboard | low | accept | See AR-10 | closed |
| T-03-R1-CR01 | DoS | nostrconnect after "Log in again" (review CR-01) | high | mitigate | `loginOpts{skipConnect, automatic, pairedKey}`; TestNostrConnectAfterLoginWithoutKeyring | closed |
| T-03-R1-WR01 | Tampering | lost state + unreachable keyring → pairing overwrite | high | mitigate | `stateLost` routes to wait/failed; TestSecretsUnavailableAfterLostStateWaits | closed |
| T-03-R1-WR02 | Repudiation | unreadable state.json blocks saves silently | medium | mitigate | Notice raised; `saveState` returns `errStateSaveBlocked` | closed |
| T-03-R1-WR03 | Tampering/DoS | builds racing over shared child dir | medium | mitigate | Per-content version dirs, 24 h GC, young temp files kept | closed |
| T-03-R1-WR04 | DoS (build) | generator fails on cold module cache | low | mitigate | `go mod download -json` | closed |
| T-03-R1-WR05 | DoS/Integrity | dir fsync failure reported as failed write | low | mitigate | Unsupported dir-fsync errnos tolerated, EIO still fails | closed |
| T-03-R2-WR01 | Tampering | keyring marker saved too late | high | mitigate | Marker written with the reset state; TestSecretsLostStateMarkerSurvivesEarlyExit | closed |
| T-03-R2-WR02 | DoS/Integrity | version dir collected between verify and exec | medium | mitigate | Shared `.lock` through ensure; GC only under non-blocking exclusive lock | closed |
| T-03-R2-WR03 | DoS (launcher crash) | nil keyer deref on logout during prompt | medium | mitigate | `handleRPC` recovers; keyer read once; TestHandleRPCRecoversPanic (residue AR-13) | closed |
| T-03-R3-WR01 | DoS | Windows touch sharing violation fails spawn | medium | mitigate | `touchDir` with full sharing + `FILE_FLAG_OPEN_REPARSE_POINT`; TestEnsureVersionRefreshesWhileDirIsOpen | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-01 | T-03-05 | Fsync on every napp asset write is slower; correctness first, batch writes if installs get slow | Plan 03-01 | 2026-10-04 |
| AR-02 | T-03-11 | A user-approved NAP link can still be refused by `ExternalLink` (safe direction); aligning `napLinkOpen` → Phase 8 | Plan 03-02 | 2026-10-04 |
| AR-03 | T-03-16 | Child cache dir owned by another user fails closed with a visible notice (DoS, never hijack) | Plan 03-03 | 2026-10-04 |
| AR-04 | T-03-22 | Replacing a loaded DLL on Windows fails closed; per-version dirs make it mostly moot | Plan 03-04 | 2026-10-04 |
| AR-05 | T-03-27 | IPC dir pre-created by another user is refused; launcher runs without a listener (DoS, never hijack) | Plan 03-05 | 2026-10-04 |
| AR-06 | T-03-30 | `test-windows` uses no secrets; the workflow sets no `permissions:` block, so token scope follows the repo default | Plan 03-06 | 2026-10-04 |
| AR-07 | T-03-31 | `VERDANA_REQUIRE_NODE` unset on Windows; node-backed tests skip cleanly | Plan 03-06 | 2026-10-04 |
| AR-08 | T-03-41 | `LogoutPending` is a boolean, no secret material | Plan 03-08 | 2026-10-04 |
| AR-09 | T-03-45 | Same-user processes can read the macOS keychain item on unsigned builds; code signing deferred to a future milestone | Plan 03-09 / CONTEXT deferred | 2026-10-04 |
| AR-10 | T-03-51 | "Copy path" puts the user's own corrupt-file path on the clipboard (user-initiated, no secret) | Plan 03-10 | 2026-10-04 |
| AR-11 | T-03-32 residue (IN-02) | `state.json.corrupt-*` copies can keep a plaintext client key / login indefinitely (0600 inside the 0700 data dir) | 03-VERIFICATION item 16; user UAT "all good" | 2026-10-04 |
| AR-12 | Review IN-01, IN-03..IN-11 | Non-blocking info items. Security-relevant: IN-01 (a failed login save is only logged), IN-03 (a symlinked data-dir path orphans the keyring item and reads as a logout), IN-04 (every child-prep error worded as tampering), IN-06 (macOS `security -i` process leak on oversized values), IN-08 (keyring-less desktops stay on the failed screen after corruption, fail-safe), IN-10 (CR-01 test uses fixed sleeps) | Review iteration 3 | 2026-10-04 |
| AR-13 | T-03-R2-WR03 residue (IN-12) | Full identity redesign deferred: `userKeyer`/`userPubkey` reads stay data races, identity pushes can arrive out of order, `dev_publish.go` dereferences outside a recover → Phase 8 | REVIEW-FIX iteration 2 | 2026-10-04 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-10-04 | 63 (53 planned + 10 from review fixes) | 62 | 1 (T-03-37, medium, non-blocking) | gsd-security-auditor (ASVS L1, block on high) |
| 2026-10-04 | 63 | 63 | 0 | orchestrator — T-03-37 fixed in `be7ac43` with 3 regression tests |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-10-04
