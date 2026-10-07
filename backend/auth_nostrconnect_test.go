package backend

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip04"
	"fiatjaf.com/nostr/nip44"
	"fiatjaf.com/nostr/nip46"
	"fiatjaf.com/nostr/sdk"
)

// connectAnswer is the event a signer sends to accept a nostrconnect uri.
func connectAnswer(t *testing.T, signerKey nostr.SecretKey, client nostr.PubKey, result string, useNip04 bool) nostr.Event {
	t.Helper()
	plain, _ := json.Marshal(nip46.Response{ID: "x", Result: result})
	var content string
	if useNip04 {
		shared, err := nip04.ComputeSharedSecret(client, signerKey)
		if err != nil {
			t.Fatal(err)
		}
		if content, err = nip04.Encrypt(string(plain), shared); err != nil {
			t.Fatal(err)
		}
	} else {
		conv, err := nip44.GenerateConversationKey(client, signerKey)
		if err != nil {
			t.Fatal(err)
		}
		if content, err = nip44.Encrypt(string(plain), conv); err != nil {
			t.Fatal(err)
		}
	}
	evt := nostr.Event{
		Kind:      nostr.KindNostrConnect,
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"p", client.Hex()}},
		Content:   content,
	}
	if err := evt.Sign(signerKey); err != nil {
		t.Fatal(err)
	}
	return evt
}

func TestBuildNostrConnectURIRoundTrip(t *testing.T) {
	client := nostr.Generate().Public()
	uri := buildNostrConnectURI(client, []string{"wss://bucket.coracle.social"}, "s3cret")

	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "nostrconnect" || u.Host != client.Hex() {
		t.Fatalf("uri %q doesn't name the client key", uri)
	}
	q := u.Query()
	if got := q["relay"]; len(got) != 1 || got[0] != "wss://bucket.coracle.social" {
		t.Fatalf("relay = %v", got)
	}
	if q.Get("secret") != "s3cret" {
		t.Fatalf("secret = %q", q.Get("secret"))
	}
	if q.Get("name") != "Verdana" {
		t.Fatalf("name = %q", q.Get("name"))
	}
	if !strings.Contains(q.Get("perms"), "sign_event") {
		t.Fatalf("perms = %q", q.Get("perms"))
	}
}

func TestIsNostrConnectAnswer(t *testing.T) {
	clientKey, signerKey := nostr.Generate(), nostr.Generate()
	client := clientKey.Public()

	if !isNostrConnectAnswer(clientKey, connectAnswer(t, signerKey, client, "s3cret", false), "s3cret") {
		t.Error("NIP-44 answer with the secret was rejected")
	}
	if !isNostrConnectAnswer(clientKey, connectAnswer(t, signerKey, client, "s3cret", true), "s3cret") {
		t.Error("NIP-04 answer with the secret was rejected")
	}
	if isNostrConnectAnswer(clientKey, connectAnswer(t, signerKey, client, "wrong", false), "s3cret") {
		t.Error("answer with the wrong secret was accepted")
	}
	// "ack" is what signers answer a bunker:// connect with; taking it here
	// would let anyone who saw an earlier uri (same client key) log us in
	if isNostrConnectAnswer(clientKey, connectAnswer(t, signerKey, client, "ack", false), "s3cret") {
		t.Error("plain ack was accepted")
	}
	if isNostrConnectAnswer(clientKey, connectAnswer(t, clientKey, client, "s3cret", false), "s3cret") {
		t.Error("our own event was taken for a signer's")
	}
}

func TestServiceNostrConnectPairRejectsForgedAnswer(t *testing.T) {
	client, signer := nostr.Generate(), nostr.Generate()
	evt := connectAnswer(t, signer, client.Public(), "private-sentinel", false)
	evt.CreatedAt++
	if isNostrConnectAnswer(client, evt, "private-sentinel") {
		t.Fatal("accepted an event whose signature no longer matches the response")
	}
}

func TestWaitNostrConnectFindsSigner(t *testing.T) {
	srv := httptest.NewServer(khatru.NewRelay())
	defer srv.Close()
	relay := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientKey, signerKey, otherKey := nostr.Generate(), nostr.Generate(), nostr.Generate()
	client := clientKey.Public()

	type result struct {
		pk  nostr.PubKey
		err error
	}
	done := make(chan result, 1)
	go func() {
		pk, err := waitNostrConnect(ctx, nostr.NewPool(), clientKey, []string{relay}, "s3cret")
		done <- result{pk, err}
	}()
	time.Sleep(200 * time.Millisecond) // let the subscription open

	pool := nostr.NewPool()
	r, err := pool.EnsureRelay(relay)
	if err != nil {
		t.Fatal(err)
	}
	// a stale answer first, then the right one from a signer whose clock
	// is behind ours: no "since" must filter it out
	if err := r.Publish(ctx, connectAnswer(t, otherKey, client, "old", false)); err != nil {
		t.Fatal(err)
	}
	answer := connectAnswer(t, signerKey, client, "s3cret", false)
	answer.CreatedAt -= 30
	if err := answer.Sign(signerKey); err != nil {
		t.Fatal(err)
	}
	if err := r.Publish(ctx, answer); err != nil {
		t.Fatal(err)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("waitNostrConnect: %v", res.err)
	}
	if res.pk != signerKey.Public() {
		t.Fatalf("signer = %s, want %s", res.pk.Hex(), signerKey.Public().Hex())
	}
}

func TestNostrConnectBunkerURLResumes(t *testing.T) {
	signer := nostr.Generate().Public()
	stored := nostrConnectBunkerURL(signer, []string{"wss://bucket.coracle.social"})
	if !nip46.IsValidBunkerURL(stored) {
		t.Fatalf("%q is not a valid bunker url", stored)
	}
	parsed, err := nip46.ParseBunkerInput(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.HostPubKey != signer {
		t.Fatalf("host = %s, want %s", parsed.HostPubKey.Hex(), signer.Hex())
	}
	if len(parsed.Relays) != 1 || parsed.Relays[0] != "wss://bucket.coracle.social" {
		t.Fatalf("relays = %v", parsed.Relays)
	}
	if parsed.Secret != "" {
		t.Fatalf("stored login carries a secret: %q", parsed.Secret)
	}
}

func TestCleanRelayURL(t *testing.T) {
	for in, want := range map[string]string{
		"relay.nsec.app":         "wss://relay.nsec.app",
		" wss://relay.nsec.app ": "wss://relay.nsec.app",
		"ws://localhost:7777":    "ws://localhost:7777",
	} {
		got, err := cleanRelayURL(in)
		if err != nil || got != want {
			t.Errorf("cleanRelayURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "https://example.com", "wss://"} {
		if got, err := cleanRelayURL(in); err == nil {
			t.Errorf("cleanRelayURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestNostrConnectOnlyOnRequest(t *testing.T) {
	ls.mu.Lock()
	oldPhase := ls.phase
	ls.mu.Unlock()
	secretsMu.Lock()
	oldSecrets := secrets
	// the uri needs a client key, which only loaded secrets hand out
	secrets = secretsRecord{loaded: true}
	secretsMu.Unlock()
	t.Cleanup(func() {
		stopNostrConnect()
		ls.mu.Lock()
		ls.phase = oldPhase
		ls.mu.Unlock()
		secretsMu.Lock()
		secrets = oldSecrets
		secretsMu.Unlock()
	})

	setPhase(PhaseLoading)
	setPhase(PhaseLogin)
	if uri := nostrConnectURI(); uri != "" {
		t.Fatalf("login screen put up %q before the user asked", uri)
	}

	StartNostrConnect()
	first := nostrConnectURI()
	if !strings.HasPrefix(first, "nostrconnect://") {
		t.Fatalf("StartNostrConnect offered %q", first)
	}
	StartNostrConnect()
	if again := nostrConnectURI(); again == first {
		t.Fatal("a new start reused the old secret")
	}

	CancelNostrConnect()
	if uri := nostrConnectURI(); uri != "" {
		t.Fatalf("cancel left %q on offer", uri)
	}

	StartNostrConnect()
	setPhase(PhaseLoading)
	if uri := nostrConnectURI(); uri != "" {
		t.Fatalf("leaving the login phase left %q on offer", uri)
	}
}

// D-21: after "Log in again" on the keyring-failed screen, a nostrconnect
// QR login completes. The signer's answer is not a resume: the login uses
// the client key the uri advertised (made for it while the keyring is
// unreachable), skips "connect", and the login and that key are saved to
// the file. The keyring item is left alone.
func TestNostrConnectAfterLoginWithoutKeyring(t *testing.T) {
	r, store := startKeyringFailed(t)
	srv := httptest.NewServer(khatru.NewRelay())
	t.Cleanup(srv.Close)
	relay := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	finished := make(chan nostr.PubKey, 1)
	oldSys, oldFinish := sys, finishLogin
	sys = &sdk.System{Pool: nostr.NewPool()}
	finishLogin = func(_ context.Context, pk nostr.PubKey) { finished <- pk }
	stateMu.Lock()
	state.NostrConnectRelay = relay
	stateMu.Unlock()
	t.Cleanup(func() {
		if sessionCancel != nil {
			sessionCancel()
			sessionCancel = nil
		}
		// login's own "go pushIdentityChanged()" reads the user globals and
		// then takes instancesMu: let it run, and order this reset after it
		time.Sleep(100 * time.Millisecond)
		allInstances()
		userKeyer, userPubkey = nil, nostr.PubKey{}
		sys.Pool.Close("test over")
		sys, finishLogin = oldSys, oldFinish
	})

	// the remote signer: answers NIP-46 requests addressed to it
	signerKey := nostr.Generate()
	signer := nip46.NewStaticKeySigner(signerKey)
	signerPool := nostr.NewPool()
	t.Cleanup(func() { signerPool.Close("test over") })
	requests := signerPool.SubscribeMany(ctx, []string{relay}, nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindNostrConnect},
		Tags:  nostr.TagMap{"p": []string{signerKey.Public().Hex()}},
		Since: nostr.Now() - 5,
	}, nostr.SubscriptionOptions{})
	go func() {
		for ie := range requests {
			_, _, out, err := signer.HandleRequest(ctx, ie.Event)
			if err != nil {
				continue
			}
			if err := out.Sign(signerKey); err != nil {
				continue
			}
			if rl, err := signerPool.EnsureRelay(relay); err == nil {
				rl.Publish(ctx, out)
			}
		}
	}()

	before := len(store.callLog())
	LoginWithoutKeyring()
	StartNostrConnect()
	uri := nostrConnectURI()
	if uri == "" {
		t.Fatal("no nostrconnect uri on offer after LoginWithoutKeyring")
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	client, err := nostr.PubKeyFromHex(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let both subscriptions open

	// the user scans the QR code: the signer accepts with the uri's secret
	rl, err := signerPool.EnsureRelay(relay)
	if err != nil {
		t.Fatal(err)
	}
	if err := rl.Publish(ctx, connectAnswer(t, signerKey, client, u.Query().Get("secret"), false)); err != nil {
		t.Fatal(err)
	}

	loginErr := func() string {
		ls.mu.Lock()
		defer ls.mu.Unlock()
		return ls.loginErr
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
wait:
	for {
		select {
		case pk := <-finished:
			if pk != signerKey.Public() {
				t.Fatalf("logged in as %s, want %s", pk.Hex(), signerKey.Public().Hex())
			}
			break wait
		case <-tick.C:
			if msg := loginErr(); msg != "" {
				t.Fatalf("nostrconnect login failed: %s", msg)
			}
		case <-ctx.Done():
			t.Fatalf("nostrconnect login never finished (login error %q)", loginErr())
		}
	}

	if !LoggedIn() {
		t.Fatal("not logged in after the signer answered")
	}
	// the login and the key the signer paired with are in the file
	stateMu.Lock()
	login, keyHex := state.Login, state.ClientKey
	stateMu.Unlock()
	want := nostrConnectBunkerURL(signerKey.Public(), []string{relay})
	if login == nil || *login != want {
		t.Fatalf("saved login = %v, want %q", login, want)
	}
	if keyHex == nil {
		t.Fatal("no client key saved with the nostrconnect login")
	}
	k, err := nostr.SecretKeyFromHex(*keyHex)
	if err != nil || k.Public() != client {
		t.Fatalf("saved client key does not match the one the uri advertised (%v)", err)
	}
	if on := r.readState(t); !strings.Contains(on, *keyHex) {
		t.Fatalf("client key not on disk:\n%s", on)
	}
	if got := secretsLocation(); got != "file" {
		t.Fatalf("SecretsLocation = %q, want file", got)
	}
	// still not a resume: an automatic path gets no key
	if _, err := existingClientKey(); err == nil {
		t.Fatal("existingClientKey() handed out a key after LoginWithoutKeyring")
	}
	if calls := store.callsSince(before); countCalls(calls, "delete") != 0 {
		t.Fatalf("store calls = %v: the keyring item was deleted", calls)
	}
	if it, ok := store.item(t); !ok || it.Login != testLogin || it.ClientKey != testClientKeyHex {
		t.Fatalf("keyring item changed: %+v, %v", it, ok)
	}
}
