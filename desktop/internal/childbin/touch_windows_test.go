//go:build windows

package childbin

import (
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// another launcher listing the version directory (collect's ReadDir) and
// Explorer watching it (a FILE_LIST_DIRECTORY handle) keep it open while
// this build spawns. The refresh must still succeed and EnsureVersion must
// not report the directory as unavailable.
func TestEnsureVersionRefreshesWhileDirIsOpen(t *testing.T) {
	base := cacheDir(t)
	exe := newFile("child-new", childData, true)
	lib := newFile("webview.dll", []byte("library"), false)
	dir := mustEnsureVersion(t, base, exe, lib)
	age(t, dir, 2*staleAfter)

	// another launcher's os.ReadDir(dir)
	reader, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	// a ReadDirectoryChangesW watcher such as Explorer
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(watcher)

	got, err := EnsureVersion(base, []File{exe, lib})
	if err != nil {
		t.Fatalf("EnsureVersion with the directory open elsewhere: %v", err)
	}
	if got != dir {
		t.Fatalf("EnsureVersion returned %s, want %s", got, dir)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(fi.ModTime()) > time.Minute {
		t.Fatalf("version dir time %v was not refreshed", fi.ModTime())
	}
}
