package backend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip04"
	"fiatjaf.com/nostr/nip44"
	"fiatjaf.com/nostr/nip46"
	"kwakore/backend/bunker"
)

// The client-initiated half of NIP-46: while the login screen is up the
// launcher shows a nostrconnect:// uri (as a QR code) naming our client key,
// a relay and a one-time secret, and listens on that relay. A signer that
// scans it answers "connect" with the secret; its author is the remote
// signer, and from there it is an ordinary bunker login.
//
// fiatjaf.com/nostr has helpers for this (nip46.NewBunkerFromNostrConnect),
// but they hide the secret and the signer's pubkey — so the session can't be
// stored — and listen with "since": now, which bunker.Signer explains the
// trouble with. The waiting here reuses bunker.Signer's listener instead.

// defaultNostrConnectRelay is where the QR code points until the user picks
// another relay.
const defaultNostrConnectRelay = "wss://bucket.coracle.social"

// nostrConnectPerms are the requests the bridge sends a signer on a napp's
// behalf, asked for up front so the signer can grant them in one go.
var nostrConnectPerms = []string{
	"get_public_key",
	"sign_event",
	"nip44_encrypt",
	"nip44_decrypt",
	"nip04_encrypt",
	"nip04_decrypt",
}

// buildNostrConnectURI is the uri a signer scans to connect to clientPub.
func buildNostrConnectURI(clientPub nostr.PubKey, relays []string, secret string) string {
	q := url.Values{}
	for _, r := range relays {
		q.Add("relay", r)
	}
	q.Set("secret", secret)
	q.Set("perms", strings.Join(nostrConnectPerms, ","))
	q.Set("name", "Verdana")
	return "nostrconnect://" + clientPub.Hex() + "?" + q.Encode()
}

// isNostrConnectAnswer says whether evt is a signer accepting our
// nostrconnect uri: a response to clientKey carrying the uri's secret.
func isNostrConnectAnswer(clientKey nostr.SecretKey, evt nostr.Event, secret string) bool {
	if evt.Kind != nostr.KindNostrConnect || evt.PubKey == clientKey.Public() || !evt.CheckID() || !evt.VerifySignature() {
		return false
	}
	addressed := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == clientKey.Public().Hex() {
			addressed = true
			break
		}
	}
	if !addressed {
		return false
	}
	// as with bunker.Signer, some signers still answer in NIP-04
	plain := ""
	if conv44, err := nip44.GenerateConversationKey(evt.PubKey, clientKey); err == nil {
		plain, err = nip44.Decrypt(evt.Content, conv44)
		if err != nil {
			plain = ""
		}
	}
	if plain == "" {
		conv04, err := nip04.ComputeSharedSecret(evt.PubKey, clientKey)
		if err != nil {
			return false
		}
		if plain, err = nip04.Decrypt(evt.Content, conv04); err != nil {
			return false
		}
	}
	var resp nip46.Response
	if err := json.Unmarshal([]byte(plain), &resp); err != nil {
		return false
	}
	return resp.Error == "" && resp.Result == secret
}

type ServicePairStart struct {
	ClientPublicKey string `json:"client_public_key"`
	Relay           string `json:"relay"`
}

type servicePair struct {
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	generation uint64
	secret     string
	status     SignerStatus
	err        error
	finished   bool
}

var servicePairWait = func(ctx context.Context, clientKey nostr.SecretKey, relay, secret string) (nostr.PubKey, error) {
	return waitNostrConnect(ctx, sys.Pool, clientKey, []string{relay}, secret)
}

func (s *ServiceSigner) cancelPairLocked() bool {
	if s.pair == nil || s.pair.finished {
		return false
	}
	s.pair.cancel()
	s.pair.secret = ""
	s.pair.status = SignerStatus{Mode: "bunker", ConnectionState: "disconnected"}
	s.pair.err = nil
	s.pair.finished = true
	close(s.pair.done)
	return true
}

func (s *ServiceSigner) StartPair(parent context.Context, secret string, clientKey nostr.SecretKey, relay string, complete func(context.Context, string, nostr.SecretKey, uint64) (SignerStatus, error), leaseDone func()) (ServicePairStart, error) {
	decoded, err := hex.DecodeString(secret)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != secret || relay == "" {
		return ServicePairStart{}, errServiceSignerUnavailable
	}
	s.mu.Lock()
	s.cancelPairLocked()
	s.generation++
	if s.pendingCancel != nil {
		s.pendingCancel()
		s.pendingCancel = nil
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	p := &servicePair{ctx: ctx, cancel: cancel, done: make(chan struct{}), generation: s.generation, secret: secret}
	s.pair = p
	s.mu.Unlock()
	go func() {
		defer leaseDone()
		defer cancel()
		wait := s.PairWait
		if wait == nil {
			wait = servicePairWait
		}
		pk, err := wait(ctx, clientKey, relay, secret)
		if err != nil {
			s.finishPair(p, SignerStatus{Mode: "bunker", ConnectionState: "disconnected"}, context.DeadlineExceeded)
			return
		}
		s.mu.Lock()
		current := s.pair == p && !p.finished && s.generation == p.generation && ctx.Err() == nil
		s.mu.Unlock()
		if !current {
			return
		}
		url := nostrConnectBunkerURL(pk, []string{relay})
		status, switchErr := complete(ctx, url, clientKey, p.generation)
		s.finishPair(p, status, switchErr)
	}()
	return ServicePairStart{ClientPublicKey: clientKey.Public().Hex(), Relay: relay}, nil
}

func (s *ServiceSigner) finishPair(p *servicePair, status SignerStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pair != p || p.finished {
		return
	}
	p.status, p.err, p.finished, p.secret = status, err, true, ""
	close(p.done)
}

func (s *ServiceSigner) WaitPair(ctx context.Context) (SignerStatus, error) {
	s.mu.Lock()
	p := s.pair
	s.mu.Unlock()
	if p == nil {
		return SignerStatus{}, errServiceSignerUnavailable
	}
	select {
	case <-p.done:
		s.mu.Lock()
		status, err := p.status, p.err
		s.mu.Unlock()
		return status, err
	case <-ctx.Done():
		return SignerStatus{}, context.DeadlineExceeded
	}
}

func (s *ServiceSigner) CancelPair() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancelled := s.cancelPairLocked()
	if cancelled {
		s.generation++
	}
	return cancelled
}

func ServiceDefaultPairRelay() string { return defaultNostrConnectRelay }

// waitNostrConnect listens on relays until a signer answers the
// nostrconnect uri with secret, and returns that signer's pubkey.
func waitNostrConnect(ctx context.Context, pool *nostr.Pool, clientKey nostr.SecretKey, relays []string, secret string) (nostr.PubKey, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	found := make(chan nostr.PubKey, 1)
	onEvent := func(evt nostr.Event) {
		if isNostrConnectAnswer(clientKey, evt, secret) {
			select {
			case found <- evt.PubKey:
			default:
			}
		}
	}
	for _, r := range relays {
		go bunker.Listen(ctx, pool, nostr.NormalizeURL(r), clientKey.Public(), onEvent, func() {})
	}

	select {
	case pk := <-found:
		return pk, nil
	case <-ctx.Done():
		return nostr.ZeroPK, context.Cause(ctx)
	}
}

// nostrConnectBunkerURL is how a nostrconnect login is stored: the bunker
// url of the signer that answered, without a secret. The signer knows our
// client key from then on, so resuming never sends "connect" again.
func nostrConnectBunkerURL(signer nostr.PubKey, relays []string) string {
	q := url.Values{}
	for _, r := range relays {
		q.Add("relay", r)
	}
	return "bunker://" + signer.Hex() + "?" + q.Encode()
}

// nc is the nostrconnect uri on offer while the login screen shows its
// "connect signer" view. It is started from that view (StartNostrConnect)
// and withdrawn with it or with the login phase (see setPhaseLocked), so its
// mutex nests inside ls.mu.
var nc struct {
	mu     sync.Mutex
	uri    string
	cancel context.CancelFunc
}

// startNostrConnectLocked offers a fresh nostrconnect uri — new secret, the
// stored relay — and waits for a signer to answer it in the background,
// replacing whatever uri was on offer. ls.mu must be held.
func startNostrConnectLocked() {
	// a user flow: the client key is made here if there is none yet, and
	// saved with the login once a signer answers
	ck, err := clientKey()
	if err != nil {
		log.Warn().Err(err).Msg("no client key for a nostrconnect uri")
		return
	}
	stateMu.Lock()
	relay := state.NostrConnectRelay
	stateMu.Unlock()
	if relay == "" {
		relay = defaultNostrConnectRelay
	}
	relays := []string{relay}

	var raw [16]byte
	rand.Read(raw[:])
	secret := hex.EncodeToString(raw[:])

	ctx, cancel := context.WithCancel(context.Background())
	nc.mu.Lock()
	if nc.cancel != nil {
		nc.cancel()
	}
	nc.uri = buildNostrConnectURI(ck.Public(), relays, secret)
	nc.cancel = cancel
	nc.mu.Unlock()

	if sys == nil {
		return // not started: nothing to listen with
	}
	pool := sys.Pool

	log.Debug().Str("relay", relay).Msg("waiting for a nostrconnect signer")
	go func() {
		signer, err := waitNostrConnect(ctx, pool, ck, relays, secret)
		if err != nil {
			return // replaced, or the login screen went away
		}

		// only the uri still on offer gets to log in: a manual login or a
		// relay change in the meantime cancelled this one
		nc.mu.Lock()
		if ctx.Err() != nil {
			nc.mu.Unlock()
			return
		}
		nc.uri = ""
		nc.cancel = nil
		nc.mu.Unlock()
		cancel()

		log.Info().Str("signer", signer.Hex()).Msg("nostrconnect signer answered")
		// the user started this login (the QR code), so it may use the
		// client key made for it, even after "Log in again" (D-21); it is
		// not a resume. The signer knows ck already: no "connect".
		login(nostrConnectBunkerURL(signer, relays), loginOpts{skipConnect: true, pairedKey: &ck})
	}()
}

// stopNostrConnect withdraws the nostrconnect uri on offer, if any.
func stopNostrConnect() {
	nc.mu.Lock()
	if nc.cancel != nil {
		nc.cancel()
		nc.cancel = nil
	}
	nc.uri = ""
	nc.mu.Unlock()
}

// nostrConnectURI is the nostrconnect uri on offer, or "".
func nostrConnectURI() string {
	nc.mu.Lock()
	defer nc.mu.Unlock()
	return nc.uri
}

// StartNostrConnect puts up a fresh nostrconnect uri for the login screen's
// "connect signer" view and waits for a signer to answer it.
func StartNostrConnect() {
	ls.mu.Lock()
	if ls.phase == PhaseLogin {
		ls.loginErr = ""
		startNostrConnectLocked()
	}
	ls.mu.Unlock()
	notifyState()
}

// CancelNostrConnect withdraws the nostrconnect uri: the user went back from
// the "connect signer" view.
func CancelNostrConnect() {
	stopNostrConnect()
	ls.mu.Lock()
	ls.loginErr = ""
	ls.mu.Unlock()
	notifyState()
}

// NostrConnectRelay is the relay the login screen's nostrconnect uri points
// signers to.
func NostrConnectRelay() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	if state.NostrConnectRelay == "" {
		return defaultNostrConnectRelay
	}
	return state.NostrConnectRelay
}

// SetNostrConnectRelay stores the relay for nostrconnect logins and, when a
// uri is on offer, replaces it with one on the new relay.
func SetNostrConnectRelay(input string) {
	relay, err := cleanRelayURL(input)
	if err != nil {
		ls.mu.Lock()
		ls.loginErr = err.Error()
		ls.mu.Unlock()
		notifyState()
		return
	}

	stateMu.Lock()
	changed := state.NostrConnectRelay != relay
	state.NostrConnectRelay = relay
	if changed {
		saveState()
	}
	stateMu.Unlock()

	ls.mu.Lock()
	ls.loginErr = ""
	if changed && ls.phase == PhaseLogin && nostrConnectURI() != "" {
		startNostrConnectLocked()
	}
	ls.mu.Unlock()
	notifyState()
}

// cleanRelayURL turns what was typed into a relay field into a ws(s) url.
func cleanRelayURL(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("no relay given")
	}
	if !strings.Contains(input, "://") {
		input = "wss://" + input
	}
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" {
		return "", errors.New("not a relay url: " + strings.TrimSpace(input))
	}
	return nostr.NormalizeURL(input), nil
}
