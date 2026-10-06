# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v0.1 — Hardening

**Shipped:** 2026-10-06 (closed early, phases 1-5 of 8)
**Phases:** 5 | **Plans:** 40 | **Commits:** 356 over 5 days

### What Was Built
- `d`-tag containment: napp dirs come from one `hex(sha256(id))` choke point, and every OS shortcut carries only launch tokens
- A canonical baseline: shim 0.30.0 vendored byte-identical, 18 pinned spec snapshots, and `spec/CONFORMANCE.md` guarded by a test
- A gated NAP dispatcher: 68 declared routes with permission gates, Go-side bounds, rate limits, quotas, exactly-once replies and owned prompts
- Desktop process and secrets hardening: a verified per-user child and libwebview, user-only instance IPC, link validation, keyring secrets and atomic state
- Frame sandbox lifecycle: reload or replace resets the session, the CSP is enforced, engines are hardened, and a hostile-napplet smoke test runs in CI
- Napplet identity and storage keyed by address plus artifact hash, NIP-01 manifest selection, public-only verified blob downloads, and reclamation on update and uninstall

### What Worked
- Landing the route-table dispatcher (Phase 2) before any per-domain work gave every later fix one choke point instead of scattered handler checks
- Tests that pin invariants (shim sha256, the route-table golden, the conformance checklist skeleton, Go/JS failure-shape parity) caught drift automatically
- Running a real hostile napplet in the child under WebKitGTK turned sandbox claims into measured evidence
- Recording the "no deployments" decision early removed a whole class of migration and compatibility work

### What Was Inefficient
- Code review ran to 3 iterations in several phases (01, 04, 05), and fix commits make up a large share of the 356. Plans would have benefited from stating cross-window and cross-session ownership up front
- Every verification passed, but each phase left backstop truths that only a person can confirm (WebView2, WKWebView, Windows and macOS shortcuts, Android). These piled up as partial UATs that were never closed
- Deferred-items files used ad-hoc shapes (a table in 02, headings elsewhere) that the GSD tooling couldn't acknowledge, so they had to be fixed by hand at close
- The milestone scope (8 phases, strict conformance across every domain) was larger than the direction held for. It was closed with 24 requirements unmet

### Patterns Established
- Declare permission gates and failure shapes in a table, and have a golden test hold it equal to the JS mirror
- Treat paths as untrusted: one containment function per kind of path, never raw `d` values in filesystem names
- Fail closed with a visible notice instead of a silent fallback (child binary, keyring, corrupt state)
- Conformance claims must cite code and a test, and quotes must be verbatim from the pinned specs

### Key Lessons
1. Put the cross-cutting enforcement point in place first (dispatcher, containment), then do domain work. It kept phases 3-5 from conflicting.
2. Size milestones to a likely-stable direction. A long conformance roadmap is easier to abandon half-done than a shorter one closed on purpose.
3. Write deferred items in one consistent shape, one entry per heading with a `Status:` field, so they can be tracked and closed by tooling.
4. Schedule the cross-platform manual checks (Windows, macOS) as real work instead of carrying them as UAT residue.

### Cost Observations
- Model mix: not tracked
- Sessions: not tracked
- Notable: review and fix iterations were the biggest cost driver. Phase 5 alone took 12 plans and 3 review rounds.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Commits | Phases | Key Change |
|-----------|---------|--------|------------|
| v0.1 | 356 | 5 (of 8 planned) | First GSD milestone; route table and pinned-spec audit established |

### Cumulative Quality

| Milestone | Test files touched | Verification | Zero-Dep Additions |
|-----------|--------------------|--------------|--------------------|
| v0.1 | 76 | 5/5 phases passed (human backstops deferred) | — |

### Top Lessons (Verified Across Milestones)

1. (Needs a second milestone to cross-validate.)
