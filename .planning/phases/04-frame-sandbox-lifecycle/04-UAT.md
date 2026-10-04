---
status: testing
phase: 04-frame-sandbox-lifecycle
source: [04-VERIFICATION.md]
started: 2026-10-04T19:52:19Z
updated: 2026-10-04T19:52:19Z
---

## Current Test

number: 1
name: Linux dev tab, adversarial fixture
awaiting: user response

## Tests

### 1. Linux dev tab, adversarial fixture
expected: With just run, loading backend/testdata/adversarial-napplet from the dev tab runs to DONE with no FAIL; "Run reload loop" shows "This napplet keeps reloading itself and was stopped."; dev reload recovers it; the log shows "webkit hardening applied" with webrtc/media_stream/link_preconnect all false and at most one "without the window token" Warn per 5 s
result: [pending]

### 2. Probe-napplet regression
expected: backend/testdata/probe-napplet still passes scope, domains, config, notify controls (once per load), INC ping, intent handler and resource
result: [pending]

### 3. Real napplet reload
expected: A real napplet with relay subscriptions, after location.reload() from the inspector, logs a session reset then a new session, works again, and its old subscriptions stop
result: [pending]

### 4. Napp and settings windows
expected: Napp (35130) windows and settings windows render and work as before
result: [pending]

### 5. Windows (WebView2)
expected: Napplet, napp and settings windows open in one session; the fixture runs from the dev tab; results recorded in CONFORMANCE 5D-NG-webview2 and committed
result: [pending]

### 6. macOS (WKWebView)
expected: Same as 5, recorded in 5D-NG-wkwebview and committed
result: [pending]

### 7. Android
expected: just apk builds and installs; a napplet, a napp and a settings window open
result: [pending]

### 8. CI after push
expected: The desktop job's xvfb "webkit smoke" step and the Android AAR bind are green
result: [pending]

### 9. Hardening-failure notice
expected: With hardening forced to fail in a dev build, the window closes, the "A napplet was closed before it ran" notice shows with the neutral detail, and a try napplet gets no install prompt or reopen entry
result: [pending]

### 10. Prohibition sign-off
expected: The 8 judgment-tier prohibitions in 04-VERIFICATION.md hold
result: [pending]

## Summary

total: 10
passed: 0
issues: 0
pending: 10
skipped: 0
blocked: 0

## Gaps

[none yet]
