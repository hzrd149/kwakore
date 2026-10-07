//go:build linux

package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"

	"kwakore/backend"
)

// ─── installed-artifact smoke fixture ───────────────────────────────────────
//
// scripts/smoke-linux-service.sh --full needs one installed napplet without
// any relay or blossom server, so it runs this test with
//
//	KWAKORE_SMOKE_SEED_DATA  the stopped daemon's data directory
//	                         ($XDG_DATA_HOME/kwakore, owner-only)
//	KWAKORE_SMOKE_SEED_OUT   an owner-only directory for the smoke's inputs
//
// It adds one napplet to state.json the way a committed install records it
// (every other state field is kept byte for byte), writes its index.html in
// the napp directory, and writes into the output directory:
//
//	address      the napplet's canonical address
//	signer.nsec  a fresh throwaway nsec (0600), for the signer secret check
//	signer.pub   that key's public key in hex
//
// The napplet is authored by a fixed test key with a fixed manifest, so the
// address, the artifact hash and the manifest event id are the same on every
// run; the manifest event is really signed and its signature checked. With
// neither variable set (the ordinary `go test ./...`) the test seeds a
// temporary directory holding an unrelated state field and checks the result,
// so the fixture cannot rot unnoticed. Setting only one variable is an error.

const smokeSeedD = "kwakore-smoke"

var smokeSeedContent = []byte(`<!doctype html>
<html><head><meta charset="utf-8"><title>Kwakore smoke</title></head>
<body><p>kwakore installed-artifact smoke</p></body></html>
`)

func TestInstalledSmokeSeed(t *testing.T) {
	dataDir := os.Getenv("KWAKORE_SMOKE_SEED_DATA")
	outDir := os.Getenv("KWAKORE_SMOKE_SEED_OUT")
	selfCheck := dataDir == "" && outDir == ""
	switch {
	case selfCheck:
		root := t.TempDir()
		dataDir = filepath.Join(root, "data", "kwakore")
		outDir = filepath.Join(root, "out")
		for _, dir := range []string{dataDir, outDir} {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		// a state file the daemon already wrote; the seed must keep it
		if err := os.WriteFile(filepath.Join(dataDir, "state.json"), []byte(`{"smoke_unrelated":{"kept":true},"installed_napps":{}}`), 0600); err != nil {
			t.Fatal(err)
		}
	case dataDir == "" || outDir == "":
		t.Fatal("set both KWAKORE_SMOKE_SEED_DATA and KWAKORE_SMOKE_SEED_OUT, or neither")
	}
	for _, dir := range []string{dataDir, outDir} {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
			t.Fatalf("%s is not an absolute clean path", dir)
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			t.Fatalf("%s must be an existing 0700 directory: %v", dir, err)
		}
	}

	napp := seedSmokeNapplet(t, dataDir)
	seedSmokeOutputs(t, outDir, napp)
	t.Logf("seeded %s", napp.Address())

	if selfCheck {
		checkSmokeSeed(t, dataDir, outDir, napp)
	}
}

// seedSmokeNapplet records the fixture napplet in dataDir/state.json and its
// file under dataDir/napps, as a committed install leaves them.
func seedSmokeNapplet(t *testing.T, dataDir string) backend.Napp {
	t.Helper()
	key := nostr.MustSecretKeyFromHex(strings.Repeat("0", 63) + "1")
	sum := sha256.Sum256(smokeSeedContent)
	artifact := hex.EncodeToString(sum[:])
	manifest := nostr.Event{
		Kind:      backend.KindNapplet,
		CreatedAt: 1767225600, // 2026-01-01T00:00:00Z
		Tags: nostr.Tags{
			{"d", smokeSeedD},
			{"title", "Kwakore smoke"},
			{"path", "/index.html", artifact},
		},
	}
	if err := manifest.Sign(key); err != nil {
		t.Fatal(err)
	}
	if !manifest.VerifySignature() || manifest.PubKey != key.Public() {
		t.Fatal("fixture manifest signature does not verify")
	}
	napp := backend.Napp{
		D:           smokeSeedD,
		Name:        "Kwakore smoke",
		Description: "Installed-artifact smoke napplet",
		Author:      key.Public(),
		CreatedAt:   manifest.CreatedAt,
		Paths:       []backend.NappPath{{Path: "/index.html", Sha256: artifact}},
		Format:      backend.FormatNapplet,
		Kind:        backend.KindNapplet,
		// the one-file napplet's artifact is its index.html
		ArtifactHash: artifact,
		EventID:      manifest.ID.Hex(),
	}
	napp.ID = napp.Address()

	statePath := filepath.Join(dataDir, "state.json")
	fields := map[string]json.RawMessage{}
	switch raw, err := os.ReadFile(statePath); {
	case err == nil:
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("existing state.json is not a JSON object: %v", err)
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		t.Fatal(err)
	}
	installed := map[string]backend.Napp{}
	if raw, ok := fields["installed_napps"]; ok && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &installed); err != nil {
			t.Fatalf("existing installed_napps: %v", err)
		}
	}
	if _, ok := installed[napp.ID]; ok {
		t.Fatalf("%s is already installed; seed a fresh data directory", napp.ID)
	}
	installed[napp.ID] = napp
	encoded, err := json.Marshal(installed)
	if err != nil {
		t.Fatal(err)
	}
	fields["installed_napps"] = encoded
	state, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	writePrivate(t, statePath, state)

	dirSum := sha256.Sum256([]byte(napp.ID))
	nappDir := filepath.Join(dataDir, "napps", hex.EncodeToString(dirSum[:]))
	if err := os.MkdirAll(nappDir, 0700); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(nappDir, "index.html"), smokeSeedContent)
	return napp
}

func seedSmokeOutputs(t *testing.T, outDir string, napp backend.Napp) {
	t.Helper()
	signer := nostr.Generate()
	writePrivate(t, filepath.Join(outDir, "address"), []byte(napp.Address()+"\n"))
	writePrivate(t, filepath.Join(outDir, "signer.nsec"), []byte(nip19.EncodeNsec(signer)+"\n"))
	writePrivate(t, filepath.Join(outDir, "signer.pub"), []byte(signer.Public().Hex()+"\n"))
}

// writePrivate replaces path with an owner-only file through a rename, so a
// reader never sees a partial file.
func writePrivate(t *testing.T, path string, data []byte) {
	t.Helper()
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		t.Fatal(err)
	}
}

func checkSmokeSeed(t *testing.T, dataDir, outDir string, napp backend.Napp) {
	t.Helper()
	const wantAddress = "35129:79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798:" + smokeSeedD
	if napp.Address() != wantAddress {
		t.Fatalf("fixture address %s, want the fixed %s", napp.Address(), wantAddress)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["smoke_unrelated"]) != `{"kept":true}` {
		t.Fatalf("seed dropped an unrelated state field: %s", raw)
	}
	var state backend.AppState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	got, ok := state.InstalledNapps[napp.Address()]
	if !ok || len(state.InstalledNapps) != 1 || !got.IsNapplet() || got.Address() != napp.Address() {
		t.Fatalf("installed napps after seeding: %+v", state.InstalledNapps)
	}
	dirSum := sha256.Sum256([]byte(napp.Address()))
	index, err := os.ReadFile(filepath.Join(dataDir, "napps", hex.EncodeToString(dirSum[:]), "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(index); hex.EncodeToString(sum[:]) != got.ArtifactHash || got.IndexHash() != got.ArtifactHash {
		t.Fatal("seeded index.html does not match the recorded hashes")
	}
	for _, name := range []string{"address", "signer.nsec", "signer.pub"} {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s is not an owner-only file: %v", name, err)
		}
	}
	nsec, _ := os.ReadFile(filepath.Join(outDir, "signer.nsec"))
	pub, _ := os.ReadFile(filepath.Join(outDir, "signer.pub"))
	prefix, value, err := nip19.Decode(strings.TrimSpace(string(nsec)))
	sk, isKey := value.(nostr.SecretKey)
	if err != nil || prefix != "nsec" || !isKey || sk.Public().Hex() != strings.TrimSpace(string(pub)) {
		t.Fatalf("signer.nsec does not decode to signer.pub: %v", err)
	}
}
