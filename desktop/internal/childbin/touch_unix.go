//go:build !windows

package childbin

import (
	"os"
	"time"
)

// touchDir sets dir's access and modification times to t, marking the
// version directory as in use. The caller has just verified dir as a plain
// directory, so this does not go through a symlink.
func touchDir(dir string, t time.Time) error {
	return os.Chtimes(dir, t, t)
}
