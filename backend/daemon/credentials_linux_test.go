//go:build linux

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/nip19"
	"verdana/backend/serviceconfig"
)

func TestCredentialStorePrivateAndRestore(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, err := openCredentialStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.write("nsec", "nsec-test"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode: %v %v", info, err)
	}
	again, err := openCredentialStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := again.read()
	if err != nil || got.Mode != "nsec" || got.Secret != "nsec-test" {
		t.Fatalf("restore: %+v %v", got, err)
	}
}

func TestBunkerCredentialStableClientKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := openCredentialStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	url := "bunker://" + nostr.Generate().Public().Hex() + "?relay=wss%3A%2F%2Fexample.com&secret=private-sentinel"
	if err := s.write("bunker", url); err != nil {
		t.Fatal(err)
	}
	again, err := openCredentialStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := again.read()
	if err != nil || rec.Mode != "bunker" || rec.Secret != url || !validCredentialKey(rec.ClientKey) {
		t.Fatalf("restore: %+v %v", rec, err)
	}
	if err := again.write("none", ""); err != nil {
		t.Fatal(err)
	}
	retained, err := again.read()
	if err != nil || retained.ClientKey != rec.ClientKey {
		t.Fatalf("client key changed: %+v %v", retained, err)
	}
	if err := os.WriteFile(again.path, []byte(`{"version":1,"mode":"bunker","secret":"`+url+`","client_key":"invalid-sentinel"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := again.read(); err == nil || err.Error() != errCredential.Error() {
		t.Fatalf("invalid key accepted or leaked: %v", err)
	}
}

func TestCredentialStoreRejectsUnsafeFile(t *testing.T) {
	for _, kind := range []string{"symlink", "mode", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "signer-credentials.json")
			switch kind {
			case "symlink":
				if err := os.Symlink(filepath.Join(dir, "target"), path); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.WriteFile(path, []byte(`{"version":1,"mode":"none"}`), 0644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := openCredentialStore(dir); err == nil || err.Error() != errCredential.Error() {
				t.Fatalf("unsafe %s: %v", kind, err)
			}
		})
	}
}

func TestCredentialStoreAtomicFailureIsNotSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := openCredentialStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := credentialWriteAtomic
	credentialWriteAtomic = func(string, []byte, os.FileMode) error { return errors.New("private-sentinel") }
	t.Cleanup(func() { credentialWriteAtomic = old })
	if err := s.write("nsec", "nsec-test"); err == nil || err.Error() != errCredential.Error() {
		t.Fatalf("write failure: %v", err)
	}
	rec, err := s.read()
	if err != nil || rec.Mode != "none" {
		t.Fatalf("failed write changed record: %+v %v", rec, err)
	}
}

func TestDaemonSignerRestore(t *testing.T) {
	p := daemonPaths(t)
	first, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	secret := nip19.EncodeNsec(nostr.Generate())
	status, err := first.SwitchSigner(context.Background(), "nsec", secret)
	if err != nil || status.ConnectionState != "connected" {
		t.Fatalf("switch: %+v %v", status, err)
	}
	first.Close()
	second, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got := second.signer.Status()
	if got.ConnectionState != "connected" || got.PublicKey != status.PublicKey {
		t.Fatalf("restore: %+v", got)
	}
	state, err := os.ReadFile(filepath.Join(p.DataDir, "state.json"))
	if err != nil || bytes.Contains(state, []byte(secret)) {
		t.Fatalf("secret in state: %v", err)
	}
}

func TestDaemonFailedSignerSwitchRetainsCredentialAcrossRestart(t *testing.T) {
	for _, attempt := range []struct{ mode, secret string }{
		{"nsec", "malformed-nsec"},
		{"bunker", "bunker://invalid"},
	} {
		t.Run(attempt.mode, func(t *testing.T) {
			p := daemonPaths(t)
			first, err := Open(p, "test")
			if err != nil {
				t.Fatal(err)
			}
			secret := nip19.EncodeNsec(nostr.Generate())
			old, err := first.SwitchSigner(context.Background(), "nsec", secret)
			if err != nil || old.ConnectionState != "connected" {
				t.Fatalf("initial switch: %+v %v", old, err)
			}
			if _, err := first.SwitchSigner(context.Background(), attempt.mode, attempt.secret); err == nil {
				t.Fatal("invalid replacement accepted")
			}
			rec, err := first.credentials.read()
			if err != nil || rec.Mode != "nsec" || rec.Secret != secret || first.manager.Effective().Signer.Mode != "nsec" {
				t.Fatalf("failed switch changed durable signer: mode=%q config=%q err=%v", rec.Mode, first.manager.Effective().Signer.Mode, err)
			}
			first.Close()
			second, err := Open(p, "test")
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if got := second.signer.Status(); got.ConnectionState != "connected" || got.PublicKey != old.PublicKey {
				t.Fatalf("previous signer did not restore: %+v", got)
			}
		})
	}
}

func TestDaemonInterruptedSignerTransitionRestoresPrevious(t *testing.T) {
	p := daemonPaths(t)
	first, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	oldSecret := nip19.EncodeNsec(nostr.Generate())
	old, err := first.SwitchSigner(context.Background(), "nsec", oldSecret)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := first.credentials.read()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.credentials.beginTransition(rec, first.manager.Effective().Signer); err != nil {
		t.Fatal(err)
	}
	if err := first.credentials.write("nsec", nip19.EncodeNsec(nostr.Generate())); err != nil {
		t.Fatal(err)
	}
	if err := first.manager.SetSignerOverride(serviceconfig.Signer{Mode: "none"}); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := second.credentials.read()
	if err != nil || got != rec || second.manager.Effective().Signer.Mode != "nsec" {
		t.Fatalf("interrupted transition was not rolled back: mode=%q err=%v", got.Mode, err)
	}
	if status := second.signer.Status(); status.ConnectionState != "connected" || status.PublicKey != old.PublicKey {
		t.Fatalf("previous signer did not restore: %+v", status)
	}
	if pending, err := second.credentials.readTransition(); err != nil || pending != nil {
		t.Fatalf("transition journal was not cleared: %v", err)
	}
}

func TestDaemonFailedPairOverrideRestoresPreviousSigner(t *testing.T) {
	for _, afterWrite := range []bool{false, true} {
		name := "before-write"
		if afterWrite {
			name = "after-write"
		}
		t.Run(name, func(t *testing.T) {
			p := daemonPaths(t)
			s, err := Open(p, "test")
			if err != nil {
				t.Fatal(err)
			}
			oldSecret := nip19.EncodeNsec(nostr.Generate())
			old, err := s.SwitchSigner(context.Background(), "nsec", oldSecret)
			if err != nil {
				t.Fatal(err)
			}
			remote := nostr.Generate()
			s.signer.PairWait = func(context.Context, nostr.SecretKey, string, string) (nostr.PubKey, error) {
				return remote.Public(), nil
			}
			s.signer.BunkerConnect = func(context.Context, context.Context, nostr.SecretKey, string, bool) (nostr.Keyer, error) {
				return keyer.New(context.Background(), nil, nip19.EncodeNsec(remote), &keyer.SignerOptions{})
			}
			s.setSignerOverride = func(signer serviceconfig.Signer) error {
				if afterWrite {
					if err := s.manager.SetSignerOverride(signer); err != nil {
						return err
					}
				}
				return errors.New("simulated override failure")
			}
			if _, err := s.StartSignerPair(strings.Repeat("a", 32)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = s.WaitSignerPair(ctx)
			cancel()
			if err == nil {
				t.Fatal("failed pairing reported success")
			}
			rec, err := s.credentials.read()
			if err != nil || rec.Mode != "nsec" || rec.Secret != oldSecret || s.manager.Effective().Signer.Mode != "nsec" {
				t.Fatalf("failed pair changed durable signer: mode=%q config=%q err=%v", rec.Mode, s.manager.Effective().Signer.Mode, err)
			}
			s.Close()
			restarted, err := Open(p, "test")
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			if got := restarted.signer.Status(); got.ConnectionState != "connected" || got.PublicKey != old.PublicKey {
				t.Fatalf("failed pair changed restored signer: %+v", got)
			}
		})
	}
}

func TestDaemonLongBunkerRelayCanSwitchToNone(t *testing.T) {
	p := daemonPaths(t)
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	remote := nostr.Generate()
	relay := "wss://example.com/" + strings.Repeat("a", 1900)
	bunkerURL := "bunker://" + remote.Public().Hex() + "?relay=" + url.QueryEscape(relay)
	if len(bunkerURL) > 2048 {
		t.Fatalf("test bunker URL exceeds credential limit: %d", len(bunkerURL))
	}
	s.signer.BunkerConnect = func(context.Context, context.Context, nostr.SecretKey, string, bool) (nostr.Keyer, error) {
		return keyer.New(context.Background(), nil, nip19.EncodeNsec(remote), &keyer.SignerOptions{})
	}
	if status, err := s.SwitchSigner(context.Background(), "bunker", bunkerURL); err != nil || status.ConnectionState != "connected" {
		t.Fatalf("install long bunker signer: %+v %v", status, err)
	}
	previous, err := s.credentials.read()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := json.Marshal(signerTransition{Version: 1, Previous: previous, Signer: s.manager.Effective().Signer})
	if err != nil || len(journal) <= maxCredentialBytes {
		t.Fatalf("test transition does not exceed old limit: %d bytes, %v", len(journal), err)
	}
	if status, err := s.SwitchSigner(context.Background(), "none", ""); err != nil || status.Mode != "none" || status.ConnectionState != "disconnected" {
		t.Fatalf("clear long bunker signer: %+v %v", status, err)
	}
	if rec, err := s.credentials.read(); err != nil || rec.Mode != "none" || s.manager.Effective().Signer.Mode != "none" {
		t.Fatalf("clear did not persist: credential=%q config=%q err=%v", rec.Mode, s.manager.Effective().Signer.Mode, err)
	}
	s.Close()
	restarted, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if got := restarted.signer.Status(); got.Mode != "none" || got.ConnectionState != "disconnected" {
		t.Fatalf("cleared signer restored unexpectedly: %+v", got)
	}
}

func TestDaemonAmpersandHeavyConfiguredRelayCanSwitchToNone(t *testing.T) {
	p := daemonPaths(t)
	relay := "wss://example.com/" + strings.Repeat("&", 180000)
	config := []byte(`{"signer":{"mode":"bunker","relay":"` + relay + `"}}`)
	if len(config) >= 1<<20 {
		t.Fatalf("test config exceeds accepted size: %d", len(config))
	}
	if err := os.WriteFile(p.ConfigFile, config, 0600); err != nil {
		t.Fatal(err)
	}
	transition := signerTransition{Version: 1, Previous: credentialRecord{Version: 1, Mode: "none"}, Signer: serviceconfig.Signer{Mode: "bunker", Relay: relay}}
	htmlJournal, err := json.Marshal(transition)
	if err != nil || len(htmlJournal) <= maxSignerTransitionBytes {
		t.Fatalf("test does not reproduce HTML expansion: size=%d err=%v", len(htmlJournal), err)
	}
	journal, err := encodeSignerTransition(transition)
	if err != nil || len(journal) > maxSignerTransitionBytes || bytes.Contains(journal, []byte(`\u0026`)) {
		t.Fatalf("journal encoding exceeds bound or HTML-escapes relay: size=%d err=%v", len(journal), err)
	}
	first, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	if got := first.manager.Effective().Signer; got.Mode != "bunker" || got.Relay != relay {
		t.Fatalf("declarative relay not loaded: mode=%q relay_length=%d", got.Mode, len(got.Relay))
	}
	if status, err := first.SwitchSigner(context.Background(), "none", ""); err != nil || status.Mode != "none" {
		t.Fatalf("clear ampersand-heavy configured relay: %+v %v", status, err)
	}
	first.Close()
	restarted, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if rec, err := restarted.credentials.read(); err != nil || rec.Mode != "none" || restarted.manager.Effective().Signer.Mode != "none" {
		t.Fatalf("cleared signer did not persist: credential=%q config=%q err=%v", rec.Mode, restarted.manager.Effective().Signer.Mode, err)
	}
}

func TestReloadSignerReconcilesAndKeepsValidConfig(t *testing.T) {
	p := daemonPaths(t)
	secret := nip19.EncodeNsec(nostr.Generate())
	s, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.credentials.write("nsec", secret); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"signer":{"mode":"nsec"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil || s.signer.Status().ConnectionState != "connected" {
		t.Fatalf("reload: %v %+v", err, s.signer.Status())
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"signer":{"mode":"none","secret":"sentinel"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err == nil || s.signer.Status().ConnectionState != "connected" || s.manager.Effective().Signer.Mode != "nsec" {
		t.Fatalf("unsafe reload: %v %+v", err, s.signer.Status())
	}
	if err := s.credentials.write("none", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.ConfigFile, []byte(`{"signer":{"mode":"none"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil || s.signer.Status().ConnectionState != "disconnected" {
		t.Fatalf("clear: %v %+v", err, s.signer.Status())
	}
}
