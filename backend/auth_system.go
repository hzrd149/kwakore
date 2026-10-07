package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"slices"
	"time"

	"fiatjaf.com/nostr"
)

// systemSignerTimeout bounds one request when the caller set no deadline. A
// system signer may itself wait on a remote signer, so this is generous.
const systemSignerTimeout = 90 * time.Second

// systemSignerResponseLimit caps one response line.
const systemSignerResponseLimit = 1 << 20

// systemKeyer signs through a signer service on a local Unix socket, as
// described in docs/system-signer.md. The service picks the key from the
// connecting user, so the key never enters this process.
type systemKeyer struct {
	socket string
	pk     nostr.PubKey
}

func (k *systemKeyer) call(ctx context.Context, op string, fields map[string]any, result any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, systemSignerTimeout)
		defer cancel()
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", k.socket)
	if err != nil {
		return errServiceSignerUnavailable
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	request := map[string]any{"op": op}
	for name, value := range fields {
		request[name] = value
	}
	line, err := json.Marshal(request)
	if err != nil {
		return errServiceSignerUnavailable
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return errServiceSignerUnavailable
	}
	reader := bufio.NewReaderSize(conn, 64<<10)
	var reply []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			return errServiceSignerUnavailable
		}
		reply = append(reply, chunk...)
		if len(reply) > systemSignerResponseLimit {
			return errServiceSignerUnavailable
		}
		if !isPrefix {
			break
		}
	}
	var response struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(reply, &response) != nil || !response.OK || json.Unmarshal(response.Result, result) != nil {
		return errServiceSignerUnavailable
	}
	return nil
}

func (k *systemKeyer) fetchPublicKey(ctx context.Context) (nostr.PubKey, error) {
	var hex string
	if err := k.call(ctx, "signer.get_public_key", nil, &hex); err != nil {
		return nostr.ZeroPK, err
	}
	pk, err := nostr.PubKeyFromHex(hex)
	if err != nil || pk == nostr.ZeroPK {
		return nostr.ZeroPK, errServiceSignerUnavailable
	}
	return pk, nil
}

func (k *systemKeyer) GetPublicKey(context.Context) (nostr.PubKey, error) { return k.pk, nil }

// SignEvent sends the unsigned event and accepts the reply only when it is
// the same event, signed by the session's key.
func (k *systemKeyer) SignEvent(ctx context.Context, event *nostr.Event) error {
	tags := event.Tags
	if tags == nil {
		tags = nostr.Tags{}
	}
	unsigned := map[string]any{"kind": event.Kind, "created_at": event.CreatedAt, "tags": tags, "content": event.Content}
	var signed nostr.Event
	if err := k.call(ctx, "signer.sign_event", map[string]any{"event": unsigned}, &signed); err != nil {
		return err
	}
	if signed.PubKey != k.pk || signed.Kind != event.Kind || signed.CreatedAt != event.CreatedAt || signed.Content != event.Content ||
		!slices.EqualFunc(signed.Tags, tags, func(a, b nostr.Tag) bool { return slices.Equal(a, b) }) ||
		!signed.CheckID() || !signed.VerifySignature() {
		return errServiceSignerUnavailable
	}
	event.ID, event.PubKey, event.Sig = signed.ID, signed.PubKey, signed.Sig
	return nil
}

func (k *systemKeyer) cipher(ctx context.Context, op, field, text string, peer nostr.PubKey) (string, error) {
	var out string
	if err := k.call(ctx, op, map[string]any{"pubkey": peer.Hex(), field: text}, &out); err != nil {
		return "", err
	}
	return out, nil
}

func (k *systemKeyer) Encrypt(ctx context.Context, plaintext string, recipient nostr.PubKey) (string, error) {
	return k.cipher(ctx, "signer.nip44_encrypt", "plaintext", plaintext, recipient)
}
func (k *systemKeyer) Decrypt(ctx context.Context, ciphertext string, sender nostr.PubKey) (string, error) {
	return k.cipher(ctx, "signer.nip44_decrypt", "ciphertext", ciphertext, sender)
}
func (k *systemKeyer) Nip04Encrypt(ctx context.Context, plaintext string, recipient nostr.PubKey) (string, error) {
	return k.cipher(ctx, "signer.nip04_encrypt", "plaintext", plaintext, recipient)
}
func (k *systemKeyer) Nip04Decrypt(ctx context.Context, ciphertext string, sender nostr.PubKey) (string, error) {
	return k.cipher(ctx, "signer.nip04_decrypt", "ciphertext", ciphertext, sender)
}

// systemSignerRetry spaces reconnection attempts while the system signer has
// no key for this user, for example before the user unlocks their session.
var systemSignerRetry = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

// SwitchSystem retires the old session and connects to the system signer on
// socket. When the signer does not answer yet, the mode stays selected and
// disconnected, and a background loop keeps trying until it answers or the
// signer is switched again.
func (s *ServiceSigner) SwitchSystem(ctx context.Context, socket string, persist func() error) (SignerStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.cancelPairLocked()
	if s.pendingCancel != nil {
		s.pendingCancel()
		s.pendingCancel = nil
	}
	stopped := s.stopLocked(ctx)
	s.status = SignerStatus{Mode: "system", ConnectionState: "disconnected"}
	if !stopped || !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return s.status, errServiceSignerUnavailable
	}
	if persist != nil && persist() != nil {
		return s.status, errServiceSignerUnavailable
	}
	k := &systemKeyer{socket: socket}
	attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pk, err := k.fetchPublicKey(attemptCtx)
	cancel()
	if err == nil {
		s.connectSystemLocked(k, pk)
		return s.status, nil
	}
	retryCtx, cancelRetry := context.WithCancel(context.Background())
	s.pendingCancel = cancelRetry
	go s.retrySystem(retryCtx, k, s.generation, systemSignerRetry)
	return s.status, nil
}

func (s *ServiceSigner) connectSystemLocked(k *systemKeyer, pk nostr.PubKey) {
	k.pk = pk
	_, cancel := context.WithCancel(context.Background())
	s.keyer = &revocableKeyer{active: true, inner: k}
	publishIdentity(s.keyer, pk, cancel)
	s.status = SignerStatus{Mode: "system", PublicKey: pk.Hex(), ConnectionState: "connected"}
	pushIdentityChanged()
}

func (s *ServiceSigner) retrySystem(ctx context.Context, k *systemKeyer, generation uint64, schedule []time.Duration) {
	for attempt := 0; ; attempt++ {
		wait := schedule[min(attempt, len(schedule)-1)]
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		pk, err := k.fetchPublicKey(attemptCtx)
		cancel()
		if err != nil {
			continue
		}
		s.mu.Lock()
		if s.generation == generation && ctx.Err() == nil {
			s.pendingCancel = nil
			s.connectSystemLocked(k, pk)
		}
		s.mu.Unlock()
		return
	}
}

var _ nostr.Keyer = (*systemKeyer)(nil)
