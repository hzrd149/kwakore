---
schema_version: 1
open_count: 2
waived_count: 0
fixed_count: 1
total_count: 3
last_updated: 2026-10-07T03:47:17.590Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 06 | unrun-verify | backend |  | Default Android cross-build could not complete because the Android SDK/NDK is absent; CGO_ENABLED=0 cross-build passed | open |  | 2026-10-06T20:05:52.402Z |  |
| 2 | 06 | deviation | backend/daemon/daemon_linux.go | 218 | Unexpected custom config basename could enter reload diagnostics; fixed with redaction marker | fixed |  | 2026-10-06T20:28:20.375Z | 2026-10-06T20:28:31.761Z |
| 3 | 08 | unrun-verify | .github/workflows/desktop.yml |  | Exact xvfb-run real-child wrapper unavailable locally; same required graphical test passed on active DISPLAY and CI installs xvfb | open |  | 2026-10-07T03:47:17.590Z |  |

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
  },
  {
    "id": 3,
    "kind": "unrun-verify",
    "phase": "08",
    "file": ".github/workflows/desktop.yml",
    "line": null,
    "description": "Exact xvfb-run real-child wrapper unavailable locally; same required graphical test passed on active DISPLAY and CI installs xvfb",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-10-07T03:47:17.590Z",
    "resolved_at": null
  }
]
````
