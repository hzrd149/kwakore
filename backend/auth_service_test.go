package backend

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
	"github.com/rs/zerolog"
)

func TestServiceSignerBunkerUserKeyAndSigning(t *testing.T) {
	client := nostr.Generate()
	remote := nostr.Generate()
	user := nostr.Generate()
	inner, err := keyer.New(context.Background(), nil, nip19.EncodeNsec(user), &keyer.SignerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	old := serviceBunkerConnect
	serviceBunkerConnect = func(_, _ context.Context, got nostr.SecretKey, _ string, _ bool) (nostr.Keyer, error) {
		if got != client {
			t.Error("client key changed")
		}
		return inner, nil
	}
	t.Cleanup(func() { serviceBunkerConnect = old })
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	url := "bunker://" + remote.Public().Hex() + "?relay=wss%3A%2F%2Fexample.com&secret=private-sentinel"
	status, err := s.SwitchBunker(context.Background(), url, client, false, nil, func(gotURL, gotKey string) error {
		if gotURL != url || gotKey != client.Hex() {
			t.Error("wrong private credential")
		}
		return nil
	})
	if err != nil || status.PublicKey != user.Public().Hex() || status.PublicKey == remote.Public().Hex() {
		t.Fatalf("identity: %+v %v", status, err)
	}
	evt := &nostr.Event{Kind: 1, Content: "test", CreatedAt: nostr.Now()}
	if err := userKeyer.SignEvent(context.Background(), evt); err != nil || !evt.VerifySignature() {
		t.Fatalf("post-handshake signing: %v", err)
	}
}

func TestServiceSignerBunkerSuperseded(t *testing.T) {
	client := nostr.Generate()
	remote := nostr.Generate()
	user := nostr.Generate()
	inner, err := keyer.New(context.Background(), nil, nip19.EncodeNsec(user), &keyer.SignerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	old := serviceBunkerConnect
	serviceBunkerConnect = func(_, _ context.Context, _ nostr.SecretKey, _ string, _ bool) (nostr.Keyer, error) {
		close(entered)
		<-release
		return inner, nil
	}
	t.Cleanup(func() { serviceBunkerConnect = old })
	s := &ServiceSigner{}
	done := make(chan error, 1)
	url := "bunker://" + remote.Public().Hex() + "?relay=wss%3A%2F%2Fexample.com&secret=private-sentinel"
	go func() { _, err := s.SwitchBunker(context.Background(), url, client, false, nil, nil); done <- err }()
	<-entered
	if _, err := s.Switch(context.Background(), "none", "", nil); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil || s.Status().ConnectionState != "disconnected" {
		t.Fatalf("stale result: %v %+v", err, s.Status())
	}
}

func TestServiceSignerBunkerFixedRemoteError(t *testing.T) {
	old := serviceBunkerConnect
	serviceBunkerConnect = func(_, _ context.Context, _ nostr.SecretKey, _ string, _ bool) (nostr.Keyer, error) {
		return nil, errors.New("private-sentinel")
	}
	t.Cleanup(func() { serviceBunkerConnect = old })
	s := &ServiceSigner{}
	url := "bunker://" + nostr.Generate().Public().Hex() + "?relay=wss%3A%2F%2Fexample.com&secret=private-sentinel"
	_, err := s.SwitchBunker(context.Background(), url, nostr.Generate(), false, nil, nil)
	if err == nil || strings.Contains(err.Error(), "private-sentinel") {
		t.Fatalf("leaked remote failure: %v", err)
	}
}

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

func TestSignerLeakNoSecretInCapturedLog(t *testing.T) {
	var captured bytes.Buffer
	previous := log
	log = zerolog.New(&captured)
	t.Cleanup(func() { log = previous })
	signer := &ServiceSigner{}
	_, err := signer.Switch(context.Background(), "nsec", "private-sentinel", nil)
	if err == nil || bytes.Contains(captured.Bytes(), []byte("private-sentinel")) || bytes.Contains([]byte(err.Error()), []byte("private-sentinel")) {
		t.Fatalf("signer failure leaked: %v %q", err, captured.String())
	}
}
