---
quick_id: 261011-czi
status: complete
commit: d1ae23b
---

# Quick Task 261011-czi Summary

TUI install and uninstall are now one yes/no question with Yes highlighted (Enter/y accept,
n/Esc cancel, arrows switch). Discover's `l` opens any napplet without installing it, through
the new `napplet.try` control method (`kwak try ADDRESS`).

- `backend/window_service.go`: `ServiceTry` opens an installed napplet like `ServiceLaunch`;
  otherwise resolves the address like install and opens an in-memory trial via `openTrial`.
- `backend/registry_install.go`: `tryNapplet` split into `openTrial` (optional launch lock held
  only around the window open; quiet flag). `finishNappletTrial` skips the install prompt for
  quiet trials and drops their data unless the napplet was installed meanwhile.
- `backend/window_instances.go`: `Instance.quietTrial`, set through `launchWindow`.
- Protocol, daemon route, CLI (`try`, 180 s default wait, relay hints), help and docs updated.
- Tests: TUI yes/no flow and `try` argv; CLI mapping and relay hints; RPC try validation and
  headless path; `ServiceTry` opens a quiet trial without installing and without holding the
  lock during download; refusals; quiet trial close drops data without a prompt (verified to
  fail without the fix).

Verification: `go vet ./... && go test ./...` in `backend/` and `desktop/`; race run of the new
tests; `scripts/check-product-identity.sh`. Not run: real-engine/graphical tests and the
user-service smoke.
