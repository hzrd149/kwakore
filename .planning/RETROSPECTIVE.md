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

## Milestone: v0.2 — Linux Service Pivot

**Shipped:** 2026-10-07
**Phases:** 4 | **Plans:** 42 | **Commits:** 266 over 2 days

### What Was Built
- A foreground per-user daemon with strict XDG config, a private override file with precedence, validated reload, live diagnostics, a five-second shutdown deadline and recovery of interrupted registry mutations
- A private, versioned JSON-RPC Unix socket (peer UID checked, fixed error codes, 21 methods) with a JSON CLI for every method and a public protocol reference
- Daemon-owned napplet launch/stop with a structured headless error, per-napplet permission edits that keep NAP gates, and nsec/bunker/nostrconnect signers whose secrets never reach reads or logs
- Socket-activated systemd user units, a reproducible checksummed bundle with an install helper, a Nix package and NixOS module, and native desktop entries backed by launch tokens
- Removal of the Gio manager/store, Android app, gomobile and nine retired desktop packages, plus a full rename to `kwakore` gated by an identity scan

### What Worked
- Ordering the phases by dependency (daemon → socket → runtime → packaging) meant each phase had a working, testable client of the one before it
- Extracting the daemon from the launcher behind the existing `backend.Host` boundary let the napplet runtime and its v0.1 security tests carry over unchanged
- One installed-artifact smoke (`smoke-linux-service.sh --full`) became the single acceptance check for install, activation, control, launch, headless and uninstall
- Splitting the Phase 9 retirements and renames into many small single-commit plans kept every intermediate commit building and testable
- The identity scan made the rename checkable instead of a one-time grep

### What Was Inefficient
- CI integration lanes (isolated systemd user manager, sudo, Nix) failed three times on runner issues, not product bugs, and were eventually removed (quick task 261007-ej4). Building them took significant time before that decision
- Phase 9 grew to 24 plans; many deferred-items entries were doc follow-ups that later plans picked up, so the file became long and hard to close
- Verification records drifted from final state: a stale `human_needed` body under a `passed` frontmatter, LNXS-03 missing from SUMMARY frontmatter, a roadmap plan list missing 07-07
- Phase 8 verified features (a settings child, CI graphical smoke) that Phase 9 then removed, so later verification had to explain the supersession

### Patterns Established
- Control-protocol errors are fixed code and message pairs with only allow-listed `data`; nothing private reaches the wire or logs
- Secrets enter only through stdin or owner-only files, are stored apart from ordinary config, and are rejected if placed in config
- Accept an inherited systemd socket strictly or fail; never fall back to binding a socket of your own
- Generic and Nix packaging render the same unit templates, with tests checking they stay equal
- Expensive real-environment checks (WebKit, systemd, Nix) run locally before each tag, and CI keeps only fast deterministic lanes

### Key Lessons
1. Decide early which integration checks are CI work and which are local release gates; the runner environment can cost more than the feature.
2. When a later phase will remove something, don't build verification on top of it in an earlier phase; note the planned retirement in the earlier phase's context.
3. Keep the SUMMARY `requirements-completed` field and the VERIFICATION body updated when a human check later closes a requirement.
4. Write deferred items one per heading with a `Status:` line; v0.2 repeated v0.1's problem of a shape the tooling cannot acknowledge.

### Cost Observations
- Model mix: not tracked
- Sessions: not tracked
- Notable: Phase 9 (24 plans) was the cost centre. The CI user-manager lanes and their three failed runs were the largest single waste.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Commits | Phases | Key Change |
|-----------|---------|--------|------------|
| v0.1 | 356 | 5 (of 8 planned) | First GSD milestone; route table and pinned-spec audit established |
| v0.2 | 266 | 4 (of 4) | Direction change to a Linux service; milestone audit run before close; CI cut back to Go lanes |

### Cumulative Quality

| Milestone | Test files touched | Verification | Zero-Dep Additions |
|-----------|--------------------|--------------|--------------------|
| v0.1 | 76 | 5/5 phases passed (human backstops deferred) | — |
| v0.2 | 78 | 4/4 phases passed, UATs closed, audit tech_debt with 23/23 requirements | — |

### Top Lessons (Verified Across Milestones)

1. Build the shared enforcement or boundary point first (v0.1 dispatcher, v0.2 daemon behind `backend.Host`), then layer features on it.
2. Deferred-items files need one entry per heading with a `Status:` line; both milestones needed manual fixes at close.
3. Size milestones to a stable direction and close them on purpose; v0.2 (4 phases, 2 days) closed complete where v0.1 (8 planned) did not.
