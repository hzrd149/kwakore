package backend

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBlossomCacheSharedDownloads(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	data := testWebP(t)
	hash := blobs.add(data)
	if _, err := cacheNappletIcon(t.Context(), iconNapplet(hash, "image/webp")); err != nil {
		t.Fatal(err)
	}
	got, err := downloadBlob(t.Context(), nil, hash)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("shared download: %v", err)
	}
	if got := readCachedBlossom(hash, int64(len(data)-1)); got != nil {
		t.Fatal("cache bypassed caller size limit")
	}
	if err := os.WriteFile(cachedArtifactPath(hash), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readCachedArtifact(hash); got != nil {
		t.Fatal("corrupt blob accepted")
	}
}

func TestBlossomCacheMigratesArtifacts(t *testing.T) {
	newReclaimRig(t)
	data := []byte("old installed file")
	hash := blobHash(data)
	dir := filepath.Join(dataDir, "artifact-cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, hash), data, 0600); err != nil {
		t.Fatal(err)
	}
	if got := readCachedArtifact(hash); !bytes.Equal(got, data) {
		t.Fatal("old artifact not reused")
	}
	if got, err := os.ReadFile(cachedArtifactPath(hash)); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("migration: %v", err)
	}
	info, err := os.Stat(cachedArtifactPath(hash))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache permissions: %v", err)
	}
}
