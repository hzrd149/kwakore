//go:build linux

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCLIExactInstallAndSelectionParams(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	id := strings.Repeat("b", 64)
	for _, args := range [][]string{{"install", address, "--event", id}, {"select", address, id}} {
		method, raw, _, err := command(args)
		if err != nil || method != "napplet."+args[0] {
			t.Fatalf("%v: %s %v", args, method, err)
		}
		var params map[string]string
		if json.Unmarshal(raw, &params) != nil || params["address"] != address || params["event_id"] != id {
			t.Fatalf("params %s", raw)
		}
	}
	for _, args := range [][]string{
		{"select"}, {"select", address}, {"select", address, id, "extra"},
		{"select", address, strings.Repeat("A", 64)}, {"select", address, "note1invalid"},
		{"install", address, "--event"}, {"install", address, "--event", "short"},
		{"install", address, "--event", strings.Repeat("B", 64)},
		{"install", address, "--unknown", id}, {"install", address, "--event", id, "extra"},
	} {
		if _, _, _, err := command(args); err == nil {
			t.Fatalf("accepted invalid selection %v", args)
		}
	}
}
