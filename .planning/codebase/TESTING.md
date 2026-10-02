# Testing Patterns

**Analysis Date:** 2026-10-02

## Test Framework

**Runner:**
- Go standard `testing` package (34 `*_test.go` files, ~5.9k lines)
- Config: none; CI in `.github/workflows/desktop.yml`

**Assertion Library:**
- None. Plain `if ... { t.Fatalf / t.Errorf }`.

**Run Commands:**
```bash
cd backend && go test ./...                                                   # backend
cd desktop && go build -o child/child ./child && go test -tags novulkan ./...  # desktop (child binary must exist first)
cd backend && go test -run TestNapUpload ./...                                # focused
```
- Android (`android/`) has no unit tests; CI only runs `./gradlew assembleDebug`.

## Test File Organization

**Location:**
- Co-located with implementation, same package (white-box: tests call unexported funcs like `mergeFollowTags`).

**Naming:**
- `TestBehavior` describing the outcome: `TestNapUploadIsCancelledOnReload`, `TestNapReplyIsBoundToTheSendingWindow`, `TestNapPanickingHandlerStillReplies`. Prefix with the area (`TestNap...`, `TestNapCommon...`).
- Platform tests use suffix files: `desktop/internal/osintegration/autostart_linux_test.go`.

**Structure:**
```
backend/nap_test.go             # shared NAP test rig + core NAP tests
backend/nap_<area>_test.go      # per-handler tests
backend/testdata/               # fixtures (nip5d-napplets.jsonl)
backend/napconfig/testdata/     # package fixtures (config/full.json)
desktop/*_test.go, desktop/internal/*/..._test.go
```

## Test Structure

**Suite Organization:**
```go
func TestNapCommonMergeFollowTags(t *testing.T) {
	a, b, c := nostr.Generate().Public(), nostr.Generate().Public(), nostr.Generate().Public()
	current := nostr.Tags{{"p", a.Hex(), "wss://r.example.com", "alice"}, {"t", "nostr"}, {"p", b.Hex()}}

	tags, changed := mergeFollowTags(current, []nostr.PubKey{a, c}, true)
	if !changed || len(tags) != 4 || tags[3][1] != c.Hex() {
		t.Fatalf("follow: changed=%v %v", changed, tags)
	}
	// the petname and relay hint on an existing follow survive
	if len(tags[0]) != 4 || tags[0][3] != "alice" {
		t.Errorf("existing entry rewritten: %v", tags[0])
	}
}
```

**Patterns:**
- Setup: helpers with `t.Helper()`; `setupNapTest(t)` sets `dataDir = t.TempDir()`, `host = noopHost{}`, `napconfig.Init(..., zerolog.Nop())` (`backend/nap_test.go`).
- Teardown: `t.Cleanup` resets package globals (e.g. `storages` map). Because tests mutate package-level state, do not use `t.Parallel()` in `backend`.
- Assertions: `t.Fatalf` when continuing is meaningless, `t.Errorf` otherwise; message is short label + got values (`"p tag: %v"`).
- Table cases: map or slice of anonymous structs, with `t.Run(name, ...)` where named (`backend/napplet_test.go`), or plain loops over `[]struct{...}` (`backend/nap_common_test.go`).

## Mocking

**Framework:** Hand-written fakes; no mocking library.

**Patterns:**
```go
// recTransport stands in for a napplet window: it keeps every NAP push
type recTransport struct { mu sync.Mutex; pushes []map[string]any; notify chan struct{}; focused int }
func (r *recTransport) Send(m WireMsg) { /* decode __nap_push eval, record */ }

ci, rec := openNapplet(t, d)
post(t, ci, env)
got := rec.wait(t, "storage.result", 1) // blocks up to 3s on notify chan
```
- Interface fakes embedded per test: `deadlineCheckingSigner` (implements `SignEvent`), `intentDiscoveryHost` (records `OpenDiscovery` into a channel), `noopHost`.
- HTTP: `httptest.NewServer(http.HandlerFunc(...))` for Blossom/upload servers (`backend/nap_test.go`).
- Keys: fresh `nostr.Generate()` per test.

**What to Mock:**
- `Host` (platform), transports/windows, signers, remote HTTP servers.

**What NOT to Mock:**
- Pure logic, event signing/verification, `napconfig` (use real with temp dir), filesystem (use `t.TempDir()`).

## Fixtures and Factories

**Test Data:**
```go
raw, err := os.ReadFile("testdata/nip5d-napplets.jsonl")   // real relay events
evt := signedNapplet(t, validNappletTags(), "desc")          // factory helper
```

**Location:**
- `backend/testdata/`, `backend/napconfig/testdata/`. Add new fixtures there.

## Coverage

**Requirements:** None enforced. Changes to parsing, permissions, storage, networking or napplet lifecycle must add focused regression tests (CLAUDE.md).

**View Coverage:**
```bash
cd backend && go test -cover ./...
```

## Test Types

**Unit Tests:**
- Pure helpers (tag merging, templates, schema validation in `backend/napconfig/schema_test.go`, `backend/netguard/netguard_test.go`).

**Integration Tests:**
- In-process NAP round-trips via `recTransport` (`backend/nap_*_test.go`); desktop process/instance tests (`desktop/startup_test.go`, `desktop/internal/instancelock/lock_unix_test.go`).
- JS shim tests in `backend/webview/shim_test.go` run snippets under `node` and `t.Skip` when node is missing.

**E2E Tests:**
- Not used.

## Common Patterns

**Async Testing:**
```go
rec.wait(t, "upload.result", 1) // channel + time.After(3*time.Second) deadline, t.Fatalf on timeout
```

**Error Testing:**
```go
if _, err := nappletFromEvent(evt); err == nil {
	t.Fatal("accepted")
}
if _, code := reactionTemplate(note, "🤙", ""); code != "" {
	t.Errorf("emoji refused: %q", code)
}
```

---

*Testing analysis: 2026-10-02*
