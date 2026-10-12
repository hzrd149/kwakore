//go:build linux

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

func TestCLIHistoryParameters(t *testing.T) {
	pk := nostr.Generate().Public()
	address := "35129:" + pk.Hex() + ":app"
	naddr := nip19.EncodeNaddr(pk, 35129, "app", nil)
	for _, name := range []string{"versions", "snapshots"} {
		method, params, _, err := command([]string{name, "nostr:" + naddr, "--refresh", "--offset", "5", "--limit", "20"})
		if err != nil || method != "napplet."+name {
			t.Fatalf("%s: %s %v", name, method, err)
		}
		var decoded struct {
			Address string `json:"address"`
			Refresh bool   `json:"refresh"`
			Offset  int    `json:"offset"`
			Limit   int    `json:"limit"`
		}
		if json.Unmarshal(params, &decoded) != nil || decoded.Address != address || !decoded.Refresh || decoded.Offset != 5 || decoded.Limit != 20 {
			t.Fatalf("params %s", params)
		}
		if commandDeadline(method, 0) < 45*time.Second {
			t.Fatal("refresh deadline too short")
		}
		for _, args := range [][]string{
			{name}, {name, "bad"}, {name, address, "extra"}, {name, address, "--limit", "0"},
			{name, address, "--limit", "501"}, {name, address, "--offset", "-1"}, {name, address, "--refresh", "true"},
			{name, address, "--unknown"},
		} {
			if _, _, _, err := command(args); err == nil {
				t.Fatalf("accepted %v", args)
			}
		}
		if text, ok := helpTopic([]string{name, "--help"}); !ok || !strings.Contains(text, "--refresh") {
			t.Fatalf("missing help for %s", name)
		}
	}
}
