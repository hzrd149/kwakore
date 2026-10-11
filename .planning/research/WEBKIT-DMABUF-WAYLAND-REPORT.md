# Report: napplet windows flash and close on Hyprland (WebKitGTK DMABUF)

Date: 2026-10-11. kwakore rev `4ea6eda`, as packaged by kwak-os (`nix/package.nix`,
WebKitGTK 2.54.1, GTK 3.24.52, nixpkgs 26.05).

## Symptom

On a physical kwakOS install (Hyprland 0.56, Wayland), launching an installed
napplet opens a window that flashes and closes immediately. `kwak launch` reports
`opened`, and the daemon log shows only:

```
DBG bridge rpc call method=nap.boot
INF napp window closed
```

No error, no exit status, no core dump.

## Root cause

The child (`kwaklet`) gets through startup and engine hardening, then GTK dies
inside `w.Run()` on a Wayland protocol error caused by WebKitGTK's DMABUF renderer.
GTK treats it as fatal and exits the process with status 1:

```
INF window program started kind=napplet ...
Overriding existing handler for signal 10. Set JSC_SIGNAL_FOR_GC ...
INF webkit hardening applied link_preconnect=false media_stream=false webrtc=false
Gdk-Message: 10:49:06.416: Error 71 (Protocol error) dispatching to Wayland display.
=== exit 1
```

This was captured by temporarily replacing the sibling `kwaklet` with a wrapper
script that redirected its stderr to a file.

- Reproduced on an NVIDIA RTX 2070 Super with **both** `nouveau` and the
  proprietary NVIDIA driver (595.71.05, open kernel modules). The driver is not
  the fix.
- With `WEBKIT_DISABLE_DMABUF_RENDERER=1` in the daemon's environment (the child
  inherits it), the same napplet opens, reaches `nap.loaded`, uses `nap.gamepad`,
  stays open, and exits 0 on a normal close. Verified on the real machine.
- On an X11 session (Ubuntu 22.04 host, same Nix-built package) the bug does not
  appear. This is Wayland-specific.

Ruled out:
- **Packaging/RUNPATH:** the identical store path works under X11.
- **Hardening:** `LinkPreconnect` exists in 2.54.1, and the hardening log line shows
  every channel off. A hardening failure would also send `windowFailed`
  `engine-hardening`, which didn't happen.
- **Display env:** a missing `WAYLAND_DISPLAY`/`DISPLAY` returns `session_unavailable`
  from `OpenWindowContext` before any window appears.

## Why it went unnoticed

1. **Child stderr is discarded.** `backend/linuxhost/host_linux.go:217`
   `cmd.Stderr = io.Discard`. Every GTK/WebKit fatal message is lost, and the exit
   status is never logged, so the daemon reports a normal "napp window closed".
2. **The WebKit tests mask it.** `desktop/child/webkit_test.go:72-73` always sets
   `WEBKIT_DISABLE_COMPOSITING_MODE=1` and `WEBKIT_DISABLE_DMABUF_RENDERER=1` (added
   for xvfb, per `.planning/milestones/v0.1-phases/04-frame-sandbox-lifecycle/04-RESEARCH.md`).
   The real-engine smoke never runs with the renderer that production uses.
3. The full smoke (`scripts/smoke-linux-service.sh --full`) needs `DISPLAY` and has
   so far only run on X11.

## Current workaround (downstream)

kwak-os sets it on the user unit, in `modules/napplets.nix`:

```nix
systemd.user.services.kwakore.environment.WEBKIT_DISABLE_DMABUF_RENDERER = "1";
```

Non-Nix installs (`scripts/install.sh`, `packaging/systemd/user/kwakore.service`)
have no workaround and will hit the same bug on affected Wayland setups.

## Recommended improvements (in priority order)

### 1. Log child stderr and exit status (diagnosability)
In `OpenWindowContext` (`backend/linuxhost/host_linux.go`):
- Replace `io.Discard` with `cmd.StderrPipe()` and a goroutine that logs each
  line through the package's zerolog logger, at Warn or Debug, with
  `instance=<spec.Instance>`. Cap line length (e.g. 2 KiB) and lines per window
  (e.g. 200, then one "further output suppressed" line) so a noisy engine
  can't flood the journal. Child zerolog lines are already structured, so
  consider passing them through at Debug and raising non-zerolog lines (GTK/GLib
  `Gdk-Message`, `GLib-CRITICAL`, WebKit) to Warn.
- When `cmd.Wait()` returns a non-zero status or a signal and the daemon did not
  ask the window to close, log it ("window program exited unexpectedly",
  code/signal) beside "napp window closed". Consider exposing it as a recent
  error in `kwak diagnostics`.
- Keep the environment allowlist and the D-10 comment as they are; this changes
  only stderr handling.
- Test in `backend/linuxhost`: a fake child that prints to stderr and exits 1
  after reporting ready. Assert that the line and the exit status are logged,
  and that the caps hold.

### 2. Fix the crash for every install, not only kwak-os
Options, roughly from least to most invasive:
- **a.** In the child, before GTK init (`desktop/child`, before `webview.New`), set
  `WEBKIT_DISABLE_DMABUF_RENDERER=1` when `WAYLAND_DISPLAY` is set and the user
  hasn't set it, with `os.Setenv` like `harden_windows.go` does for WebView2. This
  is self-contained and covers bundle, Nix and any other install. The cost is
  losing DMABUF zero-copy rendering on Wayland, which matters for heavy WebGL
  napplets (games), so measure frame rate on a three.js napplet before and after.
- **b.** Set it in `packaging/systemd/user/kwakore.service` (`Environment=`). The
  NixOS module renders this template, so it reaches Nix too. Simpler, but it
  affects everything the daemon spawns.
- **c.** Detect the failure and retry: if the child exits non-zero soon after
  `ready`, with the Error 71 signature on stderr (needs item 1), relaunch it once
  with the variable set and remember that per session. Most robust for
  performance, but the most complex.

Recommendation: **a** now, guarded by an opt-out (e.g. `KWAKORE_WEBKIT_DMABUF=1`
keeps DMABUF on). Revisit **c** if WebGL performance turns out to matter.
Once it's upstream, kwak-os can drop its unit override.

### 3. Close the test gap
- Make the forced `WEBKIT_DISABLE_DMABUF_RENDERER`/`COMPOSITING_MODE` in
  `desktop/child/webkit_test.go` apply only under xvfb (no `WAYLAND_DISPLAY`), or
  add a variant that runs on a real Wayland compositor without them.
- Add a Wayland stage to `scripts/smoke-linux-service.sh --full`, e.g. under a
  headless compositor (`weston --backend=headless` or Hyprland headless), and
  assert that the window survives a few seconds after `opened`. With item 1 in
  place, also assert that the child logged no `Error 71`.

### 4. Minor: launch should surface early death
`kwak launch` returns `opened` as soon as the child reports ready, and a death
100 ms later is silent. Consider a short grace period, or a follow-up event or
notice in the TUI ("window closed unexpectedly: exit 1"), so users see a failure
instead of a flash.

## Reproduction recipe (no code changes)

On any Hyprland/Wayland session with an affected GPU:

```sh
P=$(dirname "$(readlink -f "$(command -v kwakore)")")   # package bin dir
D=$(mktemp -d /tmp/kwk.XXXX); mkdir -p $D/bin $D/home $D/cfg $D/data; mkdir -m700 $D/run
cp $P/.kwakore-wrapped $P/kwak $P/libwebview.so $D/bin/
printf '#!/bin/sh\n%s/kwaklet "$@" 2>>%s/child.err\necho "exit $?" >>%s/child.err\n' "$P" "$D" "$D" > $D/bin/kwaklet
chmod 755 $D/bin/*
ln -s "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY" $D/run/
HOME=$D/home XDG_CONFIG_HOME=$D/cfg XDG_DATA_HOME=$D/data XDG_RUNTIME_DIR=$D/run \
  GSETTINGS_SCHEMA_DIR=<from the kwakore wrapper> $D/bin/.kwakore-wrapped &
# then, with the same env: kwak install ADDR; kwak launch ADDR; cat $D/child.err
```

Keep the runtime dir path short: Unix socket paths are limited to 108 bytes.
Test napplet: `35129:266815e0c9210dfa324c6cba3573b14bee49da4209a9456f9484e5106cd408a5:n-143146b0d6f`
(Supersonic RC Revive, WebGL + gamepad).
