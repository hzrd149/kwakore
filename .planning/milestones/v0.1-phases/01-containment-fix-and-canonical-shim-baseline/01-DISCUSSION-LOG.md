# Phase 1: Containment Fix and Canonical Shim Baseline - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md. This log preserves the alternatives considered.

**Date:** 2026-10-02
**Phase:** 01-containment-fix-and-canonical-shim-baseline
**Areas discussed:** Napp dir naming, Session start w/o handshake, Dropped patch fallout, Spec/checklist layout + CI

---

## Napp dir naming

| Option | Description | Selected |
|--------|-------------|----------|
| hex(sha256(id)) | Fixed length, collision-proof, no separators/reserved names; opaque | ✓ |
| Readable prefix + hash | Sanitized truncated `d` plus hash8; readable but more code | |
| Reversible percent-encoding | Readable; path-length and Windows reserved-name issues | |

| Option | Description | Selected |
|--------|-------------|----------|
| Hash today's id now | Small; Phase 5 may change the hash input | ✓ |
| Hash a structured key now | Future-stable, but pulls Phase 5 design forward | |

| Option | Description | Selected |
|--------|-------------|----------|
| One choke point | nappBaseDir returns validated path or error; asset sub-paths checked too | ✓ |
| Check at each call site | Less churn, easier to miss one | |
| You decide | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Everything hashed, ignore old dirs | Dev napps hashed too; old raw dirs orphaned | ✓ |
| Hashed + delete stale raw dirs | Startup RemoveAll sweep | |

---

## Session start w/o handshake

| Option | Description | Selected |
|--------|-------------|----------|
| Host page signals frame load | Trusted, unforgeable; Phase 4 builds on it | ✓ |
| Implicit on first envelope | Simplest; no reload boundary | |
| Keep wrapper-posted ready | Runs in frame, so forgeable | |

| Option | Description | Selected |
|--------|-------------|----------|
| Drop shell.init entirely | NIP-5D presence detection; conflict recorded | ✓ |
| Keep sending it harmlessly | Dead traffic | |

| Option | Description | Selected |
|--------|-------------|----------|
| Start session when srcdoc is set | Before napplet code runs; queue until Go acks | ✓ |
| Buffer until load, then flush | Boot delays, bounded buffer | |
| You decide | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, wrap in srcdoc | File stays byte-identical; scope in buildSrcdoc | ✓ |
| Research first | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Shared host JS, works on both | | |
| Desktop only, Android may break | | |

**User's choice (free text):** "Our target is desktop and not android"
**Notes:** Recorded as desktop-only runtime work. Android only has to keep compiling.

---

## Dropped patch fallout

| Option | Description | Selected |
|--------|-------------|----------|
| Accept upstream, Go answers late too | Cancel prompts outliving the timeout | ✓ |
| Accept upstream, no prompt cancel | User may approve a result the napplet never sees | |
| Ask upstream / defer | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Record in P1, fix where it belongs | Checklist rows now; fixes in domain phases | ✓ |
| Reimplement all in P1 | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Phase 2 with the prompt queue | | ✓ |
| Phase 1 | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Go tests + manual smoke list | | ✓ |
| Go tests only | | |
| I'll name the napplets | | |

---

## Spec/checklist layout + CI

| Option | Description | Selected |
|--------|-------------|----------|
| Top-level `spec/` dir | Public, lasting artifact | ✓ |
| Under `.planning/audit/` | Filtered from PRs | |
| Under `docs/conformance/` | | |

| Option | Description | Selected |
|--------|-------------|----------|
| Markdown tables per spec | ID / quote / level / status / reason / code ref | ✓ |
| YAML/JSON + generated md | | |

| Option | Description | Selected |
|--------|-------------|----------|
| AAR bind only, path-filtered | Full APK stays on dispatch/tags | ✓ |
| Full APK on every PR | | |
| Cheap GOOS=android vet | Misses gobind errors | |

| Option | Description | Selected |
|--------|-------------|----------|
| Spec .md file only, with SHA header | | ✓ |
| Spec + PR discussion | | |

---

## Claude's Discretion

- Host↔Go session-start message names and shapes; pre-ack queue bound
- Trigger for the notify.controls push without shell.init
- Layout of the shim hash test and the envelope coverage test
- How the conformance fixture is regenerated

## Deferred Ideas

- Prompt cancel on shim timeout: Phase 2
- Dropped-patch replacements: INC (P6), resource (P7), notify (P8)
- Final id scheme: Phase 5
- Android napplet runtime parity: out of scope
