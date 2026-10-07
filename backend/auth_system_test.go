package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip44"
)

// fakeSystemSigner serves docs/system-signer.md with one key. While locked it
// refuses every request, like a system signer that holds no key yet.
type fakeSystemSigner struct {
	path   string
	sk     nostr.SecretKey
	locked atomic.Bool
	forge  atomic.Bool
}

func startFakeSystemSigner(t *testing.T) *fakeSystemSigner {
	t.Helper()
	f := &fakeSystemSigner{path: filepath.Join(t.TempDir(), "signer.sock"), sk: nostr.Generate()}
	listener, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go f.handle(conn)
		}
	}()
	return f
}

func (f *fakeSystemSigner) handle(conn net.Conn) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var request struct {
		Op         string          `json:"op"`
		Event      json.RawMessage `json:"event"`
		PubKey     string          `json:"pubkey"`
		Plaintext  string          `json:"plaintext"`
		Ciphertext string          `json:"ciphertext"`
	}
	reply := func(ok bool, result any) {
		out, _ := json.Marshal(map[string]any{"ok": ok, "result": result, "error": "locked"})
		conn.Write(append(out, '\n'))
	}
	if json.Unmarshal(line, &request) != nil || f.locked.Load() {
		reply(false, nil)
		return
	}
	switch request.Op {
	case "signer.get_public_key":
		reply(true, f.sk.Public().Hex())
	case "signer.sign_event":
		var event nostr.Event
		if json.Unmarshal(request.Event, &event) != nil {
			reply(false, nil)
			return
		}
		if f.forge.Load() {
			event.Content += " (changed)"
		}
		event.Sign(f.sk)
		reply(true, event)
	case "signer.nip44_encrypt", "signer.nip44_decrypt":
		peer, err := nostr.PubKeyFromHex(request.PubKey)
		if err != nil {
			reply(false, nil)
			return
		}
		key, _ := nip44.GenerateConversationKey(peer, f.sk)
		if request.Op == "signer.nip44_encrypt" {
			out, _ := nip44.Encrypt(request.Plaintext, key)
			reply(true, out)
		} else {
			out, err := nip44.Decrypt(request.Ciphertext, key)
			reply(err == nil, out)
		}
	default:
		reply(false, nil)
	}
}

func TestSystemSignerSignsAndEncrypts(t *testing.T) {
	f := startFakeSystemSigner(t)
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	persisted := false
	status, err := s.SwitchSystem(context.Background(), f.path, func() error { persisted = true; return nil })
	if err != nil || !persisted {
		t.Fatalf("switch: %v persisted=%v", err, persisted)
	}
	if status.Mode != "system" || status.ConnectionState != "connected" || status.PublicKey != f.sk.Public().Hex() {
		t.Fatalf("status = %+v", status)
	}
	if pk, ok := currentUser(); !ok || pk != f.sk.Public() {
		t.Fatal("identity not published")
	}
	k := s.keyer
	event := &nostr.Event{Kind: 1, CreatedAt: nostr.Now(), Tags: nostr.Tags{{"t", "kwak"}}, Content: "hello"}
	if err := k.SignEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if event.PubKey != f.sk.Public() || !event.VerifySignature() {
		t.Fatal("event not signed by the system key")
	}
	peer := nostr.Generate()
	ciphertext, err := k.Encrypt(context.Background(), "secret note", peer.Public())
	if err != nil {
		t.Fatal(err)
	}
	key, _ := nip44.GenerateConversationKey(f.sk.Public(), peer)
	if plain, err := nip44.Decrypt(ciphertext, key); err != nil || plain != "secret note" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
	if plain, err := k.Decrypt(context.Background(), ciphertext, peer.Public()); err != nil || plain != "secret note" {
		t.Fatalf("round trip = %q, %v", plain, err)
	}
}

func TestSystemSignerRejectsAlteredEvent(t *testing.T) {
	f := startFakeSystemSigner(t)
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	if _, err := s.SwitchSystem(context.Background(), f.path, nil); err != nil {
		t.Fatal(err)
	}
	f.forge.Store(true)
	if err := s.keyer.SignEvent(context.Background(), &nostr.Event{Kind: 1, CreatedAt: nostr.Now(), Content: "hello"}); err == nil {
		t.Fatal("accepted an event the signer changed")
	}
}

func TestSystemSignerConnectsWhenUnlocked(t *testing.T) {
	saved := systemSignerRetry
	systemSignerRetry = []time.Duration{10 * time.Millisecond}
	t.Cleanup(func() { systemSignerRetry = saved })
	f := startFakeSystemSigner(t)
	f.locked.Store(true)
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	status, err := s.SwitchSystem(context.Background(), f.path, nil)
	if err != nil || status.Mode != "system" || status.ConnectionState != "disconnected" {
		t.Fatalf("locked switch = %+v, %v", status, err)
	}
	f.locked.Store(false)
	deadline := time.Now().Add(5 * time.Second)
	for s.Status().ConnectionState != "connected" {
		if time.Now().After(deadline) {
			t.Fatal("never connected after unlock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Status().PublicKey != f.sk.Public().Hex() {
		t.Fatalf("status = %+v", s.Status())
	}
}

func TestSystemSignerSwitchAwayStopsRetry(t *testing.T) {
	saved := systemSignerRetry
	systemSignerRetry = []time.Duration{10 * time.Millisecond}
	t.Cleanup(func() { systemSignerRetry = saved })
	f := startFakeSystemSigner(t)
	f.locked.Store(true)
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	if _, err := s.SwitchSystem(context.Background(), f.path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Switch(context.Background(), "none", "", nil); err != nil {
		t.Fatal(err)
	}
	f.locked.Store(false)
	time.Sleep(100 * time.Millisecond)
	if status := s.Status(); status.Mode != "none" || status.ConnectionState != "disconnected" {
		t.Fatalf("a stale retry reconnected: %+v", status)
	}
}

func TestSystemSignerRejectsRelativeSocket(t *testing.T) {
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	if _, err := s.SwitchSystem(context.Background(), "signer.sock", nil); err == nil {
		t.Fatal("accepted a relative socket")
	}
}
