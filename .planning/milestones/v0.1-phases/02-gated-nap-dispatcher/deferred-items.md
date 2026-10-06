# Phase 02 Deferred Items

Out-of-scope discoveries logged during execution. Not fixed by the plan that found them.

## 02-01: Storage decode failure still answers the prose `"invalid request"` instead of `invalid-request` (D-07)

- **File:** `backend/nap_basic.go` `storageReq`
- **Suggested owner:** RESOLVED in 02-04 (failWith(napErrInvalid))
- **Status:** resolved

## 02-01: `media.session.create` decode failure still answers the prose `"invalid request"` instead of `invalid-request` (D-07)

- **File:** `backend/nap_media.go` `napMediaCreate`
- **Suggested owner:** RESOLVED in 02-04 (failWith(napErrInvalid))
- **Status:** resolved

## 02-04: `outbox.query`/`outbox.subscribe` filter errors and `outbox.publish` fan-out errors still pass `err.Error()` through a local `fail(msg)` closure (D-07); the AST vocabulary guard only sees `"error":` composite-literal values, so it does not catch this indirection

- **File:** `backend/nap_outbox.go` (`fail(err.Error())` at the outboxFilters and outboxFanout call sites)
- **Suggested owner:** Phase 6 RELY-03..05 (or a later 02-xx plan that owns nap_outbox.go)
- **Status:** acknowledged
- **Deferred:** at v0.1 milestone close, 2026-10-06

## 02-04: `resource.*` error replies carry Go error text in their `message` field (`resourceErrFields`, `rerr(..., err.Error())`); `error` itself is always a code

- **File:** `backend/nap_resource.go`, `backend/nap_sink.go` `httpsResource`
- **Suggested owner:** Phase 7 RES-*
- **Status:** acknowledged
- **Deferred:** at v0.1 milestone close, 2026-10-06
