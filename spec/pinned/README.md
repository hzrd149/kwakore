# Pinned spec snapshots

These are the exact spec texts the conformance audit (`spec/CONFORMANCE.md`)
measures Kwakore against. The pins themselves, and the decisions behind them,
are recorded in `.planning/research/SPEC-PINS.md`. Most NAP domains exist only
as open draft pull requests that can be force-pushed or closed, so each pinned
text is committed here at its commit SHA and the audit quotes these files, not
the moving upstream.

Each snapshot holds the spec `.md` text only (no pull request discussion).

## Snapshots

Listed in SPEC-PINS order, which `spec/CONFORMANCE.md` sections also follow.

| File | Spec | Role | Repo | Ref | Commit | body sha256 |
|------|------|------|------|-----|--------|-------------|
| [NIP-5D@24711d9c.md](NIP-5D@24711d9c.md) | NIP-5D | pin | nostr-protocol/nips | `refs/pull/2303/head` | `24711d9c47bbdd07908bf1d52bf677d9cbc530f0` | `3adea2e3db6d32807ed5832bd928c72ae02bbd26266994d8b8d6f26a158f41e2` |
| [WEB-NAPPLET@7ae5b19a.md](WEB-NAPPLET@7ae5b19a.md) | WEB-NAPPLET | pin | hzrd149/naps | `web-napplet-event` | `7ae5b19a9c32fbd4c881836f4d821c02630e2b4f` | `5af565a1a78a76daeac567b67ef1013b3ac2ed8dec4a155b7e38c4f987cde451` |
| [NAP-SHELL@a040914b.md](NAP-SHELL@a040914b.md) | NAP-SHELL | pin | napplet/naps | `master` | `a040914b4bbd3a5cd8a14b0f316a723c968ebfb2` | `a12ae633679b5fb3acff43e502e3749c640ee105ff211928b7eecb7d127590ec` |
| [NAP-IDENTITY@a040914b.md](NAP-IDENTITY@a040914b.md) | NAP-IDENTITY | pin | napplet/naps | `master` | `a040914b4bbd3a5cd8a14b0f316a723c968ebfb2` | `f599c0b0cf68fb2bf4c2f6f08a3675d09cca1d53c620e7863957f3ab3dc45bce` |
| [NAP-INC@a040914b.md](NAP-INC@a040914b.md) | NAP-INC | pin | napplet/naps | `master` | `a040914b4bbd3a5cd8a14b0f316a723c968ebfb2` | `352f27c8d0cb22da93a541b95b832d9d7f7f9840a19a98c863c2de2d0fbdccbe` |
| [NAP-INTENT@a040914b.md](NAP-INTENT@a040914b.md) | NAP-INTENT | pin | napplet/naps | `master` | `a040914b4bbd3a5cd8a14b0f316a723c968ebfb2` | `d6a533ea9c132f0196057c177e87452a7c9edda452fea4b89368afc202b709f0` |
| [NAP-THEME@a040914b.md](NAP-THEME@a040914b.md) | NAP-THEME | pin | napplet/naps | `master` | `a040914b4bbd3a5cd8a14b0f316a723c968ebfb2` | `f87fc4afcd0cfdc9401540935421a9a876036384953efa8ceaf9b1ba607e9762` |
| [NAP-RELAY@0be8abce.md](NAP-RELAY@0be8abce.md) | NAP-RELAY | pin | napplet/naps | `refs/pull/2/head` | `0be8abce18beb46ca37bd4ddd042f58d30b4eedc` | `898a1f316b5750a368354fa58abdd9358bc6ce65efd11d33c3eb2e47646ef140` |
| [NAP-STORAGE@f71e84eb.md](NAP-STORAGE@f71e84eb.md) | NAP-STORAGE | pin | napplet/naps | `refs/pull/3/head` | `f71e84ebca7474db260346cbfc2d88f41b4e421e` | `0045b6e304b194e20e83b5a29431f81e66b0a45a28c112d11e8cadc07d32a850` |
| [NAP-MEDIA@2b2d29e9.md](NAP-MEDIA@2b2d29e9.md) | NAP-MEDIA | pin | napplet/naps | `refs/pull/10/head` | `2b2d29e90c30b994bf5035a65b57e5fe7f08a9a2` | `4dab9c6c658860ad266aa74e47e30faf9b3b63f1c3bba4fe4d98d0c7f05c83bb` |
| [NAP-NOTIFY@e14f5c9d.md](NAP-NOTIFY@e14f5c9d.md) | NAP-NOTIFY | pin | napplet/naps | `refs/pull/11/head` | `e14f5c9d6a6dd2a69ccf79668c4a3c1e955e1ac9` | `892d338587627f71899a4594096cf08795faa5e9fa230af72c31083f12abc5a2` |
| [NAP-CONFIG@448013e6.md](NAP-CONFIG@448013e6.md) | NAP-CONFIG | pin | napplet/naps | `refs/pull/14/head` | `448013e6d8cb8c75dce49576b3e7c0d46d960eac` | `2d34af51d84edd4353ec142002d9ffe0a7a5477ce9b0fd71835273fd4034931b` |
| [NAP-OUTBOX@4589a8f9.md](NAP-OUTBOX@4589a8f9.md) | NAP-OUTBOX | pin | napplet/naps | `refs/pull/32/head` | `4589a8f9a16d8aa29b3740e2b3b0cdca11e0976e` | `1761cd6515b125f2ab802343c19749d9da09863ce62a90a7a156714a0239d0f4` |
| [NAP-UPLOAD@a7cc1746.md](NAP-UPLOAD@a7cc1746.md) | NAP-UPLOAD | pin | napplet/naps | `refs/pull/33/head` | `a7cc17463cbf5d9cb87884b31071bc4fc826034c` | `fa9ef6df22091f4ee874853b64834f10d5fe4547f92d41d90e1d15f66a1466e8` |
| [NAP-LINK@e2514335.md](NAP-LINK@e2514335.md) | NAP-LINK | pin | napplet/naps | `refs/pull/53/head` | `e25143355f6d416bfce73b12ec814f1c795ec16a` | `e65cb78b0d30aff82c370a3b21840282649dec16ed984d99dc10e1b800d9ba1d` |
| [NAP-COMMON@de603e20.md](NAP-COMMON@de603e20.md) | NAP-COMMON | pin | napplet/naps | `refs/pull/67/head` | `de603e205a9b498f252be9a5e8e6825c4648df39` | `0e65f63eaf74483fcacb069117c5239063db0af1073ce92b27fd5076be706e75` |
| [NAP-RESOURCE@fa6bcc69.md](NAP-RESOURCE@fa6bcc69.md) | NAP-RESOURCE | pin | napplet/naps | `refs/pull/80/head` | `fa6bcc6935aa19e7b70ab2a2c721dafca77c78e1` | `109f7f9107b6c1548a50faf0b7c4ad59dedee53d05249a574d5636017b7f446d` |
| [NAP-RESOURCE@9511232f.md](NAP-RESOURCE@9511232f.md) | NAP-RESOURCE | tolerance | napplet/naps | `nap-resource` | `9511232f69313aa7953d110e35d32cc28d506f66` | `acac746006fe29d3bdd4e214a5aa0069a99624c5c1f9402cd905d220e4cb50bd` |

Repo URLs: nostr-protocol/nips is `https://github.com/nostr-protocol/nips`,
hzrd149/naps is `https://github.com/hzrd149/naps` and napplet/naps is
`https://github.com/napplet/naps`. Each snapshot's front matter carries the
full URL.

### The NAP-RESOURCE tolerance

`NAP-RESOURCE@9511232f.md` is a recorded tolerance, not a pin. NAP-RESOURCE is
pinned to draft PR #80 (`NAP-RESOURCE@fa6bcc69.md`). The host also accepts the
server-hint request shape (`requests:[{url,servers}]`) that the canonical
`@napplet/shim` 0.30.0 sends, which comes from the `nap-resource` branch text
kept here so that tolerance can be quoted. The two texts are separate files and
are never merged.

## File format

Every snapshot is a short front matter block followed by the spec text:

```
---
spec: NAP-RELAY
role: pin
repo: https://github.com/napplet/naps
ref: refs/pull/2/head
commit: 0be8abce18beb46ca37bd4ddd042f58d30b4eedc
path: naps/NAP-RELAY.md
fetched: 2026-10-02
body_sha256: 898a1f316b5750a368354fa58abdd9358bc6ce65efd11d33c3eb2e47646ef140
---
<exact bytes of `git show {commit}:{path}`>
```

The body is every byte after the closing `---` line. It is the git blob as
stored upstream, with no newline, line-ending or Unicode normalization
(`.gitattributes` marks these files `-text` so git never converts them).
`body_sha256` is the sha256 of those raw bytes.

## Re-verifying

Against upstream, in a clone that has the commit:

```
git show {commit}:{path} | sha256sum
```

must print the snapshot's `body_sha256`. Against the files themselves:

```
cd backend && go test -run TestPinnedSpecSnapshotsMatchTheirHashes .
```

checks that every listed snapshot exists (and nothing else does), that its
front matter is complete and matches the list in `backend/spec_pinned_test.go`,
that its body hashes to `body_sha256`, and that this README lists the files in
SPEC-PINS order.

## Re-pin rule

Never edit a snapshot, not even for a typo or re-wrapping. When a pin moves,
re-pin deliberately in `.planning/research/SPEC-PINS.md`, add a new file at the
new SHA, update the list in `backend/spec_pinned_test.go` and this README, and
remove the old file only once nothing cites it.
