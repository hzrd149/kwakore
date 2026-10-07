# Phase 8 source coverage audit

The numbered D-01 through D-16 entries below label the sixteen locked bullets in `08-CONTEXT.md` in source order. They add traceability without changing those decisions. All four source types are covered; there are no deferred Phase 8 ideas.

| Source | ID | Feature or constraint | Plan | Status |
|---|---|---|---|---|
| GOAL | — | Daemon controls napplet windows, permissions and signer options with v0.1 security boundaries | 01–05 | COVERED |
| REQ | SRVC-03 | Installed napplet launch/stop, sandbox and permissions intact | 01, 05 | COVERED |
| REQ | SRVC-04 | Clear graphical-session unavailable error | 01 | COVERED |
| REQ | SOCK-06 | Per-napplet permission inspection and one-key changes without bypass | 02, 05 | COVERED |
| REQ | SIGN-01 | Supported Linux signer modes through files and socket | 03, 04 | COVERED |
| REQ | SIGN-02 | Public-only signer mode and connection status | 03–05 | COVERED |
| REQ | SIGN-03 | Protected secret input and no secret in ordinary config, reads or logs | 03–05 | COVERED |
| CONTEXT | D-01 | Launch identifies full canonical Nostr address | 01 task 1 | COVERED |
| CONTEXT | D-02 | Launch returns window ID and final outcome | 01 task 1 | COVERED |
| CONTEXT | D-03 | No graphical session returns prompt structured session_unavailable | 01 task 2 | COVERED |
| CONTEXT | D-04 | Stop identifies window ID and confirms closure | 01 task 2 | COVERED |
| CONTEXT | D-05 | Permissions identify full canonical address | 02 task 1 | COVERED |
| CONTEXT | D-06 | Permission reads show declarations and stored allow/deny | 02 task 1 | COVERED |
| CONTEXT | D-07 | Set/clear one named permission at a time | 02 task 2 | COVERED |
| CONTEXT | D-08 | Runtime gates and prompts remain authoritative | 02 task 2, 05 | COVERED |
| CONTEXT | D-09 | Local nsec, NIP-46 bunker and nostrconnect on Linux; no Amber work | 03, 04 | COVERED |
| CONTEXT | D-10 | Signer switch is separate from ordinary settings | 03 task 3 | COVERED |
| CONTEXT | D-11 | Status returns mode, user public key and connection state only | 03 task 3, 05 | COVERED |
| CONTEXT | D-12 | End old session before final new outcome | 03 task 1, 04 task 1, 05 | COVERED |
| CONTEXT | D-13 | CLI secret input from stdin or owner-only 0600 file, never argv | 03 task 3, 04 task 3 | COVERED |
| CONTEXT | D-14 | Retained secret in separate owner-only XDG credential store | 03 task 1, 04 task 2 | COVERED |
| CONTEXT | D-15 | Ordinary config secret field rejected with fixed field-name error | 03 task 2 | COVERED |
| CONTEXT | D-16 | No secrets or raw signer errors in socket reads, errors or logs | 03 task 3, 04, 05 | COVERED |
| RESEARCH | — | Daemon-callable Linux Host and child independent of Gio manager | 01, 05 | COVERED |
| RESEARCH | — | Readiness after actual WebKit host-page start; early exit and stale display bounded | 01, 05 | COVERED |
| RESEARCH | — | Exact installed record/reclaim lock for synchronous launch and gone acknowledgement for stop | 01 | COVERED |
| RESEARCH | — | Distinguish manifest declarations from host permission rules and saved from session decisions | 02 | COVERED |
| RESEARCH | — | Persist one exact RuleKey and preserve route gates and prompt ownership | 02, 05 | COVERED |
| RESEARCH | — | Serialized signer lifecycle, stale result fence, coherent concurrent identity reads | 03–05 | COVERED |
| RESEARCH | — | Stable NIP-46 client key and validated nostrconnect secret response | 04 | COVERED |
| RESEARCH | — | Private credential transaction and legacy state secret scrub | 03, 04 | COVERED |
| RESEARCH | — | Config reload and file/socket schema consistency with fixed secret-field rejection | 03 | COVERED |
| RESEARCH | — | Additive v1 RPC/CLI contract, fixed errors and protocol guide parity | 01–04 | COVERED |
| RESEARCH | — | Adversarial secret, spoofed NAP, stale signer and child-failure tests; live child smoke | 01–05 | COVERED |
| RESEARCH | — | No new packages; preserve cross-platform backend build and existing hardened child | 01, 05 | COVERED |

Phase 9 exclusions: systemd/NixOS packaging, native desktop entries, product rename, removal of Gio/Android source. The Phase 8 runtime uses the existing child source but launches it directly from the daemon; the executable packaging location is a Phase 9 contract.
