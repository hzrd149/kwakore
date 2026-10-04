//go:build windows

package childbin

import (
	"io/fs"
	"os"
	"syscall"
)

// checkModes: Windows synthesizes Unix permission bits from the read-only
// attribute, so they say nothing about who can write. Access is governed by
// the ACL that %LocalAppData% passes down to everything under it, which only
// lets the user (plus SYSTEM and Administrators) in. Ensure relies on that
// and on the hash instead.
const checkModes = false

// getuid is a test seam; it is unused on Windows.
var getuid = os.Getuid

// ownedByUs is always true: Lstat carries no owner on Windows, and the
// inherited ACL of the cache directory is what keeps other users out.
func ownedByUs(fs.FileInfo) bool { return true }

// plain rejects symlinks, junctions and any other reparse point, so Ensure
// never verifies or runs a file through a link someone else could retarget.
func plain(fi fs.FileInfo) bool {
	if fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return false
	}
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok &&
		d.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	return true
}
