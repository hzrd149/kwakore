# Phase 05 Deferred Items

Out-of-scope discoveries logged during execution. Not fixed by the plan that found them.

## From 05-08

- **applyUpdate skips the user's own Blossom servers when a manifest names any** (`backend/registry_updates.go`, `applyUpdate`). It does `servers := newer.Servers` and falls back to `newer.BlossomServers(ctx)` only when the manifest has no `server` tag. Install and trial use `n.BlossomServers(ctx)`, which lists the user's servers first, then the manifest's, then the author's kind 10063 list. With the 05-08 guard in place, an update whose manifest names only public servers never asks the user's LAN or localhost server, even if that is where the files are. An install of the same version would ask it. This is behaviour that predates 05-08, and it is not a security problem: the guard still applies to every server that is not the user's. The fix is probably to use `newer.BlossomServers(ctx)` everywhere. Found while moving `containment_test.go` onto user-server registration: the hostile-d update step needs `n.Servers` set to the rig URL because of this.
- **Status:** acknowledged
- **Deferred:** at v0.1 milestone close, 2026-10-06

## From 05-11

- **NAPPLETS.md still has pre-Phase-5 text outside this phase's scope.** The "How a napplet runs" diagram and step 3 still describe an activation script that posts `shell.ready`, and the Domains table still has a `shell` row (`shell.ready` -> `shell.init`). Phase 1 removed both (CONFORMANCE A18: the host page starts each session with `nap.start`). The "Updating the shim" section still names `@napplet/shim` 0.29.2, while SHIM-01 pins 0.30.0. The "Not implemented yet" line still lists `notify`, although `notify.controls` is pushed (DEC-2) and NAP-NOTIFY has routes. 05-11 only corrected the Phase 5 parts (storage, config, manifest selection, source rules, blob downloads, trials, requires warning, updates and uninstall). A docs pass (Phase 8 SPEC-02 or a quick task) should fix the rest.
- **Status:** acknowledged
- **Deferred:** at v0.1 milestone close, 2026-10-06
