---
spec: WEB-NAPPLET
role: pin
repo: https://github.com/hzrd149/naps
ref: web-napplet-event
commit: 7ae5b19a9c32fbd4c881836f4d821c02630e2b4f
path: WEB-NAPPLET.md
fetched: 2026-10-02
body_sha256: 5af565a1a78a76daeac567b67ef1013b3ac2ed8dec4a155b7e38c4f987cde451
---
Web Napplet Event
=================

Kind `35129`
------------

`draft`

This document defines the Nostr event that publishes a web napplet. It defines
the napplet's stable address, verified HTML artifact, display metadata, roles,
accepted conventions, and NAP domain needs.

The event follows [NIP-01](https://github.com/nostr-protocol/nips/blob/master/01.md).
[NIP-5D](https://github.com/nostr-protocol/nips/pull/2303) remains the legacy
reference for iframe loading, sandboxing, namespace injection, and transport
until its manifest section adopts this event shape. Its current NIP-5A-derived
manifest shape is not this specification.

## Event

A web napplet is an addressable kind `35129` event. Its address
`35129:<pubkey>:<d>` is the stable napplet identity. Its `x` tag identifies one
exact build.

The event `content` MUST be a non-empty plain-text description. Clients MUST
render `content` and `title` as text, not markup.

| Tag | Cardinality | Value |
|-----|-------------|-------|
| `d` | exactly 1 | Non-empty, opaque napplet identifier. |
| `x` | exactly 1 | Lowercase hex SHA-256 of the HTML artifact bytes. |
| `server` | 1+ | HTTPS Blossom origin holding the artifacts. |
| `title` | exactly 1 | Non-empty plain-text title. |
| `icon` | 0-1 | Icon SHA-256 followed by its media type. |
| `source` | 0+ | Cloneable Git remote. |
| `z` | 0+ | Role or archetype slug. |
| `i` | 0+ | Accepted convention identity followed by parameter names. |
| `R` | 0+ | NAP domain required for full functionality. |
| `O` | 0+ | Optional NAP domain. |

`d` is exact and case-sensitive. Clients MUST NOT normalize it. The `d` value
alone is not a napplet identity; it is scoped by kind and publisher pubkey.

Unknown tags MUST be ignored. A malformed optional metadata tag (`icon` or
`source`) MUST be ignored without invalidating an otherwise valid event.
Malformed `z`, `i`, `R`, or `O` tags MUST invalidate the event because silently
dropping routing or capability declarations changes its behavior.

`d`, `x`, `server`, `title`, `source`, `z`, `R`, and `O` tags MUST contain
exactly two elements. `icon` MUST contain exactly three. `i` MUST contain at
least two. Extra elements make a tag malformed unless its shape explicitly
permits them.

Example:

```json
{
  "kind": 35129,
  "content": "Displays and filters a chronological Nostr feed.",
  "tags": [
    ["d", "feed-reader"],
    ["x", "186ea5fd14e88fd1ac49351759e7ab906fa94892002b60bf7f5a428f28ca1c99"],
    ["server", "https://blossom.example.com"],
    ["title", "Feed Reader"],
    ["icon", "0c1b82b9559f922f6f921fe4ba7fd4c3d8b406630978e0e408f55b15674f5d27", "image/png"],
    ["source", "nostr://<repository-reference>"],
    ["z", "feed"],
    ["i", "napplet:feed/open", "filter", "relay"],
    ["R", "relay"],
    ["O", "theme"]
  ]
}
```

The example omits the NIP-01 `id`, `pubkey`, `created_at`, and `sig` fields.

## HTML Artifact

A web napplet is one self-contained HTML file. The `x` tag is:

```json
["x", "<sha256>"]
```

The hash is 64 lowercase hex characters computed over the raw HTML bytes. The
artifact MUST be valid UTF-8 and MUST contain every script, style, font, and
required media asset needed to run. It MUST NOT depend on external subresources
or direct network access. Runtime-provided NAP operations are not external
resources.

Each `server` value MUST be an absolute HTTPS origin with no path, query, or
fragment. To retrieve an artifact, append `/<sha256>` to an origin as specified
by [BUD-01](https://github.com/hzrd149/blossom/blob/master/buds/01.md). A runtime
MAY try the declared servers in any order. It MUST reject bytes whose SHA-256
does not equal `x`. Repeated identical origins are equivalent to one tag.

Before execution, a runtime MUST:

1. resolve the latest event for `35129:<pubkey>:<d>` under NIP-01,
2. verify its event ID and signature,
3. validate its required fields and reject legacy markers,
4. fetch and verify the HTML artifact, and
5. bind the loaded frame to `(35129:<pubkey>:<d>, artifactHash)`.

`artifactHash` is the verified value of the event's `x` tag.

The web projection loads the verified bytes according to NIP-5D. Runtime
injection and policy MUST remain outside the bytes used to compute `x`.

## Display Metadata

`title` is required. `content` is its longer description.

An optional icon is content-addressed:

```json
["icon", "<sha256>", "<media-type>"]
```

The allowed media types are `image/png`, `image/jpeg`, and `image/webp`. The
runtime retrieves the icon from the declared `server` origins, verifies its
hash, and MUST positively decode it as the declared raster format. A hash-valid
blob whose bytes do not match the declared format is invalid. A missing,
malformed, unavailable, or unverifiable icon MUST NOT prevent the napplet from
loading. The runtime MUST use a generic fallback and MUST NOT render unverified
bytes.

Each `source` tag carries one remote suitable for `git clone`:

```json
["source", "<git-remote>"]
```

The tag name preserves compatibility with earlier napplet event shapes. This
document narrows its value to a cloneable Git URL.

`source` values MUST be absolute `https://`, `ssh://`, `git://`, or `nostr://`
URLs. The `nostr://` repository references are defined by
[NIP-34](https://github.com/nostr-protocol/nips/blob/master/34.md). Local paths,
`file://` URLs, and scp-like remotes are not portable and are invalid. This is
source metadata. A runtime MUST NOT clone or execute it as part of loading the
napplet.

## NAP Domains

NAP domain tokens MUST begin with a lowercase ASCII letter and contain only
lowercase ASCII letters, digits, and hyphens.

```json
["R", "<domain>"]
["O", "<domain>"]
```

`R` means the napplet needs the domain for full functionality. `O` means the
napplet can use the domain when available. Values MAY name registry, future, or
private domains. `shell` MAY be listed but is redundant for conformant runtimes.

Repeated values are equivalent to one declaration. If a domain appears in both
`R` and `O`, `R` wins. These tags are discovery metadata only. Neither tag
grants a capability, widens runtime policy, or controls the domains exposed to
the napplet.

A runtime MUST NOT use `R` or `O` to gate loading, issue compatibility warnings,
assign degraded status, or decide which APIs to inject. The shell determines
exposure independently under its own policy. A napplet MUST detect actual
availability through the active projection before calling a domain and MUST NOT
infer availability from its event declarations.

### Filtering

Single-letter tags are indexed under NIP-01. Clients can use `#R`, `#O`, and the
proposed [NIP-91](https://github.com/nostr-protocol/nips/pull/2252) `&R` operand
for targeted discovery.

NIP-91 does not express the subset test needed to discover every napplet whose
advertised requirements a runtime can support. Given event requirements `E` and
runtime capabilities `C`, that discovery test is `E` being a subset of `C`. An
`&R` filter for `C` instead asks for events containing every member of `C`. It
can miss events with fewer requirements and return events with additional
unsupported requirements.

Clients MUST inspect the complete `R` set locally when applying that discovery
filter. This filtering result MUST NOT control loading or runtime API exposure.
When a client uses `&R`, NIP-91 also requires the same values in `#R` for relays
without NIP-91 support and local post-filtering of returned events.

## Roles And Conventions

A `z` tag advertises a role or archetype:

```json
["z", "<role>"]
```

Role slugs MUST begin with a lowercase ASCII letter and contain only lowercase
ASCII letters, digits, and hyphens. Publishers MAY invent roles. The
[NAAT registry](ARCHETYPES.md) standardizes shared role meanings for
interoperability; it is not an allowlist. A runtime that routes by role MUST
match registered and private roles by exact equality.

An `i` tag advertises one stable, queryless convention identity and the shallow
query parameter names it accepts:

```json
["i", "napplet:<role>/<intent>", "<parameter>", "..."]
```

Every `i` role MUST have a matching `z` tag. Parameter names MUST match
`[A-Za-z_][A-Za-z0-9_]*` and be unique within that tag. They are case-sensitive
and correspond to the query-to-payload transposition defined by the active
projection. Structured or non-text data uses an explicit payload instead.
Each convention identity MUST appear in at most one `i` tag. The final path
segment names the action the napplet accepts for the matching role.

`z` and `i` are untrusted routing metadata. They MUST NOT grant capabilities or
widen what the runtime exposes. Both are optional. A napplet without them can
still be addressed directly but cannot be discovered as a convention handler.
A `z` tag by itself advertises the convention-free `open` action for that role.
Matching `i` tags advertise additional accepted actions and conventions.

## Legacy Events

Earlier NIP-5D drafts used NIP-5A `path` tags, `requires` tags, and an aggregate
`x`. A later unmerged draft used an artifact `x` with `C` capability tags. Both
shapes are incompatible with this specification.

A runtime MUST reject a kind `35129` event containing a `path`, `requires`, or
`C` tag. It MUST NOT reinterpret or partially load those legacy shapes.
Publishers must replace legacy addressable events with this schema.

## Security

Event signatures identify the publisher. Artifact hashes identify exact bytes.
Neither makes the publisher or artifact trustworthy. Runtimes MUST apply the
NIP-5D sandbox and sender-binding rules, enforce their own capability policy,
and verify every fetched artifact before use.

Display fields, roles, conventions, capability declarations, source remotes, and
server origins are untrusted input. Clients MUST escape displayed text, MUST NOT
execute source metadata, and MUST NOT treat event declarations as grants.

## References

- [NIP-01](https://github.com/nostr-protocol/nips/blob/master/01.md) — events, addressable kinds, tags, and filters
- [NIP-34](https://github.com/nostr-protocol/nips/blob/master/34.md) — Git repository announcements and clone URLs
- [NIP-5D](https://github.com/nostr-protocol/nips/pull/2303) — legacy web projection and loading contract
- [NIP-91 proposal](https://github.com/nostr-protocol/nips/pull/2252) — AND tag-filter operand
- [BUD-01](https://github.com/hzrd149/blossom/blob/master/buds/01.md) — Blossom blob retrieval
- [NAP registry](README.md) — runtime-provided capability domains
- [NAAT registry](ARCHETYPES.md) — standardized role meanings
