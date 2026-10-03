# Phase 02 Deferred Items

Out-of-scope discoveries logged during execution. Not fixed by the plan that found them.

| Found in | Item | File | Suggested owner |
|----------|------|------|-----------------|
| 02-01 | Storage decode failure still answers the prose `"invalid request"` instead of `invalid-request` (D-07) | `backend/nap_basic.go` `storageReq` | 02-03 (bounds touch the same decode path) or Phase 5 KEY-* |
| 02-01 | `media.session.create` decode failure still answers the prose `"invalid request"` instead of `invalid-request` (D-07) | `backend/nap_media.go` `napMediaCreate` | 02-03/02-04 or Phase 7 MDIA-* |
