package backend

import (
	"context"
	"errors"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

func TestServiceSignerNsecTransition(t *testing.T) {
	signer := &ServiceSigner{}
	first := nostr.Generate()
	second := nostr.Generate()
	old, err := signer.Switch(context.Background(), "nsec", nip19.EncodeNsec(first), func(string, string) error { return nil })
	if err != nil || old.PublicKey != first.Public().Hex() || old.ConnectionState != "connected" {
		t.Fatalf("first switch: %+v %v", old, err)
	}
	oldHandle := userKeyer
	status, err := signer.Switch(context.Background(), "nsec", nip19.EncodeNsec(second), func(string, string) error { return nil })
	if err != nil || status.PublicKey != second.Public().Hex() || status.ConnectionState != "connected" || signer.Generation() != 2 {
		t.Fatalf("second switch: %+v %v", status, err)
	}
	if err := oldHandle.SignEvent(context.Background(), &nostr.Event{}); err == nil || err.Error() != "signer unavailable" {
		t.Fatalf("old captured signer still usable: %v", err)
	}
	_, err = signer.Switch(context.Background(), "nsec", "bad secret sentinel", func(string, string) error { return nil })
	if err == nil || err.Error() != "signer unavailable" || signer.Status().ConnectionState != "disconnected" {
		t.Fatalf("invalid switch leaked or retained signer: %v %+v", err, signer.Status())
	}
	_, err = signer.Switch(context.Background(), "nsec", nip19.EncodeNsec(first), func(string, string) error { return errors.New("private sentinel") })
	if err == nil || err.Error() != "signer unavailable" || signer.Status().ConnectionState != "disconnected" {
		t.Fatalf("persistence failure leaked or retained signer: %v %+v", err, signer.Status())
	}
}
