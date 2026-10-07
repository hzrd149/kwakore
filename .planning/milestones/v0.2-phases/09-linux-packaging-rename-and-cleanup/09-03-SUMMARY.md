---
phase: 09-linux-packaging-rename-and-cleanup
plan: 03
subsystem: infra
tags: [desktop-entry, xdg, linux, cli, launch-token, base64url, socket-activation, glib]

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 01
    provides: kwakore.socket owning $XDG_RUNTIME_DIR/kwakore/daemon.sock, so a CLI dial activates the daemon
  - phase: 08-runtime-and-signer-integration
    provides: napplet.launch with the headless error 1004 data {"reason":"session_unavailable"}, revalidated by the CLI
  - phase: 07-control-protocol
    provides: version 1 JSON-RPC, fixed CLI JSON errors on stderr, server UID check
provides:
  - "kwakore launch-token TOKEN: decodes the unpadded base64url of a full canonical address before dialing and sends the same napplet.launch {address} request as kwakore launch ADDRESS. --socket is refused, so it always reaches the standard user socket"
  - "backend/desktopentry, a leaf package with no build tag for the token codec (EncodeToken, DecodeToken, CanonicalAddress), pinned to backend.ParseCanonicalServiceAddress by an equivalence test"
  - "Linux writer desktopentry.Reconcile(dir, cli, entries), plus ApplicationsDir, FileName and Render: one kwakore-napplet-<sha256[:16] hex>.desktop per canonical address, Terminal=true"
affects: [09-04+ backend reconciliation on install/uninstall and at service startup, 09-10 docs (desktop entries and where the headless error appears), 09-11 live desktop-shell smoke of Terminal=true]

actuals:
  tokens: 11400
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Desktop entry Exec values: quote per the spec (\" ` $ \\ escaped, %% for %), then apply the string escape (every \\ doubled). Author text goes only into Name and Comment, collapsed to one line with control and format runes turned into spaces"
    - "Managed namespace: write and remove only names matching ^kwakore-napplet-[0-9a-f]{32}\\.desktop$; leave directories, near misses and other products' files alone"
    - "Client-side pre-dial validation that mirrors a backend parser lives in a leaf package, with an external test (package X_test) that imports the backend root and asserts both give the same verdict"

key-files:
  created:
    - backend/desktopentry/token.go
    - backend/desktopentry/token_test.go
    - backend/desktopentry/entry_linux.go
    - backend/desktopentry/entry_linux_test.go
  modified:
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - backend/go.mod
    - docs/control-protocol.md

key-decisions:
  - "The token is the unpadded base64url of the address bytes, with no prefix. Decoding checks the length (at most EncodedLen(4096)) and the [A-Za-z0-9_-] alphabet first, then decodes strictly and requires the re-encoding to match. Each address therefore has exactly one token"
  - "The CLI does not link the backend root. desktopentry.CanonicalAddress reproduces ParseCanonicalServiceAddress: a kind of exactly 35130, 35129 or 15129, lowercase hex that is a valid x-only point (btcec/v2 schnorr, as nostr.PubKeyFromHex uses), a d tag present for the addressable kinds and absent for 15129, no surrounding whitespace, and no embedded naddr. An equivalence test runs a hostile corpus plus 2000 seeded mutations through both"
  - "launch-token with --socket is an input error, never a dial (D-05). The server UID check and fixed JSON stderr are the existing run() path, unchanged"
  - "Entry files are 0600, in a directory created 0700 when missing. They are written through fileutil.WriteFileAtomic (CreateTemp 0600, fsync, rename), and a byte-identical entry is not rewritten"
  - "A repeated address keeps its first entry. An invalid address is skipped and reported through errors.Join, which names only the hashed file name, and the valid entries beside it are still written. A refused CLI path stops Reconcile before it writes or removes anything"
  - "A CLI path containing % is refused. The spec allows %%, but GLib resolves the Exec program before it expands %%, so GDesktopAppInfo returned NULL for such an entry (seen live with GLib on this host)"
  - "The entries carry no Icon or TryExec. The writer's input is only the address, title and description; icons can come later without changing the file name or Exec"

patterns-established:
  - "Test names for this surface: TestCLILaunchToken, TestEntryWrite, TestEntryReconcile, TestEntryReject, TestTokenCanonicalMatchesService, TestTokenRejectsMalformed"

requirements-completed: []
requirements-advanced: [LNXS-03]

coverage:
  - id: D1
    description: "A valid token sends exactly one napplet.launch request with the exact bytes of {address}, prints the result, and dials once"
    requirement: LNXS-03
    verification:
      - kind: integration
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLILaunchToken"
        status: pass
    human_judgment: false
  - id: D2
    description: "A headless launch prints exactly {\"error\":{\"code\":1004,\"message\":\"Unavailable\",\"data\":{\"reason\":\"session_unavailable\"}}} on stderr with empty stdout and a nonzero exit; the daemon's own message is not shown"
    requirement: LNXS-03
    verification:
      - kind: integration
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLILaunchToken"
        status: pass
    human_judgment: false
  - id: D3
    description: "Raw, padded, standard-alphabet, truncated, oversized, noncanonical, wrong-kind, empty, missing and extra-argument tokens, and --socket, never dial and print the fixed Invalid params error"
    verification:
      - kind: integration
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLILaunchToken"
        status: pass
      - kind: unit
        ref: "backend/desktopentry/token_test.go#TestTokenRejectsMalformed"
        status: pass
    human_judgment: false
  - id: D4
    description: "The CLI's pre-dial canonical check agrees with backend.ParseCanonicalServiceAddress on every corpus input and mutation"
    verification:
      - kind: unit
        ref: "backend/desktopentry/token_test.go#TestTokenCanonicalMatchesService"
        status: pass
    human_judgment: false
  - id: D5
    description: "Each canonical address yields one valid entry whose Exec parses to [CLI, launch-token, TOKEN]. Hostile newlines, percent signs, shell metacharacters, backslash escapes, bidi controls and Unicode in the title or description cannot add a key or a command, and neither the file name nor the file contains the raw address"
    requirement: LNXS-03
    verification:
      - kind: unit
        ref: "backend/desktopentry/entry_linux_test.go#TestEntryWrite (includes desktop-file-validate when installed)"
        status: pass
      - kind: manual
        ref: "GLib GDesktopAppInfo loaded and launched generated entries (Terminal flipped to false) with a recording CLI; argv matched exactly"
        status: pass
    human_judgment: false
  - id: D6
    description: "Repeated reconciliation leaves files untouched, a duplicate address yields one file, changed metadata rewrites only that entry, an uninstall removes only its own entry, and unrelated or near-miss files and directories survive"
    verification:
      - kind: unit
        ref: "backend/desktopentry/entry_linux_test.go#TestEntryReconcile"
        status: pass
    human_judgment: false
  - id: D7
    description: "A refused CLI path (relative, unclean, control, bidi, invalid UTF-8, percent, missing, directory, not executable) changes nothing; invalid addresses are reported without leaking the raw address while the valid entries are written"
    verification:
      - kind: unit
        ref: "backend/desktopentry/entry_linux_test.go#TestEntryReject"
        status: pass
    human_judgment: false
  - id: D8
    description: "Terminal=true actually shows the headless JSON error to a desktop-shell user"
    requirement: LNXS-03
    verification:
      - kind: e2e
        ref: "09-11 live desktop-shell smoke"
        status: pending
    human_judgment: true

duration: 12min
completed: 2026-10-07
status: complete
---

# Phase 9 Plan 03: Native Desktop Entry Launch Path Summary

**`kwakore launch-token TOKEN` turns one unpadded base64url token into the existing `napplet.launch {address}` request. The token is checked against the backend's canonical-address rules before the CLI dials, and the request always goes to the standard user socket so systemd can activate the daemon. The new `backend/desktopentry` package writes one owner-only `kwakore-napplet-<hash>.desktop` per installed address. Its `Exec` is the quoted CLI path plus that inert token, and it sets `Terminal=true` so the fixed JSON headless error stays visible.**

## Performance

- **Duration:** about 12 min
- **Started:** 2026-10-07T05:40Z
- **Completed:** 2026-10-07T05:52Z
- **Tasks:** 2
- **Files modified:** 8 (4 created)

## Accomplishments

- **CLI adapter.** `command()` maps `launch-token TOKEN` to `napplet.launch` with the decoded address. The method, params shape and protocol version are unchanged. `run()` refuses `launch-token` combined with `--socket`. Everything after that is the existing path: the `XDG_RUNTIME_DIR` checks, the server UID check, response revalidation (including the `session_unavailable` data, accepted only for `napplet.launch`), and one fixed JSON object on stderr.
- **Token codec.** `desktopentry.EncodeToken`, `DecodeToken` and `CanonicalAddress` live in a leaf package with no build tag, so the CLI's dependencies are still only `controlprotocol`, `desktopentry`, `fileutil`, btcec and `x/sys`. `TestTokenCanonicalMatchesService` keeps the copy of the canonical rules identical to `backend.ParseCanonicalServiceAddress`.
- **Entry writer.** `Reconcile` checks the CLI path, renders one entry per unique valid address, skips unchanged files, writes the rest atomically with mode 0600, and removes only stale files whose names match the exact managed shape. `ApplicationsDir` follows the base directory spec: it ignores a relative `XDG_DATA_HOME` and falls back to `~/.local/share/applications`.
- `docs/control-protocol.md` has a new row for `launch-token TOKEN` in the CLI table.

## Task Commits

1. **Task 1: Launch a canonical address from one inert token.** `2863fe8`
2. **Task 2: Write one safe managed entry per canonical address.** `6335171`

**Plan metadata:** recorded in the final docs commit.

## Files Created/Modified

- `backend/desktopentry/token.go` (new): the token codec and the canonical-address check
- `backend/desktopentry/token_test.go` (new): the equivalence test with the backend parser (external test package), and malformed-token cases
- `backend/desktopentry/entry_linux.go` (new): `ApplicationsDir`, `FileName`, `Render` and `Reconcile`
- `backend/desktopentry/entry_linux_test.go` (new): `TestEntryWrite`, `TestEntryReconcile` and `TestEntryReject`, with a strict key-file parser and an Exec argv parser written to the spec
- `backend/cmd/kwakore/main_linux.go`: the `launch-token` command, the `--socket` refusal and the usage string
- `backend/cmd/kwakore/main_linux_test.go`: `TestCLILaunchToken`
- `backend/go.mod`: `github.com/btcsuite/btcd/btcec/v2` moves from indirect to direct (`go mod tidy`; same version, `go.sum` unchanged)
- `docs/control-protocol.md`: the `launch-token` row

## Verification Output

`cd backend && go test -v ./cmd/kwakore -run '^TestCLILaunchToken$' -count=1`:

```
--- PASS: TestCLILaunchToken (0.32s)
    --- PASS: TestCLILaunchToken/dispatches_one_canonical_napplet.launch (0.00s)
    --- PASS: TestCLILaunchToken/headless_error_is_fixed_JSON_on_stderr (0.00s)
    --- PASS: TestCLILaunchToken/invalid_tokens_never_dial (0.05s)
    --- PASS: TestCLILaunchToken/server_uid_still_checked (0.00s)
ok  	verdana/backend/cmd/kwakore	0.320s
```

`cd backend && go test -v ./desktopentry -run '^TestEntry(Write|Reconcile|Reject)$' -count=1`:

```
--- PASS: TestEntryWrite (0.01s)
--- PASS: TestEntryReconcile (0.00s)
--- PASS: TestEntryReject (0.00s)
ok  	verdana/backend/desktopentry	0.016s
```

`desktop-file-validate` is installed on this host, so `TestEntryWrite` ran it on the hostile entry, the empty-metadata root napplet entry and the capped long-title entry. All three passed with no output.

Manual GLib check: I generated entries for a CLI path containing a space, `$HOME`, both quote kinds, a backtick, a backslash, `;&|<>~*?#()` and `é`, and for a plain path. I loaded each copy (with `Terminal=false`, so no terminal window opened) through `Gio.DesktopAppInfo` and launched it with a recording CLI. The argv matched `[CLI, launch-token, TOKEN]` byte for byte in both cases, and `get_name()` returned the literal `\n` text, not a newline. In the same check, a path containing `50%` loaded as NULL, which is why `%` is now refused.

Manual daemon round trip: I ran a foreground `kwakore-daemon` with an offline config, data under the scratchpad and a temporary runtime dir under `$XDG_RUNTIME_DIR`, with `DISPLAY` and `WAYLAND_DISPLAY` unset. `kwakore launch-token <token>` and `kwakore launch <address>` for the same uninstalled canonical address both returned `{"error":{"code":1002,"message":"Not found"}}`, and `status` succeeded. The daemon exited 0 on SIGTERM, and the temporary directories were removed.

Regression: `cd backend && go test ./...` passed in full on the second run. The first run hit one `TestNapDeliversDMsAsSigned` timeout in the root package, which this plan does not touch; it passed 5/5 when run alone. Desktop CI also passed: `go build` of both child binaries, then `go test -tags novulkan ./...`. `go vet` and `gofmt -l` are clean for the touched packages.

## Decisions Made

See `key-decisions` in the frontmatter. The most consequential:
- **Mirror the canonical parser instead of linking it.** The CLI stays a small client adapter. The equivalence test makes any drift from `ParseCanonicalServiceAddress` a test failure, not a gap where a desktop entry carries an address the daemon refuses.
- **Refuse `%` in the CLI path.** GLib's loader is stricter than the spec here. Failing loudly at reconcile time is better than writing entries every GLib-based shell silently ignores.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Token codec placed in `backend/desktopentry/token.go` in Task 1**
- **Found during:** Task 1
- **Issue:** The plan asked the CLI to use `ParseCanonicalServiceAddress` validation. That function lives in the backend root package, and importing it would link the relay pool, stores and their `init()` functions into the control CLI. The writer in Task 2 needs the same encoder.
- **Fix:** A leaf package holds the codec and a faithful copy of the canonical rules, with an equivalence test against the real parser. `btcec/v2` became a direct dependency for the curve-point check.
- **Files modified:** backend/desktopentry/token.go, backend/desktopentry/token_test.go, backend/go.mod (not in Task 1's file list)
- **Commit:** 2863fe8

**2. [Rule 2 - Critical] Documented `launch-token` in the CLI table**
- **Found during:** Task 1
- **Issue:** `docs/control-protocol.md` lists every CLI command. Leaving the new one out would make the reference wrong.
- **Fix:** Added one table row. The protocol text is unchanged.
- **Files modified:** docs/control-protocol.md
- **Commit:** 2863fe8

**3. [Rule 1 - Bug] Refuse a CLI path containing `%`**
- **Found during:** Task 2 (manual GLib check)
- **Issue:** A spec-correct `%%` in the quoted Exec program made `GDesktopAppInfo` return NULL, so the entry would never show up.
- **Fix:** `checkCLIText` refuses `%` (`ErrInvalidCLI`, nothing written). `TestEntryReject` covers it.
- **Files modified:** backend/desktopentry/entry_linux.go, backend/desktopentry/entry_linux_test.go
- **Commit:** 6335171

---

**Total deviations:** 3 auto-fixed (Rule 1, Rule 2, Rule 3)
**Impact on plan:** All three were needed for correctness, accurate docs, or to keep the CLI a lean adapter. There is no scope creep.

## Issues Encountered

- The standard-alphabet and trailing-bit malformed tokens needed care to construct: an address whose length is a multiple of 3 has no spare bits. The test now uses a 73-byte address.
- The first foreground daemon run failed with "XDG_RUNTIME_DIR path is too long for a Unix socket" because the scratchpad path is too long. The rerun used a short temporary directory under the real `$XDG_RUNTIME_DIR`, removed by a trap.

## Environment-Dependent Verification

- **Terminal=true is not yet proven in a live desktop shell.** The tests prove the CLI writes the fixed JSON to stderr. Whether a user sees it depends on the shell's terminal choice (for example `xdg-terminal-exec` or `gnome-terminal`), and many terminals close as soon as the command exits, so the error may only flash on screen. A terminal window also opens briefly on every successful launch, for up to the daemon's 12-second launch deadline. 09-11 must check this live, as the research resolution requires. If the result is unacceptable, the entry or the docs must change before acceptance; options include a journal-capture wrapper or a CLI pause on a TTY error.
- **Systemd activation through `launch-token`** was not run under the user manager here. `launch-token` uses the exact socket path and dial code that `status` uses, and the 09-01 smoke proves that path activates the daemon. The 09-11 full smoke covers launching from an entry.
- **Headless `session_unavailable` against a real installed napplet** was not run, because installing one needs relays. A stub server covers it, and the Phase 8 tests cover the daemon side.
- **The writer is not wired up yet.** Nothing calls `Reconcile` at service startup or after install and uninstall. That integration is listed in 09-PATTERNS (`backend/app_shortcuts.go`, `backend/backend.go`, `backend/registry_install.go`) and belongs to a later plan.

## Known Stubs

None. `Reconcile` is complete but not called from the service yet. That is the intended scope split (see above), not a stub.

## Threat Flags

None. T-09-03-1 is mitigated: hashed file names, sanitized Name and Comment, an inert token, a quoted and checked Exec, and stale cleanup only inside the managed namespace. T-09-03-2 is mitigated: only the fixed JSON error data reaches the terminal, and the daemon's message is replaced by fixed text, as `TestCLILaunchToken` asserts.

## Requirements Status

LNXS-03 is advanced but not checked off. It still needs backend reconciliation on install and uninstall, the docs (09-10), and the live entry-launch smoke (09-11).

## Next Phase Readiness

- The backend integration can call `desktopentry.ApplicationsDir()` and then `desktopentry.Reconcile(dir, cliPath, entries)` with one `Entry{Address, Title, Description}` per installed canonical napplet address. Run it at startup and after each committed install or uninstall. `cliPath` should be the installed `~/.local/bin/kwakore` or the bundle's `current/kwakore`, and it must be absolute with no `%`.
- 09-10 should document where the headless error appears (the terminal opened by `Terminal=true`) and the `launch-token` command.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

All 6 key source files exist; task commits 2863fe8 and 6335171 are present in git history.
