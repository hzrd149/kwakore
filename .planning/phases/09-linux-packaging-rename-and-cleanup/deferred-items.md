# Deferred items: Phase 9

- **09-01 (found while running `cd backend && go test ./...`):** `TestRPCInstallValidationAndFixedErrors` in `backend/daemon/rpc_linux_test.go` failed once with `read unix ... daemon.sock: i/o timeout` during a full parallel run. The test installs a missing address, which queries the default public relays inside the 5-second client deadline. It passed 5/5 alone and on a full rerun. This is a pre-existing network-timing flake, not caused by the socket activation change. Out of scope for 09-01; a fix would use offline relays in that test's `rpcService` setup.
