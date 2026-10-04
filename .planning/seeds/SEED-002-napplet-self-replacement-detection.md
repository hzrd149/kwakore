---
id: SEED-002
status: dormant
planted: 2026-10-04
planted_during: Phase 04 (Frame Sandbox Lifecycle) — hardening & conformance milestone
trigger_when: a webview engine exposes a per-document or document-replaced signal for sandboxed srcdoc sub-frames, or NIP-5D tightens the reload clause
scope: small-to-medium
---

# SEED-002: Detect a napplet replacing its own document without a navigation or load

## Why This Matters

CONFORMANCE row `NIP-5D-reload-residual` (MUST, open) is not satisfied. A napplet can replace its own frame document in three ways that the Phase 4 host page cannot detect:

- a `javascript:` navigation before the first load
- a `javascript:` navigation after the first load
- `document.open()` with no `close()`

Phase 4 detects a replaced document by a second `load` event or a second document-start marker. None of these three cases reliably produces either. The replacing document keeps the existing session but has no `window.napplet`.

Measured on WebKitGTK 2.52.6, the CSP and the sandbox still hold in these cases, and the attacker listener saw 0 connections. So this is the napplet's own code continuing under its own session, not a containment escape. It is still a gap against the NIP-5D reload MUST.

The user chose to record this as a residual risk in Phase 4 (review CR-01, 2026-10-04) instead of attempting an engine-level fix.

## When to Surface

**Trigger:** any of these:

- A webview engine exposes a signal that sub-frame documents were replaced (for example a WebKitGTK frame or document API, or a WebView2 frame-navigation event that covers `javascript:` URLs).
- NIP-5D changes its reload clause.
- A milestone takes up napplet sandbox work again.

## Scope Estimate

**Small to medium.** The work is an engine signal hooked up through purego or WebView2, plus a rebuild through the existing `replaced(frame)` path. A fixture step already exists for each case: `nav-js`, `doc-open-unclosed`, and `TestWebKitNappletJavascriptBeforeLoad`.

## Breadcrumbs

- `spec/CONFORMANCE.md` rows `NIP-5D-reload-residual`, `5D-3`, `NIP-5D-reload`, `DEC-5`, `5D-NG-webkitgtk`
- `backend/webview/napplet-host.js` — `replaced()`, load and marker counting
- `desktop/child/harden_linux.go` — purego access to the `WebKitWebView`
- `backend/testdata/adversarial-napplet/index.html` — `nav-js`, `doc-open-unclosed`, and the `nav-js-early` mode
- `.planning/phases/04-frame-sandbox-lifecycle/04-REVIEW.md` CR-01, `04-RESEARCH.md` (the pagehide signal was tried and never arrives on WebKitGTK)
