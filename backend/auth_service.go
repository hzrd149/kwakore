package backend

import (
	"context"
	"errors"
	"sync"
)

// SignerStatus is the complete public signer read surface.
type SignerStatus struct {
	Mode            string `json:"mode"`
	PublicKey       string `json:"public_key"`
	ConnectionState string `json:"connection_state"`
}

type ServiceSigner struct {
	mu         sync.Mutex
	generation uint64
	status     SignerStatus
}

func (s *ServiceSigner) Generation() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.generation }
func (s *ServiceSigner) Status() SignerStatus { s.mu.Lock(); defer s.mu.Unlock(); return s.status }
func (s *ServiceSigner) Switch(ctx context.Context, mode, secret string, persist func(string, string) error) (SignerStatus, error) {
	return SignerStatus{Mode: mode, ConnectionState: "disconnected"}, errors.New("signer unavailable")
}
