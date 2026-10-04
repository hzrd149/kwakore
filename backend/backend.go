// Package backend is everything Verdana does that isn't drawing: the nostr
// system and its local eventstore, the napp registry (discovery, install,
// launch), the action router and the whole window.nostr / window.nostrdb /
// window.napp surface a napp gets — env.d.ts is the contract for that surface
// and behavior.md for the behaviors around it.
//
// It deliberately knows nothing about how the launcher is drawn, nor what a
// "window" is. Both are the Host's business: the Gio desktop app gives a napp
// an OS window backed by its own webview process, the Android app gives it a
// tab backed by an in-process WebView, and neither difference reaches this
// package.
package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fiatjaf.com/nostr/sdk"
	"github.com/rs/zerolog"
	"verdana/backend/bunker"
	"verdana/backend/fileutil"
	"verdana/backend/napconfig"
)

var (
	sys     *sdk.System
	log     zerolog.Logger
	host    Host
	dataDir string
)

// Options is what a GUI has to hand over to get a working backend.
type Options struct {
	// DataDir is where the eventstore, the kvstore, state.json and the
	// installed napps live. It is created if missing.
	DataDir string

	// Host is the platform: windows, prompts, clipboard, files, links.
	Host Host

	// Log is optional; without one, logs go to stderr.
	Log *zerolog.Logger
}

// Start brings the backend up: stores open, state loaded, profile index
// building, and the stored login being resumed (so the GUI can render
// State().Phase right away). The returned function closes the stores.
func Start(opts Options) (func(), error) {
	if opts.Log != nil {
		log = *opts.Log
	} else {
		log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	}
	for _, err := range cacheInitErrs {
		log.Warn().Err(err).Msg("cache disabled")
	}

	if opts.Host == nil {
		opts.Host = noopHost{}
	}
	host = opts.Host

	dataDir = opts.DataDir
	if err := ensureDataDir(); err != nil {
		return nil, err
	}

	napconfig.Init(filepath.Join(dataDir, "config"), log)
	bunker.SetLogger(log)

	closeStores, err := initSystem(dataDir)
	if err != nil {
		return nil, err
	}

	loadState()
	refreshInstalled()
	go buildUserIndex()

	// a first update round on its own, a bit after startup: not blocking the
	// launcher, and late enough not to compete with whatever the user is
	// doing in the first seconds (the manual reload button in the UI is
	// there for when they don't want to wait).
	go func() {
		time.Sleep(startupUpdateCheckDelay)
		CheckForUpdates()
	}()

	// load the login secrets, then resume the stored login or ask for one
	go loadSecrets(nil)

	return closeStores, nil
}

// ensureDataDir creates the data dir private to the user (0700) and, on
// Unix, tightens one an older build created 0755. Failing to tighten is
// logged, not fatal: the launcher still works, only less privately.
func ensureDataDir() error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	if err := fileutil.TightenDir(dataDir); err != nil {
		log.Warn().Err(err).Str("dir", dataDir).Msg("could not make the data dir private")
	}
	return nil
}

// startupUpdateCheckDelay is how long after startup the automatic check for
// napp updates waits before going out.
const startupUpdateCheckDelay = 20 * time.Second

// DataDir is where everything the backend persists lives.
func DataDir() string { return dataDir }

// Logger is the backend's logger, so a GUI can log into the same stream.
func Logger() zerolog.Logger { return log }

// nappBaseDir is the one place a napp's install directory is named, and every
// filesystem operation on a napp's files (install, the failed-install cleanup,
// update, launch, the napplet document and icon reads, uninstall) starts here.
//
// The id, and the author's d tag inside it, is opaque: it is never a path
// segment. A d of "../../.." used to turn {dataDir}/napps/{id} into {dataDir}
// itself, so a failed install or an uninstall removed the whole data
// directory; "/../x" landed in another napp's namespace and "a/b" nested under
// napp "a". The directory is instead the hex sha256 of today's id string
// ({pk16}~{d} for napps, napplet~{pk16}~{d} for napplets), a fixed-width name
// directly under {dataDir}/napps, and the result is still checked to sit
// there. The id itself, in state, storage keys and wire messages, keeps the
// raw d byte for byte.
//
// Install directories are all this names. The localStorage and NAP-CONFIG
// files are named by safeFileName, which stays inside its directory but maps
// unsafe characters to "_", so two d values of one author can still share a
// file there (CONFORMANCE CF-2, Phase 5 KEY-04).
//
// What goes into the hash may change later (the full address and artifact
// hash are candidates); nothing is migrated, and directories under the old
// raw-id layout are left alone rather than swept, since deleting unknown
// directories automatically is the riskier act.
//
// A caller that gets an error must not touch the filesystem at all.
func nappBaseDir(id string) (string, error) {
	return nappBaseDirIn(dataDir, id)
}

// nappBaseDirIn is nappBaseDir for an explicit data directory.
func nappBaseDirIn(dataDir, id string) (string, error) {
	if !filepath.IsAbs(dataDir) {
		return "", errors.New("data directory is not set")
	}
	root := filepath.Join(dataDir, "napps")
	sum := sha256.Sum256([]byte(id))
	name := hex.EncodeToString(sum[:])
	dir := filepath.Join(root, name)
	if rel, err := filepath.Rel(root, dir); err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("napp directory escapes %s", root)
	}
	return dir, nil
}

// nappAssetPath places a manifest path (author input, like "/assets/app.js")
// under a napp's directory. It is the one rule for both the install writer and
// the icon reader: nothing may land outside base, so "../" segments, absolute
// paths and "//" are refused. "/" and an empty path mean index.html, since
// NIP-5D lets a napplet name its index "/".
func nappAssetPath(base, manifestPath string) (string, error) {
	rel := strings.TrimPrefix(manifestPath, "/")
	if rel == "" {
		rel = "index.html"
	}
	rel = filepath.FromSlash(rel)
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%s: path escapes the napp directory", manifestPath)
	}
	dest := filepath.Join(base, rel)
	// "." is local too, but names the directory itself, not a file in it
	if r, err := filepath.Rel(base, dest); err != nil || r == "." || !filepath.IsLocal(r) {
		return "", fmt.Errorf("%s: path escapes the napp directory", manifestPath)
	}
	return dest, nil
}
