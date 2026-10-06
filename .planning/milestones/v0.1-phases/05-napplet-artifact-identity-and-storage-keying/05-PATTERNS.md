# Phase 5: Napplet Artifact Identity and Storage Keying - Pattern Map

**Mapped:** 2026-10-05
**Files analyzed:** 27 (new + modified)
**Analogs found:** 26 / 27

All paths are relative to the repo root `/home/user/Projects/verdana`. Line numbers were read on 2026-10-05.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `backend/napplet.go` (ID = Address(), `validSource` per schema, `EventID`/`Unavailable` fields, reason catalogue) | model / parser | transform | itself (`nappletID` :121-123, `validSource` :323-333, `hex64` :144) | exact |
| `backend/napplet_nip5d.go` (ID = Address()) | model / parser | transform | `napplet.go:292` | exact |
| `backend/registry_select.go` (NEW: `newerEvent`, `eventAddress`, `latestByAddress`, unavailable entries) | service helper | transform | `registry_discovery.go:97-141` `collectDiscovery` | role-match |
| `backend/registry_discovery.go`, `registry_address.go`, `registry_detail.go`, `registry_updates.go` | service | request-response (relay fetch) | `registry_discovery.go:114-125` (site to convert) | exact |
| `backend/registry_install.go` (`blobClient`, capped `downloadBlob`, `tryNapplet` all paths, promotion compare, Uninstall reclaim + close windows) | service | file-I/O + HTTP | `nap_resource.go:49-68` (`resourceClient`), `dev.go:277-286` (`DevUnload`) | exact |
| `backend/window_storage.go` (`nappletScope`, `nappletStorageKey`, `keyFileName`, `napplet-storage/` dir, reclaim, dead flag, sweep) | service / storage | file-I/O | itself `storageFileFor`/`safeFileName` :36-58; `backend.go:155-172` `nappBaseDirIn` (hex naming) | exact |
| `backend/nap_basic.go` (`napStoreID` → scope helper, fail on error) | NAP handler | request-response | itself :80-101 | exact |
| `backend/nap_config.go`, `backend/window_settings.go` (push/settings by scope) | NAP handler | event-driven | `nap_config.go:136-153` `pushConfigValues` | exact |
| `backend/napconfig/store.go` (opaque scope, hex file names, `Forget(scope)`) | storage package | file-I/O | itself :40-60, :210-230 | exact |
| `backend/launcher_state.go` (drop pre-D-01 records at load) | state loader | file-I/O | `Uninstall` forgets, `registry_install.go:84-110` | role-match |
| `backend/backend.go` (`Start`: sweep after `loadState`) | entry | batch | `Start` :60; `nappBaseDirIn` :155-172 | exact |
| `backend/launcher_notices.go` (new notice IDs, ranks, cap, copy, sanitizer) | notice registry | event-driven | itself :60-110; sanitizer `nap_basic.go:205-224` `napLinkLabel` | exact |
| `backend/window_instances.go` (`launchWindow` requires notice, `forgetWindow` reclaim, launch-time check D-19) | service | event-driven | itself; `windows.Delete` at :549 | exact |
| `backend/window_permissions.go` (injective rule ids, `ForgetPermission` on uninstall) | service | CRUD | itself :74-105, :288-321 | exact |
| `backend/launcher_usage.go` (injective `usageID`) | service | CRUD | `window_permissions.go:96-104` | exact |
| `backend/launcher_ui.go` (Snapshot stamps `Unavailable`) | state snapshot | transform | `UpdateAvailable` stamping in `Snapshot()` | exact |
| `desktop/layout.go` (`layoutConfirmLogout` → `layoutConfirm`) | component | UI | itself :743-799 | exact |
| `desktop/store.go` (`confirm` state, three update + three uninstall entry points, stale guard, notice strip) | component / controller | UI event | itself :28-50, :404, :470, :516-518 | exact |
| `desktop/store_layout.go`, `desktop/detail.go`, `desktop/grid.go` (unavailable block, "Opening…") | component | UI | `detail.go` "An update is available." line; `renderNappTile` | exact |
| `desktop/notices.go` (`noticeUI` → per-window type, filtered store strip) | component | UI | itself :39-57, :74-130 | exact |
| `desktop/internal/osintegration/appshortcut_linux.go` + `shortcutfile_linux.go` (encoded id, control chars) | OS integration | file-I/O | itself :25-45 / :145-157 | exact |
| `desktop/main.go` / `--launch-napp` decode (D-21) | entry | request-response | existing `--launch-napp` handling | exact |
| `spec/CONFORMANCE.md`, `NAPPLETS.md` | docs | — | existing rows, `spec_conformance_test.go:100-200` rules | exact |
| Tests: `backend/nap_test.go` (fixture), `nap_storage_test.go`, `nap_config_test.go`, `containment_test.go`, `preview_test.go`, `registry_discovery_test.go`, `registry_address_test.go`, new `registry_select_test.go`, `window_storage_test.go`, `napconfig/schema_test.go` | test | — | `containment_test.go:144,204,357`, `nap_route_test.go:492-498` | exact |
| `desktop/store_test.go` (confirm state) | test | — | desktop tests under `-tags novulkan` | role-match |
| `desktop/internal/osintegration/appshortcut_linux_test.go` (hostile `d`) | test | — | `containment_test.go:357 TestHostileDTagStaysInsideDataDir` | partial |
| `backend/mobile/mobile.go` | binding | — | unchanged; compile check only (D-17) | n/a |

## Pattern Assignments

### `backend/window_storage.go` + `backend/nap_basic.go` (storage, file-I/O)

**Current choke point to replace** (`backend/nap_basic.go:80-101`), note the two fallbacks D-02 removes (address-only when hash empty; `c.ci.instance` when `storageInstance` empty):
```go
func napStoreID(c *napCall, scope string) string {
	if c.ci.napp.ArtifactHash == "" && scope != "instance" {
		return c.ci.napp.ID
	}
	identity := c.ci.napp.ID + "\x00" + c.ci.napp.ArtifactHash
	if scope == "instance" {
		instance := c.ci.storageInstance
		if instance == "" {
			instance = c.ci.instance
		}
		identity += "\x00" + instance
	}
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("napplet-%x", sum)
}
```
Replace with `nappletScope` / `nappletStorageKey` / `keyFileName` (RESEARCH Code Example 1). On error: `log.Error()...; c.failWith(napErrInternal); return` (codes in `nap_route.go:104-110`).

**Hex naming to copy** (`backend/backend.go:160-172`, `nappBaseDirIn`):
```go
	root := filepath.Join(dataDir, "napps")
	sum := sha256.Sum256([]byte(id))
	name := hex.EncodeToString(sum[:])
	dir := filepath.Join(root, name)
	if rel, err := filepath.Rel(root, dir); err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("napp directory escapes %s", root)
	}
```
Delete `safeFileName` (`window_storage.go:40-58`) and `storageFileFor`'s use of it (:36-38); `StorageFile(nappID)` keeps its signature (used by `desktop/childproc.go:60`). Napplet storage goes to `{dataDir}/napplet-storage/<hex>.json`. Key the `storages` map (:28-31) by full file path.

**Writes:** `fileutil.WriteFileAtomic` (`backend/fileutil/atomic.go`). **Sweep:** `os.ReadDir`, `DirEntry.Type().IsRegular()`, regex `^[0-9a-f]{64}\.json$`, `os.Remove` only, never `napps/`.

---

### `backend/napconfig/store.go` (storage package, file-I/O)

**Analog: itself.** Current keying (:53-55) and duplicate sanitizer (:212-230) to remove:
```go
func configFileFor(nappID string) string {
	return filepath.Join(configDir, safeFileName(nappID)+".json")
}
```
Rename parameter to opaque `scope string`, name file `hex(sha256(scope)).json` (same as backend `keyFileName`, but implemented locally since `napconfig` must not import root `backend`). Keep `configMu`/`configs` cache (:40-50); add `Forget(scope)` that deletes file + evicts under `configMu`. `Init` (:44-51) unchanged.

---

### `backend/registry_install.go` (service, HTTP + file-I/O)

**Guarded client analog** (`backend/nap_resource.go:49-68`): copy verbatim as package var `blobClient` (so tests can swap it), change limits:
```go
var resourceClient = &http.Client{
	Timeout: resourceTimeout,
	Transport: &http.Transport{
		DialContext:           netguard.DialContext,
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= resourceMaxRedirects {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("redirect away from https")
		}
		return nil
	},
}
```
D-20: user-configured Blossom servers (`launcher_settings.go:18-21`) use an unguarded-dial client; manifest `server` tags and kind 10063 use `blobClient`. `downloadBlob` therefore needs to know per server which client to use (e.g. pass a `[]blobServer{url, trusted bool}` or a set of trusted URLs).

**downloadBlob loop to modify** (`registry_install.go:297-340`): replace `http.DefaultClient.Do(req)` and `io.ReadAll(resp.Body)` with client choice + `resp.ContentLength > blobMaxBytes` early reject + `io.ReadAll(io.LimitReader(resp.Body, blobMaxBytes+1))`. Keep the per-attempt `blobAttemptTimeout`, the `log.Debug()...Msg("... trying the next")` style and the sha256 check.

**Uninstall analog** (`registry_install.go:84-110`) plus window close from `dev.go:277-286`:
```go
func DevUnload(id string) {
	for _, ci := range runningForNapp(id) {
		ci.Close()
	}
	...
```
Add to `Uninstall`: close windows first, reclaim scope + instance keys, `ForgetPermission(id, "")`, keep existing `forgetActionUsage`/`forgetDispatchTarget`/`os.RemoveAll(base)`/`setBusy` pattern.

**Trial:** `tryNapplet` (:137-159) failure path `SetFetchErr("try failed: …")` (:131-134) stays the error channel, with fixed copy from UI-SPEC plus `addNotice(napplet-trial-failed)`. Promotion: `finishNappletTrial` (:179-206).

---

### `backend/registry_select.go` (NEW, transform) + registry sites

**Site to replace** (`backend/registry_discovery.go:114-125`), validate-before-select and CreatedAt-only compare:
```go
			n, ok := nappFromEvent(re.Event)
			if !ok {
				continue
			}
			if i, seen := index[n.ID]; seen {
				if n.CreatedAt <= list[i].CreatedAt {
					continue
				}
				list[i] = n
			} else {
				index[n.ID] = len(list)
				list = append(list, n)
```
New flow: `latestByAddress.add(evt)` (CheckID + VerifySignature + `newerEvent`) on raw events, then `nappFromEvent(winner)`; invalid → `Napp{ID: address, Kind, Author, D, CreatedAt, EventID, Name: sanitized title, Unavailable: <catalogue phrase>}`. Same conversion at `registry_address.go:113-164,238-272`, `registry_detail.go:78-86`, `registry_updates.go:54-112,182-228`. Use RESEARCH Code Example 2 verbatim. Put new file under `registry_` prefix per CLAUDE.md.

---

### `backend/launcher_notices.go` (notice registry, event-driven)

**Analog: itself** (:66-110). Extend `noticeRank` switch and add constants; `addNotice` replaces in-slot, callers `notifyState()` afterwards with no lock held:
```go
func noticeRank(id string) int {
	switch {
	case id == noticeChildUnavailable:
		return 0
	case id == noticeNappletHardening:
		return 1
	case strings.HasPrefix(id, noticeStateCorruptPrefix):
		return 2
	case id == noticeKeyringFallback:
		return 3
	default:
		return 4
	}
}
```
New order per UI-SPEC S4: child-unavailable, hardening, `napplet-trial-failed`, state-corrupt, keyring, `napplets-reinstall`, then `napplet-requires:*` / `trial-data-discarded:*`. Cap of 3 for those two prefixes enforced inside `addNotice`.

**Sanitizer analog** (`backend/nap_basic.go:207-224` `napLinkLabel`): copy shape into `noticeName(name, d string)` with 48-rune cut and "Unnamed napplet" fallback:
```go
	label = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, label)
	label = strings.Join(strings.Fields(label), " ")
	runes := []rune(label)
	if len(runes) > maxNapLinkLabelRunes {
		label = string(runes[:maxNapLinkLabelRunes]) + "…"
	}
```

---

### `backend/launcher_state.go` / `backend/backend.go` (load + startup sweep, batch)

`loadState` at `launcher_state.go:124`. After load: for each `InstalledNapps[k]` with `n.IsNapplet() && k != n.Address()`, delete and run the same forgets as `Uninstall` (`registry_install.go:99-107`): `delete(state.LastLaunched, id)`, `ForgetPermission(id, "")`, `forgetActionUsage(id)`, `forgetDispatchTarget(id)`; `saveState()`; raise `napplets-reinstall`. In `Start` (`backend.go:60`) call the sweep synchronously after `loadState` and before `refreshInstalled`.

---

### `backend/window_permissions.go` / `launcher_usage.go` (CRUD)

`ruleID` joins with `"\x1f"` (`window_permissions.go:96-104`); `usageID` same (`launcher_usage.go:49-58`). Switch to an injective encoding (JSON array or length-prefixed). `ForgetPermission` (:288-321) clears `state.Rules` + `sessionRules`: reuse as-is from Uninstall.

---

### `desktop/layout.go` (component, UI)

**Analog: `layoutConfirmLogout`** (:743-799). Generalize to `layoutConfirm(gtx, th, title, body, yesLabel, noLabel string, yesBtn, noBtn *widget.Clickable, maxWidth unit.Dp)`. Keep geometry; change the yes button style per UI-D4:
```go
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							pointer.CursorPointer.Add(gtx.Ops)
							b := material.Button(th, yesBtn, "Log out")
							b.Background = p.chipBg   // -> p.danger
							b.Color = p.danger        // -> p.bg
							return b.Layout(gtx)
						}),
```
Card background pattern (:787-797): `op.Record` macro → `clip.RRect{... NW:10,...}` → `paint.Fill(gtx.Ops, p.card)` → `call.Add`. Logout caller passes `"Log out?"`, existing body, `"Log out"`, `"Cancel"`, `maxWidth 0`.

---

### `desktop/store.go` (controller, UI event)

**State analog** `storeState` (:28-46): add `confirm *storeConfirm` (`kind, id, name string; target backend.Napp; viaInstall bool`) guarded by `store.mu`. **Entry points to gate** (napplets only, skip when `busy[id]`):
- installed tile: `go backend.Update(st.Installed[i].ID)` (:404)
- detail: `go backend.Update(n.ID)` (:470)
- profile: `go backend.Install(pn)` (:516-518)
- uninstall: `uninstBtns`, `detailPrimaryBtn` while installed, `profileActionBtns` while installed.
Stale guard each frame; clear on `app.DestroyEvent`. While `confirm != nil` draw only `layoutConfirm` (mirrors manager logout replacement).

---

### `desktop/notices.go` (component, UI)

`noticeUI` is a package-level struct (:39-55) used by `layoutNotices` (:74-130). Convert to a type (`noticeState`) with one instance owned by the manager and one by the store; `layoutNotices` takes `*noticeState`. Store strip filters IDs `napplet-trial-failed`, `napplet-requires:*`, `trial-data-discarded:*`. Reuse `chipButton` (:57).

---

### `desktop/internal/osintegration/appshortcut_linux.go` + `shortcutfile_linux.go` (OS integration, file-I/O; D-21)

Vulnerable template (`appshortcut_linux.go:31-42`): raw `shortcut.ID` in `X-Verdana-Napp-ID=%s` and `quoteExecField(shortcut.ID)`. `quoteExecField` (`shortcutfile_linux.go:149-157`) escapes `\ " `` ` `` $` but not newlines/control chars. Fix: write `base64.RawURLEncoding` of the id in both places, decode in the `--launch-napp` handler, and make `quoteExecField`/`appShortcutText` reject or escape control runes. Stale file removal (`removeStaleAppShortcutFiles`, :48,58) already handles the id change.

---

### Tests

- **Fixture to fix first** (`backend/nap_test.go:147-149`):
  ```go
  n := Napp{ID: "napplet~0123456789abcdef~" + d, D: d, Name: d, Format: FormatNapplet, Kind: KindNapplet}
  ```
  → generated pubkey `Author`, `ID = n.Address()`, 64-hex `ArtifactHash`. Same for `storageTestInstance` (`nap_storage_test.go:9-24`), `napconfig/schema_test.go` (`const id = "napplet~..."`), `preview_test.go`, `containment_test.go`.
- **Client swap analog** (`backend/nap_route_test.go:492-498`):
  ```go
  prev := resourceClient
  resourceClient = &http.Client{Transport: napRoundTripFunc(...)}
  t.Cleanup(func() { resourceClient = prev })
  ```
  Use for `blobClient` in loopback tests; one test keeps the production client and asserts `netguard.ErrPrivateAddress`.
- **Hostile-`d` analogs**: `containment_test.go:144 TestHostileDTagInstallStaysInsideDataDir`, `:204 TestNappBaseDirIsHashedAndContained`, `:357 TestHostileDTagStaysInsideDataDir`; signed-event helper `containment_test.go:301 signedWith`.
- Invert `TestNapConfigValuesSurviveUpdate` (`nap_config_test.go:350-372`).
- Logger: `zerolog.Nop()`; napconfig init in fixture `napconfig.Init(filepath.Join(dataDir, "config"), zerolog.Nop())` (`nap_test.go:142`).

## Shared Patterns

### Error handling in NAP handlers
**Source:** `backend/nap_route.go:104-110, 293-302`; `napCall.async` in `backend/nap.go`
**Apply to:** storage/config handlers. `c.failWith(napErrInternal)` on scope errors; never a bare `go` in `nap_*.go` (enforced by `nap_guard_test.go`).

### Busy + refresh lifecycle
**Source:** `backend/registry_install.go:88-110` (`setBusy(id,true)`/`defer setBusy(id,false)`, `stateMu.Lock` → mutate → `saveState()` → unlock, `refreshInstalled()`)
**Apply to:** Uninstall reclaim, update reclaim, trial promotion, old-record drop.

### Notices
**Source:** `backend/launcher_notices.go:91-110` (`addNotice` / `removeNotice`, then `notifyState()` without locks)
**Apply to:** requires, trial-failed, trial-data-discarded, napplets-reinstall. Copy fixed in UI-SPEC; names via the `napLinkLabel`-style sanitizer.

### Public-only networking
**Source:** `backend/nap_resource.go:49-68`, `backend/netguard/netguard.go:85`
**Apply to:** `blobClient` (except user-configured Blossom servers, D-20).

### Hex naming of author-derived identifiers
**Source:** `backend/backend.go:160-172`
**Apply to:** storage files, napplet-storage files, config files, shortcut ids (encoded).

### Logging
zerolog chaining, lowercase, no trailing punctuation: `log.Warn().Str("napp", id).Err(err).Msg("napp directory not removed")` (`registry_install.go:94`).

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| Git-URL `source` grammar in `validSource` (scp-like parsing, schema-split per D-18) | parser | transform | No existing git URL parser; use RESEARCH Code Example 4 and Pattern 8 table, with the WEB-NAPPLET set (`https`, `ssh`, `git`, `nostr`, no scp) for web-napplet events |

## Metadata

**Analog search scope:** `backend/` (root, `napconfig`, `netguard`, `fileutil`), `desktop/` (root, `internal/osintegration`)
**Files scanned:** ~20
**Pattern extraction date:** 2026-10-05
