//go:build !windows

package childbin

import (
	"io/fs"
	"os"
	"syscall"
)

// checkModes: Unix permission bits are real, so Ensure checks and sets them.
const checkModes = true

// getuid is a test seam.
var getuid = os.Getuid

// ownedByUs says whether the file or directory belongs to the current user.
func ownedByUs(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == getuid()
}

// plain says whether fi is not a symlink or other special entry. On Unix the
// mode already says everything; IsDir/IsRegular checks do the rest.
func plain(fi fs.FileInfo) bool {
	return fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}
