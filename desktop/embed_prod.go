//go:build !dev

package main

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"sync"
)

// A prod launcher carries its child program inside itself and runs nothing
// else: prepareChild extracts these bytes into the per-user cache dir and
// re-verifies them before every spawn. There is no fallback to a child next
// to the executable or in the working directory.

//go:embed child/child
var childBinary []byte

// failClosed makes prepareChild wrap its errors in
// backend.ErrWindowProgramUnavailable, so the user sees the child-unavailable
// notice instead of nothing happening.
const failClosed = true

// embeddedChildSum hashes the embedded child once per process (D-02); the
// bytes cannot change while the launcher runs.
var embeddedChildSum = sync.OnceValues(func() ([32]byte, error) {
	if len(childBinary) == 0 {
		return [32]byte{}, errors.New("no window program was built into this launcher")
	}
	return sha256.Sum256(childBinary), nil
})

// childSource is the child program to run and its sha256.
func childSource() (data []byte, sum [32]byte, err error) {
	sum, err = embeddedChildSum()
	if err != nil {
		return nil, sum, err
	}
	return childBinary, sum, nil
}
