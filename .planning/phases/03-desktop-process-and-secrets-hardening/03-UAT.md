---
status: testing
phase: 03-desktop-process-and-secrets-hardening
source: [03-VERIFICATION.md]
started: 2026-10-04T05:36:14Z
updated: 2026-10-04T05:36:14Z
---

## Current Test

number: 1
name: Windows CI first run
awaiting: user response

## Tests

### 1. Windows CI first run
expected: After pushing, the "test (windows)" job is green, including TestPipeRoundTrip, TestPipeDACLIsOwnerOnly, TestPipeDialRefusesForeignServer, TestEnsureRefusesSymlinkedDir, TestEnsureVersionRefreshesWhileDirIsOpen and the bbolt backend tests
result: [pending]

### 2. Real keyring round trip
expected: With GNOME Keyring or KeePassXC, secret-tool search service Verdana shows the item, state.json has no client_key or login, and a restart resumes the login
result: [pending]

### 3. Locked keyring at start
expected: About 1 s in, the manager opens with "Waiting for your system keyring…"; dismissing the unlock prompt shows "Couldn't reach your system keyring" with Try again and Log in again; Try again shows one prompt; neither button deletes the item
result: [pending]

### 4. No Secret Service
expected: On Hyprland or sway, the "Secure storage unavailable" notice shows, the login survives a restart, and Dismiss persists
result: [pending]

### 5. Corrupt state.json
expected: Verdana starts with defaults; the notice shows the full .corrupt-<unix> path; Copy path becomes Copied; Dismiss persists and the file stays
result: [pending]

### 6. Symlinked child cache dir (prod build)
expected: Opening from the store, the tray Settings and a shortcut shows "Napp windows can't open", raises the manager and adds the store launch-failed line; no duplicate card; after Dismiss it returns on the next failure; restoring the dir fixes napps
result: [pending]

### 7. Linux single-instance forwarding
expected: A second launch and a shortcut click reach the running instance; ss -xl shows the socket, ss -tln shows no instance listener; no launcher.port
result: [pending]

### 8. Real Windows
expected: Pipe forwarding works; child-<sha>.exe and webview.dll are under %LocalAppData%\Verdana\child\<version>; windows render; login in Credential Manager; a second account can't open the pipe; opening a napp works while Explorer has the version folder open
result: [pending]

### 9. macOS
expected: Windows render with libwebview.dylib from the cache dir; a second launch forwards; the keychain item exists
result: [pending]

### 10. Overflow, screenshots and themes
expected: At 560x640 with three notices the windows list scrolls and the profile row stays visible; light and dark palettes are used, the error notice title is in the danger color; screenshots taken for the PR
result: [pending]

### 11. Android
expected: just apk builds and installs; login, resume and links work; no notices or keyring screens appear
result: [pending]

### 12. Log in again with nostrconnect (CR-01)
expected: With the keyring down, Log in again then a nostrconnect login completes, is saved to the file with the fallback notice, the keyring item survives, and the next start with the keyring back resumes the newer file login
result: [pending]

### 13. Lost state with a locked keyring (WR-01)
expected: A corrupt state.json with the keyring locked shows the failed screen, never the login screen; quit and restart still waits; after unlocking the item resumes unchanged
result: [pending]

### 14. Two builds at once
expected: Two builds or data dirs opening napps at the same time never hit the "can't open" notice
result: [pending]

## Summary

total: 14
passed: 0
issues: 0
pending: 14
skipped: 0
blocked: 0

## Gaps

[none yet]
