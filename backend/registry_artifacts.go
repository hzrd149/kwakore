package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"kwakore/backend/fileutil"
)

// The artifact cache retains verified code, never version storage or settings.
func cachedArtifactPath(hash string) string {
	return filepath.Join(dataDir, "artifact-cache", hash)
}

func readCachedArtifact(hash string) []byte {
	if !hex64.MatchString(hash) {
		return nil
	}
	path := cachedArtifactPath(hash)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > blobMaxBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return nil
	}
	return data
}

func cacheArtifact(hash string, data []byte) error {
	if !hex64.MatchString(hash) {
		return errFilesFailed
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash || int64(len(data)) > blobMaxBytes {
		return errFilesFailed
	}
	root := filepath.Dir(cachedArtifactPath(hash))
	if err := safeMutationRoot(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(cachedArtifactPath(hash), data, 0600)
}

// Seed the cache for installations that predate it, before replacing their
// directory. Only bytes matching the signed manifest's per-file hashes survive.
func retainInstalledArtifacts(n Napp, base string) {
	for _, p := range n.Paths {
		path, err := nappAssetPath(base, p.Path)
		if err != nil {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > blobMaxBytes {
			continue
		}
		data, err := os.ReadFile(path)
		if err == nil {
			if err := cacheArtifact(p.Sha256, data); err != nil {
				log.Debug().Err(err).Msg("previous artifact could not be cached")
			}
		}
	}
}

func hasCachedArtifacts(n Napp) bool {
	if n.Unavailable != "" || len(n.Paths) == 0 {
		return false
	}
	for _, p := range n.Paths {
		if !hex64.MatchString(p.Sha256) {
			return false
		}
		info, err := os.Lstat(cachedArtifactPath(p.Sha256))
		if err != nil || !info.Mode().IsRegular() || info.Size() > blobMaxBytes {
			return false
		}
	}
	return true
}

func haveVerifiedArtifacts(n Napp) bool {
	if len(n.Paths) == 0 {
		return false
	}
	for _, p := range n.Paths {
		if readCachedArtifact(p.Sha256) == nil {
			return false
		}
	}
	return true
}
