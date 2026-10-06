# Phase 3: Desktop Process and Secrets Hardening - Pattern Map

**Mapped:** 2026-10-03
**Files analyzed:** 34 (new + modified)
**Analogs found:** 30 / 34

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/fileutil/atomic.go` (+`syncdir_unix.go`, `syncdir_windows.go`, `atomic_test.go`) NEW | utility | file-I/O | `desktop/internal/osintegration/appshortcut.go:49-71` (`writeAtomic`), `backend/window_storage.go:119-150` | exact |
| `backend/netguard/link.go` + `link_test.go` NEW | utility (validator) | transform | `backend/netguard/netguard.go`, `netguard_test.go` | exact (same pkg) |
| `backend/launcher_state.go` MOD | model/store | file-I/O | itself `:79-141` | self |
| `backend/launcher_secrets.go` + `_test.go` NEW | service | request-response (async store) | `backend/launcher_state.go` + `backend/auth_login.go` | role-match |
| `backend/launcher_notices.go` NEW | model/store | event-driven (pull snapshot) | `backend/launcher_ui.go` (`setLoginErr`, `setPhase`, `notifyState`) | role-match |
| `backend/launcher_ui.go` MOD (`State.Notices`, `State.KeyringWait`) | model | pull snapshot | itself `:27-32`, `:185` | self |
| `backend/backend.go` MOD (`Options.Secrets`, 0700, secrets goroutine) | config/entry | — | itself `:38-48`, `:68-99` | self |
| `backend/auth_login.go`, `backend/auth_nostrconnect.go` MOD | service | request-response | itself (`state.ClientKey` at `auth_login.go:104`, `auth_nostrconnect.go:141`; `state.Login` at `:51,:163,:229`) | self |
| `backend/bridge_files.go` MOD (`openExternalLink`) | controller | request-response | itself `:100-111` | self |
| `backend/mobile/mobile.go` MOD (`OpenLink`) | adapter | request-response | itself `:132` | self |
| `backend/registry_install.go` MOD (`fetchNappAsset` write) | service | file-I/O | `backend/fileutil` (new) | role-match |
| `backend/window_instances.go`, `window_settings.go` MOD (child-unavailable notice) | service | event-driven | `setLoginErr` pattern in `launcher_ui.go:302` | role-match |
| `desktop/internal/childbin/childbin.go` (+`owner_unix.go`, `owner_windows.go`, tests) NEW | utility (OS) | file-I/O | `desktop/embed_prod.go:16-48` (to replace) + `osintegration/appshortcut.go:49-71` | role-match |
| `desktop/embed_prod.go` MOD | config | file-I/O | itself | self |
| `desktop/embed_dev.go` MOD (gains disk fallbacks) | config | file-I/O | `desktop/childproc.go:189-207` (fallbacks move here) | exact |
| `desktop/webviewlib_<goos>_<goarch>.go` / `desktop/internal/webviewlib/*` NEW + sync test | config (embed) | — | `desktop/embed_prod.go:12-13` (`//go:embed`) | role-match |
| `justfile` MOD (copy libs step, D-17), `.gitignore` | config | batch | existing `just fonts` recipe | partial |
| `desktop/childproc.go` MOD (`WEBVIEW_PATH`, error return) | service | process spawn | itself `:42-75`, `:189-207` | self |
| `desktop/child/main.go` MOD (drop `embedded` import `:22`) | entry | — | itself | self |
| `desktop/internal/instanceipc/ipc.go`, `ipc_unix.go`, `peercred_linux.go`, `peercred_darwin.go`, `peercred_other.go`, `ipc_windows.go`, tests NEW | utility (OS) | request-response (socket) | `desktop/internal/instancelock/lock_unix.go` / `lock_windows.go` | role-match |
| `desktop/singleinstance.go` MOD (v2 router) | controller | request-response | itself `:1-140` + `desktop/internal/wireline/wireline.go` (bounded read) | self |
| `desktop/startup_test.go` REWRITE | test | — | itself `:12-35` (table test) | self |
| `desktop/host.go` MOD (`OpenLink`, `saveFile`) | adapter | request-response / file-I/O | itself `:94`, `:179-190` | self |
| `desktop/internal/osintegration/*` MOD (autostart/shortcutfile writers, drop `writeAtomic`) | utility | file-I/O | `appshortcut.go:49-71` | exact |
| `desktop/internal/secretstore/secretstore.go` (+`probe_linux.go`, `probe_darwin.go`, `probe_windows.go`, `probe_other.go`, tests) NEW | service (OS) | request-response (serialized worker) | `desktop/internal/themesystem/system_linux.go:27` (dbus session) | partial |
| `desktop/layout.go` MOD (notice stack) | component | pull render | `renderNappCard` `layout.go:968-1010` | exact |
| `desktop/login.go` MOD (S4 + chip) | component | pull render | itself `:102-130`, `:76-90` | self |
| `desktop/main.go` MOD (loading screen S3, `Options.Secrets`) | component/entry | pull render | `login.go` chip + `layoutMain` | role-match |
| `desktop/lifecycle.go` MOD (`showPendingPrimary` KeyringWait) | controller | event-driven | itself `:58-75` | self |
| `.github/workflows/desktop.yml` MOD (windows-2022 job) | config (CI) | batch | existing `test` job `:10-51` | exact |
| `backend/launcher_state.go` LogoutPending (D-20) | model | file-I/O | `state.Login=""` at `auth_login.go:229` | self |

## Pattern Assignments

### `backend/fileutil/atomic.go` (utility, file-I/O)

**Analog:** `desktop/internal/osintegration/appshortcut.go` lines 49-71 — copy and extend with `tmp.Sync()` + dir fsync, return named `err` with deferred cleanup:
```go
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".verdana-*")
	...
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil { tmp.Close(); return err }
	if _, err := tmp.Write(data); err != nil { tmp.Close(); return err }
	if err := tmp.Close(); err != nil { return err }
	return os.Rename(tmpPath, path)
}
```
Second analog `backend/window_storage.go:131-150` uses `.tmp-*` prefix (use that prefix, as RESEARCH Pattern 1). Target code is RESEARCH Pattern 1. Platform split follows `instancelock/lock_unix.go` (`//go:build !windows`) / `lock_windows.go`. Then delete `writeAtomic` from `appshortcut.go` and call `fileutil.WriteFileAtomic` (desktop module already `replace verdana/backend => ../backend`, so `verdana/backend/fileutil` is importable). Note `appshortcut.go` callers relied on `MkdirAll` inside the helper — keep that at call sites.

**Call sites to switch:** `backend/launcher_state.go:131-141` `saveState` (`os.WriteFile(statePath, data, 0600)`), `backend/registry_install.go` asset write, `desktop/host.go:94` (`os.WriteFile(dest, data, 0644)` — Pitfall 17: keep the free-name loop, do not clobber), osintegration `autostart_linux.go:47`, `autostart_darwin.go:44`, `shortcutfile_linux.go:30`, `shortcutfile_windows.go:58`, `shortcutfile_darwin.go:29,47`; optionally `window_storage.go:131-150`, `napconfig/store.go:89-103`.

---

### `backend/netguard/link.go` + `link_test.go` (validator, transform)

**Analog:** `backend/netguard/netguard.go` — package doc + sentinel error style:
```go
var ErrPrivateAddress = errors.New("address is not public")
```
Add `ErrBadLink = errors.New("only http(s) links can be opened")` (same text as current `bridge_files.go:108`). Body = RESEARCH Pattern 6 (check `u.Hostname()`).

**Test analog:** `backend/netguard/netguard_test.go:9-27` — loop over string lists with `t.Errorf("%s counted as …")`; use a table of accepted/rejected URLs (control chars, `javascript:`, `https://:80`, userinfo, >8 KiB, whitespace, `mailto:`, opaque).

**Callers:**
- `backend/bridge_files.go:100-111` — replace TrimSpace/HasPrefix block with `u, err := netguard.ExternalLink(url); if err != nil { return err }; log.Info().Str("url", u)...; return host.OpenLink(u)`.
- `desktop/host.go:179-190` — validate, then exec through `var startCommand = func(c *exec.Cmd) error { if err := c.Start(); err != nil { return err }; go c.Wait(); return nil }`. Current:
```go
func (gioHost) OpenLink(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":  cmd = exec.Command("open", url)
	case "windows": cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:        cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
```
- `backend/mobile/mobile.go:132` one-liner `func (h mobileHost) OpenLink(url string) error { return h.ui.OpenLink(url) }` → validate first.

---

### `backend/launcher_state.go` (model, file-I/O) — MODIFY

Current `loadState` (`:79-129`) ignores unmarshal error and generates ClientKey:
```go
data, err := os.ReadFile(statePath)
if err == nil {
	json.Unmarshal(data, &state)
} else {
	log.Debug().Err(err).Msg("no existing state file, using defaults")
}
if state.ClientKey == (nostr.SecretKey{}) {
	state.ClientKey = nostr.Generate()
```
Changes: Pattern 9 corrupt backup (rename to `state.json.corrupt-<unix>`, log Warn, skip save if rename fails); remove ClientKey generation (moves to lazy `clientKey()` in `launcher_secrets.go`); field `ClientKey nostr.SecretKey \`json:"client_key"\`` (`:17`) → `*string` hex omitempty (Pitfall 4); add `SecretsLocation`, `DismissedNotices`, `LogoutPending`. `saveState` keeps signature and "must be called with stateMu held" contract; logging style: `log.Error().Err(err).Msg("failed to write state file")`. Keep defaults-fill block `:93-127` untouched.

`StoredLogin()` (`:173-176`) becomes an accessor over in-memory secrets under `secretsMu`.

---

### `backend/launcher_secrets.go` (service, async store) — NEW

**Analogs:** startup hook in `backend/backend.go:94-99`:
```go
if stored := StoredLogin(); stored != "" {
	go resumeLogin(stored)
} else {
	setPhase(PhaseLogin)
}
```
→ replace with `go loadSecrets(opts.Secrets)` implementing the RESEARCH Pattern 8 table. Interface + errors per RESEARCH Pattern 7. Locking: never hold `stateMu` / `ls.mu` across a store call (mirror `setPhase` in `launcher_ui.go:261-266`, which takes `ls.mu`, mutates, unlocks, then `notifyState()`). `KeyringWait` via `time.AfterFunc(time.Second, …)`. Exported `RetryKeyring()`, `LoginWithoutKeyring()` follow the exported `setPhase`/`Phase()` style. Logout path at `auth_login.go:229` (`state.Login = ""`) routes through `setStoredLogin("")` + `LogoutPending` (D-20).

**Tests:** `launcher_secrets_test.go` — fake `SecretStore` (map + injectable errors / blocking channel), table over the Pattern 8 matrix; set `statePath`/`dataDir` to `t.TempDir()` and call `loadState`/`saveState` directly as existing tests do (`backend/launcher_settings_test.go:14`, `containment_test.go:87`); `zerolog.Nop()`.

---

### `backend/launcher_notices.go` + `launcher_ui.go` State fields — NEW/MOD

**Analog:** `backend/launcher_ui.go` — `State` struct `:27-32` with json-tagged commented fields:
```go
// LoginErr is why the last login attempt failed, if it did.
LoginErr string `json:"loginErr"`
```
Snapshot fill at `:185` (`LoginErr: ls.loginErr,`); setter `setLoginErr` at `:302`; `notifyState()` at `:241-245`. Add `Notices []Notice \`json:"notices"\`` and `KeyringWait string \`json:"keyringWait"\``; `DismissNotice(id)` = lock, mutate, `saveState` for persisted IDs (under `stateMu`), unlock, `notifyState()`. Copy constants verbatim from UI-SPEC Copywriting table. Fixed order: child-unavailable → state-corrupt → keyring-fallback.

---

### `desktop/internal/childbin/` (utility OS, file-I/O) — NEW

**Analog to replace:** `desktop/embed_prod.go:16-48` (shared `/tmp/verdana-child`, 4-byte hash, Stat-only reuse, 0755 — all the bugs being fixed). Shape new API on RESEARCH Pattern 2 (`File{Name, Data, Exec}`, `Ensure(dir, files, want)`). Write path = `fileutil.WriteFileAtomic` style (CreateTemp `.tmp-*` → Chmod → Write → Sync → Close → Rename). Ownership check in `owner_unix.go` (`//go:build !windows`, `fi.Sys().(*syscall.Stat_t).Uid`) and `owner_windows.go` (reparse-point check only) — split mirrors `instancelock/lock_unix.go` / `lock_windows.go`.

**Caller `desktop/embed_prod.go`:** keep `//go:embed child/child\nvar childBinary []byte`; hash via `sync.OnceValues`; dir `filepath.Join(os.UserCacheDir(), "Verdana", "child")`.

**`desktop/embed_dev.go`:** currently `func extractChild() (string, error) { return "", nil }`. Move the fallbacks from `desktop/childproc.go:189-207` here:
```go
exe, err := os.Executable()
if err == nil {
	candidate := filepath.Join(filepath.Dir(exe), "child", "child")
	if _, err := os.Stat(candidate); err == nil { return candidate }
}
try := "./child/child"
if _, err := os.Stat(try); err == nil { return try }
return "child/child"
```
`childExePath()` then becomes `(string, error)`; `startChild` (`childproc.go:42-44`) and `startSettingsChild` (`:70-72`) return the error before `exec.Command`, and append `"WEBVIEW_PATH="+dir` **last** to `cmd.Env = append(os.Environ(), …)`.

---

### libwebview embed files (`desktop/internal/webviewlib/` or `desktop/webviewlib_*.go`) — NEW

**Analog:** `desktop/embed_prod.go:3-13` `//go:embed` + `_ "embed"` import. One build-tagged file per `goos_goarch` (tags like `//go:build linux && amd64`), compiled in dev and prod (Pitfall 1); stub file for other targets returning nil → fail closed. D-17: files copied by a `just` recipe (model on existing `just fonts` recipe in `justfile`) into a git-ignored dir; sync test uses `go list -m -f '{{.Dir}}' github.com/abemedia/go-webview` and `bytes.Equal`, `t.Skip` if `go` missing.

---

### `desktop/internal/instanceipc/` (utility OS, socket) — NEW

**Analog:** `desktop/internal/instancelock/lock_unix.go` (whole file):
```go
//go:build !windows

package instancelock
...
func Acquire(dataDir string) (release func(), acquired bool, err error) {
	f, err := os.OpenFile(filepath.Join(dataDir, "launcher.lock"), os.O_CREATE|os.O_RDWR, 0600)
```
Copy: package doc file (`instancelock/doc.go`), `!windows` vs `_windows.go` split, functions taking `dataDir`. API and algorithms per RESEARCH Pattern 4; peer-cred code in RESEARCH "Code Examples → Peer uid (Linux)" and "Owner-only pipe (Windows)" (RESEARCH lines ~517-560). Injectable `var getuid = os.Getuid` for tests. Tests: `lock_unix_test.go` style, `t.TempDir()` (watch 104-byte path on macOS).

---

### `desktop/singleinstance.go` (controller, request-response) — MODIFY

Keep: constants `:26-31`, `launcherReady`, `runBundleToken`, `runInstanceCommand` `:119-140`. Remove: `portFilePath`, `readPort`, TCP in `forwardToInstance` `:49-68`, `startInstanceListener` TCP `:74-97`, legacy mapping `:107-111`:
```go
if msg.Command == "" && msg.Token != "" {
	msg.Command = commandRunShortcut
}
```
Keep `serveForward` shape (`defer conn.Close()`, `conn.SetDeadline(time.Now().Add(5 * time.Second))`, `go runInstanceCommand(msg)`, `conn.Write([]byte("ok\n"))`) but bounded read: reuse `desktop/internal/wireline.Read(r, max, fn)` (`wireline.go:28-50`) with `max = 64<<10`, or `io.LimitReader`+`bufio`. Struct gains `V int \`json:"v"\``; command json key becomes `cmd`. Handler injected: `startInstanceListener(dataDir, handle func(instanceCommand))`. Delete stale `launcher.port` at listener start. Logging style already present: `log.Warn().Err(err).Msg("no instance listener: shortcuts will start a new launcher")`.

**Test:** `desktop/startup_test.go` — keep `TestStartupArgs` (`:12-35`, table pattern); replace `TestForwardToInstance`/`TestInstanceListenerRemovesPortFile` with socket round trip, v!=2 rejection, oversize line, token >16 KiB, stale port file deleted.

---

### `desktop/internal/secretstore/` (service OS, serialized worker) — NEW

**Analog (partial):** `desktop/internal/themesystem/system_linux.go:27` — private dbus use:
```go
conn, err := dbus.ConnectSessionBus()
if err != nil {
	return watchOmarchyWithoutPortal(changes, Appearance{})
}
```
For the probe use `dbus.SessionBusPrivate()`-style private conn + `NameHasOwner("org.freedesktop.secrets")`, close after. Platform split `probe_linux.go`/`_darwin.go`/`_windows.go`/`_other.go` like `themesystem/system_linux.go` + `system_other.go`. Worker design, error mapping and `provider` interface per RESEARCH Pattern 7. No existing worker-goroutine analog in `desktop/internal`.

---

### `desktop/layout.go` notice stack (component) — MODIFY

**Analog:** `renderNappCard` `layout.go:968-1010` — card record/replay + 12dp inset:
```go
return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{
		Top: unit.Dp(12), Bottom: unit.Dp(12),
		Left: unit.Dp(12), Right: unit.Dp(12),
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			...
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := material.Body1(th, napp.Name)
						label.Font.Weight = font.Bold
```
(read the rest of the function for the `call := macro.Stop()` → `clip.UniformRRect` 8dp → `paint.Fill(card)` → `call.Add` replay). Notice uses `Alignment: layout.Start`, Body2, no `MaxLines`. Path box: 6dp radius, `codeBg`, `text.WrapGraphemes` (copy from `layoutPrompt` code block). Insert as `layout.Rigid` above the `Flexed(1)` content in `layoutMain` (`layout.go:236`) and in the login screen.

### `desktop/login.go` chip + clipboard (component) — MODIFY/REUSE

**Chip helper** (`login.go:113-124`) — copy into a shared top-level helper or reuse for "Dismiss", "Copy path", "Log in again":
```go
chip := func(btn *widget.Clickable, label string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		t := currentTheme()
		b := material.Button(th, btn, label)
		b.Background = t.chipBg
		b.Color = t.chipFg
		b.TextSize = unit.Sp(13)
		b.Inset = layout.UniformInset(unit.Dp(8))
		return b.Layout(gtx)
	}
}
```
**Clipboard in frame** (`login.go:81-85`):
```go
gtx.Execute(clipboard.WriteCmd{
	Type: "application/text",
	Data: io.NopCloser(strings.NewReader(st.NostrConnectURI)),
})
```
**Subtle text** (`login.go:105-111`): `l := emph(material.Body2(th, text)); l.Color = currentTheme().subtle`. Click handling in `update()` (`:54+`) with `go backend.X()` calls — same for `DismissNotice`, `RetryKeyring`, `LoginWithoutKeyring`. S4: swap "Log in" label + ignore submit when `st.KeyringWait == "waiting"`.

### `desktop/main.go` loading screen S3 — MODIFY
Replace bare "Loading…" label with `layout.Center` + vertical flex: `material.Loader` 24dp, Body1 lines, failed state with `material.Button` "Try again" + chip. Pass `Secrets: secretstore.New(...)` in `backend.Options` at the `backend.Start` call (`main.go` ~:159).

### `desktop/lifecycle.go` — MODIFY
`showPendingPrimary` `:65-69`:
```go
phase := backend.Phase()
if phase == backend.PhaseLoading {
	return
}
```
→ also proceed when `backend.Snapshot().KeyringWait != ""` (Pitfall 6 / D-19). For child-unavailable, call `showManager()` after notice added.

### `.github/workflows/desktop.yml` — MODIFY
**Analog:** `test` job `:10-51` (checkout, `actions/setup-go@v5` with `go-version-file: desktop/go.mod`, `cache-dependency-path`). New job `runs-on: windows-2022`, steps `go vet`/`go test` in `backend` and `desktop` `./internal/...` (no child build, no cgo needed). Pitfalls 11 (close bbolt before TempDir cleanup) and 12 (`.gitattributes` eol for fixtures) — see RESEARCH Code Example "Windows CI job".

## Shared Patterns

### Error handling / logging
**Source:** `backend/launcher_state.go:131-141`, `desktop/singleinstance.go:76`
```go
log.Error().Err(err).Msg("failed to write state file")
log.Warn().Err(err).Msg("no instance listener: shortcuts will start a new launcher")
```
Lowercase, no punctuation; `fmt.Errorf("...: %w", err)`; Warn recoverable, Error bugs/integrity failures (child hash mismatch).

### Platform split
**Source:** `desktop/internal/instancelock/lock_unix.go` (`//go:build !windows`) + `lock_windows.go`. Apply to `fileutil/syncdir_*`, `childbin/owner_*`, `instanceipc/*`, `secretstore/probe_*`. Backend must never import go-keyring/go-winio/godbus (GOOS=android = linux).

### Atomic write
`fileutil.WriteFileAtomic` (new) — apply to every writer listed under Pattern Assignment 1 and to `childbin`.

### State mutation + notify
**Source:** `backend/launcher_ui.go:261-266`
```go
func setPhase(phase string) {
	ls.mu.Lock()
	setPhaseLocked(phase)
	ls.mu.Unlock()
	notifyState()
}
```
Apply to notices, KeyringWait, DismissNotice. Never call a SecretStore under `ls.mu`/`stateMu`.

### Tests
Table tests with `t.Run` (`desktop/startup_test.go:12-35`), `t.TempDir()`, `zerolog.Nop()`, backend tests set `statePath` then call `saveState` directly.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `desktop/internal/instanceipc/peercred_*.go`, `ipc_windows.go` | utility | socket auth | No peer-cred or named-pipe code exists; use RESEARCH Pattern 4 + Code Examples |
| `desktop/internal/secretstore` worker | service | serialized async with timeouts | No keyring or worker-queue code; use RESEARCH Pattern 7 |
| libwebview sync test / just copy step | test/config | batch | No `go list -m` based test exists; RESEARCH Pattern 3 |
| `backend/launcher_secrets.go` migration state machine | service | — | Logic is new; RESEARCH Pattern 8 table is the spec |

## Metadata

**Analog search scope:** `backend/` (root, `netguard`, `mobile`), `desktop/` root, `desktop/internal/*`, `.github/workflows/`
**Files scanned:** ~25
**Pattern extraction date:** 2026-10-03
