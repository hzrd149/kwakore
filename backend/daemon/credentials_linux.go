//go:build linux

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"verdana/backend/fileutil"
)

const maxCredentialBytes = 4096

type credentialRecord struct {
	Version int    `json:"version"`
	Mode    string `json:"mode"`
	Secret  string `json:"secret,omitempty"`
}

type credentialStore struct{ path string }

var errCredential = errors.New("signer credential unavailable")

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
	fd, err := syscall.Open(s.path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
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
	data := make([]byte, info.Size()+1)
	n, err := f.Read(data)
	if err != nil && n == 0 {
		return credentialRecord{}, errCredential
	}
	if n != int(info.Size()) {
		return credentialRecord{}, errCredential
	}
	var rec credentialRecord
	dec := json.NewDecoder(bytes.NewReader(data[:n]))
	dec.DisallowUnknownFields()
	if dec.Decode(&rec) != nil || rec.Version != 1 || (rec.Mode != "none" && rec.Mode != "nsec") || (rec.Mode == "none" && rec.Secret != "") || (rec.Mode == "nsec" && (rec.Secret == "" || len(rec.Secret) > 256)) {
		return credentialRecord{}, errCredential
	}
	return rec, nil
}

func (s *credentialStore) write(mode, secret string) error {
	if mode != "none" && mode != "nsec" || len(secret) > 256 || (mode == "nsec" && secret == "") {
		return errCredential
	}
	// Refuse an unsafe existing destination before atomic replacement.
	if _, err := s.read(); err != nil {
		return errCredential
	}
	data, _ := json.Marshal(credentialRecord{Version: 1, Mode: mode, Secret: secret})
	if err := fileutil.WriteFileAtomic(s.path, data, 0600); err != nil {
		return errCredential
	}
	got, err := s.read()
	if err != nil || got.Mode != mode || got.Secret != secret {
		return errCredential
	}
	return nil
}
