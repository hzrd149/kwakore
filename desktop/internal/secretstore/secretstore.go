// Package secretstore keeps the launcher's login secrets in the OS keyring
// (Secret Service on Linux and the BSDs, the login keychain on macOS,
// Credential Manager on Windows) through github.com/zalando/go-keyring, and
// implements backend.SecretStore on top of it.
//
// go-keyring calls block for as long as the OS takes, with no way to cancel
// them: on Linux an unlock prompt waits for the user with no timeout. So
// every call goes to one worker goroutine that runs them one at a time, in
// the order they were made, and the caller waits only so long (callTimeout).
// A call that times out keeps running in the worker and the calls queued
// behind it wait for it. That is on purpose: a late Set can never land after
// a newer one, and two unlock prompts are never on screen at once. A Get for
// an item that is already being read joins that read instead of queueing a
// second one, so retrying while a prompt is open does not open another.
//
// Only "no such item" maps to backend.ErrSecretNotFound. Everything else,
// including an unlock prompt the user dismissed (which go-keyring reports as
// a failure to unlock the collection, not as a missing item), a value too
// large for the platform, a missing keyring service and a timeout, wraps
// backend.ErrSecretStoreUnavailable: the backend must never read "locked" as
// "empty" and replace the user's login with a fresh one.
//
// Errors carry what failed and why, never the value being stored.
package secretstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"verdana/backend"
)

// service is the keyring service every item is stored under; the backend
// names the account.
const service = "Verdana"

// How long a caller waits. A Get may show an unlock prompt, so it gets long
// enough for a person to type a password.
var (
	callTimeout = 120 * time.Second
)

// queueSize bounds the calls waiting for the worker. The backend makes one
// store call at a time, so a full queue means the keyring has been stuck for
// a long while and failing fast is the right answer.
const queueSize = 64

// provider is the part of go-keyring the store uses, so tests can put a
// blocking or failing fake in its place.
type provider interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// keyringProvider is the real OS keyring.
type keyringProvider struct{}

func (keyringProvider) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (keyringProvider) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (keyringProvider) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

type opKind int

const (
	opGet opKind = iota
	opSet
	opDelete
)

func (k opKind) String() string {
	switch k {
	case opGet:
		return "read"
	case opSet:
		return "write"
	default:
		return "delete"
	}
}

// request is one call queued for the worker. done is closed once value and
// err are set, so any number of waiters (or none, when they all timed out)
// can read the result without ever blocking the worker.
type request struct {
	op    opKind
	name  string
	value string

	done   chan struct{}
	result string
	err    error
}

// Store is a backend.SecretStore backed by the OS keyring.
type Store struct {
	p           provider
	queue       chan *request
	callTimeout time.Duration
	// probe reports whether a keyring service is there at all, so a
	// session without one fails at once instead of after a timeout.
	probe func() bool
}

var _ backend.SecretStore = (*Store)(nil)

// New starts the worker and returns the store for Options.Secrets.
func New() backend.SecretStore {
	return newStore(keyringProvider{})
}

func newStore(p provider) *Store {
	s := &Store{
		p:           p,
		queue:       make(chan *request, queueSize),
		callTimeout: callTimeout,
		probe:       func() bool { return true },
	}
	go s.work()
	return s
}

// work is the only goroutine that calls the provider.
func (s *Store) work() {
	for req := range s.queue {
		s.run(req)
	}
}

func (s *Store) run(req *request) {
	defer close(req.done)
	defer func() {
		if r := recover(); r != nil {
			req.result = ""
			req.err = fmt.Errorf("%w: keyring %s panicked", backend.ErrSecretStoreUnavailable, req.op)
		}
	}()
	var err error
	switch req.op {
	case opGet:
		req.result, err = s.p.Get(service, req.name)
	case opSet:
		err = s.p.Set(service, req.name, req.value)
	case opDelete:
		err = s.p.Delete(service, req.name)
	}
	if err != nil {
		req.result = ""
		req.err = mapError(req.op, err, req.value)
	}
}

// mapError turns a go-keyring error into one of the backend's two kinds.
// value is what a Set was storing; it is cut out of the provider's message
// in case a platform tool echoed it back.
func mapError(op opKind, err error, value string) error {
	if errors.Is(err, keyring.ErrNotFound) {
		return backend.ErrSecretNotFound
	}
	msg := err.Error()
	if value != "" {
		msg = strings.ReplaceAll(msg, value, "[redacted]")
	}
	return fmt.Errorf("%w: keyring %s failed: %s", backend.ErrSecretStoreUnavailable, op, msg)
}

func (s *Store) Get(name string) (string, error) {
	return s.call(opGet, name, "")
}

func (s *Store) Set(name, value string) error {
	_, err := s.call(opSet, name, value)
	return err
}

func (s *Store) Delete(name string) error {
	_, err := s.call(opDelete, name, "")
	return err
}

func (s *Store) call(op opKind, name, value string) (string, error) {
	if !s.probe() {
		return "", fmt.Errorf("%w: no keyring service", backend.ErrSecretStoreUnavailable)
	}
	req := &request{op: op, name: name, value: value, done: make(chan struct{})}
	select {
	case s.queue <- req:
	default:
		return "", fmt.Errorf("%w: keyring busy", backend.ErrSecretStoreUnavailable)
	}
	return s.wait(req)
}

func (s *Store) wait(req *request) (string, error) {
	timer := time.NewTimer(s.callTimeout)
	defer timer.Stop()
	select {
	case <-req.done:
		return req.result, req.err
	case <-timer.C:
		return "", fmt.Errorf("%w: keyring %s timed out after %s", backend.ErrSecretStoreUnavailable, req.op, s.callTimeout)
	}
}
