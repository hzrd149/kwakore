package netguard

import (
	"context"
	"net/netip"
	"testing"
)

func TestPublicAddr(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.1", "172.20.0.1", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "::1", "fe80::1", "fd00::1", "::ffff:127.0.0.1"} {
		if PublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s counted as public", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !PublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s counted as private", s)
		}
	}
	if err := PublicHost(context.Background(), "localhost"); err == nil {
		t.Error("localhost allowed")
	}
	if err := PublicHost(context.Background(), "[::1]"); err == nil {
		t.Error("::1 allowed")
	}
}
