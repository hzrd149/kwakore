//go:build linux

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip46"

	"kwakore/backend/fileutil"
	"kwakore/backend/serviceconfig"
)

const maxCredentialBytes = 4096

// A transition keeps both the private credential and the prior public signer
// config. With HTML escaping disabled below, the latter is bounded by the
// service config's 1 MiB file limit; 512 bytes covers journal field overhead.
const maxSignerTransitionBytes = maxCredentialBytes + (1 << 20) + 512

type credentialRecord struct {
	Version   int    `json:"version"`
	Mode      string `json:"mode"`
	Secret    string `json:"secret,omitempty"`
	ClientKey string `json:"client_key,omitempty"`
}

type credentialStore struct{ path string }

// A prepared transition retains the last known-good signer until both
// durable records have been written. Startup rolls it back after a crash.
type signerTransition struct {
	Version   int                  `json:"version"`
	Previous  credentialRecord     `json:"previous"`
	Signer    serviceconfig.Signer `json:"signer"`
	Committed bool                 `json:"committed,omitempty"`
}

var errCredential = errors.New("signer credential unavailable")
var credentialWriteAtomic = fileutil.WriteFileAtomic

func openCredentialStore(dataDir string) (*credentialStore, error) {
	if err := checkNoSymlinkComponents(dataDir); err != nil {
		return nil, errCredential
	}
	if err := checkPrivateDir(dataDir); err != nil {
		return nil, errCredential
	}
	s := &credentialStore{path: filepath.Join(dataDir, "signer-credentials.json")}
	if _, err := s.read(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *credentialStore) read() (credentialRecord, error) {
	fd, err := syscall.Open(s.path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return credentialRecord{Version: 1, Mode: "none"}, nil
	}
	if err != nil {
		return credentialRecord{}, errCredential
	}
	f := os.NewFile(uintptr(fd), s.path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() > maxCredentialBytes {
		return credentialRecord{}, errCredential
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return credentialRecord{}, errCredential
	}
	data := make([]byte, info.Size())
	if _, err := io.ReadFull(f, data); err != nil {
		return credentialRecord{}, errCredential
	}
	var rec credentialRecord
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&rec) != nil || rec.Version != 1 || (rec.Mode != "none" && rec.Mode != "nsec" && rec.Mode != "bunker") || (rec.Mode == "none" && rec.Secret != "") || (rec.Mode == "nsec" && (rec.Secret == "" || len(rec.Secret) > 256)) || (rec.Mode == "bunker" && (len(rec.Secret) > 2048 || !nip46.IsValidBunkerURL(rec.Secret))) || (rec.ClientKey != "" && !validCredentialKey(rec.ClientKey)) || (rec.Mode == "bunker" && rec.ClientKey == "") {
		return credentialRecord{}, errCredential
	}
	if dec.Decode(new(any)) != io.EOF {
		return credentialRecord{}, errCredential
	}
	return rec, nil
}

func (s *credentialStore) write(mode, secret string) error {
	if mode != "none" && mode != "nsec" && mode != "bunker" || len(secret) > 2048 || (mode == "nsec" && (secret == "" || len(secret) > 256)) || (mode == "none" && secret != "") || (mode == "bunker" && !nip46.IsValidBunkerURL(secret)) {
		return errCredential
	}
	// Refuse an unsafe existing destination before atomic replacement.
	previous, err := s.read()
	if err != nil {
		return errCredential
	}
	key := previous.ClientKey
	if mode == "bunker" && key == "" {
		key = nostr.Generate().Hex()
	}
	return s.writeRecord(credentialRecord{Version: 1, Mode: mode, Secret: secret, ClientKey: key})
}

func (s *credentialStore) writeBunker(secret, clientKey string) error {
	if len(secret) > 2048 || !nip46.IsValidBunkerURL(secret) || !validCredentialKey(clientKey) {
		return errCredential
	}
	return s.writeRecord(credentialRecord{Version: 1, Mode: "bunker", Secret: secret, ClientKey: clientKey})
}

func validCredentialKey(raw string) bool {
	key, err := nostr.SecretKeyFromHex(raw)
	return err == nil && key.Hex() == raw
}

func (s *credentialStore) writeRecord(rec credentialRecord) error {
	if _, err := s.read(); err != nil {
		return errCredential
	}
	data, _ := json.Marshal(rec)
	if err := credentialWriteAtomic(s.path, data, 0600); err != nil {
		return errCredential
	}
	got, err := s.read()
	if err != nil || got != rec {
		return errCredential
	}
	return nil
}

func (s *credentialStore) transitionPath() string { return s.path + ".transition" }

func encodeSignerTransition(transition signerTransition) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	// A canonical relay may contain many '&' characters. HTML escaping would
	// expand each one to six bytes, beyond the accepted config size bound.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(transition); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *credentialStore) readTransition() (*signerTransition, error) {
	path := s.transitionPath()
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errCredential
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > maxSignerTransitionBytes {
		return nil, errCredential
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errCredential
	}
	data := make([]byte, info.Size())
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, errCredential
	}
	var transition signerTransition
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&transition) != nil || dec.Decode(new(any)) != io.EOF || transition.Version != 1 || transition.Previous.Version != 1 {
		return nil, errCredential
	}
	return &transition, nil
}

func (s *credentialStore) beginTransition(previous credentialRecord, signer serviceconfig.Signer) error {
	if pending, err := s.readTransition(); err != nil || pending != nil {
		return errCredential
	}
	data, err := encodeSignerTransition(signerTransition{Version: 1, Previous: previous, Signer: signer})
	if err != nil || len(data) > maxSignerTransitionBytes {
		return errCredential
	}
	if err := credentialWriteAtomic(s.transitionPath(), data, 0600); err != nil {
		return errCredential
	}
	if pending, err := s.readTransition(); err != nil || pending == nil || pending.Previous != previous || pending.Signer != signer {
		return errCredential
	}
	return nil
}

func (s *credentialStore) commitTransition() error {
	pending, err := s.readTransition()
	if err != nil || pending == nil || pending.Committed {
		return errCredential
	}
	pending.Committed = true
	data, err := encodeSignerTransition(*pending)
	if err != nil || len(data) > maxSignerTransitionBytes {
		return errCredential
	}
	writeErr := credentialWriteAtomic(s.transitionPath(), data, 0600)
	observed, readErr := s.readTransition()
	if readErr != nil || observed == nil || *observed != *pending {
		return errCredential
	}
	// A sync error after rename may report failure even though the committed
	// marker is readable. Recovery will finish cleanup if the process stops.
	_ = writeErr
	return nil
}

func (s *credentialStore) endTransition() error {
	if err := os.Remove(s.transitionPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errCredential
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return errCredential
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return errCredential
	}
	return nil
}
