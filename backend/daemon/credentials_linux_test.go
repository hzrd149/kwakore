//go:build linux

package daemon

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
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
	if err := os.Chmod(dir, 0700); err != nil { t.Fatal(err) }
	s, err := openCredentialStore(dir)
	if err != nil { t.Fatal(err) }
	url := "bunker://" + nostr.Generate().Public().Hex() + "?relay=wss%3A%2F%2Fexample.com&secret=private-sentinel"
	if err := s.write("bunker", url); err != nil { t.Fatal(err) }
	again, err := openCredentialStore(dir)
	if err != nil { t.Fatal(err) }
	rec, err := again.read()
	if err != nil || rec.Mode != "bunker" || rec.Secret != url { t.Fatalf("restore: %+v %v", rec, err) }
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
