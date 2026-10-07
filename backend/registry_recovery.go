package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"verdana/backend/fileutil"
)

// mutationIntent survives a process exit at any directory or state boundary.
// All directory names are single, validated entries beneath napps/.
type mutationIntent struct {
	Version        int    `json:"version"`
	ID             string `json:"id"`
	Operation      string `json:"operation"`
	Token          string `json:"token"`
	PriorEvent     string `json:"prior_event,omitempty"`
	HadPrior       bool   `json:"had_prior,omitempty"`
	NewEvent       string `json:"new_event,omitempty"`
	Prior          *Napp  `json:"prior,omitempty"`
	Base           string `json:"base"`
	Staging        string `json:"staging,omitempty"`
	Old            string `json:"old,omitempty"`
	PendingReclaim bool   `json:"pending_reclaim,omitempty"`
}

func mutationPath(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(dataDir, "mutations", hex.EncodeToString(sum[:])+".json")
}

func safeMutationRoot(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe registry directory %s", filepath.Base(path))
	}
	return nil
}

func validMutationName(name, base string) bool {
	return name == base || (len(name) <= len(base)+64 &&
		(strings.HasPrefix(name, base+stagingInfix) || strings.HasPrefix(name, base+oldInfix)) &&
		filepath.Base(name) == name && filepath.IsLocal(name))
}

func validateMutation(m mutationIntent, filename string) error {
	base, err := nappBaseDir(m.ID)
	if err != nil || m.ID == "" || m.Version != 1 || len(m.ID) > 4096 || len(m.Token) != 32 ||
		(m.Operation != "install" && m.Operation != "update" && m.Operation != "uninstall") ||
		m.Base != filepath.Base(base) || filename != mutationPath(m.ID) ||
		(m.Staging != "" && (!validMutationName(m.Staging, m.Base) || !strings.HasPrefix(m.Staging, m.Base+stagingInfix))) ||
		(m.Old != "" && (!validMutationName(m.Old, m.Base) || !strings.HasPrefix(m.Old, m.Base+oldInfix))) {
		return errors.New("invalid registry mutation intent")
	}
	for _, c := range m.Token {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return errors.New("invalid mutation token")
		}
	}
	if m.Operation == "uninstall" && (m.Staging != "" || m.Old != "") {
		return errors.New("invalid uninstall intent")
	}
	if m.Prior != nil && (m.Prior.ID != m.ID || m.Prior.EventID != m.PriorEvent) {
		return errors.New("prior registry identity differs from intent")
	}
	if m.HadPrior && m.Prior == nil {
		return errors.New("mutation intent lacks prior record")
	}
	if m.Operation == "uninstall" && (!m.HadPrior || m.Prior == nil) {
		return errors.New("uninstall intent lacks prior record")
	}
	return nil
}

func writeMutation(m mutationIntent) error {
	path := mutationPath(m.ID)
	if err := validateMutation(m, path); err != nil {
		return err
	}
	if err := safeMutationRoot(filepath.Join(dataDir, "napps")); err != nil {
		return err
	}
	if err := safeMutationRoot(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("registry mutation pending for %s", m.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) > 16384 {
		return errors.New("registry mutation intent exceeds size limit")
	}
	return fileutil.WriteFileAtomic(path, b, 0600)
}

func deferReclaimMutation(m mutationIntent) error {
	m.PendingReclaim = true
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(mutationPath(m.ID), b, 0600)
}

func supersedePendingUninstall(id string) error {
	b, err := os.ReadFile(mutationPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var m mutationIntent
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if err := validateMutation(m, mutationPath(id)); err != nil {
		return err
	}
	if m.Operation != "uninstall" || !m.PendingReclaim || state.MutationTokens[id] != m.Token {
		return fmt.Errorf("registry mutation pending for %s", id)
	}
	return removeMutation(m)
}

func removeMutation(m mutationIntent) error { return os.Remove(mutationPath(m.ID)) }

func mutationDir(name string) string { return filepath.Join(dataDir, "napps", name) }

func mutationExists(id string) bool {
	_, err := os.Lstat(mutationPath(id))
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func mutationExistsByBase(base string) bool {
	entries, err := os.ReadDir(filepath.Join(dataDir, "mutations"))
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(dataDir, "mutations", entry.Name()))
		if err != nil {
			return true
		}
		var m mutationIntent
		if json.Unmarshal(b, &m) != nil {
			return true
		}
		if m.Base == filepath.Base(base) {
			return true
		}
	}
	return false
}

func removeMutationDir(name string) error {
	if name == "" {
		return nil
	}
	path := mutationDir(name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe registry directory %s", name)
	}
	return os.RemoveAll(path)
}

func dirExists(name string) (bool, error) {
	info, err := os.Lstat(mutationDir(name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("unsafe registry directory %s", name)
	}
	return true, nil
}

// reconcileMutation makes the persisted token and record authoritative.
// It is safe to call again after an interruption during its own cleanup.
func reconcileMutation(m mutationIntent) error {
	committed := state.MutationTokens[m.ID] == m.Token
	record, installed := state.InstalledNapps[m.ID]
	if m.Operation == "uninstall" {
		if committed {
			if installed {
				return errors.New("committed uninstall still has a record")
			}
			if err := removeMutationDir(m.Base); err != nil {
				return err
			}
			if m.Prior == nil {
				return errors.New("uninstall intent lacks prior record")
			}
			if err := finishUninstallCleanup(m.ID, *m.Prior); err != nil {
				return err
			}
		} else if !installed {
			return errors.New("uncommitted uninstall lost its record")
		}
		return removeMutation(m)
	}
	if committed {
		if !installed || record.EventID != m.NewEvent {
			return errors.New("committed registry record differs from intent")
		}
		exists, err := dirExists(m.Base)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("committed install directory missing")
		}
		if err := removeMutationDir(m.Old); err != nil {
			return err
		}
		if err := removeMutationDir(m.Staging); err != nil {
			return err
		}
	} else {
		if m.HadPrior && (!installed || record.EventID != m.PriorEvent) {
			return errors.New("prior registry record differs from intent")
		}
		if !m.HadPrior && installed {
			return errors.New("uncommitted install has a record")
		}
		oldExists := false
		if m.Old != "" {
			var err error
			oldExists, err = dirExists(m.Old)
			if err != nil {
				return err
			}
		}
		if oldExists {
			if err := removeMutationDir(m.Base); err != nil {
				return err
			}
			if err := os.Rename(mutationDir(m.Old), mutationDir(m.Base)); err != nil {
				return err
			}
		} else if m.HadPrior {
			exists, err := dirExists(m.Base)
			if err != nil {
				return err
			}
			if !exists {
				return errors.New("prior install directory missing")
			}
		} else if err := removeMutationDir(m.Base); err != nil {
			return err
		}
		if err := removeMutationDir(m.Staging); err != nil {
			return err
		}
	}
	return removeMutation(m)
}

func recoverRegistryMutations() error {
	root := filepath.Join(dataDir, "mutations")
	if err := safeMutationRoot(root); err != nil {
		return err
	}
	if err := safeMutationRoot(filepath.Join(dataDir, "napps")); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 1024 {
		return errors.New("too many pending registry mutations")
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 16384 {
			return fmt.Errorf("unsafe registry intent %s", entry.Name())
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var m mutationIntent
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("invalid registry intent %s: %w", entry.Name(), err)
		}
		if err := validateMutation(m, path); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if err := reconcileMutation(m); err != nil {
			return fmt.Errorf("registry recovery for %s incomplete: %w", m.ID, err)
		}
	}
	return nil
}

func finishUninstallCleanup(id string, record Napp) error {
	var err error
	if record.IsNapplet() {
		var complete bool
		complete, err = reclaimNapplet(record, instancesForNapp(id))
		if !complete {
			err = errors.Join(err, ErrServicePartialCleanup)
		}
	}
	return errors.Join(err, ForgetPermission(id, ""), forgetActionUsage(id), forgetDispatchTarget(id))
}

// commitInstallMutation retains the old directory until state.json commits.
// The caller holds stateMu and the per-napp busy claim.
func commitInstallMutation(id, operation, staging, base string, next Napp, clearLastLaunched bool) (Napp, bool, error) {
	previous, had := state.InstalledNapps[id]
	if err := supersedePendingUninstall(id); err != nil {
		return previous, had, err
	}
	token := randomID()[:32]
	m := mutationIntent{Version: 1, ID: id, Operation: operation, Token: token,
		NewEvent: next.EventID, Base: filepath.Base(base), Staging: filepath.Base(staging)}
	if had {
		m.HadPrior = true
		m.PriorEvent = previous.EventID
		m.Prior = &previous
		if exists, err := dirExists(m.Base); err != nil {
			return previous, had, err
		} else if exists {
			m.Old = m.Base + oldInfix + token
		}
	}
	if err := writeMutation(m); err != nil {
		return previous, had, err
	}
	if m.Old != "" {
		if err := renameInstallDirRetrying(base, mutationDir(m.Old)); err != nil {
			_ = reconcileMutation(m)
			return previous, had, fmt.Errorf("set aside install: %w", err)
		}
	}
	if err := renameInstallDirRetrying(staging, base); err != nil {
		_ = reconcileMutation(m)
		return previous, had, fmt.Errorf("place install: %w", err)
	}
	state.InstalledNapps[id] = next
	if state.MutationTokens == nil {
		state.MutationTokens = make(map[string]string)
	}
	priorToken, tokenExisted := state.MutationTokens[id]
	state.MutationTokens[id] = token
	last, hadLast := state.LastLaunched[id]
	if clearLastLaunched {
		delete(state.LastLaunched, id)
	}
	if err := saveState(); err != nil {
		if had {
			state.InstalledNapps[id] = previous
		} else {
			delete(state.InstalledNapps, id)
		}
		if tokenExisted {
			state.MutationTokens[id] = priorToken
		} else {
			delete(state.MutationTokens, id)
		}
		if clearLastLaunched && hadLast {
			state.LastLaunched[id] = last
		}
		// Atomic persistence can fail after rename. Re-read disk before deciding.
		if rerr := reconcileMutationFromDisk(m); rerr != nil {
			return previous, had, fmt.Errorf("state persistence failed: %w; recovery pending: %v", err, rerr)
		}
		return previous, had, err
	}
	if err := reconcileMutation(m); err != nil {
		return previous, had, fmt.Errorf("install committed, cleanup pending: %w", err)
	}
	return previous, had, nil
}

func reconcileMutationFromDisk(m mutationIntent) error {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	var disk AppState
	if err := json.Unmarshal(b, &disk); err != nil {
		return err
	}
	state = disk
	return reconcileMutation(m)
}
