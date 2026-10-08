# Spec Pins

**Pinned:** 2026-10-02

The conformance audit measures Verdana against these exact commits. Re-fetch with
`git fetch --depth 1 <repo> <ref> && git checkout FETCH_HEAD`. If a ref has moved,
re-pin deliberately and record the change here.

## Runtime contract

| Spec | Repo | Ref | Commit |
|------|------|-----|--------|
| NIP-5D (`5D.md`) | nostr-protocol/nips | `refs/pull/2303/head` | `24711d9c47bbdd07908bf1d52bf677d9cbc530f0` |

## Napplet event schema

| Spec | Repo | Ref | Commit |
|------|------|-----|--------|
| WEB-NAPPLET (`WEB-NAPPLET.md`) | hzrd149/naps | branch `web-napplet-event` | `7ae5b19a9c32fbd4c881836f4d821c02630e2b4f` |

## NAP domains (napplet/naps)

| Domain | Ref | Status | Commit |
|--------|-----|--------|--------|
| NAP-SHELL, NAP-IDENTITY, NAP-INC, NAP-INTENT, NAP-THEME | `master` | merged | `a040914b4bbd3a5cd8a14b0f316a723c968ebfb2` |
| NAP-RELAY | `refs/pull/2/head` | draft PR #2 | `0be8abce18beb46ca37bd4ddd042f58d30b4eedc` |
| NAP-STORAGE | `refs/pull/3/head` | draft PR #3 | `f71e84ebca7474db260346cbfc2d88f41b4e421e` |
| NAP-MEDIA | `refs/pull/10/head` | draft PR #10 | `2b2d29e90c30b994bf5035a65b57e5fe7f08a9a2` |
| NAP-NOTIFY | `refs/pull/11/head` | draft PR #11 | `e14f5c9d6a6dd2a69ccf79668c4a3c1e955e1ac9` |
| NAP-CONFIG | `refs/pull/14/head` | draft PR #14 | `448013e6d8cb8c75dce49576b3e7c0d46d960eac` |
| NAP-OUTBOX | `refs/pull/32/head` | draft PR #32 | `4589a8f9a16d8aa29b3740e2b3b0cdca11e0976e` |
| NAP-UPLOAD | `refs/pull/33/head` | draft PR #33 | `a7cc17463cbf5d9cb87884b31071bc4fc826034c` |
| NAP-LINK | `refs/pull/53/head` | draft PR #53 | `e25143355f6d416bfce73b12ec814f1c795ec16a` |
| NAP-COMMON | `refs/pull/67/head` | draft PR #67 | `de603e205a9b498f252be9a5e8e6825c4648df39` |
| NAP-RESOURCE | `refs/pull/80/head` | draft PR #80 | `fa6bcc6935aa19e7b70ab2a2c721dafca77c78e1` |
| NAP-CATALOG | `refs/pull/95/head` | draft PR #95 | `7573383ffe33b9ef7c57248ec84736cc93d8d184` |

## Reference implementation (napplet/web)

| Package | Version | Commit |
|---------|---------|--------|
| `@napplet/shim` | 0.30.0 (Verdana vendors a patched 0.29.2 build labelled `0.30.0+verdana.2`) | `956135bfc41a2cff5e45d6c68d9f9a4d68c50531` |
| `@napplet/nap` (per-domain `src/<domain>/shim.ts`) | 0.32.0 | same |
| `@napplet/conformance` | 0.17.0 | same |

Repo: https://github.com/napplet/web, branch `main`.

## Decisions (2026-10-02)

- **Upstream `napplet/web` is canonical.** The vendored shim is byte-identical npm `@napplet/shim` 0.30.0 with no Verdana patches.
- **NAP-SHELL:** the upstream shim follows NIP-5D presence-based capability detection (`window.napplet` holds only domain objects; napplet/web #96) and installs no `shell.ready` handshake. Kwakore adds `shell.supports` for NAP-CATALOG but keeps the shim byte-identical. The conflict with merged NAP-SHELL's "every runtime MUST implement" is recorded in the checklist.
- **NAP-INTENT:** pinned to naps master, like the upstream shim (which deliberately omits draft PR #91 delivery hooks). PR #91 is no longer pinned.
- **NAP-RESOURCE:** pinned to PR #80 head `fa6bcc6` (the live replacement for #13, which was merged by accident and reverted in `a9ad2cf`). The host also accepts the shim 0.30.0 server-hint shape (`requests:[{url,servers}]`, from branch `nap-resource` @ `9511232f69313aa7953d110e35d32cc28d506f66`) as a recorded tolerance, so the canonical shim works.
- **Napplet ciphertext:** NIP-5D Security #7 ("Shells MUST NOT sign or broadcast events containing ciphertext received from a napplet") is applied as written.

## Notes

- Most domains Verdana implements exist only as open draft PRs on napplet/naps. Drafts can change; the audit pins to the SHAs above.
- `nap/src` also contains domains Verdana does not implement (`keys`, `lists`, `dm`, `count`, `fs`, `ble`, `serial`, `webrtc`, `cvm`, `ifc`); they are out of scope for this milestone.
