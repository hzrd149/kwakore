//go:build linux

// Package daemon owns the foreground Linux service lifecycle.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"fiatjaf.com/nostr"

	"verdana/backend"
	"verdana/backend/linuxhost"
	"verdana/backend/serviceconfig"
)

var ErrClosing = errors.New("service is shutting down")

// windowProgramPath is replaceable by the socket integration test.
var windowProgramPath = linuxhost.DefaultProgramPath

type Service struct {
	mu           sync.Mutex
	operationMu  sync.Mutex
	work         sync.WaitGroup
	workContext  context.Context
	cancelWork   context.CancelFunc
	closeDone    chan struct{}
	closeOnce    sync.Once
	closing      bool
	ready        bool
	start        time.Time
	version      string
	warning      string
	recentErrors [32]DiagnosticError
	errorNext    int
	errorCount   int
	manager      *serviceconfig.Manager
	signer       *backend.ServiceSigner
	credentials  *credentialStore
	closeBackend func()
	lock         *os.File
}

func Open(paths serviceconfig.Paths, version string) (_ *Service, err error) {
	if err := checkNoSymlinkComponents(paths.DataDir); err != nil {
		return nil, err
	}
	m, err := serviceconfig.Load(paths)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("%s: %w", paths.DataDir, err)
	}
	if err := checkNoSymlinkComponents(paths.DataDir); err != nil {
		return nil, err
	}
	if err := checkPrivateDir(paths.DataDir); err != nil {
		return nil, err
	}
	credentials, err := openCredentialStore(paths.DataDir)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(paths.DataDir, "daemon.lock")
	if info, e := os.Lstat(lockPath); e == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: lock must be a regular file", lockPath)
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("%s: open private lock: %w", lockPath, err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer func() {
		if err != nil {
			lock.Close()
		}
	}()
	info, err := lock.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s: inspect lock: %w", lockPath, err)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("%s: lock must be a regular 0600 file owned by current user", lockPath)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("daemon already running for this user; inspect status or stop the existing instance: %w", err)
	}
	closeBackend, err := backend.Start(backend.Options{DataDir: paths.DataDir, ServiceConfig: m, Host: linuxhost.New(windowProgramPath())})
	if err != nil {
		return nil, err
	}
	workContext, cancelWork := context.WithCancel(context.Background())
	s := &Service{ready: true, start: time.Now(), version: version, manager: m, signer: &backend.ServiceSigner{}, credentials: credentials, closeBackend: closeBackend, lock: lock, workContext: workContext, cancelWork: cancelWork, closeDone: make(chan struct{})}
	// Startup stays available with a disconnected public status when the
	// requested signer cannot be restored. The private record never enters
	// ordinary service configuration or diagnostics.
	_ = s.reconcileSigner(context.Background())
	return s, nil
}

func (s *Service) reconcileSigner(ctx context.Context) error {
	requested := s.manager.Effective().Signer.Mode
	if requested == "" {
		requested = "none"
	}
	current := s.signer.Status()
	if current.Mode == requested && (requested == "none" || current.ConnectionState == "connected") {
		return nil
	}
	rec, err := s.credentials.read()
	if err != nil {
		_, _ = s.signer.Switch(ctx, requested, "", nil)
		return errCredential
	}
	secret := ""
	if requested == "nsec" && rec.Mode == "nsec" {
		secret = rec.Secret
	}
	if requested == "bunker" {
		if rec.Mode != "bunker" || rec.ClientKey == "" {
			_, _ = s.signer.Switch(ctx, "bunker", "", nil)
			return errCredential
		}
		key, keyErr := nostr.SecretKeyFromHex(rec.ClientKey)
		if keyErr != nil {
			return errCredential
		}
		_, err = s.signer.SwitchBunker(ctx, rec.Secret, key, true, nil, nil)
		return err
	}
	_, err = s.signer.Switch(ctx, requested, secret, nil)
	return err
}

func (s *Service) SwitchSigner(ctx context.Context, mode, secret string) (backend.SignerStatus, error) {
	done, err := s.Begin()
	if err != nil {
		return backend.SignerStatus{}, err
	}
	defer done()
	s.signer.PreemptPending()
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	if err := s.manager.SetSignerOverride(serviceconfig.Signer{Mode: mode}); err != nil {
		return backend.SignerStatus{}, errCredential
	}
	if mode == "bunker" {
		rec, err := s.credentials.read()
		if err != nil {
			return backend.SignerStatus{}, errCredential
		}
		key := nostr.Generate()
		if rec.ClientKey != "" {
			key, err = nostr.SecretKeyFromHex(rec.ClientKey)
			if err != nil {
				return backend.SignerStatus{}, errCredential
			}
		}
		return s.signer.SwitchBunker(ctx, secret, key, false, s.credentials.write, s.credentials.writeBunker)
	}
	return s.signer.Switch(ctx, mode, secret, s.credentials.write)
}

func (s *Service) StartSignerPair(secret string) (backend.ServicePairStart, error) {
	done, err := s.Begin()
	if err != nil {
		return backend.ServicePairStart{}, err
	}
	s.signer.PreemptPending()
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	rec, err := s.credentials.read()
	if err != nil {
		done()
		return backend.ServicePairStart{}, errCredential
	}
	key := nostr.Generate()
	if rec.ClientKey != "" {
		key, err = nostr.SecretKeyFromHex(rec.ClientKey)
		if err != nil {
			done()
			return backend.ServicePairStart{}, errCredential
		}
	}
	relay := s.manager.Effective().Signer.Relay
	if relay == "" {
		relay = backend.ServiceDefaultPairRelay()
	}
	start, err := s.signer.StartPair(s.workContext, secret, key, relay, func(ctx context.Context, url string, key nostr.SecretKey, expected uint64) (backend.SignerStatus, error) {
		s.operationMu.Lock()
		defer s.operationMu.Unlock()
		if s.signer.Generation() != expected || ctx.Err() != nil {
			return backend.SignerStatus{}, errCredential
		}
		if err := s.manager.SetSignerOverride(serviceconfig.Signer{Mode: "bunker", Relay: relay}); err != nil {
			return backend.SignerStatus{}, errCredential
		}
		return s.signer.SwitchBunkerPair(ctx, url, key, expected, s.credentials.writeBunker)
	}, done)
	if err != nil {
		done()
	}
	return start, err
}

func (s *Service) WaitSignerPair(ctx context.Context) (backend.SignerStatus, error) {
	done, err := s.Begin()
	if err != nil {
		return backend.SignerStatus{}, err
	}
	defer done()
	return s.signer.WaitPair(ctx)
}

func (s *Service) CancelSignerPair() (bool, error) {
	done, err := s.Begin()
	if err != nil {
		return false, err
	}
	defer done()
	return s.signer.CancelPair(), nil
}

func checkNoSymlinkComponents(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s: data directory must be absolute", path)
	}
	current := string(filepath.Separator)
	for _, part := range splitPath(path) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinked data path is unsafe; use a real directory", current)
		}
	}
	return nil
}

func splitPath(path string) []string {
	return strings.FieldsFunc(filepath.Clean(path), func(r rune) bool { return r == filepath.Separator })
}

func checkPrivateDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
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
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	before := s.manager.Effective()
	if err := s.manager.SetOverride(field, value); err != nil {
		s.recordError("setting_update", "setting update rejected")
		return err
	}
	s.notifySettingsChange(before)
	return nil
}

func (s *Service) ClearSetting(field string) error {
	done, err := s.Begin()
	if err != nil {
		return err
	}
	defer done()
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	before := s.manager.Effective()
	if err := s.manager.ClearOverride(field); err != nil {
		s.recordError("setting_update", "setting clear rejected")
		return err
	}
	s.notifySettingsChange(before)
	return nil
}

func (s *Service) notifySettingsChange(before serviceconfig.Effective) {
	after := s.manager.Effective()
	if slices.Equal(before.Relays, after.Relays) && slices.Equal(before.BlossomServers, after.BlossomServers) && before.DiscoverOnUserRelays == after.DiscoverOnUserRelays {
		return
	}
	discoveryChanged := !slices.Equal(before.Relays, after.Relays) || before.DiscoverOnUserRelays != after.DiscoverOnUserRelays
	backend.ServiceSettingsChanged(discoveryChanged)
}

func (s *Service) Reload() error {
	done, err := s.Begin()
	if err != nil {
		return err
	}
	defer done()
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	before := s.manager.Effective()
	err = s.manager.Reload()
	s.mu.Lock()
	if err == nil {
		s.warning = ""
	} else {
		s.warning = reloadWarning(s.manager.Paths().ConfigFile, err)
		s.recordErrorLocked("config_reload", s.warning)
	}
	warning := s.warning
	s.mu.Unlock()
	if err != nil {
		fmt.Fprintln(os.Stderr, warning)
		return err
	}
	s.notifySettingsChange(before)
	if before.Signer != s.manager.Effective().Signer {
		if err := s.reconcileSigner(s.workContext); err != nil {
			s.recordError("signer", "signer unavailable")
			return errCredential
		}
	}
	return nil
}

// reloadWarning contains only a fixed reason, the known config basename (or
// a redaction marker), and a supported field name. Validation errors can
// contain operator-supplied URLs and must never reach diagnostics or stderr.
func reloadWarning(path string, err error) string {
	name := filepath.Base(path)
	if name != "config.json" {
		name = "[redacted]"
	}
	field := "file"
	for _, name := range []string{"relays", "blossom_servers", "discover_on_user_relays", "signer"} {
		if strings.Contains(err.Error(), name) {
			field = name
			break
		}
	}
	return fmt.Sprintf("configuration reload rejected: %s: invalid %s", name, field)
}

// BeginShutdown rejects new leases and cancels accepted network work. It is
// safe to call more than once and never waits for a worker.
func (s *Service) BeginShutdown() {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.ready = false
	if s.closeDone == nil {
		s.closeDone = make(chan struct{})
	}
	s.mu.Unlock()
	if s.cancelWork != nil {
		s.cancelWork()
	}
}

func (s *Service) Close() {
	s.BeginShutdown()
	s.mu.Lock()
	done := s.closeDone
	s.mu.Unlock()
	s.closeOnce.Do(func() {
		s.work.Wait()
		if s.signer != nil {
			s.signer.Close()
		}
		if s.closeBackend != nil {
			s.closeBackend()
		}
		if s.lock != nil {
			_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
			_ = s.lock.Close()
		}
		close(done)
	})
	<-done
}

// registryContext ties network and staging work to both the client and the
// service. A lease remains held after cancellation until the operation returns.
func (s *Service) registryContext(client context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(client)
	if s.workContext == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(s.workContext, cancel)
	return ctx, func() { stop(); cancel() }
}

func Run(ctx context.Context, paths serviceconfig.Paths, version string, ready func(string)) error {
	s, err := Open(paths, version)
	if err != nil {
		return err
	}
	listener, err := s.Listen()
	if err != nil {
		s.Close()
		return err
	}
	ready(fmt.Sprintf("kwakore-daemon %s ready (config: %s)", version, paths.ConfigFile))
	<-ctx.Done()
	s.BeginShutdown()
	_ = listener.Close()
	s.Close()
	return nil
}

func (s *Service) Manager() *serviceconfig.Manager { return s.manager }
