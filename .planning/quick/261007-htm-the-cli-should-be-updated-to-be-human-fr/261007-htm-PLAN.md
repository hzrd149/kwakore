---
quick_id: 261007-htm
status: complete
---

# Human-friendly CLI output with optional JSON

1. Add a global `--json` option and preserve the existing success and error JSON contracts when selected.
2. Render labeled, readable default results and errors, including nested lists and objects, while escaping terminal control characters.
3. Update CLI documentation and test both output modes; run backend and desktop checks.
