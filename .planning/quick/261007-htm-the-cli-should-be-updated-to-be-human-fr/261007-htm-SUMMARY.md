---
quick_id: 261007-htm
status: complete
---

# Summary

The CLI now prints readable labeled output by default. `--json` before the command preserves the existing JSON result and structured error output. Documentation and focused tests cover both formats.

Implementation commit: `ec2b210`. Verification passed: `go vet ./...` and `go test ./...` in backend; desktop generation, build, vet and tests; runtime identity scan; `bash -n scripts/*.sh`; `git diff --check`.
