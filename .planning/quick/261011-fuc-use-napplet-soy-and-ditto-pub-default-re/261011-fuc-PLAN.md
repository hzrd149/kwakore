---
quick_id: 261011-fuc
mode: quick
---

# Quick Task 261011-fuc: use napplet.soy and ditto.pub default relays and Blossom servers

Default to common public servers that carry more napplets.

## Task 1: change the built-in defaults

- files: `backend/serviceconfig/config.go`, `backend/launcher_settings.go`, `backend/launcher_state.go`,
  `backend/dev_publish.go`, `docs/service.md`
- action: relays `wss://relay.napplet.soy`, `wss://relay.ditto.pub`; Blossom servers
  `https://blossom.napplet.soy`, `https://blossom.ditto.pub`. Leave the last-resort resource
  fallbacks in `nap_resource.go` alone.
- verify: `go vet ./... && go test ./...` in `backend/`; a test pinning `Defaults()` and checking
  it validates; identity scan; Nix module test.
- done: a fresh install discovers and fetches from the new servers.
