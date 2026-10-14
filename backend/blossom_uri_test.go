package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBUD10BlossomURI(t *testing.T) {
	hash := strings.Repeat("a", 64)
	author := strings.Repeat("b", 64)
	uri, err := parseBlossomURI("blossom:" + hash + ".png?xs=cdn.example.com&xs=https%3A%2F%2Fother.example&as=" + author + "&as=" + hash + "&sz=123")
	if err != nil || uri.Hash != hash || uri.Extension != "png" || uri.Size != 123 || len(uri.Servers) != 2 || uri.Servers[0] != "https://cdn.example.com" || len(uri.Authors) != 2 {
		t.Fatalf("BUD-10 parse: %#v %v", uri, err)
	}
	if _, err := parseBlossomURI("blossom:" + hash + ".bin"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"blossom::" + hash + ".png", "blossom:" + hash, "blossom:" + strings.ToUpper(hash) + ".png",
		"blossom:" + hash + ".png?as=nope", "blossom:" + hash + ".png?sz=0", "blossom:" + hash + ".png?sz=-1",
		"blossom:" + hash + ".png?sz=1&sz=2", "blossom:" + hash + ".png?sz=%2B1", "blossom:" + hash + ".png?xs=https://user:pass@example.com",
		"blossom:" + hash + ".png?xs=example.com/path", "blossom:" + hash + ".png#fragment",
	} {
		if _, err := parseBlossomURI(raw); err == nil {
			t.Fatalf("accepted invalid BUD-10 URI: %s", raw)
		}
	}
}

// Scan tracked files so examples and archived specifications cannot reintroduce
// a nested digest scheme in Blossom URIs.
func TestProjectBlossomURIsUseBUD10(t *testing.T) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = ".."
	files, err := cmd.Output()
	if err != nil {
		t.Skip("source-tree URI check requires a git checkout")
	}
	qualified := regexp.MustCompile(`(?i)blossom:[a-z0-9]+:`)
	for _, file := range strings.Split(string(files), "\x00") {
		if file == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", file))
		if err != nil {
			t.Fatal(err)
		}
		if qualified.Match(data) {
			t.Errorf("%s contains a non-BUD-10 digest-qualified URI", file)
		}
	}
}
