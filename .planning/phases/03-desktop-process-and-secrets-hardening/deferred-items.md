# Phase 03 Deferred Items

Out-of-scope discoveries logged during execution. Not fixed by the plan that found them.

## From 03-02

- **Flaky `TestSyncAppShortcutsCreatesAndReconcilesDesktopEntries` (desktop/internal/osintegration).**
  `RefreshShortcutParent` (`desktop/internal/osintegration/shortcutfile.go:47-48`) starts
  `update-desktop-database <dir>` with `exec.Command(...).Start()` and never waits. On machines that
  have the tool, it can still be writing `mimeinfo.cache` into the test's `t.TempDir()` when cleanup
  runs, which fails once with `unlinkat .../xdg/applications: directory not empty` (seen 1 time in
  about 12 runs). The process is also never reaped. A fix would route it through a seam like
  `desktop/host.go` `startCommand` (Start plus `go c.Wait()`) and stub it in tests. This was already
  the case before 03-02; it is not caused by the switch to `fileutil.WriteFileAtomic`.
