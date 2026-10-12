---
quick_id: 261012-cbz
type: quick
autonomous: true
---

# Quick Task 261012-cbz: hide the Kwakore desktop entry

The `org.kwakore.Search.desktop` entry shows a "Kwakore" app in launchers, but Kwakore is a background daemon and the entry only runs `kwak discover` in a terminal. The user wants it hidden and the search provider code kept (decision recorded 2026-10-12). Known consequence: GNOME Shell skips search providers whose DesktopId app is not shown (`remoteSearch.js` checks `appInfo.should_show()`), so on GNOME the provider stops answering; installed napplets stay searchable through their own entries.

## Task 1: add NoDisplay=true to both copies of the entry, document it

- files: backend/linuxhost/gnome_search_linux.go, nix/package.nix, backend/linuxhost/*_test.go, docs/service.md, docs/control-protocol.md
- action: add `NoDisplay=true` to the runtime-written entry and the Nix-packaged one; assert it in the linuxhost test that checks the written files; say in docs/service.md (GNOME overview search) that the entry is hidden and GNOME therefore ignores the provider; also fix the stale `settings.set` row in docs/control-protocol.md (value may be a string for `sync_id`).
- verify: `cd backend && go vet ./... && go test ./linuxhost ./...`; identity check.
- done: no visible Kwakore app entry; tests pass.
