# Phase 5 — UI Review

**Audited:** 2026-10-06
**Baseline:** 05-UI-SPEC.md (approved)
**Screenshots:** not captured (Gio desktop app, no dev server). This was a code-only audit. The user reported that the live checks passed: dialogs and notices in light and dark, the 280dp unavailable tile, and notice overflow at 560x640 and 1000x720.

---

## Pillar Scores

| Pillar | Score | Key Finding |
|--------|-------|-------------|
| 1. Copywriting | 4/4 | All of the contract's copy (confirm labels, the unavailable status, the reason catalogue, "Opening…") matches word for word and is checked by tests |
| 2. Visuals | 3/4 | Focal order is right. The confirm dialog has no keyboard dismissal or default focus |
| 3. Color | 4/4 | Uses palette fields only. The destructive fill (danger with a bg label) follows UI-D4, and the accent is not used anywhere new |
| 4. Typography | 4/4 | Only H6 Bold, Body2 and Body2 Bold are new. Two weights, no overrides |
| 5. Spacing | 4/4 | New code uses 4/8/12/16/20dp, and 420dp is the declared cap. The 10dp values are the declared pre-existing exceptions |
| 6. Experience Design | 3/4 | The stale guard, busy guards and the Try loading label are present. There is no Escape/Enter handling on the destructive dialog, and the busy state is only a label change |

**Overall: 22/24**

---

## Top 3 Priority Fixes

1. **WARNING: The confirm dialog can only be used with a pointer.** A keyboard user cannot dismiss or confirm it. In `layoutConfirm` (desktop/layout.go:748), add a `key.Filter{Name: key.NameEscape}` that triggers the dismiss button, and give the dismiss chip the initial focus so Enter cannot fire the destructive action by default.
2. **WARNING: "Opening…" is the only busy feedback for Try.** A slow blob download looks stalled, because there is no progress and no spinner. Keep the label but add an indeterminate `material.Loader` (16dp) inside the busy Try button, or show elapsed seconds after 5s (desktop/store_unavailable.go `tryLabel`).
3. **WARNING: The update confirm body never names the napplet.** The update body (desktop/store_confirm.go:76) says only "this napplet". If a profile-list update is clicked while another dialog was just replaced, the user cannot tell which napplet they are confirming. Interpolate the sanitized display name (for example "Updating {name} resets its saved data…"), using the same fallback that `confirmName` already provides for the title.

---

## Detailed Findings

### Pillar 1: Copywriting (4/4)
- The S1 and S2 labels "Update and reset data" / "Keep current version" and "Uninstall napplet" / "Keep napplet" are at desktop/store_confirm.go:73-77 and are asserted in store_confirm_test.go:126-133.
- The S3 strings are at desktop/store_unavailable.go:20-25. The reason catalogue in backend/napplet.go:130-135 is fixed, in sentence case, and has no trailing period. The period is added at render time, which matches the spec.
- The trial-failed detail (backend/launcher_notices.go:91) is specific and actionable.
- The name fallback goes to "Unnamed napplet" and never to the address (store_unavailable_test.go:85-91).
- Minor: see fix 3 (the update body does not name the napplet).

### Pillar 2: Visuals (3/4)
- The dialog replaces the whole store view, so its focal point is clear: H6 Bold title, then the filled danger button (layout.go:784-805).
- The unavailable block puts the bold danger status first, which matches the focal rule.
- No icons were added, as the contract requires.
- Deduction: the dialog has no focus or keyboard affordance (fix 1), so a keyboard user has no visible entry point to it.

### Pillar 3: Color (4/4)
- `layoutConfirmCard` and `layoutUnavailableBlock` read `currentTheme()` on every frame.
- No hex literals were added in the phase files.
- The danger fill with a `p.bg` label replaces the logout dialog's 4.49:1 chip pairing, as UI-D4 specifies.
- Accent (`contrastBg`) is not used on the confirm dialogs, the unavailable state or the notices.

### Pillar 4: Typography (4/4)
- New sizes: 20sp (H6), 14sp (Body2 and the default Button). Tile and card buttons keep the existing 13sp.
- Weights: Regular, plus Bold through `font.Bold`. No italic and no line-height overrides.
- `MaxLines` is set on tiles and cards and is 0 on the napp page, as specified.

### Pillar 5: Spacing (4/4)
- Confirm dialog: 20dp padding, 8dp between title and body, 16dp above the button row, 12dp between the buttons, corner radius 10. These match the spec exactly.
- Unavailable block: 4dp between lines.
- The 10dp and 6dp hits in grid.go, layout.go and detail.go are the pre-existing exceptions that the spec lists. No new 2, 3, 5, 6 or 10dp values were found in the phase-added `store_confirm.go` or `store_unavailable.go`.

### Pillar 6: Experience Design (3/4)
Present:
- confirmations for destructive update and uninstall;
- the stale guard (no longer installed, busy, or `UpdateAvailable` cleared);
- the confirmation is cleared on DestroyEvent;
- clicks are ignored while busy;
- refusals behind the UI;
- session-only notices with a cap of 3.

Gaps:
- No keyboard path through the destructive dialog (fix 1).
- The loading state is a text swap only (fix 2).
- There is no feedback after a successful uninstall beyond the tile disappearing. This is acceptable, but a transient notice would make it explicit.

Registry audit: not applicable (no shadcn).

---

## Files Audited
- desktop/layout.go (layoutConfirm, layoutConfirmCard)
- desktop/store_confirm.go, desktop/store_confirm_test.go
- desktop/store_unavailable.go, desktop/store_unavailable_test.go
- desktop/detail.go, desktop/grid.go, desktop/store_layout.go (spacing scan)
- backend/napplet.go (reason catalogue), backend/launcher_notices.go
