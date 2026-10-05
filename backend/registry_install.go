package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"

	"verdana/backend/fileutil"
	"verdana/backend/napconfig"
	"verdana/backend/netguard"
)

// backgroundSyncs tracks the shortcut and intent passes that installs,
// uninstalls and settings changes start in the background, the launch-time
// update checks and the Trys. They read host, dataDir and the update check's
// seams, so tests wait on it before they swap those globals.
var backgroundSyncs sync.WaitGroup

// refreshInstalled republishes the installed list into the launcher state.
// Ordering is installedNapps' business (most recently launched first), and the
// discovery list gets resorted around the new set: an install or uninstall
// moves its napp between the top and bottom halves of that list.
func refreshInstalled() {
	ls.mu.Lock()
	ls.installed = installedNapps()
	ls.sortDiscovery()
	ls.mu.Unlock()
	notifyState()
	backgroundSyncs.Go(broadcastIntentChanges)
	backgroundSyncs.Go(syncAppShortcuts)
}

// errUnavailable refuses to install, update to, try or open an entry whose
// latest manifest is invalid (Napp.Unavailable): there is nothing valid to
// download. Fixed text, so the launcher error reads "install failed: the
// latest version is invalid" and never the validator's own error, which can
// quote author input.
var errUnavailable = errors.New("the latest version is invalid")

// errBusy refuses an install, update or uninstall of a napp another one of
// them (or a Try) is already working on. Each claims the napp's busy flag
// with trySetBusy, so two of them never write the same install directory or
// race on its record, and none clears a claim it does not own.
var errBusy = errors.New("this napp is already being installed, updated or removed")

// errOlderVersion refuses to install a version older than the installed one
// (NIP-01 order: created_at, then the lowest event id). Installing it would
// downgrade the napp and, for a napplet, reclaim the newer version's
// storage and settings.
var errOlderVersion = errors.New("a newer version is already installed")

// olderThanInstalledLocked says whether n is older than the version
// installed under its id. stateMu is held.
func olderThanInstalledLocked(n Napp) bool {
	current, ok := state.InstalledNapps[n.ID]
	return ok && nappNewer(current, n)
}

// Install downloads a napp's files and records it as installed. Blocking:
// call it from a goroutine (progress shows up as IsBusy). It also takes
// updates: an already-installed napp is simply re-downloaded over. A failure
// is shown in the launcher.
func Install(n Napp) {
	if err := InstallNapp(n); err != nil {
		SetFetchErr("install failed: " + err.Error())
	}
}

// InstallNapp is Install for a caller that handles the failure itself. An
// unavailable entry is refused before anything is downloaded. The saved
// record keeps its EventID and never carries UpdateAvailable or
// Unavailable: those are Snapshot's to stamp.
func InstallNapp(n Napp) error {
	if n.Unavailable != "" {
		log.Warn().Str("napp", n.ID).Str("event", n.EventID).Str("reason", n.Unavailable).
			Msg("refusing to install an invalid latest version")
		return errUnavailable
	}
	n.UpdateAvailable = nil
	if !trySetBusy(n.ID) {
		log.Warn().Str("napp", n.ID).Msg("napp is busy, not installing")
		return errBusy
	}
	defer setBusy(n.ID, false)
	// checked before the download, which it would waste, and again when
	// the record is written
	stateMu.Lock()
	older := olderThanInstalledLocked(n)
	stateMu.Unlock()
	if older {
		log.Warn().Str("napp", n.ID).Str("event", n.EventID).Msg("refusing to install over a newer version")
		return errOlderVersion
	}
	log.Info().Str("napp", n.ID).Str("name", n.Name).Msg("installing napp")

	base, err := nappBaseDir(n.ID)
	if err != nil {
		log.Error().Err(err).Str("napp", n.ID).Msg("install failed")
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// the files land next to the install dir first: a failed download
	// leaves an installed copy exactly as it was, still launchable (D-10)
	servers := n.BlossomServers(ctx)
	staging, err := stageNappFiles(ctx, n, base, servers)
	if err != nil {
		log.Error().Err(err).Str("napp", n.ID).Msg("install failed")
		return err
	}

	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	if olderThanInstalledLocked(n) {
		stateMu.Unlock()
		os.RemoveAll(staging)
		log.Warn().Str("napp", n.ID).Str("event", n.EventID).Msg("refusing to install over a newer version")
		return errOlderVersion
	}
	// files and record change together, under stateMu
	removeOld, err := swapInstallDir(staging, base)
	if err != nil {
		stateMu.Unlock()
		os.RemoveAll(staging)
		log.Error().Err(err).Str("napp", n.ID).Msg("install failed")
		return err
	}
	previous, overwrote := state.InstalledNapps[n.ID]
	state.InstalledNapps[n.ID] = n
	saveState()
	stateMu.Unlock()
	removeOld()

	// an uninstall of this very version that was waiting for a window to
	// close must not delete what is installed again (D-24); the reclaim
	// itself checks the installed records too, this only tidies up
	if scope, err := nappletScope(n); err == nil {
		cancelPendingReclaim(scope)
	}
	// an install over another version of the napplet (the store's Update
	// button installs the newer event) supersedes it like an update does
	// (D-05); reinstalling the same version reclaims nothing
	if overwrote && previous.IsNapplet() && previous.ArtifactHash != n.ArtifactHash {
		reclaimNapplet(previous, instancesForNapp(n.ID))
	}

	refreshInstalled()
	log.Info().Str("napp", n.ID).Str("name", n.Name).Msg("install complete")
	return nil
}

// Uninstall removes a napp's files and forgets it. Installed-only by
// convention: it silently no-ops for ids the launcher doesn't know.
//
// A napplet goes entirely (D-06): its open windows close first (D-24), then
// its install directory, its record, its NAP-STORAGE files (shared and every
// instance a window of it had this run), its NAP-CONFIG file, its remembered
// answers, its action usage and the defaults pointing at it. Its storage and
// config wait for the last of its windows to be gone before they are
// deleted, so nothing is removed under a window still running.
func Uninstall(id string) {
	if !trySetBusy(id) {
		log.Warn().Str("napp", id).Msg("napp is busy, not uninstalling")
		SetFetchErr("uninstall failed: " + errBusy.Error())
		return
	}
	defer setBusy(id, false)
	log.Info().Str("napp", id).Msg("uninstalling napp")

	// the record goes first, and it goes together with listing the windows
	// to close, under reclaimMu: launchWindow re-reads the record and
	// registers its window under the same lock, so a launch in progress
	// either registered before this and is closed below, or reads the
	// record gone and opens nothing. The reclaim, which keeps any scope
	// that is still installed, then sees this one gone. Lock order:
	// reclaimMu, then stateMu, then the instance list.
	reclaimMu.Lock()
	stateMu.Lock()
	record, installed := state.InstalledNapps[id]
	delete(state.InstalledNapps, id)
	delete(state.LastLaunched, id)
	saveState()
	stateMu.Unlock()
	napplet := installed && record.IsNapplet()
	var closing []*Instance
	if napplet {
		closing = runningForNapp(id)
	}
	reclaimMu.Unlock()
	// a window registered but not yet attached to its transport closes as
	// soon as it attaches (Instance.Close)
	for _, ci := range closing {
		ci.Close()
	}

	// a napp whose directory cannot be named safely gets nothing removed,
	// but is still forgotten
	if base, err := nappBaseDir(id); err != nil {
		log.Warn().Err(err).Str("napp", id).Msg("napp directory not removed")
	} else {
		os.RemoveAll(base)
		removeStaleStaging(base)
	}

	if napplet {
		reclaimNapplet(record, instancesForNapp(id))
	}

	// what the user allowed or denied it is about the copy they had; a
	// reinstall starts from asking again
	ForgetPermission(id, "")
	// a napp that isn't installed can't be anyone's habitual handler, and
	// whatever the next one installed under that id shouldn't inherit it
	forgetActionUsage(id)
	forgetDispatchTarget(id)

	refreshInstalled()
	log.Info().Str("napp", id).Msg("uninstall complete")
}

// InstallFromDiscovery resolves an id the launcher knows — installed or just
// discovered — into a napp and installs (or updates) it.
func InstallFromDiscovery(id string) bool {
	if n, ok := InstalledNapp(id); ok {
		go Install(n)
		return true
	}
	if n, ok := DiscoveredNapp(id); ok {
		if n.Unavailable != "" {
			// refused right here: Install returns before any download, so
			// there is nothing to run in the background
			Install(n)
			return true
		}
		go Install(n)
		return true
	}
	return false
}

// TryNapplet downloads, verifies and opens a napplet without installing it.
// Its document stays in memory for the lifetime of the window and the napp is
// never added to InstalledNapps. A Try for a napplet that is busy (another
// Try, an install) is ignored. A Try that opens nothing says so in the
// notice stack and the store's error line, in fixed copy: the raw error,
// which can name servers and hashes, only goes to the log.
func TryNapplet(n Napp) {
	if n.Unavailable != "" {
		log.Warn().Str("napp", n.ID).Str("event", n.EventID).Str("reason", n.Unavailable).
			Msg("refusing to try an invalid latest version")
		SetFetchErr("try failed: " + errUnavailable.Error())
		raiseTrialFailed(n, trialFailedUnavailable)
		return
	}
	backgroundSyncs.Go(func() {
		err := tryNapplet(context.Background(), n)
		if err == nil {
			return
		}
		log.Error().Err(err).Str("napp", n.ID).Msg("napplet preview failed")
		switch {
		case errors.Is(err, errTrialFiles):
			SetFetchErr(trialFilesFetchErr)
			raiseTrialFailed(n, trialFailedBlob)
		case errors.Is(err, errUnavailable):
			SetFetchErr("try failed: " + errUnavailable.Error())
			raiseTrialFailed(n, trialFailedUnavailable)
		case errors.Is(err, ErrWindowProgramUnavailable):
			// launchWindow raised the child-unavailable notice already
			SetFetchErr(childUnavailableFetchErr)
		default:
			SetFetchErr("try failed: " + err.Error())
		}
	})
}

// errTrialFiles is a Try that opened nothing because one of the manifest's
// files could not be downloaded or did not match its hash (D-13).
var errTrialFiles = errors.New("a trial file failed to download or verify")

// trialFilesFetchErr is the store's error line for errTrialFiles.
const trialFilesFetchErr = "try failed: a file couldn't be downloaded or didn't match its manifest"

// tryNapplet downloads every file the manifest lists and checks each against
// its sha256 before the window opens with the index document (NIP-5D: fetch
// each path blob and verify it). A trial never opens on part of a napplet:
// any file that fails stops the others and opens nothing. A napplet that is
// busy already is left alone and nil is returned.
func tryNapplet(ctx context.Context, n Napp) error {
	if n.Unavailable != "" {
		return errUnavailable
	}
	if !n.IsNapplet() {
		return errors.New("only napplets can be tried without installing")
	}
	if n.ID == "" {
		return errors.New("napplet has no id")
	}
	index, ok := nappletIndexPath(n.Paths)
	if !ok {
		return errors.New("napplet has no index document")
	}
	// one claim, under one lock: a second click, a search launcher and a
	// single-instance token all land here, and only one may download
	if !trySetBusy(n.ID) {
		log.Debug().Str("napp", n.ID).Msg("napplet is busy, ignoring the try")
		return nil
	}
	defer setBusy(n.ID, false)

	fetchCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	files, err := fetchTrialFiles(fetchCtx, n.Paths, n.BlossomServers(fetchCtx))
	if err != nil {
		return fmt.Errorf("%w: %w", errTrialFiles, err)
	}
	if err := fetchCtx.Err(); err != nil {
		// cancelled or timed out after the last file came in: still nothing
		// to open
		return fmt.Errorf("%w: %w", errTrialFiles, err)
	}
	var document []byte
	for i, p := range n.Paths {
		if p.Path == index.Path {
			document = files[i]
			break
		}
	}
	_, err = launchWithDocument(ctx, n, "", document)
	return err
}

// fetchTrialFiles downloads and verifies every path of a trial into memory,
// at most maxParallelAssets at a time, in the order of paths. Each path is
// fetched and checked on its own, even when another path has the same hash.
// The first failure cancels the rest and is returned.
func fetchTrialFiles(ctx context.Context, paths []NappPath, servers []string) ([][]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	files := make([][]byte, len(paths))
	sem := make(chan struct{}, maxParallelAssets)

	for i, p := range paths {
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				return
			}

			data, err := downloadBlob(ctx, servers, p.Sha256)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", p.Path, err)
					cancel()
				}
				return
			}
			files[i] = data
		})
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return files, nil
}

// TryNappletFromDiscovery resolves an uninstalled discovery result and opens
// it ephemerally. Installed napplets are opened normally. An unavailable
// discovery entry is refused by TryNapplet and opens nothing. The argument is a
// launch token (what the macOS and Windows search launchers pass) or a raw id
// (GNOME search, Android and launchers written by earlier builds); a token
// that does not decode is refused and starts nothing.
func TryNappletFromDiscovery(arg string) bool {
	n, installed, ok := trialTarget(arg)
	if !ok {
		return false
	}
	if installed {
		Launch(n)
	} else {
		TryNapplet(n)
	}
	return true
}

// trialTarget resolves a --try-napplet argument to the napplet it names:
// the installed napp if there is one, otherwise a discovered napplet.
func trialTarget(arg string) (n Napp, installed bool, ok bool) {
	id, err := launchIDFromToken(arg)
	if err != nil {
		log.Warn().Err(err).Int("length", len(arg)).Msg("refusing undecodable napplet trial token")
		return Napp{}, false, false
	}
	if n, ok := InstalledNapp(id); ok {
		return n, true, true
	}
	if n, ok := DiscoveredNapp(id); ok && n.IsNapplet() {
		return n, false, true
	}
	return Napp{}, false, false
}

// finishNappletTrial asks whether a just-closed preview should become an
// installation. Trial NAP storage remains in memory while the question is up;
// it is kept only under the version actually installed (D-09) and never over
// data that version already has on this device (D-25). The prompt copy is
// shared with Android and stays as it is (UI-D10); when the data is not
// kept, a notice says why.
func finishNappletTrial(ci *Instance) {
	if installed, ok := InstalledNapp(ci.napp.ID); ok {
		promoteTrial(ci, installed, "could not keep trial data: ")
		return
	}
	p := newPrompt(
		ci.napp.Label(),
		"Did you like "+ci.napp.Label()+"?",
		"Install it to keep the data it saved while you tried it.",
		"", nil,
	)
	p.AcceptLabel = "Install"
	p.RejectLabel = "Not now"
	p.CloseOnReject = true
	enqueuePrompt(p)
	if !p.wait().OK {
		forgetWindow(ci)
		dropTrial(ci)
		return
	}

	// install what the address holds now, not what the trial ran: the
	// trial's event may have been replaced since discovery
	target := ci.napp
	lookup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	latest, found := latestManifest(lookup, ci.napp)
	cancel()
	if found && !nappNewer(ci.napp, latest) {
		if latest.Unavailable != "" {
			log.Warn().Str("napp", ci.napp.ID).Str("event", latest.EventID).Str("reason", latest.Unavailable).
				Msg("refusing to install a trial whose latest version is invalid")
			SetFetchErr("install failed: " + errUnavailable.Error())
			forgetWindow(ci)
			dropTrial(ci)
			return
		}
		target = latest
	}
	// not found (offline, or no relay has it any more): the trial's own
	// event is the newest thing known
	//
	// the user may have installed it from the store while the question was
	// up: that version, or a newer one, is not replaced by what the trial
	// or a lagging relay knows (InstallNapp refuses an older one too)
	if current, ok := InstalledNapp(ci.napp.ID); ok && !nappNewer(target, current) {
		promoteTrial(ci, current, "could not keep trial data: ")
		return
	}
	err := InstallNapp(target)
	if errors.Is(err, errBusy) || errors.Is(err, errOlderVersion) {
		// a store Install or Update of the same napplet ran at the same
		// time: wait for it, then settle the trial against what it left
		// installed, as when it finished before the question was answered
		waitNotBusy(ci.napp.ID, trialInstallWait)
		if current, ok := InstalledNapp(ci.napp.ID); ok && !nappNewer(target, current) {
			promoteTrial(ci, current, "could not keep trial data: ")
			return
		}
		if errors.Is(err, errBusy) {
			// what held the claim (an uninstall, an older install) left
			// nothing at least as new: the user still asked to install
			err = InstallNapp(target)
		}
	}
	if err != nil {
		SetFetchErr("install failed: " + err.Error())
		forgetWindow(ci)
		dropTrial(ci)
		return
	}
	installed, ok := InstalledNapp(target.ID)
	if !ok {
		installed = target
	}
	promoteTrial(ci, installed, "installed, but could not keep trial data: ")
}

// trialInstallWait bounds how long a trial waits for another install of its
// napplet to finish; an install downloads, so it may take a while.
var trialInstallWait = 2 * time.Minute

// waitNotBusy waits until id has no busy claim, or for at most max.
func waitNotBusy(id string, max time.Duration) {
	deadline := time.Now().Add(max)
	for IsBusy(id) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

// promoteTrial settles a closed trial's data against the installed record.
// Only data saved by the same artifact goes in, and only into an empty
// shared store; anything else is dropped with a notice that says why.
// errPrefix starts the launcher error for a write that failed.
func promoteTrial(ci *Instance, installed Napp, errPrefix string) {
	if installed.ArtifactHash != ci.napp.ArtifactHash {
		had := trialHasData(ci)
		dropTrial(ci)
		if had {
			raiseTrialDataDiscarded(ci.napp, trialDataDifferentVersion)
		}
		return
	}
	had := trialHasData(ci)
	err := persistTrialStorage(ci)
	switch {
	case errors.Is(err, errInstalledHasData):
		// checked and refused under the store's lock, in the same step
		// that would have written (D-25)
		dropTrial(ci)
		if had {
			raiseTrialDataDiscarded(ci.napp, trialDataExistingData)
		}
	case errors.Is(err, errTrialNotInstalled):
		// uninstalled or updated since the trial closed: its data would
		// be written for a version nothing runs
		log.Info().Str("napp", ci.napp.ID).Msg("trial data dropped: its version is no longer installed")
		dropTrial(ci)
	case err != nil:
		SetFetchErr(errPrefix + err.Error())
	}
}

// dropTrial discards a closed trial's data: its in-memory stores and, unless
// the installed copy or another open window runs the same artifact, the
// config its version registered on disk.
func dropTrial(ci *Instance) {
	discardTrialStorage(ci)
	scope, err := nappletScope(ci.napp)
	if err != nil {
		return
	}
	if installed, ok := InstalledNapp(ci.napp.ID); ok {
		if s, err := nappletScope(installed); err == nil && s == scope {
			return
		}
	}
	for _, other := range runningForNapp(ci.napp.ID) {
		if other == ci {
			continue
		}
		if s, err := nappletScope(other.napp); err == nil && s == scope {
			return
		}
	}
	if err := napconfig.Forget(scope); err != nil {
		log.Warn().Err(err).Str("napp", ci.napp.ID).Msg("could not forget a trial's config")
	}
}

// ─── staging an install ──────────────────────────────────────────

// An install or update never writes into the directory a record points at.
// It downloads into a fresh directory next to it (same parent, so the swap
// is a rename on one file system), and only once every file is there and
// verified does swapInstallDir put it in place. A failure at any point
// before that leaves the installed copy as it was, files and record.

// stagingInfix and oldInfix name the directories an install works in,
// after the install dir's own name: napps/<hex>.staging-* and
// napps/<hex>.old-*. They belong to that one napp, so removeStaleStaging
// can clear what a crash left behind while the napp's busy claim is held.
const (
	stagingInfix = ".staging-"
	oldInfix     = ".old-"
)

// renameInstallDir is os.Rename, swappable in tests.
var renameInstallDir = os.Rename

// On Windows a directory whose files were just written, or are being read
// (an antivirus or indexer handle, a window's file server), can refuse a
// rename for a moment. A swap tries each rename installRenameAttempts times
// there, waiting a little longer each time (installRenameBackoff, then
// twice that, and so on), before it gives up. The swap holds stateMu, so the
// waits stay short: at most 200ms per rename.
var (
	installRenameAttempts = 1
	installRenameBackoff  = 20 * time.Millisecond
)

func init() {
	if runtime.GOOS == "windows" {
		installRenameAttempts = 5
	}
}

// renameInstallDirRetrying is renameInstallDir with the retries above. A
// source that is not there is not retried.
func renameInstallDirRetrying(from, to string) error {
	attempt := 1
	for {
		err := renameInstallDir(from, to)
		if err == nil {
			if attempt > 1 {
				log.Info().Int("attempts", attempt).Str("dir", filepath.Base(to)).Msg("install directory renamed after retrying")
			}
			return nil
		}
		if errors.Is(err, os.ErrNotExist) || attempt >= installRenameAttempts {
			if attempt > 1 {
				return fmt.Errorf("%w (after %d attempts)", err, attempt)
			}
			return err
		}
		time.Sleep(time.Duration(attempt) * installRenameBackoff)
		attempt++
	}
}

// stageNappFiles downloads every file of n into a new directory next to
// base and returns it. On failure nothing is left behind and base is never
// touched. The caller holds n's busy claim.
func stageNappFiles(ctx context.Context, n Napp, base string, servers []string) (string, error) {
	parent := filepath.Dir(base)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	removeStaleStaging(base)
	staging, err := os.MkdirTemp(parent, filepath.Base(base)+stagingInfix)
	if err != nil {
		return "", err
	}
	// MkdirTemp makes it 0700; an install dir has always been 0755
	if err := os.Chmod(staging, 0755); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if err := fetchNappAssets(ctx, n, staging, servers); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	if err := ctx.Err(); err != nil {
		os.RemoveAll(staging)
		return "", err
	}
	return staging, nil
}

// swapInstallDir puts staging where base is: base (when there is one) is
// renamed aside, staging renamed in, and the returned func removes the old
// copy, outside any lock. When staging cannot be renamed in, the old copy
// is renamed back, so base is never left missing by a failed swap.
func swapInstallDir(staging, base string) (func(), error) {
	old := ""
	if _, err := os.Lstat(base); err == nil {
		old = base + oldInfix + randomID()[:8]
		if err := renameInstallDirRetrying(base, old); err != nil {
			return nil, fmt.Errorf("could not move the installed copy aside: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := renameInstallDirRetrying(staging, base); err != nil {
		if old != "" {
			if rerr := renameInstallDirRetrying(old, base); rerr != nil {
				log.Error().Err(rerr).Str("dir", filepath.Base(base)).Msg("could not put the installed copy back")
			}
		}
		return nil, fmt.Errorf("could not move the new files in place: %w", err)
	}
	return func() {
		if old != "" {
			os.RemoveAll(old)
		}
	}, nil
}

// removeStaleStaging removes the staging and set-aside directories of
// base's napp that an interrupted install or update left behind. Only
// directories named after base are touched; the napp's busy claim keeps
// any other install of it from running meanwhile.
func removeStaleStaging(base string) {
	parent, name := filepath.Dir(base), filepath.Base(base)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), name+stagingInfix) || strings.HasPrefix(e.Name(), name+oldInfix) {
			if err := os.RemoveAll(filepath.Join(parent, e.Name())); err != nil {
				log.Warn().Err(err).Str("dir", e.Name()).Msg("could not remove a stale install directory")
			}
		}
	}
}

// maxParallelAssets caps how many of a napp's files are in flight at once, so
// a big napp doesn't open a connection per asset against the same server.
const maxParallelAssets = 6

// fetchNappAssets downloads every file of a napp into base. The assets go in
// parallel; the servers for any one asset are still tried in order, so a napp
// whose first server has everything is served entirely from there.
//
// A server that is down costs nothing: its attempt is skipped and the next
// one is tried. A file that no server can produce does fail the whole napp —
// it would be an install that cannot start — but only after every other
// asset had its own chance, and the error names what went missing.
func fetchNappAssets(ctx context.Context, n Napp, base string, servers []string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	sem := make(chan struct{}, maxParallelAssets)

	for _, p := range n.Paths {
		wg.Add(1)
		go func(p NappPath) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			err := fetchNappAsset(ctx, servers, base, p)
			if err == nil {
				return
			}

			mu.Lock()
			defer mu.Unlock()
			if firstErr == nil {
				// what the others report from here on is just the
				// cancellation we are about to cause
				firstErr = err
				cancel()
			}
		}(p)
	}
	wg.Wait()

	return firstErr
}

// fetchNappAsset downloads one file and writes it where the napp expects it.
func fetchNappAsset(ctx context.Context, servers []string, base string, p NappPath) error {
	// manifest paths are author input: the destination is settled (and an
	// escaping one refused) before anything is downloaded
	dest, err := nappAssetPath(base, p.Path)
	if err != nil {
		return err
	}
	data, err := downloadBlob(ctx, servers, p.Sha256)
	if err != nil {
		// the file, not the hash: an install or update that failed has to
		// say which of the napp's files nobody could serve
		return fmt.Errorf("%s: %w", p.Path, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("%s: %w", p.Path, err)
	}
	if err := fileutil.WriteFileAtomic(dest, data, 0644); err != nil {
		return fmt.Errorf("%s: %w", p.Path, err)
	}
	return nil
}

// blobAttemptTimeout bounds a single server attempt. Without it a server that
// accepts the connection and then stalls holds the download until the whole
// context is gone, taking the other servers down with it: one bad server
// failed the entire napp instead of being skipped for the next one.
var blobAttemptTimeout = 20 * time.Second

// blobMaxBytes caps one blob. Napp files are pages, scripts and images; a
// server that sends more (by Content-Length or by just streaming on) is
// skipped for the next one rather than read into memory without end.
var blobMaxBytes int64 = 64 << 20

// blobMaxRedirects is how many redirects one blob request may follow.
const blobMaxRedirects = 3

// blobRedirect keeps a blob request's redirects short, and never lets an
// https request continue over anything weaker.
func blobRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > blobMaxRedirects {
		return errors.New("too many redirects")
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("redirect away from https")
	}
	return nil
}

// blobClient fetches from the servers a manifest named (its server tags), the
// ones its author lists (kind 10063) and the built-in default servers. The
// first two are author input, so it dials
// public addresses only, on every connection and every redirect hop, and a
// manifest cannot point installs, trials or icon fetches at the user's
// machine, LAN or a cloud metadata address. Proxy is nil on purpose, as for
// NAP-RESOURCE: a proxy would dial on our behalf and the address check would
// never see where the request goes. Users behind a proxy only reach the
// servers they configured themselves (trustedBlobClient).
var blobClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           netguard.DialContext,
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: blobRedirect,
}

// trustedBlobClient fetches from the Blossom servers the user configured in
// settings (D-20). The user may run one on the LAN or on this machine on
// purpose, so a connection to one of those servers is dialed without the
// public-address check. Every other connection it makes, which can only be
// a redirect hop, goes through netguard like blobClient's: a configured
// server cannot send the launcher to a private address the user never
// named. The size cap, timeouts and redirect rules are the same. A manifest
// naming one of those servers in its own tags gets nothing it could not
// already have.
var trustedBlobClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           trustedBlobDial,
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: blobRedirect,
}

var trustedBlobDialer = &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

// trustedBlobDial dials address unchecked only when it is the host and port
// of a server the user configured, and through netguard otherwise. The
// transport has no proxy, so address is where the request really goes.
func trustedBlobDial(ctx context.Context, network, address string) (net.Conn, error) {
	if key, ok := blobHostKey(address, ""); ok && userBlobHosts()[key] {
		return trustedBlobDialer.DialContext(ctx, network, address)
	}
	return netguard.DialContext(ctx, network, address)
}

// userBlobServers is the Blossom servers the user configured in settings,
// normalized the way Napp.BlossomServers normalizes every server, so a
// manifest's copy of the same url matches. The built-in defaults are not
// among them: the user never named those, and they are public hosts that
// gain nothing from skipping the check (D-20).
func userBlobServers() map[string]bool {
	stateMu.Lock()
	configured := append([]string(nil), state.BlossomServers...)
	stateMu.Unlock()
	trusted := make(map[string]bool)
	for _, raw := range configured {
		if u, err := nostr.NormalizeHTTPURL(raw); err == nil && u != "" {
			trusted[u] = true
		}
	}
	return trusted
}

// userBlobHosts is the host:port of every server userBlobServers trusts,
// as blobHostKey spells it.
func userBlobHosts() map[string]bool {
	hosts := make(map[string]bool)
	for raw := range userBlobServers() {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		port := u.Port()
		if port == "" {
			switch u.Scheme {
			case "https":
				port = "443"
			case "http":
				port = "80"
			default:
				continue
			}
		}
		if key, ok := blobHostKey(u.Hostname(), port); ok {
			hosts[key] = true
		}
	}
	return hosts
}

// blobHostKey is host and port as one comparable key: the host lowercased,
// with no brackets around an IPv6 literal. With port empty, host is a
// host:port pair to split.
func blobHostKey(host, port string) (string, bool) {
	if port == "" {
		h, p, err := net.SplitHostPort(host)
		if err != nil {
			return "", false
		}
		host, port = h, p
	}
	if host == "" || port == "" {
		return "", false
	}
	return net.JoinHostPort(strings.ToLower(host), port), true
}

// downloadBlob fetches a blob from the first server that has it and verifies
// it against its hash before returning it. Every server gets its own deadline,
// so an unreachable or stalling one is skipped rather than waited out. Only
// the servers the user configured are fetched with trustedBlobClient; every
// other one, the built-in defaults included, goes through the public-only
// blobClient.
func downloadBlob(ctx context.Context, servers []string, sha string) ([]byte, error) {
	log.Debug().Str("sha256", sha).Int("servers", len(servers)).Msg("downloading blob")
	trusted := userBlobServers()
	var lastErr error = errors.New("no servers")
	for _, srv := range servers {
		client := blobClient
		if norm, err := nostr.NormalizeHTTPURL(srv); err == nil && trusted[norm] {
			client = trustedBlobClient
		}
		data, err := fetchBlobFrom(ctx, client, srv, sha)
		if err != nil {
			log.Debug().Str("server", srv).Err(err).Msg("blob download failed, trying the next")
			lastErr = err
			continue
		}
		log.Debug().Str("server", srv).Msg("blob downloaded and verified")
		return data, nil
	}
	return nil, fmt.Errorf("could not fetch/verify %s: %w", sha, lastErr)
}

// fetchBlobFrom is one server attempt: its own deadline, at most
// blobMaxBytes, a 200 and the right sha256, or an error.
func fetchBlobFrom(ctx context.Context, client *http.Client, srv, sha string) ([]byte, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, blobAttemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, srv+"/"+sha, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(srv + ": status " + resp.Status)
	}
	if resp.ContentLength > blobMaxBytes {
		return nil, fmt.Errorf("%s: blob of %d bytes is over the %d byte limit", srv, resp.ContentLength, blobMaxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, blobMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > blobMaxBytes {
		return nil, fmt.Errorf("%s: blob is over the %d byte limit", srv, blobMaxBytes)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != sha {
		return nil, errors.New(srv + ": sha256 mismatch")
	}
	return data, nil
}
