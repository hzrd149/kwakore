//go:build windows

package childbin

import (
	"time"

	"golang.org/x/sys/windows"
)

// touchDir sets dir's access and modification times to t, marking the
// version directory as in use.
//
// It does not use os.Chtimes: Go opens the path for that with FILE_SHARE_WRITE
// alone, and follows a reparse point at it. A failed refresh fails
// EnsureVersion (the user sees the "Reinstall Verdana" notice), so the
// refresh must not depend on who else has the directory open: another
// launcher of the same build listing it in collect, Explorer showing the
// folder, or anything watching it with ReadDirectoryChangesW. This handle
// asks only for FILE_WRITE_ATTRIBUTES, which is not data access, and shares
// read, write and delete, so neither side's share mode can refuse the other
// and the open cannot fail with a sharing violation.
//
// That is why a failure here stays fatal on Windows as on Unix. Skipping a
// refresh that failed would let a directory nobody touched for staleAfter
// look stale again as soon as base/.lock is released: another build could
// then collect it between the verification and the exec, which is the race
// the refresh exists to close. With the sharing question gone, what is left
// (the directory vanished, access denied, an I/O error) is a real problem.
//
// FILE_FLAG_OPEN_REPARSE_POINT opens a link itself rather than its target,
// so even if dir were swapped for a junction after verifyDir, the refresh
// would not reach through it.
func touchDir(dir string, t time.Time) error {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	ft := windows.NsecToFiletime(t.UnixNano())
	return windows.SetFileTime(h, nil, &ft, &ft)
}
