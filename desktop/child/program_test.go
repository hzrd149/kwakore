package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowProgramsRejectWrongFormat(t *testing.T) {
	for _, tc := range []struct{ kind, format, windowKind, want string }{
		{"napplet", "", "", "napp cannot run in the napplet program"},
		{"napplet", "napplet", "settings", "settings cannot run in the napplet program"},
		{"napp", "napplet", "", "napplet cannot run in the napp program"},
	} {
		t.Run(tc.kind+"_"+tc.format+"_"+tc.windowKind, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), tc.kind)
			args := []string{"build"}
			if tc.kind == "napp" {
				args = append(args, "-tags", "napp")
			}
			args = append(args, "-o", bin, ".")
			if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
				t.Fatalf("build: %v\n%s", err, out)
			}
			cmd := exec.Command(bin)
			cmd.Env = append(os.Environ(), "VERDANA_NAPP_FORMAT="+tc.format, "VERDANA_WINDOW_KIND="+tc.windowKind)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Fatalf("run = %v, %s", err, out)
			}
		})
	}
}
