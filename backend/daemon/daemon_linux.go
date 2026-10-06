//go:build linux

// Package daemon owns the foreground Linux service lifecycle.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"verdana/backend"
	"verdana/backend/serviceconfig"
)

var ErrClosing = errors.New("service is shutting down")

type Service struct {
	mu           sync.Mutex
	work         sync.WaitGroup
	closing      bool
	ready        bool
	start        time.Time
	version      string
	warning      string
	manager      *serviceconfig.Manager
	closeBackend func()
	lock         *os.File
}

func Open(paths serviceconfig.Paths, version string) (_ *Service, err error) {
	m, err := serviceconfig.Load(paths)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("%s: %w", paths.DataDir, err)
	}
	if err := checkPrivateDir(paths.DataDir); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(paths.DataDir, "daemon.lock")
	if info, e := os.Lstat(lockPath); e == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: lock must be a regular file", lockPath)
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			lock.Close()
		}
	}()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("daemon already running for this user; inspect status or stop the existing instance: %w", err)
	}
	closeBackend, err := backend.Start(backend.Options{DataDir: paths.DataDir, ServiceConfig: m})
	if err != nil {
		return nil, err
	}
	return &Service{ready: true, start: time.Now(), version: version, manager: m, closeBackend: closeBackend, lock: lock}, nil
}

func checkPrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("%s: data directory must be a private 0700 directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("%s: data directory must be owned by current user", path)
	}
	return nil
}

func (s *Service) Begin() (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, ErrClosing
	}
	s.work.Add(1)
	return s.work.Done, nil
}

func (s *Service) SetSetting(field string, value any) error {
	done, err := s.Begin()
	if err != nil {
		return err
	}
	defer done()
	return s.manager.SetOverride(field, value)
}

func (s *Service) ClearSetting(field string) error {
	done, err := s.Begin()
	if err != nil {
		return err
	}
	defer done()
	return s.manager.ClearOverride(field)
}

func (s *Service) Reload() error {
	done, err := s.Begin()
	if err != nil {
		return err
	}
	defer done()
	err = s.manager.Reload()
	s.mu.Lock()
	if err == nil {
		s.warning = ""
	} else {
		s.warning = "configuration reload rejected"
	}
	s.mu.Unlock()
	return err
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.ready = false
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.work.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	s.closeBackend()
	syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	s.lock.Close()
}

func Run(ctx context.Context, paths serviceconfig.Paths, version string, ready func(string)) error {
	s, err := Open(paths, version)
	if err != nil {
		return err
	}
	defer s.Close()
	ready(fmt.Sprintf("kwakore-daemon %s ready (config: %s)", version, paths.ConfigFile))
	<-ctx.Done()
	return nil
}

func (s *Service) Manager() *serviceconfig.Manager { return s.manager }
