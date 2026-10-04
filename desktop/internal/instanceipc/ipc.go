// Package instanceipc is the channel a second Verdana launch uses to hand its
// command (open the manager, run a shortcut, try a napplet) to the launcher
// already running on the same data directory.
//
// It used to be a localhost TCP port published in a world-readable port file,
// which any local user could connect to (triggering shortcuts) or squat (to
// receive a shortcut token). Now it is reachable only by the user who owns the
// running launcher:
//
//   - on Unix, a socket file with mode 0600 inside a 0700, user-owned,
//     non-symlink directory, and every connection's peer uid is checked on
//     both ends (SO_PEERCRED on Linux, LOCAL_PEERCRED on macOS);
//   - on Windows, a named pipe whose DACL grants access to the current user's
//     SID only, and the client checks that the pipe server runs as that same
//     SID before sending anything.
//
// Listen removes a stale socket left behind by a launcher that died, so it
// must only be called while holding the instancelock: holding the lock is what
// proves nobody else is listening. Dial never removes anything.
package instanceipc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ErrNoInstance is returned by Dial when no launcher is listening for this
// data directory, so the caller should become the running instance.
var ErrNoInstance = errors.New("no running instance")

// dataDirHash names the channel for one data directory. The full hex digest
// is returned; callers cut it to the length their name needs.
func dataDirHash(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
