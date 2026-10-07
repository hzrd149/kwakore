---
phase: 09-linux-packaging-rename-and-cleanup
plan: 08
subsystem: nix-packaging
tags: [nix, nixos, systemd-user, socket-activation, flake, packaging, d-02, d-04, d-08, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 01
    provides: packaging/systemd/user/kwakore.{socket,service} and inherited-listener validation
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: the four-file bundle layout (kwakore-daemon, kwakore, napplet, libwebview.so)
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 04
    provides: native entries naming the kwakore CLI found beside the resolved daemon
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 06
    provides: kwakore/backend and kwakore/desktop module paths, tidied desktop/go.mod
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 07
    provides: KWAKORE_NAPP_FORMAT=napplet child handoff
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 24
    provides: scripts/check-product-identity.sh full mode
provides:
  - "nix/package.nix: kwakore-daemon, kwakore, napplet and libwebview.so as real files in one bin directory, with interpreter/RUNPATH patching and an install check that runs the wrapped daemon and asks it for status and diagnostics through the installed CLI"
  - "flake.nix: packages.kwakore/default, overlays.default.kwakore, nixosModules.kwakore/default, checks.kwakore; no Verdana outputs"
  - "nix/module.nix: programs.kwakore.{enable,package,users,settings} rendering the generic user units at eval time with only the @BINDIR@ and kill substitutions"
  - "nix/module-test.nix: eval test (prints true) comparing rendered units with the templates and checking settings and secret rejection"
  - "linuxhost.checkProgram accepts sticky shared directories (the Nix store) above the child"
affects: [09-09 CI (nix build and module eval jobs), 09-10 docs (NixOS options, effective config path, kill finding), 09-11 end-to-end smoke]

actuals:
  tokens: 12600   # chars/4 over the realized diff f6d4a12..feed3f4 (50.4k chars incl. headers)
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Two buildGoModule derivations (backend vendored, desktop proxyVendor) combined by a stdenv derivation that copies real files into one bin directory, so os.Executable sibling lookup works"
    - "NixOS units parsed from the generic packaging templates at evaluation time instead of restated in Nix"
    - "Declarative service settings as a store-backed XDG_CONFIG_HOME validated by the packaged daemon at build time"

key-files:
  created:
    - nix/module-test.nix
  modified:
    - nix/package.nix
    - nix/module.nix
    - flake.nix
    - backend/linuxhost/host_linux.go
    - backend/linuxhost/host_linux_test.go
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md
  deleted:
    - nix/verdana.svg

key-decisions:
  - "The daemon is wrapped (makeWrapper) only to add GSETTINGS_SCHEMA_DIR for gtk3 and gsettings-desktop-schemas; the real binary sits in the same bin directory, argv[0] and the pid are kept, and no LD_LIBRARY_PATH, PATH or XDG_DATA_DIRS is set. GIO_EXTRA_MODULES/glib-networking from the old launcher wrapper was dropped: the napplet CSP has connect-src 'none', so WebKit never needs TLS"
  - "The NixOS units are rendered from packaging/systemd/user at eval time with exactly two substitutions: @BINDIR@ -> ${package}/bin and a leading kill in Exec* -> ${coreutils}/bin/kill (nixpkgs' systemd only searches its own bin/)"
  - "ConditionUser=|<user> per configured user on both units; an empty users list fails an assertion; enableDefaultPath = false so the daemon keeps the user manager's PATH like the generic unit"
  - "Settings: unset leaves the user's XDG_CONFIG_HOME alone; set gives the service a store tree with only kwakore/config.json, validated at build time by kwakore-daemon validate. Unknown and secret-like keys (same fragments as serviceconfig) fail assertions without echoing values"
  - "checkProgram now accepts a sticky shared-writable directory above the child (Rule 1): /nix/store is drwxrwxr-t, so the old check refused every Nix-installed child"
  - "LNXS-02 not marked complete: 09-10 and 09-11 also list it (NixOS docs and the CI/eval job)"

patterns-established:
  - "Nix install checks run the installed daemon through its wrapper and assert no native_entries diagnostic, proving the CLI resolves beside the real executable"

requirements-completed: []  # LNXS-02 also needs 09-10 (NixOS docs) and 09-11 (CI eval/build); LNXS-01, SRVC-01, NAME-01 have pending plans

coverage:
  - id: D1
    description: "Nix package with daemon, CLI, child and library as siblings, checked child and WebKit resolution"
    requirement: LNXS-02
    verification:
      - kind: integration
        ref: "nix build .#packages.x86_64-linux.kwakore --no-link (installCheckPhase)"
        status: pass
  - id: D2
    description: "NixOS module rendering the generic per-user socket and service for configured users, with validated non-secret settings"
    requirement: LNXS-02
    verification:
      - kind: unit
        ref: "nix eval --impure --json --file nix/module-test.nix"
        status: pass
      - kind: e2e
        ref: "scratch NixOS VM test (two configured users, one unconfigured; not committed)"
        status: pass
  - id: D3
    description: "Child path check accepts the sticky Nix store"
    verification:
      - kind: unit
        ref: "backend/linuxhost/host_linux_test.go#TestLinuxHostAcceptsStickySharedDirectory"
        status: pass

duration: 85min
completed: 2026-10-07
---

# Phase 9 Plan 08: Nix Package and NixOS User Service Summary

**Four-file kwakore Nix package (daemon behind a schema-only wrapper, CLI, patched napplet child, patched libwebview.so) plus a `programs.kwakore` NixOS module that renders the generic systemd user units at eval time, with per-user ConditionUser, store-backed validated settings and secret rejection.**

## Performance

- **Duration:** about 85 min, including one stop for a permission denial and the coordinator's resume
- **Completed:** 2026-10-07
- **Tasks:** 2 of 2
- **Files modified:** 7 (plus one deletion)

## Accomplishments

- `nix/package.nix` builds `backend/cmd/kwakore-daemon` and `backend/cmd/kwakore` from the backend module (normal vendoring, `sha256-VU9WT75TONAeNPACPxjwzcwKMwVAcNsDQHyapUDCL4A=`) and the `desktop/child` napplet plus the regenerated pinned `libwebview.so` from the desktop module (`proxyVendor`, `sha256-fOcQABl8t30v8jevN9bQRSK1qiX2MpWB5wwXrnoBeiA=`). It copies all four as real files into one `$out/bin`, patches the child's interpreter and both RUNPATHs (patchELF fixup off so the dlopen paths survive), and wraps only the daemon.
- The install check asserts the exact `bin/` listing and that the package holds only `bin/`. It also asserts regular files, a same-directory wrapper target, no `LD_LIBRARY_PATH`, compiled schemas, clean `ldd`, `libwebkit2gtk-4.1` from the store, a store interpreter, the WebKit RUNPATH, and the no-`WEBVIEW_PATH` child refusal. Then it starts the wrapped daemon in the sandbox and queries `kwakore status` (version matches) and `kwakore diagnostics` (no `native_entries`) over its socket.
- `flake.nix` exposes only kwakore outputs. The Gio wrapper, the manager desktop item, the `com.verdana.Verdana` icon, the `napp` child and `VERDANA_EXECUTABLE` are gone.
- `nix/module.nix` provides `programs.kwakore.enable`, `package`, `users` and `settings`. It installs the package system-wide and renders `kwakore.socket` (wanted by `sockets.target`) and `kwakore.service` from the generic templates. Both units get one `ConditionUser=|name` line per user. There is no autostart item and no `RuntimeDirectory`.
- `nix/module-test.nix` evaluates the module on the nixpkgs pinned in `flake.lock` and covers:
  - the rendered unit lines against the template lines (and, separately, the path and modes)
  - wantedBy, user-only units and no condition for an unconfigured user
  - the settings JSON and the store-backed `XDG_CONFIG_HOME`
  - nine rejected configurations, including secret-like fields, with the secret values never echoed
  - a disabled module and the real default package
- `linuxhost.checkProgram` now accepts the sticky `/nix/store` above the child, with a regression test.

## Task Commits

1. **Rule 1 fix: sticky shared directories in checkProgram:** `92e0caf` (fix). This commit also carries the staged `nix/verdana.svg` deletion, see Deviations.
2. **Task 1: four-piece Nix package and kwakore flake outputs:** `0d7941d` (feat)
3. **Task 2: NixOS module user units, settings and eval test:** `feed3f4` (feat)

**Plan metadata:** committed together with this SUMMARY (docs).

## Verification

| Command | Result |
|---|---|
| `nix build .#packages.x86_64-linux.kwakore --no-link` | pass on the committed tree (`/nix/store/b0mvqd59...-kwakore-unstable-feed3f4-dirty`), install check included |
| `nix eval --impure --json --file nix/module-test.nix` | `true`. Sanity check: with `SocketMode = "0666"` injected, or `enableDefaultPath` back on, it fails with the matching message; both edits were reverted |
| `nix flake check` | all checks passed (x86_64); `--all-systems --no-build` also evaluates every aarch64 output |
| Store-path check of the built package (temporary test file, removed) | `checkProgram($out/bin/napplet)` passes; `cliBeside($out/bin/kwakore-daemon, $out/bin/.kwakore-daemon-wrapped)` returns `$out/bin/kwakore` |
| Config tree build | valid settings give a 0444 `kwakore/config.json`; `wss://relay.example.com/a b` gets past the Nix pattern checks but is rejected at build time by `kwakore-daemon validate` |
| Scratch NixOS VM test (alice and bob configured, carol not) | **pass** (test script finished in 114.5 s): sockets active with the service inactive; `/run/user/<uid>/kwakore` 700 and `daemon.sock` 600, each owned by its user; carol `ConditionResult=no` with no runtime dir; `kwakore status` activates a separate daemon per user (different MainPIDs and owners); `kwakore settings get` shows the Nix relays; `systemctl --user reload` logs "configuration reloaded" (the coreutils kill works); restart, stop and socket reactivation work |
| `cd backend && go vet ./... && go test -count=1 ./...` | pass |
| desktop: `go build -o child/napplet ./child && go vet ./... && go test -count=1 -tags novulkan ./...` | pass |
| `bash scripts/check-product-identity.sh` (full) | still fails overall (124 lines) as expected, but **no `nix/` or `flake.nix` hits**; the remaining lines are 09-09/09-10 files (README.md:119 mentions the old flake text) |

**Honest limits:**
- The VM test ran with **TCG emulation, not KVM**. The host has `/dev/kvm` and `system-features` lists `kvm`, but QEMU inside the build sandbox reported `failed to initialize kvm: Permission denied`, so it was slower but otherwise a real VM.
- The VM test was a scratch file and is not committed; the plan does not include one.
- No napplet was launched from the Nix package on a display: that needs an installed napplet and belongs to 09-11's smoke. Child adjacency is proven structurally and by the store-path `checkProgram` call.
- aarch64 was only evaluated, not built (no aarch64 builder).

## Decisions Made

See `key-decisions` in the frontmatter. The two less obvious ones:

- **Wrapper scope.** Napplet pages have `connect-src 'none'`, so WebKit never opens TLS and glib-networking is unnecessary. The remaining risk is a GTK abort when a widget such as the file chooser needs a schema missing from a minimal session. `GSETTINGS_SCHEMA_DIR` (multi-directory, checked with `gsettings list-schemas`) covers that without changing `XDG_DATA_DIRS` for xdg-open or players.
- **Settings model (RESEARCH A1).** This was confirmed live in the VM: the daemon reports `config: /nix/store/...-kwakore-config/kwakore/config.json` and serves the declared relays. One side effect: the child inherits that `XDG_CONFIG_HOME`, so GTK and fontconfig user files under `~/.config` do not apply in napplet windows while settings are declared. The option description documents this and it is logged for 09-10.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The Nix-installed child could never be launched**
- **Found during:** Task 1
- **Issue:** `checkProgram` refused any group- or world-writable path component, and `/nix/store` is `drwxrwxr-t root nixbld`. Every launch from the Nix package would have failed with `ErrWindowProgramUnavailable`, which breaks the plan's truth "Nix daemon executable resolves sibling napplet and libwebview.so".
- **Fix:** Accept a sticky shared-writable directory that is not the last component. Others can add entries there but cannot rename or remove the root- or user-owned entry below it, which still has to pass the owner check.
- **Files modified:** `backend/linuxhost/host_linux.go`, `backend/linuxhost/host_linux_test.go`
- **Commit:** `92e0caf`

**2. [Rule 3 - Blocking] The generic `ExecReload=kill` does not resolve on NixOS**
- **Found during:** Task 2
- **Issue:** Nixpkgs patches systemd's compiled-in search path to systemd's own `bin/`, which has no `kill`. The 09-01 SUMMARY's statement that the bare `kill` "also works on NixOS" is wrong.
- **Fix:** The renderer replaces a leading `kill` in Exec* values with `${coreutils}/bin/kill`. The eval test pins that this and `@BINDIR@` are the only differences from the template, and the VM test confirmed the reload works.
- **Files modified:** `nix/module.nix`, `nix/module-test.nix`
- **Commit:** `feed3f4`

**3. [Rule 2 - Missing critical] NixOS's default service PATH**
- **Found during:** Task 2
- **Issue:** NixOS user services get a pinned PATH (coreutils, findutils, grep, sed, systemd). The daemon runs xdg-open, xclip or wl-copy, notify-send, mpv and vlc from PATH, so behavior would differ from the generic unit.
- **Fix:** `enableDefaultPath = false`; the test asserts there is no `PATH=` environment line.
- **Commit:** `feed3f4`

### Process notes

- The staged `nix/verdana.svg` deletion was swept into the Go fix commit `92e0caf`: `git commit` without paths took the whole index. The deletion is correct and belongs to this plan. History was not rewritten, because the coordinator ruled out reset/checkout operations.
- One earlier command was denied by the permission classifier. It chained a `git checkout -- linuxhost/host_linux.go`, which would have discarded the uncommitted fix. Nothing ran, and work resumed on the coordinator's instructions.
- The vendor fixed-output derivations are named after the version. The sandbox resolver intermittently answered `server misbehaving` for `storage.googleapis.com`. For the `unstable`-named service vendor tree used by the VM and config-tree builds, the identical NAR (same hash) was added with `nix-store --add-fixed` instead of refetching. This is logged for 09-09's CI.

## Issues Encountered

- `builtins.match` rejects `\[` in Nix's ERE, so the template parser uses `[[]`/`[]]`.
- A regex may not carry store-path string context, so the test discards the context before comparing.

## Known Stubs

None.

## Threat Flags

None. The module adds no network endpoint. The settings path carries only public `serviceconfig` fields (T-09-08-01). The socket keeps `%t/kwakore/daemon.sock`, 0600/0700 and ConditionUser (T-09-08-02). The child keeps its interpreter, RUNPATH, ldd and WebKit checks (T-09-08-03).

## Requirements

None marked complete:

- **LNXS-02** is substantively delivered by this plan (package, module, eval test, live VM run), but `09-10-PLAN.md` (NixOS docs) and `09-11-PLAN.md` (CI module eval and build) also list it. Per the rule that a requirement is complete only when every contributing plan is done, it stays pending.
- **LNXS-01, SRVC-01 and NAME-01** have pending plans (09-09, 09-10, 09-11).

## Next Phase Readiness

- **09-09:** add jobs for `nix build .#packages.x86_64-linux.kwakore` and `nix eval --impure --json --file nix/module-test.nix`. Expect occasional module-proxy DNS retries.
- **09-10:** document `programs.kwakore` (users, settings, effective config path and its `XDG_CONFIG_HOME` side effect), the absolute `kill` on NixOS, and native entries holding store paths until the next daemon start. See the 09-08 entry in `deferred-items.md`.
- **09-11:** the scratch VM test sequence (two users, activation, reload, restart, stop) can serve as a template for a committed NixOS check.

## Self-Check: PASSED

- FOUND: nix/package.nix, nix/module.nix, nix/module-test.nix, flake.nix; nix/verdana.svg absent
- FOUND: commits 92e0caf, 0d7941d, feed3f4
