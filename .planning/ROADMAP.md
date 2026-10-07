# Roadmap: Kwakore

## Milestones

- ✅ **v0.1 Hardening**: Phases 1-5 (shipped 2026-10-06; phases 6-8 moved to backlog 999.10-999.12)
- ✅ **v0.2 Linux Service Pivot**: Phases 6-9 (shipped 2026-10-07)

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

<details>
<summary>✅ v0.2 Linux Service Pivot (Phases 6-9) — SHIPPED 2026-10-07</summary>

- [x] Phase 6: Daemon Core and Configuration (6/6 plans) — completed 2026-10-06
- [x] Phase 7: Unix Socket and CLI (7/7 plans) — completed 2026-10-06
- [x] Phase 8: Runtime and Signer Integration (5/5 plans) — completed 2026-10-07
- [x] Phase 9: Linux Packaging, Rename and Cleanup (24/24 plans) — completed 2026-10-07

Full details: [milestones/v0.2-ROADMAP.md](milestones/v0.2-ROADMAP.md) · Audit: [milestones/v0.2-MILESTONE-AUDIT.md](milestones/v0.2-MILESTONE-AUDIT.md)

</details>

## Backlog

Reviewed 2026-10-07: 999.7 (per-OS data folders), 999.8 (remove Android) and 999.9 (rename to kwakore) were delivered in v0.2 and removed. 999.1–999.6 were rewritten for the socket service; 999.10–999.12 hold the unfinished v0.1 conformance work.

### Phase 999.1: Omarchy launcher search and discovery integration (BACKLOG)

**Goal:** Expose installed and discoverable napplets through an Omarchy-native searchable menu or overlay, built on the control socket (`napplet.installed`, `napplet.discover`, `napplet.launch`) and the native desktop entries rather than a separate interface.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.2: Kwakore CLI and Hyprland window identity (BACKLOG)

**Goal:** Give each napplet window a stable Wayland app ID (and X11 class) that Omarchy/Hyprland can focus and manage, and add the CLI and protocol surface still missing for that: listing and focusing open windows. JSON-capable status, discover, install, launch and stop commands already shipped in v0.2 (`kwakore`, protocol v1).
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.3: Omarchy actionable notifications (BACKLOG)

**Goal:** Replace the plain `notify-send` call in `backend/linuxhost` with notifications that carry the napplet window's app identity, support replacement and dismissal and click-to-focus, and route napplet-defined actions back to the napplet.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.4: Omarchy bar widget and quick panel (BACKLOG)

**Goal:** Add an optional Quickshell plugin, as a control-socket client, showing signer identity and running napplets, with fast access to recent napplets, search, pending prompts and settings.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.5: Omarchy hotkeys (BACKLOG)

**Goal:** Let users bind napplets into Omarchy's keyboard workflow (Hyprland binds that launch through the desktop entries or `kwakore launch`) with conflict detection, explicit ownership and safe removal.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.6: Omarchy packaging, installation and diagnostics (BACKLOG)

**Goal:** Ship a clean Arch/Omarchy installation path (e.g. a PKGBUILD around the release bundle and user units) plus the optional plugin assets, with dependency setup, integration diagnostics via `service.diagnostics`, upgrades and complete removal.
**Requirements:** TBD
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.10: Relay, Outbox, Intent and INC Conformance (BACKLOG)

**Goal:** Event traffic and napplet-to-napplet messaging behave exactly as NAP-RELAY, NAP-OUTBOX, NAP-INTENT (naps master) and NAP-INC specify: napplet ciphertext is never signed, and no handler launches without the user's authorization. Carried over from v0.1 Phase 6.
**Requirements:** RELY-01..06, INTN-01..03 (`milestones/v0.1-REQUIREMENTS.md`)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.11: Resource, Upload and Media Policy (BACKLOG)

**Goal:** Every byte a napplet fetches, uploads or plays passes through the launcher's consent, host policy, quotas and verification, as NAP-RESOURCE (PR #80), NAP-UPLOAD and NAP-MEDIA require. Carried over from v0.1 Phase 7; needs a research spike (media proxy, streaming Blossom verification, D-17 reply cap).
**Requirements:** RES-01..04, UPLD-01, MDIA-01..03 (`milestones/v0.1-REQUIREMENTS.md`)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.12: Trusted Prompts, Remaining Domains and Audit Close-out (BACKLOG)

**Goal:** Every prompt shows who is really asking, the notify, config, identity, link and common domains conform, and the audit checklist and `NAPPLETS.md` describe a runtime with every MUST and SHOULD accounted for. Carried over from v0.1 Phase 8 (includes IN-12 / AR-13 identity globals).
**Requirements:** MISC-01..05, SPEC-02, SPEC-04 (`milestones/v0.1-REQUIREMENTS.md`)
**Plans:** 0 plans

Plans:

- [ ] TBD (promote with $gsd-review-backlog when ready)

## Deferred

- v0.2 audit tech debt (`milestones/v0.2-MILESTONE-AUDIT.md`): dead launcher paths, child environment hardening, stale comments.
- Replacement settings/store UI.
- macOS, Windows, and Android hosts.
