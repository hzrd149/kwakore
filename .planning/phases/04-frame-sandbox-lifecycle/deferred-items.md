# Phase 04 Deferred Items

Out-of-scope discoveries logged during execution. These were not fixed in the plan that found them.

## From 04-04

- **Flaky `TestSetGNOMESearchIntegrationCreatesAndRemovesFiles`** (`desktop/internal/osintegration`). It fails about 1 run in 5 under `-race` with `TempDir RemoveAll cleanup: unlinkat .../data-home/applications: directory not empty`, which suggests something still writes into the temp applications dir after the test returns. 04-04 does not touch this package, and the next full `go test -race -tags novulkan ./...` run passed.
