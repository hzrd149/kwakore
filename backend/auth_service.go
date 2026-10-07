package backend

import (
	"context"
	"errors"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
)

// SignerStatus is the complete public signer read surface.
type SignerStatus struct {
	Mode            string `json:"mode"`
	PublicKey       string `json:"public_key"`
	ConnectionState string `json:"connection_state"`
}

type ServiceSigner struct {
	mu         sync.Mutex
	generation uint64
	status     SignerStatus
}

func (s *ServiceSigner) Generation() uint64   { s.mu.Lock(); defer s.mu.Unlock(); return s.generation }
func (s *ServiceSigner) Status() SignerStatus { s.mu.Lock(); defer s.mu.Unlock(); return s.status }
func (s *ServiceSigner) Switch(ctx context.Context, mode, secret string, persist func(string, string) error) (SignerStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.stopLocked()
	s.status = SignerStatus{Mode: mode, ConnectionState: "disconnected"}
	failed := errors.New("signer unavailable")
	if mode != "none" && mode != "nsec" {
		return s.status, failed
	}
	if persist != nil && persist("none", "") != nil {
		return s.status, failed
	}
	if mode == "none" {
		return s.status, nil
	}
	if len(secret) > 256 {
		return s.status, failed
	}
	prefix, _, err := nip19.Decode(secret)
	if err != nil || prefix != "nsec" {
		return s.status, failed
	}
	sessionCtx, cancel := context.WithCancel(context.Background())
	// The local nsec path does not use a relay pool.
	k, err := keyer.New(sessionCtx, nil, secret, &keyer.SignerOptions{})
	if err != nil {
		cancel()
		return s.status, failed
	}
	pk, err := k.GetPublicKey(ctx)
	if err != nil || ctx.Err() != nil {
		cancel()
		return s.status, failed
	}
	if persist != nil && persist("nsec", secret) != nil {
		cancel()
		return s.status, failed
	}
	sessionCancel = cancel
	userKeyer = k
	userPubkey = pk
	s.status = SignerStatus{Mode: "nsec", PublicKey: pk.Hex(), ConnectionState: "connected"}
	pushIdentityChanged()
	return s.status, nil
}

// Close ends the live session without changing retained credentials.
func (s *ServiceSigner) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	s.status = SignerStatus{Mode: "none", ConnectionState: "disconnected"}
}

func (s *ServiceSigner) stopLocked() {
	stopNostrConnect()
	CloseAllWindows()
	if sessionCancel != nil {
		sessionCancel()
		sessionCancel = nil
	}
	userKeyer = nil
	userPubkey = nostr.ZeroPK
	pushIdentityChanged()
	stopUserRelays()
	setProfile("", "", "")
}
