# Phase 02 Deferred Items

Out-of-scope discoveries logged during execution. Not fixed by the plan that found them.

| Found in | Item | File | Suggested owner |
|----------|------|------|-----------------|
| 02-01 | Storage decode failure still answers the prose `"invalid request"` instead of `invalid-request` (D-07) | `backend/nap_basic.go` `storageReq` | RESOLVED in 02-04 (failWith(napErrInvalid)) |
| 02-01 | `media.session.create` decode failure still answers the prose `"invalid request"` instead of `invalid-request` (D-07) | `backend/nap_media.go` `napMediaCreate` | RESOLVED in 02-04 (failWith(napErrInvalid)) |
| 02-04 | `outbox.query`/`outbox.subscribe` filter errors and `outbox.publish` fan-out errors still pass `err.Error()` through a local `fail(msg)` closure (D-07); the AST vocabulary guard only sees `"error":` composite-literal values, so it does not catch this indirection | `backend/nap_outbox.go` (`fail(err.Error())` at the outboxFilters and outboxFanout call sites) | Phase 6 RELY-03..05 (or a later 02-xx plan that owns nap_outbox.go) |
| 02-04 | `resource.*` error replies carry Go error text in their `message` field (`resourceErrFields`, `rerr(..., err.Error())`); `error` itself is always a code | `backend/nap_resource.go`, `backend/nap_sink.go` `httpsResource` | Phase 7 RES-* |
