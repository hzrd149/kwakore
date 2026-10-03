---
status: testing
phase: 02-gated-nap-dispatcher
source: [02-VERIFICATION.md]
started: 2026-10-03T18:31:31Z
updated: 2026-10-03T18:31:31Z
---

## Current Test

number: 1
name: Prompt flood
expected: |
  In the probe napplet, 6 link.open calls produce at most 3 prompts; extras answer rate-limited at once; the window stays responsive
awaiting: user response

## Tests

### 1. Prompt flood
expected: In the probe napplet, 6 link.open calls produce at most 3 prompts; extras answer rate-limited at once; the window stays responsive
result: [pending]

### 2. Second window prompt
expected: While window A holds 3 prompts, window B's prompt still appears and can be answered
result: [pending]

### 3. 30 s prompt expiry
expected: An unanswered prompt disappears after 30 s, Settings shows no remembered rule, and the next request asks again
result: [pending]

### 4. Install during a flood
expected: The store's install confirmation still appears while a napplet floods prompts
result: [pending]

### 5. Probe after approval
expected: The probe's notify and fetch buttons work once approved
result: [pending]

### 6. Bridge-napp prompt overlay (desktop)
expected: Allow and Deny both work, and window.__verdana_prompt_answer is undefined in the napp page
result: [pending]

### 7. Android oversized upload
expected: An upload whose message exceeds 25 MiB gets a too-large answer and nothing hangs
result: [pending]

## Summary

total: 7
passed: 0
issues: 0
pending: 7
skipped: 0
blocked: 0

## Gaps
