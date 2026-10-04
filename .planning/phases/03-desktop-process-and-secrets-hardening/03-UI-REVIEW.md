# Phase 3 — UI Review

**Audited:** 2026-10-04
**Baseline:** 03-UI-SPEC.md (approved design contract)
**Screenshots:** not captured. Gio desktop app with no dev server; this is a code-level audit. The user ran the live smoke checks (light/dark, 560x640 overflow) and reported "all good".

---

## Pillar Scores

| Pillar | Score | Key Finding |
|--------|-------|-------------|
| 1. Copywriting | 3/4 | Every string matches the contract exactly; "Log in again" is ambiguous because it opens the login screen and does not log in |
| 2. Visuals | 3/4 | Card, path box and loader match S1/S3; the Rigid chip column squeezes the text column at 560dp, and the waiting "Log in" button looks active |
| 3. Color | 3/4 | Palette fields only, no hex literals, accent limited to the three declared elements; Omarchy/system palettes have no contrast check |
| 4. Typography | 4/4 | Only 16/14/13sp and Regular/Bold, as declared |
| 5. Spacing | 4/4 | Only 4/8/12/16dp gaps, 24dp loader, 8dp/6dp radii, 420dp cap; no new 6 or 10dp |
| 6. Experience Design | 3/4 | States covered, but the S4 login waiting state cannot be reached, there is no pending feedback on Try again or Dismiss, and the Rigid stack can starve the scroll area |

**Overall: 20/24**

---

## Top 3 Priority Fixes

1. **WARNING: the "Log in" button looks clickable while it ignores clicks** (desktop/login.go:89, 178-181). The button keeps the accent fill and the pointer cursor, and only the label changes. A user who clicks gets no response. Fix: while `KeyringWait == "waiting"`, draw it with `chipBg`/`subtle`, or use the Gio disabled pattern (`gtx = gtx.Disabled()` around the button), and skip `pointer.CursorPointer`.
2. **WARNING: the notice stack is Rigid and not scrollable** (desktop/layout.go:273, desktop/login.go:220-222). With 3 notices, a long corrupt path that wraps by grapheme, and a narrow text column (the two-chip column takes about 170dp of a 528dp content width), the stack can take most of 640dp. On HiDPI or with larger text it can push the `Flexed(1)` list to zero height. The smoke check passed only at default scale. Fix: cap the stack height (for example at 50% of `Constraints.Max.Y`) and put it in its own `material.List`, or put the chips under the text when `Max.X < ~480dp`.
3. **WARNING: there is no feedback between a click and the backend's answer** (desktop/layout.go:1210-1216, desktop/notices.go:96-97). "Try again" and "Log in again" launch goroutines, but the failed screen stays the same until the backend changes `KeyringWait`. A double click starts two `RetryKeyring` calls. Fix: set a local `pending` flag on click that shows the loader and ignores further clicks until `wait` changes. Treat Dismiss the same way by hiding the card optimistically with a local `dismissed[id]`.

---

## Detailed Findings

### Pillar 1: Copywriting (3/4)
- PASS: backend/launcher_notices.go:43-54 matches the contract strings byte for byte, including the single-character ellipsis and sentence case. The loading copy in desktop/layout.go:1183-1197 and the login copy in desktop/login.go:66 and :151 also match.
- PASS: no raw errors, hashes or D-Bus names appear in the notice copy.
- WARNING: "Log in again" (layout.go:1263) suggests it repeats the old login. What it does is leave the keyring login in place and open the manual login screen. "Log in another way" or "Use a different login" would say what happens. The contract chose this label, so this is a recommendation for the next copy revision.
- MINOR: "Copied" stays for the rest of the process (notices.go:150) and is set even if the clipboard write fails silently. This follows the spec, but the copy promises more than the code checks.

### Pillar 2: Visuals (3/4)
- PASS: notices.go:135-166. The card uses an 8dp RRect, `card` fill and a 12dp inset. Its horizontal flex has `Alignment: layout.Start`, then the text column, a 12dp gap and the chips. The path box (notices.go:197-210) uses a 6dp radius, `codeBg` and `WrapGraphemes`.
- PASS: the focal point on the failed screen is the bold title plus the accent "Try again" button, with no loader drawn (layout.go:1223, 1237).
- WARNING: in the corrupt-state card, the Rigid chip row ("Copy path", 8dp, "Dismiss") at 13sp takes a large share of a 560dp window. The path box therefore wraps into a tall, narrow column, which weakens the hierarchy and makes the stack taller (see fix 2).
- WARNING: the waiting "Log in" button keeps its full accent styling (login.go:178-181). It gives no visual sign that it is inactive.
- MINOR: the path box uses the proportional `vFont`. The spec does not require monospace, but a long path in a proportional font is harder to read by hand.

### Pillar 3: Color (3/4)
- PASS: notices.go and the new layoutLoading code read only `currentTheme()` fields (`card`, `fg`, `danger`, `subtle`, `codeBg`, `codeFg`, `chipBg`, `chipFg`). There are no hex literals.
- PASS: accent appears only on the loader (layout.go:1228), "Try again" (:1260) and the existing "Log in" button. "Dismiss", "Copy path" and "Log in again" are chips (`chipButton`, notices.go:57-68).
- PASS: severity is shown only through the title color, `danger` vs `fg` (notices.go:177-180).
- WARNING: the spec says outright that Omarchy/system overrides were not checked for contrast. `subtle` on `card` is only 5.13:1 in light mode, so an OS-accent or Omarchy palette could drop the detail text below AA. No code guards against this.

### Pillar 4: Typography (4/4)
- Sizes in use: Body1 16sp (loading titles), Body2 14sp (notice title, detail, path, waiting detail, login waiting line), 13sp chips, and the default Button at 14sp.
- Weights in use: Regular, plus `font.Bold` on notice titles (notices.go:176) and the failed title (layout.go:1237-1239). There is no italic, and LineHeight is not overridden anywhere.
- Nothing outside the declared 3 sizes and 2 weights. This pillar has no deviations to report.

### Pillar 5: Spacing (4/4)
- Gaps used: 4dp (title to detail to path, notices.go:186 and :196; layout.go:1244), 8dp (between notices at notices.go:113, between chips at :155, path inset at :199, between buttons at layout.go:1262), 12dp (card inset at notices.go:139, text-to-chips at :144, loader gap at layout.go:1230), and 16dp (below the stack at notices.go:119, above the buttons at layout.go:1255).
- 24dp loader (layout.go:1226), 420dp cap (:1220). An empty stack returns zero `Dimensions` with no spacer (notices.go:90-92), as S1 requires.
- No new 6dp or 10dp values. The `space(6)` at login.go:204 is pre-existing code the spec allows.

### Pillar 6: Experience Design (3/4)
- PASS: the loading, waiting, failed, empty, populated, partial (deduplicated by ID at notices.go:75-83) and long-text states are all handled. Keyring calls run off the frame goroutine. Clicks on the failed screen are ignored once the state has moved on (layout.go:1210). Submit and click are drained even while waiting (login.go:85-89).
- PASS: `showPendingPrimary` raises the manager once per KeyringWait value (lifecycle.go:77-108), so a startup wait never stays hidden and focus is not stolen repeatedly.
- WARNING: the S4 login waiting state cannot be reached today. login.go:61-63 says so: `login()` saves while in PhaseLoading. The button label and the waiting line exist only for a future change, and the live smoke test could not cover them.
- WARNING: Try again, Log in again and Dismiss give no pending or optimistic feedback, and double clicks can start duplicate goroutines (fix 3).
- WARNING: a Rigid notice stack with no scroll can hide the content below it at high scale (fix 2). The user's smoke check at 1x does not rule this out.
- INFO: no destructive actions were added. Dismissing a notice deletes no files, so no confirmation dialog is needed.

Registry audit: not applicable (Go/Gio, no components.json).

---

## Files Audited
- desktop/notices.go
- desktop/layout.go (loadingContent, layoutLoading, layoutMain notice placement)
- desktop/login.go
- desktop/lifecycle.go
- backend/launcher_notices.go
- .planning/phases/03-desktop-process-and-secrets-hardening/03-UI-SPEC.md, 03-10-SUMMARY.md
