package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kwakore/backend/fileutil"
)

// The verified napplet icon cache (NIP-5D "icon" tag, decision 4). An icon
// is fetched by its manifest hash from the user's, the manifest's and the
// author's Blossom servers (Napp.BlossomServers), accepted only when it
// matches the hash and decodes as the declared png, jpeg or webp, and kept
// only as a PNG Go decoded and re-encoded itself: <dataDir>/icons/<sha>.png,
// owner-only. The source is at most nappletIconMaxBytes, enforced during the
// download, and at most nappletIconMaxSide on each side, checked before the
// full decode. Nothing about where an icon came from is kept, and an icon
// that cannot be had never stands in the way of a napplet: desktop entries
// and clients fall back to generic artwork.

const (
	// nappletIconMaxBytes caps an icon's source blob.
	nappletIconMaxBytes = 1 << 20
	// nappletIconMaxSide caps an icon's width and height, in pixels.
	nappletIconMaxSide = 1024
)

var (
	// serviceIconTimeout bounds one icon fetch, verify and re-encode.
	serviceIconTimeout = 10 * time.Second
	// prefetchNappletIcons says whether an install or update fills the icon
	// cache in the background. Only the daemon does; tests swap it.
	prefetchNappletIcons = func() bool { return serviceConfig != nil }

	errNoNappletIcon = errors.New("napplet has no usable icon")
	cachedIconName   = regexp.MustCompile(`^[0-9a-f]{64}\.(png|blob)$`)
)

// nappletIconDir is where verified icons are cached.
func nappletIconDir() (string, error) {
	if !filepath.IsAbs(dataDir) {
		return "", errors.New("data directory is not set")
	}
	return filepath.Join(dataDir, "icons"), nil
}

// nappletIconPath is the cache file of the icon with hash sha. Only 64
// lowercase hex characters name a file, so no hash can leave the directory.
func nappletIconPath(sha string) (string, error) {
	if !hex64.MatchString(sha) {
		return "", errors.New("not an icon hash")
	}
	dir, err := nappletIconDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sha+".png"), nil
}

// hasNappletIcon says whether n declares an icon the cache can hold.
func hasNappletIcon(n Napp) bool {
	return n.IsNapplet() && hex64.MatchString(n.IconSha) && nappletIconMimes[n.IconMime] != ""
}

// readCachedNappletIcon reads a cached icon: a regular file that decodes as
// a PNG of 1 to nappletIconMaxSide pixels each way. A file that fails is
// removed and reported as a miss.
func readCachedNappletIcon(sha string) (data []byte, width, height int, err error) {
	path, err := nappletIconPath(sha)
	if err != nil {
		return nil, 0, 0, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, 0, errors.New("cached icon is not a regular file")
	}
	data, err = os.ReadFile(path)
	if err == nil {
		var cfg image.Config
		var format string
		cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
		if err == nil && (format != "png" || !iconSideOK(cfg.Width) || !iconSideOK(cfg.Height)) {
			err = errors.New("cached icon is not a usable png")
		}
		if err == nil {
			return data, cfg.Width, cfg.Height, nil
		}
	}
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		log.Warn().Str("sha256", sha).Err(rmErr).Msg("could not remove a bad cached icon")
	}
	return nil, 0, 0, fmt.Errorf("bad cached icon: %w", err)
}

func iconSideOK(side int) bool { return side >= 1 && side <= nappletIconMaxSide }

// cacheNappletIcon makes sure n's icon is in the cache and returns its path:
// a valid cached file is used as is, otherwise the icon is fetched (sha and
// type verified, netguarded, capped), its size checked, decoded and written
// back as a PNG. Errors are for the log only.
func cacheNappletIcon(ctx context.Context, n Napp) (string, error) {
	if !hasNappletIcon(n) {
		return "", errNoNappletIcon
	}
	path, err := nappletIconPath(n.IconSha)
	if err != nil {
		return "", err
	}
	if _, _, _, err := readCachedNappletIcon(n.IconSha); err == nil {
		return path, nil
	}
	data, err := n.nappletIconBlob(ctx)
	if err != nil {
		return "", fmt.Errorf("icon fetch: %w", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("icon does not decode: %w", err)
	}
	if !iconSideOK(cfg.Width) || !iconSideOK(cfg.Height) {
		return "", fmt.Errorf("icon is %dx%d, over the %d pixel limit", cfg.Width, cfg.Height, nappletIconMaxSide)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("icon does not decode: %w", err)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return "", fmt.Errorf("icon re-encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("icon directory: %w", err)
	}
	if err := fileutil.TightenDir(dir); err != nil {
		return "", fmt.Errorf("icon directory: %w", err)
	}
	// Preserve verified source bytes for NAP-RESOURCE's blossom:sha256 refs.
	// The desktop PNG may have different bytes and therefore a different hash.
	if err := fileutil.WriteFileAtomic(strings.TrimSuffix(path, ".png")+".blob", data, 0o600); err != nil {
		return "", fmt.Errorf("icon source write: %w", err)
	}
	if err := fileutil.WriteFileAtomic(path, out.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("icon write: %w", err)
	}
	return path, nil
}

// pruneNappletIcons removes every cached icon no installed napplet uses.
// Only <64 hex>.png and .blob names are touched; a missing directory is fine.
// Failures are logged and never returned into an uninstall's result.
func pruneNappletIcons() {
	dir, err := nappletIconDir()
	if err != nil {
		return
	}
	stateMu.Lock()
	used := make(map[string]bool, len(state.InstalledNapps))
	for _, n := range state.InstalledNapps {
		if n.IsNapplet() && hex64.MatchString(n.IconSha) {
			used[n.IconSha+".png"] = true
			used[n.IconSha+".blob"] = true
		}
	}
	stateMu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Msg("could not list the icon cache")
		}
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !cachedIconName.MatchString(name) || used[name] {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Msg("could not prune a cached icon")
		}
	}
}

// scheduleNappletIconCache fills n's icon in the background after an install
// or update, then republishes the native entries so they pick it up. A
// failure only reaches the debug log: per NIP-5D an icon that cannot be had
// falls back to generic artwork and never affects the napplet.
func scheduleNappletIconCache(n Napp) {
	if !prefetchNappletIcons() || !hasNappletIcon(n) {
		return
	}
	backgroundSyncs.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("napp", n.ID).Msg("icon cache fill panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), serviceIconTimeout)
		defer cancel()
		if _, err := cacheNappletIcon(ctx, n); err != nil {
			log.Debug().Str("napp", n.ID).Err(err).Msg("napplet icon not cached")
			return
		}
		if current, ok := InstalledNapp(n.ID); ok && current.IconSha == n.IconSha && serviceConfig != nil {
			_ = syncNativeEntries()
		}
	})
}

// ─── napplet.icon ────────────────────────────────────────────────

// ServiceIconResult is a napplet's verified icon as PNG: Sha256 is the
// manifest's icon hash, Data the standard base64 of the cached PNG.
type ServiceIconResult struct {
	Sha256 string `json:"sha256"`
	Mime   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Data   string `json:"data"`
}

// ServiceIcon returns the verified icon of the napplet at a canonical
// address: the installed record's, else the catalog entry's, else the
// newest stored manifest's. It is read from the cache, or fetched and
// cached on a miss. An unknown address or a napplet without an icon is
// ErrServiceNotFound, a fetch, verify or decode failure
// ErrServiceUnavailable, and the caller's own cancellation ErrServiceTimeout.
// No error carries a path or URL.
func ServiceIcon(ctx context.Context, address string) (ServiceIconResult, error) {
	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil {
		return ServiceIconResult{}, ErrServiceInvalidAddress
	}
	installed, listed, stored := serviceNappSources(address, ptr)
	var chosen Napp
	switch {
	case installed != nil:
		chosen = *installed
	case listed != nil:
		chosen = *listed
	case len(stored) > 0:
		chosen = stored[0]
	default:
		return ServiceIconResult{}, ErrServiceNotFound
	}
	if !hasNappletIcon(chosen) {
		return ServiceIconResult{}, ErrServiceNotFound
	}
	data, w, h, err := readCachedNappletIcon(chosen.IconSha)
	if err != nil {
		fillCtx, cancel := context.WithTimeout(ctx, serviceIconTimeout)
		_, err = cacheNappletIcon(fillCtx, chosen)
		cancel()
		if err == nil {
			data, w, h, err = readCachedNappletIcon(chosen.IconSha)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ServiceIconResult{}, ErrServiceTimeout
			}
			log.Debug().Str("address", address).Err(err).Msg("napplet icon unavailable")
			return ServiceIconResult{}, ErrServiceUnavailable
		}
	}
	return ServiceIconResult{
		Sha256: chosen.IconSha,
		Mime:   "image/png",
		Width:  w,
		Height: h,
		Data:   base64.StdEncoding.EncodeToString(data),
	}, nil
}

// readCachedOriginalIcon accepts only bounded, regular files whose bytes match
// the requested content hash. Old PNG-only cache entries can be reused only
// when re-encoding happened to preserve that hash; otherwise RESOURCE fetches
// the original through the ordinary permission-controlled Blossom path.
func readCachedOriginalIcon(sha string) ([]byte, error) {
	path, err := nappletIconPath(sha)
	if err != nil {
		return nil, err
	}
	for _, candidate := range []string{strings.TrimSuffix(path, ".png") + ".blob", path} {
		info, err := os.Lstat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Size() > nappletIconMaxBytes {
			continue
		}
		file, err := os.Open(candidate)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(file, nappletIconMaxBytes+1))
		file.Close()
		if err != nil || len(data) > nappletIconMaxBytes {
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) == sha {
			return data, nil
		}
	}
	return nil, errors.New("original icon blob not cached")
}
