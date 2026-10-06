---
id: SEED-001
status: dormant
planted: 2026-10-03
planted_during: Phase 02 (Gated NAP Dispatcher) — hardening & conformance milestone
trigger_when: when relevant
scope: unknown
audit_acknowledged:
  milestone: v0.1
  at: 2026-10-06
  status: dormant
---

# SEED-001: The napplet details page on the store should show NIP-22 comments on napplets and up/down vote reaction totals

## Why This Matters

_To be filled in. Run `/gsd-capture --seed --enrich SEED-001` to add context._

## When to Surface

**Trigger:** when relevant

This seed will surface during `/gsd-new-milestone` when the milestone scope matches.

## Scope Estimate

**Unknown** — run `/gsd-capture --seed --enrich SEED-001` to estimate effort.

## Breadcrumbs

- `desktop/detail.go` — desktop store napp/napplet detail page (where comments and vote totals would render)
- `backend/registry_detail.go` — `FetchProfileDetail`, `FetchAuthorNapps`, `LookupNapp`: backend detail data the GUI pulls; a comments/reactions fetch for the napplet's address would live alongside these
- `backend/nap_common.go` — existing reaction (`common.react`, `reactionTemplate`) and NIP-19 handling, reusable for kind 7 up/down (`+`/`-`) reactions
- Android detail screen in `android/app/src/main/java/com/verdana/app/` would need the same data through `backend/mobile`
- NIP-22 comments are kind 1111 scoped to the napplet's address (`A`/`a` tags on the 35129/15129/35130 coordinate)

## Notes

_Captured via one-shot seed capture. Enrich with trigger, why, and scope at your convenience._
