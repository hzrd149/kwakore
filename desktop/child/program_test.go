package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowProgramsRejectWrongFormat(t *testing.T) {
	// the child is napplet-only (D-10): a napp (35130) window or the retired
	// settings page is refused before any webview exists
	bin := filepath.Join(t.TempDir(), "napplet")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, tc := range []struct{ format, windowKind, want string }{
		{"", "", "napp cannot run in the napplet program"},
		{"napp", "", "napp cannot run in the napplet program"},
		{"napplet", "settings", "settings cannot run in the napplet program"},
	} {
		t.Run(tc.format+"_"+tc.windowKind, func(t *testing.T) {
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(), "VERDANA_NAPP_FORMAT="+tc.format, "VERDANA_WINDOW_KIND="+tc.windowKind)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Fatalf("run = %v, %s", err, out)
			}
		})
	}
}
