---
status: partial
phase: 05-napplet-artifact-identity-and-storage-keying
source: [05-VERIFICATION.md]
started: 2026-10-05T19:54:53Z
updated: 2026-10-06T17:10:03Z
---

## Current Test

[Linux checks 1–18 complete — all passed; 19–21 deferred]

## Tests

### 1. Old data dir
expected: Start on a data dir from an earlier build with napplets installed: the reinstall notice shows once; napplet-storage/ and config/ have no old files; storage/ has no napplet-* or napplet~* files but keeps napp files; napps/ untouched; nothing is swept when a state.json.corrupt-* copy exists; a restart shows no notice
result: pass

### 2. Persistence
expected: Napplet storage and settings survive a relaunch (one 64-hex file each in napplet-storage/ and config/); napp localStorage survives a relaunch
result: pass

### 3. Update confirmation
expected: From the installed tile, napp page and profile list: the dialog reads as specified; Keep current version does nothing; confirming resets data and settings; an old-version window keeps working and its files go when it closes; the dialog closes if the update disappears; napps update in one click
result: pass

### 4. Two settings windows
expected: After an update, an old-version window's gear and the store Settings button show separate values
result: pass

### 5. Uninstall
expected: Uninstall napplet with a window open: busy, window closes, tile removed, data/settings/permissions deleted; reinstall starts empty and asks for permissions again
result: pass

### 6. Unavailable
expected: A newer invalid manifest shows Unavailable with a reason on cards, tiles and the napp page; installed copy says it still works and opens; discovery and author page list it once; 280dp tile screenshot light/dark
result: pass

### 7. Invalid naddr
expected: Opening an naddr whose latest manifest is invalid shows no install prompt and says the latest version is invalid
result: pass

### 8. Offline launch
expected: An installed napplet opens instantly offline with no notice and unchanged store state
result: pass

### 9. Try
expected: Opening… until every file verifies; a blocked blob opens nothing and shows the fixed trial-failed line with no raw detail
result: pass

### 10. Trial promotion
expected: Same version keeps trial data; existing data or a changed version drops it with the trial-data notice in manager and store
result: pass

### 11. Requires warning
expected: A NIP-5D napplet with an unknown requires domain opens and shows the notice in both windows; Dismiss clears both; a WEB-NAPPLET with unknown R tags raises nothing
result: pass

### 12. Notice overflow
expected: Manager 560x640 and store 1000x720 with several notices still scroll and every control is reachable (screenshots)
result: pass

### 13. Blossom trust
expected: A LAN/localhost server added in settings serves installs and updates; a manifest-only LAN server is refused and sees no request
result: pass

### 14. Default servers
expected: Install, update, Try and icons work with the default servers
result: pass

### 15. Bad NIP-5D source
expected: A nostr: or relative source shows unavailable with the source reason
result: pass

### 16. Linux shortcuts
expected: Expose installed apps: .desktop Exec and X-Verdana-Napp-ID hold tokens under the same names; menu launches work; new bundles open (old-build bundles not tested: no deployments); no zombie update-desktop-database processes
result: pass

### 17. Dialogs
expected: Logout (red button), update and uninstall dialogs look right in light and dark (screenshots)
result: pass

### 18. Staged install
expected: Killing the launcher mid-install or mid-update leaves the old copy launchable; leftover staging/.old dirs are cleared next install
result: pass

### 19. Windows
expected: Start-menu Apps/Discover links carry tokens and launch; a d ending in \ launches; curly quotes, $(calc) or ; in titles/descriptions produce correct links and run nothing; a 300-char title and huge description are bounded and other entries still written; Signal/Sıgnal/ſignal get distinct links; an update succeeds while the install dir is held open
result: deferred — needs Windows/macOS/Android; carried to milestone audit

### 20. macOS
expected: Apps .app bundles carry tokens and launch; a Spotlight Discover result opens a trial; NFC/NFD Café titles are distinct bundles; a long title is cut and other entries still written
result: deferred — needs Windows/macOS/Android; carried to milestone audit

### 21. Android (optional)
expected: just apk builds, trial prompt unchanged, uninstall removes data; WR-01 pending-close is a known open issue
result: deferred — needs Windows/macOS/Android; carried to milestone audit

## Summary

total: 21
passed: 18
issues: 0
pending: 0
skipped: 3
blocked: 0

## Gaps

- Items 19–21 (Windows, macOS, Android) not yet run; carried to the milestone audit. Migration-only checks dropped (no deployments).
