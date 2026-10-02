package webview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// the vendored prelude is npm @napplet/shim byte for byte: no patches, no
// reformatting, no added newline. The README states the same version and hash
// so a reader never has to trust a stale note.
func TestShimPreludeIsPristineUpstream(t *testing.T) {
	sum := sha256.Sum256([]byte(shimPrelude))
	if got := hex.EncodeToString(sum[:]); got != ShimSHA256 {
		t.Fatalf("prelude.global.js sha256 = %s, want npm @napplet/shim@%s %s", got, ShimVersion, ShimSHA256)
	}
	if strings.Contains(ShimVersion, "+") {
		t.Fatalf("ShimVersion %q carries a local build suffix; the shim must be an unmodified upstream release", ShimVersion)
	}
	readme, err := os.ReadFile("shim/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**" + ShimVersion + "**", ShimSHA256} {
		if !bytes.Contains(readme, []byte(want)) {
			t.Errorf("shim/README.md does not state %q", want)
		}
	}
}
