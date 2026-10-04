# Phase 04 Deferred Items

Out-of-scope discoveries logged during execution. These were not fixed in the plan that found them.

## From 04-04

- **Flaky `TestSetGNOMESearchIntegrationCreatesAndRemovesFiles`** (`desktop/internal/osintegration`). It fails about 1 run in 5 under `-race` with `TempDir RemoveAll cleanup: unlinkat .../data-home/applications: directory not empty`, which suggests something still writes into the temp applications dir after the test returns. 04-04 does not touch this package, and the next full `go test -race -tags novulkan ./...` run passed.

## From 04-06

- **`TestWebKitNappletAdversarial` failed once on the live display** (`desktop/child/smoke_test.go`, `VERDANA_WEBKIT_SMOKE=1`, X11 `DISPLAY=:0`, WebKitGTK 2.52.6). It was the first smoke run after a fresh `go build -o child/child ./child` and the full desktop suite. The output was filtered to the `---` lines, so the failing assertion is not known. The next six runs passed: five full `^TestWebKit` runs and one under concurrent `go test -race` load on the backend. 04-06 changed only `spec/CONFORMANCE.md` and the checklist test, so this is pre-existing, and it bears on end-of-phase smoke item 8 (the CI `webkit smoke` step). If it recurs, keep the full `-v` log. The timing-sensitive candidates are the verdict deadline (line ~590), the loop counts (~631-646) and the child's exit at close (~243).
