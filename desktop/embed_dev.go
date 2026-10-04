//go:build dev

package main

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
)

// A dev launcher has no embedded child: it runs the one `just run` built on
// disk. The bytes are read and hashed fresh on every spawn (the developer
// rebuilds the child while the launcher runs) and still go through the same
// verified per-user directory as in prod, so there is one spawn path.

// failClosed is false: a dev build without a built child is a developer
// mistake, not a tampered install, so it never raises the child-unavailable
// notice.
const failClosed = false

// childCandidates are where a dev child may be, in order.
func childCandidates() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), "child", "child"))
	}
	return append(out, "./child/child", "child/child")
}

// childSource reads the first dev child that exists.
func childSource() (data []byte, sum [32]byte, err error) {
	for _, path := range childCandidates() {
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, sum, err
		}
		if len(data) == 0 {
			return nil, sum, errors.New(path + " is empty")
		}
		return data, sha256.Sum256(data), nil
	}
	return nil, sum, errors.New("no child program found; build it with go build -o child/child ./child")
}
