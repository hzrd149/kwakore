//go:build linux

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"sync/atomic"
	"testing"

	"kwakore/backend"
)

func TestSocialRefreshStartsWithOpenAndStopsWithClose(t *testing.T) {
	var (
		calls  atomic.Int32
		loop   context.Context
		waited atomic.Bool
	)
	startSocialRefresh = func(ctx context.Context) func() {
		calls.Add(1)
		loop = ctx
		return func() {
			// Close cancelled the work context before it waits
			if ctx.Err() == nil {
				t.Error("waited on a live social refresh context")
			}
			waited.Store(true)
		}
	}
	t.Cleanup(func() { startSocialRefresh = noSocialRefresh })

	s, err := Open(daemonPaths(t), "test")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || loop == nil || loop.Err() != nil {
		t.Fatalf("Open started the social refresh %d times, ctx err %v", calls.Load(), loop.Err())
	}
	s.Close()
	if loop.Err() == nil {
		t.Fatal("Close left the social refresh context live")
	}
	if !waited.Load() {
		t.Fatal("Close returned without waiting for the social refresh")
	}
	if calls.Load() != 1 {
		t.Fatalf("social refresh started %d times", calls.Load())
	}
}

func TestSocialRefreshWokenBySettingChanges(t *testing.T) {
	var wakes atomic.Int32
	socialRefreshChanged = func() { wakes.Add(1) }
	t.Cleanup(func() { socialRefreshChanged = backend.SocialRefreshChanged })
	s, _, _, paths := rpcService(t)
	step := func(what string, want int32, change func() error) {
		t.Helper()
		wakes.Store(0)
		if err := change(); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if got := wakes.Load(); got != want {
			t.Fatalf("%s woke the loop %d times, want %d", what, got, want)
		}
	}
	step("set 30m", 1, func() error { return s.SetSetting("social_refresh", "30m") })
	step("set 30m again", 0, func() error { return s.SetSetting("social_refresh", "30m") })
	step("set theme", 0, func() error { return s.SetSetting("theme", "dark") })
	step("clear", 1, func() error { return s.ClearSetting("social_refresh") })
	step("reload", 1, func() error {
		if err := os.WriteFile(paths.ConfigFile, []byte(`{"social_refresh":"12h"}`), 0600); err != nil {
			return err
		}
		return s.Reload()
	})
	if got := s.manager.Effective().SocialRefresh; got != "12h" {
		t.Fatalf("social_refresh after reload = %q", got)
	}
}

func TestRPCEventsSocialUpdated(t *testing.T) {
	s, reader, conn, _ := rpcService(t)
	result, rpcErr, _ := rpcCall(t, reader, conn, "events.subscribe", `{"types":["social.updated"]}`)
	if rpcErr != nil || string(result) != `{"subscribed":true,"types":["social.updated"]}` {
		t.Fatalf("subscribe: %s %+v", result, rpcErr)
	}
	const address = "35129:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:app"
	// another type is not delivered to this subscriber
	s.events.publish(backend.ServiceEvent{Type: backend.EventSettingsChanged, Fields: []string{"theme"}})
	s.events.publish(backend.ServiceEvent{Type: backend.EventSocialUpdated, Address: address, Fields: []string{"reactions"}})
	n, raw := readNotification(t, reader)
	if n.Params.Type != backend.EventSocialUpdated || n.Params.Address != address || !slices.Equal(n.Params.Fields, []string{"reactions"}) {
		t.Fatalf("notification %s", raw)
	}
	var wire struct {
		Params map[string]json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for k := range wire.Params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"address", "fields", "time", "type"}) {
		t.Fatalf("params hold %v", keys)
	}
}
