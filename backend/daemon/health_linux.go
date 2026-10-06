//go:build linux

package daemon

import (
	"time"
	"verdana/backend"
	"verdana/backend/serviceconfig"
)

type Health struct {
	Ready         bool    `json:"ready"`
	Version       string  `json:"version"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	ConfigStatus  string  `json:"config_status"`
	StorageStatus string  `json:"storage_status"`
	ActiveWindows int     `json:"active_windows"`
}

type Diagnostics struct {
	ObservedFrom string                  `json:"observed_from"`
	Health       Health                  `json:"health"`
	Settings     serviceconfig.Effective `json:"settings"`
	Warning      string                  `json:"warning,omitempty"`
}

func (s *Service) Health() Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Health{Ready: s.ready, Version: s.version, UptimeSeconds: time.Since(s.start).Seconds(), ConfigStatus: "valid", StorageStatus: "open", ActiveWindows: len(backend.OpenWindows())}
}

func (s *Service) Diagnostics() Diagnostics {
	s.mu.Lock()
	warning := s.warning
	s.mu.Unlock()
	return Diagnostics{ObservedFrom: "live", Health: s.Health(), Settings: s.manager.Effective(), Warning: warning}
}
