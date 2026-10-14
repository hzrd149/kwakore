package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"kwakore/backend/fileutil"
)

// The shared Blossom cache retains original content by hash, never settings.
func cachedArtifactPath(hash string) string {
	return filepath.Join(dataDir, "blossom-cache", hash)
}

func readCachedArtifact(hash string) []byte {
	return readCachedBlossom(hash, blobMaxBytes)
}

// Every reader verifies the digest and applies its own byte limit, including
// when another subsystem originally populated the cache.
func readCachedBlossom(hash string, limit int64) []byte {
	if !hex64.MatchString(hash) || !filepath.IsAbs(dataDir) || limit < 0 {
		return nil
	}
	candidates := []string{cachedArtifactPath(hash), filepath.Join(dataDir, "artifact-cache", hash), filepath.Join(dataDir, "icons", hash+".blob"), filepath.Join(dataDir, "icons", hash+".png")}
	for i, path := range candidates {
		if safeMutationRoot(filepath.Dir(path)) != nil {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, limit+1))
		f.Close()
		if err != nil || int64(len(data)) > limit {
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != hash {
			continue
		}
		if i > 0 {
			retainBlossom(hash, data)
		}
		return data
	}
	return nil
}

// Cache failures do not make an otherwise successful resource fetch fail.
func retainBlossom(hash string, data []byte) {
	if err := cacheArtifact(hash, data); err != nil {
		log.Debug().Err(err).Msg("could not retain Blossom blob")
	}
}

func cacheArtifact(hash string, data []byte) error {
	if !hex64.MatchString(hash) || !filepath.IsAbs(dataDir) {
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
	if err := fileutil.TightenDir(root); err != nil {
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
		if readCachedArtifact(p.Sha256) == nil {
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
