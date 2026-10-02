// Package netguard keeps requests the launcher makes for napplets on the
// public internet.
package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
)

// Napplets have no network of their own: when the launcher reaches out on
// their behalf (a relay they named, a URL they want bytes from), it must not
// become their way into the user's machine or LAN. These checks keep those
// requests on the public internet.

var ErrPrivateAddress = errors.New("address is not public")

// blockedPrefixes are the ranges that are never a napplet's business:
// loopback, private, link-local (including cloud metadata), CGNAT,
// multicast, and the IPv6 equivalents.
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "100::/64", "2001:db8::/32",
		"fc00::/7", "fe80::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// PublicAddr says whether an address is on the public internet.
func PublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// PublicHost resolves a host name and fails unless every address it has is
// public (an attacker-controlled name may resolve to both).
func PublicHost(ctx context.Context, host string) error {
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	lower := strings.ToLower(host)
	if lower == "" || lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return ErrPrivateAddress
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !PublicAddr(addr) {
			return ErrPrivateAddress
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return ErrPrivateAddress
	}
	for _, a := range addrs {
		if !PublicAddr(a) {
			return ErrPrivateAddress
		}
	}
	return nil
}

// DialContext dials only public addresses: the check happens on the
// address actually being connected to, after resolution, so DNS rebinding
// and redirects to private hosts are caught on every connection.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	var lastErr error = ErrPrivateAddress
	for _, a := range addrs {
		if !PublicAddr(a) {
			lastErr = ErrPrivateAddress
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
