# Roadmap: Verdana

## Milestones

- ✅ **v0.1 Hardening**: Phases 1-5 (shipped 2026-10-06; phases 6-8 moved to backlog 999.10-999.12)

## Phases

<details>
<summary>✅ v0.1 Hardening (Phases 1-5) — SHIPPED 2026-10-06</summary>

- [x] Phase 1: Containment Fix and Canonical Shim Baseline (5/5 plans) — completed 2026-10-03
- [x] Phase 2: Gated NAP Dispatcher (7/7 plans) — completed 2026-10-03
- [x] Phase 3: Desktop Process and Secrets Hardening (10/10 plans) — completed 2026-10-04
- [x] Phase 4: Frame Sandbox Lifecycle (6/6 plans) — completed 2026-10-05
- [x] Phase 5: Napplet Artifact Identity and Storage Keying (12/12 plans) — completed 2026-10-06

Full details: [milestones/v0.1-ROADMAP.md](milestones/v0.1-ROADMAP.md)

</details>

## Backlog

### Phase 999.1: Omarchy launcher search and discovery integration (BACKLOG)

**Goal:** Expose Verdana's installed and discoverable napps through an Omarchy-native searchable menu or overlay, backed by a stable machine-readable Verdana interface.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.2: Stable Verdana CLI and Hyprland window identity (BACKLOG)

**Goal:** Provide JSON-capable status, search, launch, install, shortcut, and window commands, plus stable Wayland app IDs that Omarchy can focus and manage reliably.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.3: Omarchy actionable notifications (BACKLOG)

**Goal:** Deliver native notifications with correct app identity, replacement and dismissal support, click-to-focus behavior, and napplet-defined actions routed back into Verdana.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.4: Omarchy bar widget and quick panel (BACKLOG)

**Goal:** Add an optional Quickshell plugin showing Verdana identity and activity, with fast access to recent napps, search, pending prompts, the store, and settings.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.5: Omarchy hotkeys and bundle shortcuts (BACKLOG)

**Goal:** Let users bind napps and Verdana bundle shortcuts into Omarchy's keyboard workflow with conflict detection, explicit ownership, and safe removal.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.6: Omarchy packaging installation and diagnostics (BACKLOG)

**Goal:** Ship a clean Arch/Omarchy installation path for Verdana and its optional plugin assets, including dependency setup, integration diagnostics, upgrades, and complete removal.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.7: Move Verdana data and napplet storage to the correct per-OS folders (BACKLOG)

**Goal:** Verdana's data directory (today Gio `app.DataDir()` + `Verdana`, i.e. `~/.config/Verdana` on Linux, including `state.json`, `napplet-storage/`, `storage/`, `config/`, `napps/`) follows each platform's conventions: XDG on Linux (data and napplet storage under `$XDG_DATA_HOME`, settings under `$XDG_CONFIG_HOME`, caches under `$XDG_CACHE_HOME`), `~/Library/Application Support` (and `Caches`) on macOS, and `%LocalAppData%`/`%AppData%` on Windows. There are no existing deployments, so no move of old data is needed.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.8: Remove the Android build and focus on desktop platforms only (BACKLOG)

**Goal:** Remove the Android app and its build path (`android/`, `backend/mobile` gomobile binding, `just aar`/`just apk`/`just install`, the Android CI workflow and AAR job, Android-only shims such as Android equivalents of host headers) and make the project desktop-only (Linux, macOS, Windows): update CLAUDE.md, .claude/CLAUDE.md, PROJECT.md constraints, README/NAPPLETS.md and CONFORMANCE rows that mention Android (e.g. 5D-NG-android), and drop the `GOOS=android` build gate.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.9: Rename the project to kwakore (BACKLOG)

**Goal:** Rename every "verdana" name in the app to kwakore. The GitHub fork has already been renamed to kwakore (first captured as "KwakCore"); the project integrates napplets into the user's existing desktop operating system. Also update the git remote and import paths to the renamed repository. Covers the user-facing name, window titles, notices and copy; Go module paths (`verdana/backend`, `fiatjaf.com/verdana/desktop`); binary, shortcut, bundle and desktop-entry identifiers (`com.verdana.*`, `X-Verdana-Napp-ID`, `--launch-napp` tokens); keyring service and account names; single-instance socket and pipe names; per-user child cache directory; environment variables (`VERDANA_*`); data-directory names (do it together with backlog 999.7, which moves data to per-OS folders). There are no existing deployments, so no migration of old names, shortcuts, keyring items or data is needed; CI, packaging, docs and planning files.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.10: Relay, outbox, intent and INC conformance (BACKLOG)

**Goal:** Event traffic and napplet-to-napplet messaging behave exactly as NAP-RELAY, NAP-OUTBOX, NAP-INTENT (naps master) and NAP-INC specify: napplet ciphertext is never signed, napplet-named relays are dialed through the public-internet guard, every subscription terminates, and no handler launches without the user's authorization.
**Requirements:** RELY-01..06, INTN-01..03 (unmet at v0.1 close; full success criteria in [v0.1-ROADMAP.md](milestones/v0.1-ROADMAP.md) Phase 6)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.11: Resource, upload and media policy (BACKLOG)

**Goal:** Every byte a napplet fetches, uploads or plays passes through the launcher's consent, host policy, quotas and verification, as NAP-RESOURCE (PR #80), NAP-UPLOAD and NAP-MEDIA require. Needs a research spike on the loopback media proxy and Blossom hash verification.
**Requirements:** RES-01..04, UPLD-01, MDIA-01..03 (unmet at v0.1 close; full success criteria in [v0.1-ROADMAP.md](milestones/v0.1-ROADMAP.md) Phase 7)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.12: Trusted prompts, remaining domains and audit close-out (BACKLOG)

**Goal:** Every prompt shows trusted napplet identity; notify, config, identity, link and common domains conform; spec/CONFORMANCE.md has no open rows and NAPPLETS.md matches the code (including the stale pre-Phase-5 text noted in 05 deferred items).
**Requirements:** MISC-01..05, SPEC-02, SPEC-04 (unmet at v0.1 close; full success criteria in [v0.1-ROADMAP.md](milestones/v0.1-ROADMAP.md) Phase 8)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)
