package backend

import (
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
		"blossom:sha256:" + hash + ".png", "blossom:" + hash, "blossom:" + strings.ToUpper(hash) + ".png",
		"blossom:" + hash + ".png?as=nope", "blossom:" + hash + ".png?sz=0", "blossom:" + hash + ".png?sz=-1",
		"blossom:" + hash + ".png?sz=1&sz=2", "blossom:" + hash + ".png?xs=https://user:pass@example.com",
		"blossom:" + hash + ".png?xs=example.com/path", "blossom:" + hash + ".png#fragment",
	} {
		if _, err := parseBlossomURI(raw); err == nil {
			t.Fatalf("accepted invalid BUD-10 URI: %s", raw)
		}
	}
}
