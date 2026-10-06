//go:build dev

package main

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
)

// A dev launcher has no embedded window programs: it runs those built by
// `just run` on disk. The bytes are read and hashed fresh on every spawn (the developer
// rebuilds a program while the launcher runs) and still go through the same
// verified per-user directory as in prod, so there is one spawn path.

// failClosed is false: a dev build without a built child is a developer
// mistake, not a tampered install, so it never raises the child-unavailable
// notice.
const failClosed = false

// childCandidates are where a dev child may be, in order.
func childCandidates() []string {
	return windowCandidates("napplet")
}

func windowCandidates(kind string) []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), "child", kind))
	}
	return append(out, "./child/"+kind, "child/"+kind)
}

// childSource reads the first dev child that exists.
func childSource() (data []byte, sum [32]byte, err error) {
	return windowSource("napplet")
}

func windowSource(kind string) (data []byte, sum [32]byte, err error) {
	for _, path := range windowCandidates(kind) {
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
	return nil, sum, errors.New("no " + kind + " program found; build it with go build -o child/" + kind + " ./child")
}
