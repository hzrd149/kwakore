//go:build linux

package daemon

import (
	"time"

	"verdana/backend"
	"verdana/backend/serviceconfig"
)

// DiagnosticError is an allow-listed summary. Detail is always generated from
// fixed service text; raw errors and operator-supplied values must not enter it.
type DiagnosticError struct {
	Category string    `json:"category"`
	Time     time.Time `json:"time"`
	Detail   string    `json:"detail"`
}

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
	RecentErrors []DiagnosticError       `json:"recent_errors"`
}

func (s *Service) Health() Health {
	s.mu.Lock()
	ready, closing, start, version := s.ready, s.closing, s.start, s.version
	s.mu.Unlock()
	storageStatus := "open"
	if closing {
		storageStatus = "closed"
	} else if s.manager.PersistenceError() != nil {
		storageStatus = "unhealthy"
	}
	return Health{Ready: ready && !closing, Version: version, UptimeSeconds: time.Since(start).Seconds(), ConfigStatus: "valid", StorageStatus: storageStatus, ActiveWindows: len(backend.OpenWindows())}
}

func (s *Service) Diagnostics() Diagnostics {
	s.mu.Lock()
	warning := s.warning
	errors := make([]DiagnosticError, s.errorCount)
	for i := range errors {
		errors[i] = s.recentErrors[(s.errorNext-s.errorCount+i+len(s.recentErrors))%len(s.recentErrors)]
	}
	s.mu.Unlock()
	return Diagnostics{ObservedFrom: "live", Health: s.Health(), Settings: s.manager.Effective(), Warning: warning, RecentErrors: errors}
}

func (s *Service) recordError(category, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordErrorLocked(category, detail)
}

// recordErrorLocked accepts only fixed, already-sanitized service summaries.
// Callers must not pass err.Error() or any client-supplied value.
func (s *Service) recordErrorLocked(category, detail string) {
	s.recentErrors[s.errorNext] = DiagnosticError{Category: category, Time: time.Now().UTC(), Detail: detail}
	s.errorNext = (s.errorNext + 1) % len(s.recentErrors)
	if s.errorCount < len(s.recentErrors) {
		s.errorCount++
	}
}
