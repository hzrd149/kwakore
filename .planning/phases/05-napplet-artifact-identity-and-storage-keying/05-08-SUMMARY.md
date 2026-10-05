---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 08
subsystem: registry
tags: [registry, blossom, ssrf, netguard, source, reg-02]

requires:
  - phase: 05-05
    provides: reason catalogue (reasonSource reserved), invalidManifest, unavailableReason, nappFromLatest
  - phase: 05-06
    provides: update rig (newBlobRig already registers its loopback server as a user Blossom server)
provides:
  - blobClient (netguard.DialContext, Proxy nil, at most 3 redirects, none away from https) and trustedBlobClient (same limits, default dialer) in registry_install.go
  - blobMaxBytes (64 MiB, a var so tests can lower it), blobRedirect, userBlobServers, fetchBlobFrom
  - downloadBlob picks the client per server and wraps the last error with %w
  - validWebNappletSource (WEB-NAPPLET @7ae5b19a) and validGitSource (NIP-5D, D-11), sharing sourceChars and sourceURL
  - nip5dFromEvent refuses a bad source with invalidManifest(reasonSource, ...)
affects: [05-09, 05-10, 05-11, registry, install, update, trial, icons]

actuals:
  tokens: 7350     # chars/4 over the realized diff ca4be2c..1a8b8a2 (29.4k chars)
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Blob fetches choose their client per server: the user's own Blossom servers (normalized) get trustedBlobClient, every other server gets the public-only blobClient"
    - "Loopback test servers serve blobs only as user Blossom servers (state.BlossomServers, restored in t.Cleanup)"
    - "source validation is per manifest schema: WEB-NAPPLET drops a bad tag, NIP-5D refuses the manifest"

key-files:
  created:
    - backend/registry_blob_test.go
  modified:
    - backend/registry_install.go
    - backend/containment_test.go
    - backend/preview_test.go
    - backend/napplet.go
    - backend/napplet_nip5d.go
    - backend/napplet_test.go

key-decisions:
  - "blobMaxBytes = 64 MiB, a package var rather than a const so tests can lower it to 1 KiB"
  - "The redirect check counts redirects (len(via) > 3), so exactly 3 are followed and the 4th refused. resourceClient's len(via) >= 3 stops at 2."
  - "https may only redirect to https, checked against the previous hop rather than the first one"
  - "trustedBlobClient also has Proxy nil and the same limits. It differs only in the dialer, so a user's LAN or localhost server works and everything else stays the same."
  - "Trust is decided per downloadBlob call from BlossomServers() normalized with nostr.NormalizeHTTPURL. A manifest server tag with the same url is trusted only because it is one of those servers. With no user list set, the defaults are trusted."
  - "Both source validators also refuse Unicode format characters (Cf, e.g. U+202E), because a bidi override would make the shown remote lie. They use u.Hostname() so https://:443/x counts as host-less."
  - "The scp-like form also refuses a host containing @. Otherwise a@b@c:path would show user a and host b@c."

requirements-completed: []  # REG-02 is also carried by later 05 plans (05-11 conformance); ticked when those land
requirements-addressed: [REG-02]

coverage:
  - id: D1
    description: "downloadBlob with a loopback manifest server and no user servers fails with netguard.ErrPrivateAddress and the server gets zero requests; swapping blobClient's dialer for the default one makes the test fail (guard proven by hand)"
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/registry_blob_test.go#TestBlobDownloadRefusesPrivateHosts"
        status: pass
    human_judgment: false
  - id: D2
    description: "InstallNapp of a manifest whose only server is 127.0.0.1 installs nothing and the server gets no request; with that server listed as a user Blossom server the same install succeeds"
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/registry_blob_test.go#TestBlobDownloadRefusesPrivateHosts/install"
        status: pass
    human_judgment: false
  - id: D3
    description: "A loopback server in the user's list (with a trailing slash, matched after normalization) serves the blob; with no list set the defaults are trusted and nothing else is"
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/registry_blob_test.go#TestBlobDownloadTrustsUserServers"
        status: pass
    human_judgment: false
  - id: D4
    description: "With blobMaxBytes at 1024: a declared Content-Length of 4096 is refused, a 4096-byte stream with no length is refused, and downloadBlob then gets the right 512-byte blob from the next server"
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/registry_blob_test.go#TestBlobDownloadSizeCap"
        status: pass
    human_judgment: false
  - id: D5
    description: "Three chained redirects are followed. A fourth is refused and never reaches its target. An https server redirecting to http is refused, and the http target gets no request."
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/registry_blob_test.go#TestBlobDownloadRedirectLimits"
        status: pass
    human_judgment: false
  - id: D6
    description: "WEB-NAPPLET source table: https/ssh/git/nostr with a host accepted. Refused: http, git+ssh, scp-like, opaque, host-less, relative, a leading - on host or user, whitespace, control and format characters."
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/napplet_test.go#TestValidSourceWebNapplet"
        status: pass
    human_judgment: false
  - id: D7
    description: "NIP-5D source table: https/http/git/ssh/git+ssh with a host and scp-like user@host:path accepted. Refused: nostr in any form, opaque, host-less, relative, file://, host:path, user@host:, a/b:c, a leading - on host or user, whitespace."
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/napplet_test.go#TestValidSourceNIP5D"
        status: pass
    human_judgment: false
  - id: D8
    description: "A WEB-NAPPLET event with a malformed source stays available. The bad source is dropped and a good one kept."
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/napplet_test.go#TestWebNappletMalformedSourceIgnored"
        status: pass
    human_judgment: false
  - id: D9
    description: "A NIP-5D manifest with a bad source is unavailable through nappFromLatest, with exactly \"Its source isn't a valid git URL\", and keeps no paths or sources. With no source tag, or a cloneable one, it stays valid. Making nip5dFromEvent drop the tag instead makes the test fail (guard proven by hand)."
    requirement: REG-02
    verification:
      - kind: unit
        ref: "backend/napplet_test.go#TestNIP5DInvalidSourceIsUnavailable"
        status: pass
    human_judgment: false
  - id: D10
    description: "On a live desktop build, installing, updating and trying napps and napplets from the default servers (relay.nostrapps.com, nostr.download) still works, and icons still load"
    verification: []
    human_judgment: true
    rationale: "Needs live Blossom servers and the Gio store window; deferred to end-of-phase verification"
  - id: D11
    description: "A Blossom server on the LAN or localhost, added in the settings window, serves installs. The same server named only in a manifest's server tag does not."
    verification: []
    human_judgment: true
    rationale: "Needs a self-hosted Blossom server and the settings window; deferred to end-of-phase verification"
  - id: D12
    description: "A NIP-5D napplet with a nostr: or relative source tag shows as unavailable in the store with the source reason"
    verification: []
    human_judgment: true
    rationale: "Needs a published invalid manifest and the desktop store; deferred to end-of-phase verification"

duration: 8min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 08: Guarded Blob Downloads and Per-Schema Source Validation Summary

**Manifest blobs now download through a public-only client: netguard on every dial and redirect hop, at most 3 redirects, none away from https, and a 64 MiB cap. Only the user's own Blossom servers may be on the LAN or localhost. `source` tags are checked by their manifest's schema: WEB-NAPPLET drops a bad one, and NIP-5D marks the manifest unavailable with "Its source isn't a valid git URL".**

## Performance

- **Duration:** about 8 min
- **Started:** 2026-10-05T16:22:48Z
- **Completed:** 2026-10-05T16:30Z
- **Tasks:** 2 (one tracer, one TDD)
- **Files modified:** 7 (1 created)

## Accomplishments

- **`downloadBlob` no longer uses `http.DefaultClient`.**
  - Each server attempt goes through `fetchBlobFrom`: a 20 s deadline, a Content-Length check and a `LimitReader` of `blobMaxBytes+1`, then the status and sha256 checks.
  - Any failure is logged at Debug and the next server is tried.
  - The last error is wrapped with `%w`, so callers and tests can see `netguard.ErrPrivateAddress`.
- **`blobClient`** dials only public addresses (`netguard.DialContext`) with `Proxy: nil`. The comment says why: a proxy would dial on the launcher's behalf and bypass the address check.
- **`trustedBlobClient`** has the same limits but the default dialer. It is used only for servers that appear, after normalization, in `BlossomServers()`: the user's list, or the defaults when none is set.
- **Source validators.**
  - `validWebNappletSource` follows the pinned WEB-NAPPLET text and keeps the "ignore a malformed source" rule.
  - `validGitSource` follows D-11: URL forms with a host, or scp-like `user@host:path`.
  - Both refuse a leading `-` on host or user, which blocks `ssh://-oProxyCommand=…`. They also refuse whitespace, control and format characters.
  - `nip5dFromEvent` now refuses a bad source with the catalogue phrase reserved by 05-05.
- **Tests.**
  - The containment rig and the trial test register their loopback servers as user Blossom servers, restored in cleanup.
  - The 05-06 update rig already did.

## Task Commits

1. **Task 1 (tracer): a manifest server on a private address serves nothing, and the user's own LAN server still works.** Commit `5bad9eb` (feat).
   - Tracer gate: the task's `<verify>` was re-run end to end before Task 2 and passed. Live checks are deferred to end-of-phase verification (D10, D11), so no interactive checkpoint was raised.
   - Guard proof: with `blobClient`'s dialer swapped for `net.Dialer`, `TestBlobDownloadRefusesPrivateHosts` fails ("a loopback manifest server served a blob"). The dialer was then restored.
2. **Task 2: `source` is checked by the rules of its manifest's schema.** Commit `1a8b8a2` (feat).
   - Guard proof: with the NIP-5D refusal disabled, `TestNIP5DInvalidSourceIsUnavailable` fails. The refusal was then restored.

## Files Created/Modified

- `backend/registry_install.go`: `blobMaxBytes`, `blobMaxRedirects`, `blobRedirect`, `blobClient`, `trustedBlobClient`, `userBlobServers`, `fetchBlobFrom`, and the per-server client choice in `downloadBlob`.
- `backend/registry_blob_test.go` (new): `TestBlobDownloadRefusesPrivateHosts` (with an `install` subtest), `TestBlobDownloadTrustsUserServers`, `TestBlobDownloadSizeCap`, `TestBlobDownloadRedirectLimits`.
- `backend/containment_test.go`:
  - The rig registers its server in `state.BlossomServers` and counts hits.
  - The install path no longer sets `n.Servers`.
  - `hostileNapp` keeps the tag (see deferred items).
- `backend/preview_test.go`: the trial test registers its server as a user server and restores the list.
- `backend/napplet.go`: `sourceChars`, `sourceURL`, `validWebNappletSource` and `validGitSource` replace `validSource`.
- `backend/napplet_nip5d.go`: a bad source returns `invalidManifest(reasonSource, …)`.
- `backend/napplet_test.go`: `TestValidSourceWebNapplet`, `TestValidSourceNIP5D`, `TestWebNappletMalformedSourceIgnored`, `TestNIP5DInvalidSourceIsUnavailable`.

## Decisions Made

See `key-decisions` in the frontmatter. In short:

- The cap is 64 MiB, in a var.
- The redirect limit counts redirects, so 3 are followed.
- The https check looks at the previous hop.
- The trusted client differs only in its dialer.
- Trust comes from the normalized user or default list, worked out on each call.
- Format characters are refused.
- The scp-like host may not contain `@`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Redirect count off by one compared with the plan**
- **Found during:** Task 1
- **Issue:** The RESEARCH example used `len(via) >= 3`, as `resourceClient` does. That refuses the 3rd redirect, so a 3-hop chain failed `TestBlobDownloadRedirectLimits`, but the plan says "at most 3 redirects".
- **Fix:** Changed it to `len(via) > blobMaxRedirects`. 3 redirects are followed and the 4th is refused.
- **Files modified:** backend/registry_install.go
- **Commit:** 5bad9eb

**2. [Rule 3 - Blocking] `hostileNapp` keeps the rig URL as a manifest server tag**
- **Found during:** Task 1
- **Issue:** The plan asked the containment rig to rely on user-server registration instead of manifest server tags. But `applyUpdate` uses only `newer.Servers` when the manifest names any, so the hostile-d update step tried the event's unreachable public server and failed, 10 s per case.
- **Fix:** The rig still registers its URL as a user Blossom server. `hostileNapp` also keeps the tag, so the D-20 path is still what makes it serve: a tag naming a server that is also a user server. The behaviour of `applyUpdate` is logged in `deferred-items.md`.
- **Files modified:** backend/containment_test.go
- **Commit:** 5bad9eb

**3. [Orchestrator rule] No separate RED commit for Task 2**
- **Found during:** Task 2
- **Issue:** Every commit has to compile and pass on its own. The new tests reference `validWebNappletSource` and `validGitSource`, which did not exist yet.
- **Fix:** RED was the compile failure before implementation, plus the guard check above. Tests and implementation are in one `feat` commit.

---

**Total deviations:** 3 (1 bug fix, 1 blocking adjustment, 1 process adjustment). **Impact:** none on scope.

## TDD Gate Compliance

There is no `test(05-08)` RED commit. Task 2's tests and implementation share commit `1a8b8a2`, because the orchestrator requires every commit to pass its package's tests. The guard check showed the NIP-5D test fails against the old drop-the-tag behaviour.

## Deferred Items

- `applyUpdate` fetches only from a manifest's own `server` tags when it has any, so it skips the user's servers. Install and trial use `n.BlossomServers`. This predates 05-08 and is logged in `.planning/phases/05-napplet-artifact-identity-and-storage-keying/deferred-items.md`.

## User-Visible Effects (for the PR)

- A manifest, or an author's kind 10063 list, that points at a private address (loopback, LAN, link-local or cloud metadata) no longer gets any request from the launcher.
- Self-hosted Blossom servers on a LAN or localhost must be added in the settings window, where they work as before.
- Users behind an HTTP proxy are not served through it. This matches NAP-RESOURCE.
- NIP-5D napplets with a non-git `source` (`nostr:`, relative, host-less) now show as unavailable.

## Known Stubs

None.

## Verification

- backend: `gofmt -l .` is empty, `go vet ./...` is clean, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes, and `go test -race -count=1 .` passes.
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` succeeds.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...` passes.
- Acceptance greps:
  - No `http.DefaultClient` outside comments in `registry_install.go`.
  - `netguard.DialContext` is present.
  - `blobMaxBytes` appears 8 times.
  - `func validSource(` is gone.
  - `validWebNappletSource` and `validGitSource` are defined once each.

## Deferred Human Checks

- D10: live installs, updates, trials and icons from the default servers.
- D11: a LAN or localhost Blossom server works when added in settings, and is refused when it is only in a manifest tag.
- D12: a NIP-5D napplet with a bad source shows as unavailable with the source reason.

## Self-Check: PASSED
