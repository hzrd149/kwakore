# Phase 9: Linux Packaging, Rename and Cleanup - Discussion Log

> **Audit trail only.** Planning and execution should use `09-CONTEXT.md` for decisions.

**Date:** 2026-10-06
**Phase:** 9-linux-packaging-rename-and-cleanup
**Areas discussed:** service installation, native desktop entries, rename and removal, documentation and smoke testing

## Service installation

| Question | Selected answer | Other answer considered |
| --- | --- | --- |
| Generic systemd start | Enable user socket; activate daemon on client connection | Start service at login; leave activation manual |
| NixOS module enablement | Also activate the user socket | Require separate user activation |
| Generic install method | Install helper plus manual instructions | Manual instructions only |
| Ordinary config path | Existing XDG config path | Distribution-managed system config |

The install-helper question was asked twice due to an overlapping prompt. The user explicitly resolved the conflict in favor of an install helper plus manual instructions.

## Native desktop entries

| Question | Selected answer | Other answer considered |
| --- | --- | --- |
| Daemon stopped at click | Connect to socket and allow systemd activation | Require manual daemon start |
| No graphical session | CLI-style error only | Notification or dialog |
| Entry creation | Automatic, one per installed napplet | Explicit request only |
| Entry removal | Automatic on uninstall | User cleanup |

## Rename and removal

| Question | Selected answer | Other answer considered |
| --- | --- | --- |
| Supported Linux paths | Direct switch to `kwakore` without aliases | Temporary Verdana aliases |
| Android | Remove sources and build wiring | Keep dormant sources |
| Gio manager/store | Remove UI module and retain required Linux child host | Keep Gio module dormant |
| Old planning records | User asked to delete archived v0.1 planning records | Preserve or rename archival records |

Deleting archival planning records was deferred outside Phase 9 product cleanup.

## Documentation and smoke testing

| Question | Selected answer | Other answer considered |
| --- | --- | --- |
| Guide order | Generic systemd, then NixOS | NixOS first |
| Linux smoke coverage | Units, socket activation, CLI control, napplet launch | Package and CLI checks only |
| Client protocol docs | Dedicated reference linked from setup docs | Fold into main guide |
| Signer and diagnostics docs | Worked examples | Brief command reference |

The user confirmed these decisions were ready to capture in `09-CONTEXT.md`.
