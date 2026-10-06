//go:build !dev

package main

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"sync"
)

// A prod launcher carries both window programs inside itself and runs nothing
// else: prepareWindowProgram extracts their bytes into the per-user cache dir and
// re-verifies them before every spawn. There is no fallback to a child next
// to the executable or in the working directory.

//go:embed child/napplet
var nappletBinary []byte

//go:embed child/napp
var nappBinary []byte

// failClosed makes prepareChild wrap its errors in
// backend.ErrWindowProgramUnavailable, so the user sees the child-unavailable
// notice instead of nothing happening.
const failClosed = true

// embeddedChildSum hashes the embedded child once per process (D-02); the
// bytes cannot change while the launcher runs.
var embeddedNappletSum = sync.OnceValues(func() ([32]byte, error) {
	if len(nappletBinary) == 0 {
		return [32]byte{}, errors.New("no window program was built into this launcher")
	}
	return sha256.Sum256(nappletBinary), nil
})

var embeddedNappSum = sync.OnceValues(func() ([32]byte, error) {
	if len(nappBinary) == 0 {
		return [32]byte{}, errors.New("no napp program was built into this launcher")
	}
	return sha256.Sum256(nappBinary), nil
})

// childSource is the child program to run and its sha256.
func childSource() (data []byte, sum [32]byte, err error) {
	return windowSource("napplet")
}

func windowSource(kind string) (data []byte, sum [32]byte, err error) {
	if kind == "napp" {
		sum, err = embeddedNappSum()
		if err != nil {
			return nil, sum, err
		}
		return nappBinary, sum, nil
	}
	sum, err = embeddedNappletSum()
	if err != nil {
		return nil, sum, err
	}
	return nappletBinary, sum, nil
}
