# Phase 05 Deferred Items

Out-of-scope discoveries logged during execution. Not fixed by the plan that found them.

## From 05-08

- **applyUpdate skips the user's own Blossom servers when a manifest names any** (`backend/registry_updates.go`, `applyUpdate`). It does `servers := newer.Servers` and falls back to `newer.BlossomServers(ctx)` only when the manifest has no `server` tag. Install and trial use `n.BlossomServers(ctx)`, which lists the user's servers first, then the manifest's, then the author's kind 10063 list. With the 05-08 guard in place, an update whose manifest names only public servers never asks the user's LAN or localhost server, even if that is where the files are. An install of the same version would ask it. This is behaviour that predates 05-08, and it is not a security problem: the guard still applies to every server that is not the user's. The fix is probably to use `newer.BlossomServers(ctx)` everywhere. Found while moving `containment_test.go` onto user-server registration: the hostile-d update step needs `n.Servers` set to the rig URL because of this.
