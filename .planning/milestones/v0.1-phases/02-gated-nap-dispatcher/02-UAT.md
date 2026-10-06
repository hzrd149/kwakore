---
status: complete
phase: 02-gated-nap-dispatcher
source: [02-VERIFICATION.md]
started: 2026-10-03T18:31:31Z
updated: 2026-10-03T23:20:54Z
---

## Current Test

[testing complete]

## Tests

### 1. Prompt flood
expected: In the probe napplet, 6 link.open calls produce at most 3 prompts; extras answer rate-limited at once; the window stays responsive
result: pass

### 2. Second window prompt
expected: While window A holds 3 prompts, window B's prompt still appears and can be answered
result: pass

### 3. 30 s prompt expiry
expected: An unanswered prompt disappears after 30 s, Settings shows no remembered rule, and the next request asks again
result: pass

### 4. Install during a flood
expected: The store's install confirmation still appears while a napplet floods prompts
result: pass

### 5. Probe after approval
expected: The probe's notify and fetch buttons work once approved
result: pass

### 6. Bridge-napp prompt overlay (desktop)
expected: Allow and Deny both work, and window.__verdana_prompt_answer is undefined in the napp page
result: pass

### 7. Android oversized upload
expected: An upload whose message exceeds 25 MiB gets a too-large answer and nothing hangs
result: pass

## Summary

total: 7
passed: 7
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
