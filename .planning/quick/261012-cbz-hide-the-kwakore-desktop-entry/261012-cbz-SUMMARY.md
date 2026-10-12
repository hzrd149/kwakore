---
quick_id: 261012-cbz
status: complete
commit: 7653cfb
---

# Quick Task 261012-cbz summary

- `NoDisplay=true` added to the runtime-written `org.kwakore.Search.desktop` (backend/linuxhost/gnome_search_linux.go) and the Nix-packaged copy (nix/package.nix); the linuxhost test asserts it.
- docs/service.md explains that GNOME Shell ignores a provider whose entry is hidden; installed napplets stay searchable via their own entries. The search provider code and `gnome_search` setting are kept (user decision).
- docs/control-protocol.md: `settings.set` row now lists the string value for `sync_id`.
- Verified: backend go vet (linuxhost), go test ./... all ok, identity check pass.
