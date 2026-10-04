//go:build linux

package instanceipc

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerUID reports the uid of the process on the other end of c, as the
// kernel recorded it when the connection was made (SO_PEERCRED).
func peerUID(c *net.UnixConn) (uint32, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return cred.Uid, nil
}
