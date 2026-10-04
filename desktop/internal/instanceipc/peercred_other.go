//go:build !linux && !darwin && !windows

package instanceipc

import "net"

// peerUID has no portable peer-credential call to make on the remaining Unix
// systems, so it reports our own uid. What keeps other users out there is the
// directory: the socket lives in a 0700 directory we own (see verifyDir), so
// nobody else can reach it to connect, and nobody else can have bound it.
func peerUID(c *net.UnixConn) (uint32, error) {
	return uint32(getuid()), nil
}
