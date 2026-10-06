---
schema_version: 1
open_count: 1
waived_count: 0
fixed_count: 1
total_count: 2
last_updated: 2026-10-06T20:28:31.761Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 06 | unrun-verify | backend |  | Default Android cross-build could not complete because the Android SDK/NDK is absent; CGO_ENABLED=0 cross-build passed | open |  | 2026-10-06T20:05:52.402Z |  |
| 2 | 06 | deviation | backend/daemon/daemon_linux.go | 218 | Unexpected custom config basename could enter reload diagnostics; fixed with redaction marker | fixed |  | 2026-10-06T20:28:20.375Z | 2026-10-06T20:28:31.761Z |

````json
[
  {
    "id": 1,
    "kind": "unrun-verify",
    "phase": "06",
    "file": "backend",
    "line": null,
    "description": "Default Android cross-build could not complete because the Android SDK/NDK is absent; CGO_ENABLED=0 cross-build passed",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-10-06T20:05:52.402Z",
    "resolved_at": null
  },
  {
    "id": 2,
    "kind": "deviation",
    "phase": "06",
    "file": "backend/daemon/daemon_linux.go",
    "line": 218,
    "description": "Unexpected custom config basename could enter reload diagnostics; fixed with redaction marker",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-10-06T20:28:20.375Z",
    "resolved_at": "2026-10-06T20:28:31.761Z"
  }
]
````
