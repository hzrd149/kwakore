package backend

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nip46"
	"fiatjaf.com/nostr/sdk"
	"github.com/rs/zerolog"
)

func TestServiceSignerIdentityRace(t *testing.T) {
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	secret := nip19.EncodeNsec(nostr.Generate())
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = currentUser()
				_ = LoggedIn()
				_ = UserPubkey()
				c := &napCall{}
				c.approved.Store(true)
				_ = c.sign(context.Background(), &nostr.Event{Kind: 1, CreatedAt: nostr.Now()})
			}
		}()
	}
	for i := 0; i < 30; i++ {
		if _, err := s.Switch(context.Background(), "nsec", secret, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Switch(context.Background(), "none", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if LoggedIn() || UserPubkey() != "" {
		t.Fatal("identity survived sign-out")
	}
}

type blockingSignKeyer struct {
	nostr.Keyer
	entered chan struct{}
	release chan struct{}
}

func (k *blockingSignKeyer) SignEvent(ctx context.Context, evt *nostr.Event) error {
	close(k.entered)
	<-k.release
	return k.Keyer.SignEvent(ctx, evt)
}

func TestServiceSignerBlockedNAPSink(t *testing.T) {
	secret := nostr.Generate()
	inner, err := keyer.New(context.Background(), nil, nip19.EncodeNsec(secret), &keyer.SignerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockingSignKeyer{Keyer: inner, entered: make(chan struct{}), release: make(chan struct{})}
	s := &ServiceSigner{keyer: &revocableKeyer{active: true, inner: blocked}}
	publishIdentity(s.keyer, secret.Public(), nil)
	t.Cleanup(s.Close)
	c := &napCall{}
	c.approved.Store(true)
	signDone := make(chan error, 1)
	go func() { signDone <- c.sign(context.Background(), &nostr.Event{Kind: 1, CreatedAt: nostr.Now()}) }()
	<-blocked.entered
	switchDone := make(chan error, 1)
	go func() { _, err := s.Switch(context.Background(), "none", "", nil); switchDone <- err }()
	select {
	case err := <-switchDone:
		t.Fatalf("switch finished before in-flight NAP sign: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(blocked.release)
	if err := <-signDone; err != nil {
		t.Fatalf("in-flight NAP sign failed: %v", err)
	}
	if err := <-switchDone; err != nil {
		t.Fatal(err)
	}
	if err := c.sign(context.Background(), &nostr.Event{Kind: 1}); err == nil || (!errors.Is(err, errServiceSignerUnavailable) && err.Error() != "not-signed-in") {
		t.Fatalf("old signer remained usable: %v", err)
	}
}

func TestServiceSignerStaleResult(t *testing.T) {
	client, remote, user := nostr.Generate(), nostr.Generate(), nostr.Generate()
	inner, err := keyer.New(context.Background(), nil, nip19.EncodeNsec(user), &keyer.SignerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	old := serviceBunkerConnect
	serviceBunkerConnect = func(_, _ context.Context, _ nostr.SecretKey, _ string, _ bool) (nostr.Keyer, error) {
		close(entered)
		<-release
		return inner, nil
	}
	t.Cleanup(func() { serviceBunkerConnect = old })
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	url := "bunker://" + remote.Public().Hex() + "?relay=wss%3A%2F%2Fexample.com"
	done := make(chan error, 1)
	go func() { _, err := s.SwitchBunker(context.Background(), url, client, false, nil); done <- err }()
	<-entered
	if _, err := s.Switch(context.Background(), "none", "", nil); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil || LoggedIn() || UserPubkey() != "" {
		t.Fatalf("stale signer revived: %v %+v", err, s.Status())
	}
}

func TestSignerConsumerSnapshot(t *testing.T) {
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	secret := nip19.EncodeNsec(nostr.Generate())
	ci := &Instance{nap: newNapSession()}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 80; i++ {
			_, _ = bridgeRPC(ci)("getPublicKey", "")
			_, _, _ = PublishDev(context.Background(), "missing-dev", nil, nil, false, nil)
			c := &napCall{ci: ci, gen: ci.nap.gen, Type: "upload.info"}
			napUploadInfo(c)
		}
	}()
	for i := 0; i < 20; i++ {
		if _, err := s.Switch(context.Background(), "nsec", secret, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Switch(context.Background(), "none", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if LoggedIn() {
		t.Fatal("consumer work revived signer")
	}
}

func TestServiceSignerBunkerLiveHandshakeAndSigning(t *testing.T) {
	srv := httptest.NewServer(khatru.NewRelay())
	defer srv.Close()
	relay := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	remote := nostr.Generate()
	signer := nip46.NewStaticKeySigner(remote)
	pool := nostr.NewPool()
	requests := pool.SubscribeMany(ctx, []string{relay}, nostr.Filter{Kinds: []nostr.Kind{nostr.KindNostrConnect}, Tags: nostr.TagMap{"p": []string{remote.Public().Hex()}}, Since: nostr.Now() - 5}, nostr.SubscriptionOptions{})
	go func() {
		for ie := range requests {
			_, _, answer, err := signer.HandleRequest(ctx, ie.Event)
			if err != nil {
				continue
			}
			if err := answer.Sign(remote); err != nil {
				continue
			}
			r, err := pool.EnsureRelay(relay)
			if err == nil {
				_ = r.Publish(ctx, answer)
			}
		}
	}()
	time.Sleep(200 * time.Millisecond)
	oldSys := sys
	sys = &sdk.System{Pool: nostr.NewPool()}
	s := &ServiceSigner{}
	t.Cleanup(func() { s.Close(); sys.Pool.Close("test over"); sys = oldSys; pool.Close("test over") })
	input := "bunker://" + remote.Public().Hex() + "?relay=" + url.QueryEscape(relay)
	status, err := s.SwitchBunker(ctx, input, nostr.Generate(), false, nil)
	if err != nil || status.ConnectionState != "connected" || status.PublicKey != remote.Public().Hex() {
		t.Fatalf("live handshake: %+v %v", status, err)
	}
	evt := &nostr.Event{Kind: 1, Content: "service-signing", CreatedAt: nostr.Now()}
	if err := userKeyer.SignEvent(ctx, evt); err != nil || !evt.VerifySignature() || evt.PubKey != remote.Public() {
		t.Fatalf("post-handshake remote signing: %v", err)
	}
}

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
	status, err := s.SwitchBunker(context.Background(), url, client, false, func(gotURL, gotKey string) error {
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
	go func() { _, err := s.SwitchBunker(context.Background(), url, client, false, nil); done <- err }()
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
	_, err := s.SwitchBunker(context.Background(), url, nostr.Generate(), false, nil)
	if err == nil || strings.Contains(err.Error(), "private-sentinel") {
		t.Fatalf("leaked remote failure: %v", err)
	}
}

func TestServiceNostrConnectPairFinalOutcome(t *testing.T) {
	s := &ServiceSigner{}
	client, remote, user := nostr.Generate(), nostr.Generate(), nostr.Generate()
	inner, err := keyer.New(context.Background(), nil, nip19.EncodeNsec(user), &keyer.SignerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	oldWait, oldConnect := servicePairWait, serviceBunkerConnect
	servicePairWait = func(_ context.Context, got nostr.SecretKey, relay, secret string) (nostr.PubKey, error) {
		if got != client || relay != "wss://example.com" || secret != strings.Repeat("a", 32) {
			t.Error("wrong pairing offer")
		}
		return remote.Public(), nil
	}
	serviceBunkerConnect = func(_, _ context.Context, _ nostr.SecretKey, url string, skip bool) (nostr.Keyer, error) {
		if !skip || strings.Contains(url, strings.Repeat("a", 32)) {
			t.Error("one-time secret reused in bunker URL")
		}
		return inner, nil
	}
	t.Cleanup(func() { servicePairWait, serviceBunkerConnect = oldWait, oldConnect; s.Close() })
	persisted := ""
	start, err := s.StartPair(context.Background(), strings.Repeat("a", 32), client, "wss://example.com", func(ctx context.Context, url string, key nostr.SecretKey, gen uint64) (SignerStatus, error) {
		return s.SwitchBunkerPair(ctx, url, key, gen, func(u, k string) error {
			persisted = u
			if k != client.Hex() {
				t.Error("client key changed")
			}
			return nil
		})
	}, func() {})
	if err != nil || start.ClientPublicKey != client.Public().Hex() || start.Relay != "wss://example.com" {
		t.Fatalf("start: %+v %v", start, err)
	}
	status, err := s.WaitPair(context.Background())
	if err != nil || status.ConnectionState != "connected" || status.PublicKey != user.Public().Hex() || persisted == "" {
		t.Fatalf("wait before final outcome: %+v %v", status, err)
	}
}

func TestServiceNostrConnectPairCancelAndStale(t *testing.T) {
	s := &ServiceSigner{}
	entered := make(chan struct{})
	release := make(chan struct{})
	old := servicePairWait
	servicePairWait = func(_ context.Context, _ nostr.SecretKey, _, _ string) (nostr.PubKey, error) {
		close(entered)
		<-release
		return nostr.Generate().Public(), nil
	}
	t.Cleanup(func() { servicePairWait = old; s.Close() })
	_, err := s.StartPair(context.Background(), strings.Repeat("b", 32), nostr.Generate(), "wss://example.com", func(context.Context, string, nostr.SecretKey, uint64) (SignerStatus, error) {
		t.Error("stale answer connected")
		return SignerStatus{}, nil
	}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if !s.CancelPair() {
		t.Fatal("pair did not cancel")
	}
	status, err := s.WaitPair(context.Background())
	if err != nil || status.ConnectionState != "disconnected" {
		t.Fatalf("cancel outcome: %+v %v", status, err)
	}
	close(release)
}

func TestServiceNostrConnectPairTimeout(t *testing.T) {
	s := &ServiceSigner{}
	old := servicePairWait
	servicePairWait = func(ctx context.Context, _ nostr.SecretKey, _, _ string) (nostr.PubKey, error) {
		<-ctx.Done()
		return nostr.ZeroPK, ctx.Err()
	}
	t.Cleanup(func() { servicePairWait = old; s.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	_, err := s.StartPair(ctx, strings.Repeat("c", 32), nostr.Generate(), "wss://example.com", func(context.Context, string, nostr.SecretKey, uint64) (SignerStatus, error) {
		t.Error("timed out pair connected")
		return SignerStatus{}, nil
	}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := s.WaitPair(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout result: %v", err)
	}
}

func TestServiceNostrConnectPairCancelAfterCommit(t *testing.T) {
	s := &ServiceSigner{}
	client, remote, user := nostr.Generate(), nostr.Generate(), nostr.Generate()
	inner, err := keyer.New(context.Background(), nil, nip19.EncodeNsec(user), &keyer.SignerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	oldWait, oldConnect := servicePairWait, serviceBunkerConnect
	servicePairWait = func(context.Context, nostr.SecretKey, string, string) (nostr.PubKey, error) {
		return remote.Public(), nil
	}
	serviceBunkerConnect = func(context.Context, context.Context, nostr.SecretKey, string, bool) (nostr.Keyer, error) {
		return inner, nil
	}
	t.Cleanup(func() { servicePairWait, serviceBunkerConnect = oldWait, oldConnect; s.Close() })
	committing, release := make(chan struct{}), make(chan struct{})
	_, err = s.StartPair(context.Background(), strings.Repeat("d", 32), client, "wss://example.com", func(ctx context.Context, url string, key nostr.SecretKey, gen uint64) (SignerStatus, error) {
		return s.SwitchBunkerPair(ctx, url, key, gen, func(string, string) error { close(committing); <-release; return nil })
	}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	<-committing
	cancelled := make(chan bool, 1)
	go func() { cancelled <- s.CancelPair() }()
	close(release)
	status, err := s.WaitPair(context.Background())
	if err != nil || status.ConnectionState != "connected" || <-cancelled {
		t.Fatalf("committed pair canceled: %+v %v", status, err)
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
