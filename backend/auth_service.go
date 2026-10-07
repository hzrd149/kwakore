package backend

import (
	"context"
	"errors"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nip46"
)

// SignerStatus is the complete public signer read surface.
type SignerStatus struct {
	Mode            string `json:"mode"`
	PublicKey       string `json:"public_key"`
	ConnectionState string `json:"connection_state"`
}

type ServiceSigner struct {
	mu            sync.Mutex
	generation    uint64
	status        SignerStatus
	keyer         *revocableKeyer
	pendingCancel context.CancelFunc
	pair          *servicePair
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
func (s *ServiceSigner) PreemptPending() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingCancel != nil {
		s.pendingCancel()
		s.pendingCancel = nil
		s.generation++
	}
	if s.cancelPairLocked() {
		s.generation++
	}
}
func (s *ServiceSigner) Switch(ctx context.Context, mode, secret string, persist func(string, string) error) (SignerStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.cancelPairLocked()
	if s.pendingCancel != nil {
		s.pendingCancel()
		s.pendingCancel = nil
	}
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

var serviceBunkerConnect = func(sessionCtx, handshakeCtx context.Context, clientKey nostr.SecretKey, input string, skipConnect bool) (nostr.Keyer, error) {
	return loginBunkerWithHandshake(sessionCtx, handshakeCtx, clientKey, input, skipConnect, nil)
}

// SwitchBunker retires the old session before connecting. The network handshake
// runs outside the state lock so a later switch can cancel and fence its result.
func (s *ServiceSigner) SwitchBunker(ctx context.Context, input string, clientKey nostr.SecretKey, skipConnect bool, clear func(string, string) error, persist func(string, string) error) (SignerStatus, error) {
	return s.switchBunker(ctx, input, clientKey, skipConnect, clear, persist, 0)
}

func (s *ServiceSigner) SwitchBunkerPair(ctx context.Context, input string, clientKey nostr.SecretKey, expectedGeneration uint64, persist func(string, string) error) (SignerStatus, error) {
	return s.switchBunker(ctx, input, clientKey, true, nil, persist, expectedGeneration)
}

func (s *ServiceSigner) switchBunker(ctx context.Context, input string, clientKey nostr.SecretKey, skipConnect bool, clear func(string, string) error, persist func(string, string) error, expected uint64) (SignerStatus, error) {
	failed := errServiceSignerUnavailable
	s.mu.Lock()
	if expected != 0 && s.generation != expected {
		status := s.status
		s.mu.Unlock()
		return status, failed
	}
	if expected == 0 {
		s.cancelPairLocked()
	}
	s.generation++
	generation := s.generation
	if s.pendingCancel != nil {
		s.pendingCancel()
	}
	stopped := s.stopLocked(ctx)
	s.status = SignerStatus{Mode: "bunker", ConnectionState: "disconnected"}
	if !stopped || clear != nil && clear("none", "") != nil {
		status := s.status
		s.mu.Unlock()
		return status, failed
	}
	if len(input) == 0 || len(input) > 2048 || !nip46.IsValidBunkerURL(input) {
		status := s.status
		s.mu.Unlock()
		return status, failed
	}
	sessionCtx, cancel := context.WithCancel(context.Background())
	s.pendingCancel = cancel
	s.mu.Unlock()

	handshakeCtx, timeoutCancel := context.WithTimeout(ctx, 20*time.Second)
	defer timeoutCancel()
	stopHandshake := context.AfterFunc(sessionCtx, timeoutCancel)
	defer stopHandshake()
	k, err := serviceBunkerConnect(sessionCtx, handshakeCtx, clientKey, input, skipConnect)
	var pk nostr.PubKey
	if err == nil && handshakeCtx.Err() == nil {
		pk, err = k.GetPublicKey(handshakeCtx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation || sessionCtx.Err() != nil || handshakeCtx.Err() != nil || err != nil || pk == nostr.ZeroPK {
		cancel()
		return s.status, failed
	}
	if persist != nil && persist(input, clientKey.Hex()) != nil {
		cancel()
		return s.status, failed
	}
	s.pendingCancel = nil
	sessionCancel = cancel
	s.keyer = &revocableKeyer{active: true, inner: k}
	userKeyer = s.keyer
	userPubkey = pk
	s.status = SignerStatus{Mode: "bunker", PublicKey: pk.Hex(), ConnectionState: "connected"}
	pushIdentityChanged()
	if expected != 0 && s.pair != nil && s.pair.generation == expected && !s.pair.finished {
		s.pair.status, s.pair.err, s.pair.finished, s.pair.secret = s.status, nil, true, ""
		close(s.pair.done)
	}
	return s.status, nil
}

// Close ends the live session without changing retained credentials.
func (s *ServiceSigner) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.cancelPairLocked()
	if s.pendingCancel != nil {
		s.pendingCancel()
		s.pendingCancel = nil
	}
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
