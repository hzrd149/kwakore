# Phase 6: Daemon Core and Configuration - Discussion Log

> **Audit trail only.** Planning, research, and execution agents use `06-CONTEXT.md`.

**Date:** 2026-10-06
**Phase:** 6-Daemon Core and Configuration
**Areas discussed:** Daemon startup and shutdown, configuration layout, reload and precedence, health and diagnostics

## Daemon startup and shutdown

| Question | Options presented | Selected |
|---|---|---|
| Second foreground launch | Exit with error; attach and report status; replace running daemon | Exit with error |
| Successful startup output | Concise ready message; detailed summary; no success message | Concise ready message |
| Shutdown handling | Bounded grace period; immediate exit; unbounded wait | Bounded grace period |
| Invalid startup configuration | Specific error and nonzero exit; collect all errors; start with defaults | Specific error and nonzero exit |

## Configuration layout

| Question | Options presented | Selected |
|---|---|---|
| Declarative file layout | One main file; main file and drop-ins; files by topic | One main file |
| Missing file | Use defaults; create default file; require file | Use defaults |
| Client mutable settings | Small supported set; every non-secret setting; none yet | Small supported set |
| Persistence destination | Separate mutable state file; edit main config; memory only | Separate mutable state file |

## Reload and precedence

| Question | Options presented | Selected |
|---|---|---|
| File change handling | Explicit reload; automatic watch; restart only | Explicit reload |
| File versus client value | File wins; client wins; most recent wins | Client wins |
| Return to declarative value | Clear one override; set matching value; clear all overrides | Clear one override |
| Invalid reload | Preserve last valid settings; apply valid fields; return to defaults | Preserve last valid settings |

## Health and diagnostics

| Question | Options presented | Selected |
|---|---|---|
| Basic health content | Readiness and component status; alive status; detailed operational snapshot | Readiness and component status |
| Default diagnostic detail | Safe summary; verbose local report; minimal status | Safe summary |
| Failed reload health | Healthy with warning; unhealthy; healthy without warning | Healthy with warning |
| Before-socket diagnostic access | Foreground command; startup/logs only; diagnostic signal | Foreground command |

## Agent Discretion

Exact grace period, configuration syntax and filename, output schemas, and qualifying update preferences were left to research and planning.

## Deferred Ideas

None.
