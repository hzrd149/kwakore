//go:build linux

package daemon

import (
	"bytes"
	"context"
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

func TestDaemonSignerRestore(t *testing.T) {
	p := daemonPaths(t)
	first, err := Open(p, "test")
	if err != nil {
		t.Fatal(err)
	}
	secret := nip19.EncodeNsec(nostr.Generate())
	status, err := first.signer.Switch(context.Background(), "nsec", secret, first.credentials.write)
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
