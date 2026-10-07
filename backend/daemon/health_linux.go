//go:build linux

package daemon

import (
	"errors"
	"os"
	"time"

	"kwakore/backend"
	"kwakore/backend/serviceconfig"
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

// FileReport is a read-only observation of configuration files. Nil live
// fields encode as JSON null, including when a daemon.lock file is present.
// A separate process cannot establish readiness or read its error history.
type FileReport struct {
	ObservedFrom   string                  `json:"observed_from"`
	Ready          *bool                   `json:"ready"`
	UptimeSeconds  *float64                `json:"uptime_seconds"`
	ActiveWindows  *int                    `json:"active_windows"`
	ConfigStatus   string                  `json:"config_status"`
	OverrideStatus string                  `json:"override_status"`
	StorageStatus  string                  `json:"storage_status"`
	Settings       serviceconfig.Effective `json:"settings"`
	Warning        *string                 `json:"warning"`
	RecentErrors   *[]DiagnosticError      `json:"recent_errors"`
}

// InspectFiles validates with the startup loader and observes only files. It
// does not acquire the service lock, initialize stores, or create directories.
func InspectFiles(paths serviceconfig.Paths) (FileReport, error) {
	m, err := serviceconfig.Load(paths)
	if err != nil {
		return FileReport{}, err
	}
	config, err := filePresence(paths.ConfigFile, "valid")
	if err != nil {
		return FileReport{}, err
	}
	override, err := filePresence(paths.OverrideFile, "valid")
	if err != nil {
		return FileReport{}, err
	}
	storage, err := filePresence(paths.DataDir, "private")
	if err != nil {
		return FileReport{}, err
	}
	return FileReport{ObservedFrom: "files", ConfigStatus: config, OverrideStatus: override, StorageStatus: storage, Settings: m.Effective()}, nil
}

func filePresence(path, present string) (string, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	return present, nil
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
