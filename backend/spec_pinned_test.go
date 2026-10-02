package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pinnedSnapshot is one spec text committed under spec/pinned at the commit
// .planning/research/SPEC-PINS.md pins it to.
type pinnedSnapshot struct {
	File, Spec, Commit, Role string
}

// pinnedSnapshots lists every snapshot in spec/pinned in SPEC-PINS order. The
// sections of spec/CONFORMANCE.md follow the same order, and the README index
// must too. It changes only on a deliberate re-pin: a new SHA is a new file,
// an existing snapshot is never edited.
var pinnedSnapshots = []pinnedSnapshot{
	{"NIP-5D@24711d9c.md", "NIP-5D", "24711d9c47bbdd07908bf1d52bf677d9cbc530f0", "pin"},
	{"WEB-NAPPLET@7ae5b19a.md", "WEB-NAPPLET", "7ae5b19a9c32fbd4c881836f4d821c02630e2b4f", "pin"},
	{"NAP-SHELL@a040914b.md", "NAP-SHELL", "a040914b4bbd3a5cd8a14b0f316a723c968ebfb2", "pin"},
	{"NAP-IDENTITY@a040914b.md", "NAP-IDENTITY", "a040914b4bbd3a5cd8a14b0f316a723c968ebfb2", "pin"},
	{"NAP-INC@a040914b.md", "NAP-INC", "a040914b4bbd3a5cd8a14b0f316a723c968ebfb2", "pin"},
	{"NAP-INTENT@a040914b.md", "NAP-INTENT", "a040914b4bbd3a5cd8a14b0f316a723c968ebfb2", "pin"},
	{"NAP-THEME@a040914b.md", "NAP-THEME", "a040914b4bbd3a5cd8a14b0f316a723c968ebfb2", "pin"},
	{"NAP-RELAY@0be8abce.md", "NAP-RELAY", "0be8abce18beb46ca37bd4ddd042f58d30b4eedc", "pin"},
	{"NAP-STORAGE@f71e84eb.md", "NAP-STORAGE", "f71e84ebca7474db260346cbfc2d88f41b4e421e", "pin"},
	{"NAP-MEDIA@2b2d29e9.md", "NAP-MEDIA", "2b2d29e90c30b994bf5035a65b57e5fe7f08a9a2", "pin"},
	{"NAP-NOTIFY@e14f5c9d.md", "NAP-NOTIFY", "e14f5c9d6a6dd2a69ccf79668c4a3c1e955e1ac9", "pin"},
	{"NAP-CONFIG@448013e6.md", "NAP-CONFIG", "448013e6d8cb8c75dce49576b3e7c0d46d960eac", "pin"},
	{"NAP-OUTBOX@4589a8f9.md", "NAP-OUTBOX", "4589a8f9a16d8aa29b3740e2b3b0cdca11e0976e", "pin"},
	{"NAP-UPLOAD@a7cc1746.md", "NAP-UPLOAD", "a7cc17463cbf5d9cb87884b31071bc4fc826034c", "pin"},
	{"NAP-LINK@e2514335.md", "NAP-LINK", "e25143355f6d416bfce73b12ec814f1c795ec16a", "pin"},
	{"NAP-COMMON@de603e20.md", "NAP-COMMON", "de603e205a9b498f252be9a5e8e6825c4648df39", "pin"},
	{"NAP-RESOURCE@fa6bcc69.md", "NAP-RESOURCE", "fa6bcc6935aa19e7b70ab2a2c721dafca77c78e1", "pin"},
	// the shim 0.30.0 server-hint shape the host also accepts; a tolerance,
	// kept as its own file and never merged with the PR #80 pin above
	{"NAP-RESOURCE@9511232f.md", "NAP-RESOURCE", "9511232f69313aa7953d110e35d32cc28d506f66", "tolerance"},
}

const pinnedSpecDir = "../spec/pinned"

// pinnedCommitHex is a full git commit id; body_sha256 reuses hex64.
var pinnedCommitHex = regexp.MustCompile(`^[0-9a-f]{40}$`)

// splitPinnedSnapshot separates the front matter from the body. The body is
// every byte after the first "\n---\n" that follows the opening "---\n", with
// nothing trimmed or normalized, because body_sha256 covers those raw bytes.
func splitPinnedSnapshot(raw []byte) (map[string]string, []byte, bool) {
	if !bytes.HasPrefix(raw, []byte("---\n")) {
		return nil, nil, false
	}
	end := bytes.Index(raw[3:], []byte("\n---\n"))
	if end < 0 {
		return nil, nil, false
	}
	end += 3
	front := map[string]string{}
	for _, line := range strings.Split(string(raw[4:end]), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, nil, false
		}
		front[key] = value
	}
	return front, raw[end+5:], true
}

func TestPinnedSpecSnapshotsMatchTheirHashes(t *testing.T) {
	onDisk, err := filepath.Glob(filepath.Join(pinnedSpecDir, "*@*.md"))
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, p := range onDisk {
		present[filepath.Base(p)] = true
	}
	listed := map[string]bool{}
	for _, s := range pinnedSnapshots {
		if listed[s.File] {
			t.Errorf("%s is listed twice", s.File)
		}
		listed[s.File] = true
		if !present[s.File] {
			t.Errorf("%s is listed but missing from spec/pinned", s.File)
		}
	}
	for name := range present {
		if !listed[name] {
			t.Errorf("spec/pinned/%s is not in pinnedSnapshots", name)
		}
	}

	for _, s := range pinnedSnapshots {
		raw, err := os.ReadFile(filepath.Join(pinnedSpecDir, s.File))
		if err != nil {
			continue // already reported above
		}
		front, body, ok := splitPinnedSnapshot(raw)
		if !ok {
			t.Errorf("%s: front matter must open with ---, hold key: value lines and close with ---", s.File)
			continue
		}
		for _, key := range []string{"spec", "role", "repo", "ref", "commit", "path", "fetched", "body_sha256"} {
			if front[key] == "" {
				t.Errorf("%s: front matter is missing %s", s.File, key)
			}
		}
		commit := front["commit"]
		if !pinnedCommitHex.MatchString(commit) {
			t.Errorf("%s: commit %q is not 40 lowercase hex", s.File, commit)
			continue
		}
		if !hex64.MatchString(front["body_sha256"]) {
			t.Errorf("%s: body_sha256 %q is not 64 lowercase hex", s.File, front["body_sha256"])
		}
		if want := front["spec"] + "@" + commit[:8] + ".md"; s.File != want {
			t.Errorf("%s: file name does not match its front matter (want %s)", s.File, want)
		}
		if front["spec"] != s.Spec || commit != s.Commit || front["role"] != s.Role {
			t.Errorf("%s: front matter spec/commit/role %s/%s/%s, pinnedSnapshots says %s/%s/%s",
				s.File, front["spec"], commit, front["role"], s.Spec, s.Commit, s.Role)
		}
		if len(body) == 0 {
			t.Errorf("%s: body is empty", s.File)
			continue
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != front["body_sha256"] {
			t.Errorf("%s: body hashes to %s, front matter says %s (snapshots are never edited; re-pin as a new file)",
				s.File, got, front["body_sha256"])
		}
	}

	// the README index lists the snapshots in SPEC-PINS order
	readme, err := os.ReadFile(filepath.Join(pinnedSpecDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	last, lastFile := -1, ""
	for _, s := range pinnedSnapshots {
		i := strings.Index(string(readme), s.File)
		if i < 0 {
			t.Errorf("README.md does not mention %s", s.File)
			continue
		}
		if i <= last {
			t.Errorf("README.md lists %s before %s, but SPEC-PINS order puts it after", s.File, lastFile)
		}
		last, lastFile = i, s.File
	}
}
