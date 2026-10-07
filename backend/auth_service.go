package backend

import (
	"context"
	"errors"
	"sync"
	"time"

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
	keyer      *revocableKeyer
}

// revocableKeyer keeps a captured old service signer from signing after a
// completed transition. The write lock waits for any in-flight operation.
type revocableKeyer struct {
	mu     sync.RWMutex
	active bool
	inner  nostr.Keyer
}

var errServiceSignerUnavailable = errors.New("signer unavailable")

func (k *revocableKeyer) revoke() { k.mu.Lock(); k.active = false; k.mu.Unlock() }
func (k *revocableKeyer) GetPublicKey(ctx context.Context) (nostr.PubKey, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.active {
		return nostr.ZeroPK, errServiceSignerUnavailable
	}
	pk, err := k.inner.GetPublicKey(ctx)
	if err != nil {
		return nostr.ZeroPK, errServiceSignerUnavailable
	}
	return pk, nil
}
func (k *revocableKeyer) SignEvent(ctx context.Context, event *nostr.Event) error {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.active || k.inner.SignEvent(ctx, event) != nil {
		return errServiceSignerUnavailable
	}
	return nil
}
func (k *revocableKeyer) Encrypt(ctx context.Context, plaintext string, recipient nostr.PubKey) (string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.active {
		return "", errServiceSignerUnavailable
	}
	out, err := k.inner.Encrypt(ctx, plaintext, recipient)
	if err != nil {
		return "", errServiceSignerUnavailable
	}
	return out, nil
}
func (k *revocableKeyer) Decrypt(ctx context.Context, ciphertext string, sender nostr.PubKey) (string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.active {
		return "", errServiceSignerUnavailable
	}
	out, err := k.inner.Decrypt(ctx, ciphertext, sender)
	if err != nil {
		return "", errServiceSignerUnavailable
	}
	return out, nil
}
func (k *revocableKeyer) Nip04Encrypt(ctx context.Context, plaintext string, recipient nostr.PubKey) (string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.active {
		return "", errServiceSignerUnavailable
	}
	out, err := k.inner.Nip04Encrypt(ctx, plaintext, recipient)
	if err != nil {
		return "", errServiceSignerUnavailable
	}
	return out, nil
}
func (k *revocableKeyer) Nip04Decrypt(ctx context.Context, ciphertext string, sender nostr.PubKey) (string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !k.active {
		return "", errServiceSignerUnavailable
	}
	out, err := k.inner.Nip04Decrypt(ctx, ciphertext, sender)
	if err != nil {
		return "", errServiceSignerUnavailable
	}
	return out, nil
}

func (s *ServiceSigner) Generation() uint64   { s.mu.Lock(); defer s.mu.Unlock(); return s.generation }
func (s *ServiceSigner) Status() SignerStatus { s.mu.Lock(); defer s.mu.Unlock(); return s.status }
func (s *ServiceSigner) Switch(ctx context.Context, mode, secret string, persist func(string, string) error) (SignerStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	stopped := s.stopLocked(ctx)
	s.status = SignerStatus{Mode: mode, ConnectionState: "disconnected"}
	failed := errors.New("signer unavailable")
	if !stopped {
		if persist != nil {
			_ = persist("none", "")
		}
		return s.status, failed
	}
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
	s.keyer = &revocableKeyer{active: true, inner: k}
	userKeyer = s.keyer
	userPubkey = pk
	s.status = SignerStatus{Mode: "nsec", PublicKey: pk.Hex(), ConnectionState: "connected"}
	pushIdentityChanged()
	return s.status, nil
}

// Close ends the live session without changing retained credentials.
func (s *ServiceSigner) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.stopLocked(ctx)
	s.status = SignerStatus{Mode: "none", ConnectionState: "disconnected"}
}

func (s *ServiceSigner) stopLocked(ctx context.Context) bool {
	stopNostrConnect()
	if sessionCancel != nil {
		sessionCancel()
		sessionCancel = nil
	}
	if s.keyer != nil {
		s.keyer.revoke()
		s.keyer = nil
	}
	userKeyer = nil
	userPubkey = nostr.ZeroPK
	pushIdentityChanged()
	stopUserRelays()
	setProfile("", "", "")
	open := allInstances()
	for _, ci := range open {
		ci.Close()
	}
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, ci := range open {
		select {
		case <-ci.gone:
		case <-deadline.Done():
			return false
		}
	}
	return true
}
