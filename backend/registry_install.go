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
	"os"
	"path/filepath"
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
	log.Info().Str("napp", n.ID).Str("name", n.Name).Msg("installing napp")
	setBusy(n.ID, true)
	defer setBusy(n.ID, false)

	base, err := nappBaseDir(n.ID)
	if err != nil {
		log.Error().Err(err).Str("napp", n.ID).Msg("install failed")
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	servers := n.BlossomServers(ctx)
	if err := fetchNappAssets(ctx, n, base, servers); err != nil {
		log.Error().Err(err).Str("napp", n.ID).Msg("install failed")
		os.RemoveAll(base)
		return err
	}

	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	state.InstalledNapps[n.ID] = n
	saveState()
	stateMu.Unlock()

	refreshInstalled()
	log.Info().Str("napp", n.ID).Str("name", n.Name).Msg("install complete")
	return nil
}

// Uninstall removes a napp's files and forgets it. Installed-only by
// convention: it silently no-ops for ids the launcher doesn't know.
func Uninstall(id string) {
	log.Info().Str("napp", id).Msg("uninstalling napp")
	setBusy(id, true)
	defer setBusy(id, false)

	// a napp whose directory cannot be named safely gets nothing removed,
	// but is still forgotten below
	if base, err := nappBaseDir(id); err != nil {
		log.Warn().Err(err).Str("napp", id).Msg("napp directory not removed")
	} else {
		os.RemoveAll(base)
	}

	stateMu.Lock()
	delete(state.InstalledNapps, id)
	delete(state.LastLaunched, id)
	saveState()
	stateMu.Unlock()

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
		dropTrial(ci)
		windows.Delete(ci.instance)
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
			dropTrial(ci)
			windows.Delete(ci.instance)
			return
		}
		target = latest
	}
	// not found (offline, or no relay has it any more): the trial's own
	// event is the newest thing known
	if err := InstallNapp(target); err != nil {
		SetFetchErr("install failed: " + err.Error())
		dropTrial(ci)
		windows.Delete(ci.instance)
		return
	}
	installed, ok := InstalledNapp(target.ID)
	if !ok {
		installed = target
	}
	promoteTrial(ci, installed, "installed, but could not keep trial data: ")
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
	if installedHasData(installed) {
		had := trialHasData(ci)
		dropTrial(ci)
		if had {
			raiseTrialDataDiscarded(ci.napp, trialDataExistingData)
		}
		return
	}
	if err := persistTrialStorage(ci); err != nil {
		SetFetchErr(errPrefix + err.Error())
	}
}

// installedHasData says whether the installed napplet's shared store holds
// anything. A record with no valid scope has nothing to keep.
func installedHasData(installed Napp) bool {
	key, err := nappletStorageKey(installed, "shared", "")
	if err != nil {
		return false
	}
	file, err := nappletStorageFile(key)
	if err != nil {
		return false
	}
	s := storageFor(file)
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data) > 0
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

// blobClient fetches from the servers a manifest named (its server tags) and
// the ones its author lists (kind 10063). Those are author input, so it dials
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

// trustedBlobClient fetches from the launcher's own Blossom servers (the ones
// the user configured, or the defaults when they set none). The user may run
// one on the LAN or on this machine on purpose, so it dials anything; the
// size cap, timeouts and redirect rules are the same. A manifest naming one
// of those servers in its own tags gets nothing it could not already have.
var trustedBlobClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: blobRedirect,
}

// userBlobServers is the launcher's own Blossom servers, normalized the way
// Napp.BlossomServers normalizes every server, so a manifest's copy of the
// same url matches.
func userBlobServers() map[string]bool {
	trusted := make(map[string]bool)
	for _, raw := range BlossomServers() {
		if u, err := nostr.NormalizeHTTPURL(raw); err == nil && u != "" {
			trusted[u] = true
		}
	}
	return trusted
}

// downloadBlob fetches a blob from the first server that has it and verifies
// it against its hash before returning it. Every server gets its own deadline,
// so an unreachable or stalling one is skipped rather than waited out. Only
// the user's own servers are fetched with trustedBlobClient; every other one
// goes through the public-only blobClient.
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
