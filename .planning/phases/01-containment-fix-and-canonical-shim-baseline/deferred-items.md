# Deferred items (phase 01)

- [01-03] `TestNapDeliversDMsAsSigned` (backend/nap_outbox_test.go:352) failed once in a full `go test ./...` run with "no relay.query.result (#1) pushed" after its 3s wait, then passed 5/5 in isolation and 3/3 full-suite reruns. Looks like a timing flake under load, pre-existing and unrelated to the spec snapshots. Out of scope for 01-03.
