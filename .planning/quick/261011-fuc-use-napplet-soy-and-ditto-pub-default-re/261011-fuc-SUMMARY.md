---
quick_id: 261011-fuc
status: complete
commit: 91629bf
---

# Quick Task 261011-fuc Summary

The built-in discovery relays are now `wss://relay.napplet.soy` and `wss://relay.ditto.pub`,
and the Blossom servers `https://blossom.napplet.soy` and `https://blossom.ditto.pub`
(service defaults, legacy state defaults, dev-publish targets, and `docs/service.md`).
`TestDefaultsUsePublicNappletServers` pins them and checks they pass config validation.

Verification: `go vet ./... && go test ./...` in `backend/`; `scripts/check-product-identity.sh`;
`nix eval --impure --json --file nix/module-test.nix` (true). The servers were checked to resolve,
and relay.napplet.soy answered NIP-11. Not run: desktop tests (untouched) and the user-service smoke.
