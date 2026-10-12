//go:build linux

package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

// ─── the hub ─────────────────────────────────────────────────────

func TestEventHubOverflowNotice(t *testing.T) {
	old := eventQueueSize
	eventQueueSize = 4
	t.Cleanup(func() { eventQueueSize = old })
	h := newEventHub()
	sub, err := h.subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		h.publish(backend.ServiceEvent{Type: backend.EventSettingsChanged, Fields: []string{fmt.Sprint(i)}})
	}
	ctx := t.Context()
	var last time.Time
	read := func() backend.ServiceEvent {
		t.Helper()
		e, ok := h.next(ctx, nil, sub)
		if !ok {
			t.Fatal("next ended")
		}
		if e.Time.Before(last) {
			t.Fatalf("time went back: %v after %v", e.Time, last)
		}
		last = e.Time
		return e
	}
	for i := 1; i <= 4; i++ {
		if e := read(); e.Type != backend.EventSettingsChanged || e.Fields[0] != fmt.Sprint(i) {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
	if e := read(); e.Type != controlprotocol.EventOverflow || e.Dropped != 6 {
		t.Fatalf("overflow: %+v", e)
	}
	for i := 11; i <= 12; i++ {
		h.publish(backend.ServiceEvent{Type: backend.EventSettingsChanged, Fields: []string{fmt.Sprint(i)}})
	}
	for i := 11; i <= 12; i++ {
		if e := read(); e.Fields[0] != fmt.Sprint(i) {
			t.Fatalf("event %d after the overflow: %+v", i, e)
		}
	}
	// an overflow notice reaches a subscriber whatever its types
	filtered, err := h.subscribe([]string{"window.opened"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		h.publish(backend.ServiceEvent{Type: backend.EventWindowOpened})
	}
	h.publish(backend.ServiceEvent{Type: backend.EventSettingsChanged})
	for i := 0; i < 4; i++ {
		if e, _ := h.next(ctx, nil, filtered); e.Type != backend.EventWindowOpened {
			t.Fatalf("filtered event %d: %+v", i, e)
		}
	}
	if e, _ := h.next(ctx, nil, filtered); e.Type != controlprotocol.EventOverflow || e.Dropped != 2 {
		t.Fatalf("filtered overflow: %+v", e)
	}
}

func TestEventHubPublishNeverBlocks(t *testing.T) {
	h := newEventHub()
	if _, err := h.subscribe(nil); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < 1000; i++ {
		h.publish(backend.ServiceEvent{Type: backend.EventSettingsChanged})
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("1000 publishes with no reader took %v", elapsed)
	}
}

// ─── over the socket ─────────────────────────────────────────────

func socketPathForTest(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "kwakore", "daemon.sock")
}

func dialService(t *testing.T) (*bufio.Reader, *net.UnixConn) {
	t.Helper()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPathForTest(t), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	return bufio.NewReader(conn), conn
}

type notification struct {
	JSONRPC string               `json:"jsonrpc"`
	Method  string               `json:"method"`
	ID      *json.RawMessage     `json:"id"`
	Params  backend.ServiceEvent `json:"params"`
}

func readNotification(t *testing.T, reader *bufio.Reader) (notification, string) {
	t.Helper()
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var n notification
	if err := json.Unmarshal([]byte(line), &n); err != nil {
		t.Fatal(err)
	}
	if n.JSONRPC != "2.0" || n.Method != controlprotocol.EventMethod || n.ID != nil {
		t.Fatalf("not an event notification: %s", line)
	}
	return n, line
}

func TestRPCEventsSettingsChanged(t *testing.T) {
	_, readerA, connA, _ := rpcService(t)
	result, rpcErr, _ := rpcCall(t, readerA, connA, "events.subscribe", `{"types":["settings.changed"]}`)
	if rpcErr != nil || string(result) != `{"subscribed":true,"types":["settings.changed"]}` {
		t.Fatalf("subscribe: %s %+v", result, rpcErr)
	}
	readerB, connB := dialService(t)
	if _, rpcErr, _ := rpcCall(t, readerB, connB, "settings.set", `{"field":"relays","value":["wss://one.example"]}`); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if _, rpcErr, _ := rpcCall(t, readerB, connB, "settings.set", `{"field":"theme","value":"dark"}`); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	first, rawFirst := readNotification(t, readerA)
	second, rawSecond := readNotification(t, readerA)
	if first.Params.Type != "settings.changed" || strings.Join(first.Params.Fields, ",") != "relays" {
		t.Fatalf("first: %s", rawFirst)
	}
	if second.Params.Type != "settings.changed" || strings.Join(second.Params.Fields, ",") != "theme" {
		t.Fatalf("second: %s", rawSecond)
	}
	if first.Params.Time.IsZero() || second.Params.Time.Before(first.Params.Time) {
		t.Fatalf("times %v then %v", first.Params.Time, second.Params.Time)
	}
	for _, raw := range []string{rawFirst, rawSecond} {
		if strings.Contains(raw, "wss://") {
			t.Fatalf("an event carries a URL: %s", raw)
		}
		var wire struct {
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(raw), &wire); err != nil {
			t.Fatal(err)
		}
		var stamp string
		if err := json.Unmarshal(wire.Params["time"], &stamp); err != nil {
			t.Fatal(err)
		}
		if _, err := time.Parse(time.RFC3339, stamp); err != nil {
			t.Fatalf("time %q is not RFC3339", stamp)
		}
	}
}

func TestRPCEventsSubscribeParams(t *testing.T) {
	s, reader, conn, _ := rpcService(t)
	for _, params := range []string{
		`{"types":["nope"]}`, `{"types":"x"}`, `{"types":null}`,
		`{"types":["window.opened","window.opened"]}`, `{"types":["events.overflow"]}`,
		`{"types":[5]}`, `{"other":1}`, `[]`,
	} {
		if _, rpcErr, _ := rpcCall(t, reader, conn, "events.subscribe", params); rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("%s: %+v", params, rpcErr)
		}
	}
	// outside a socket connection there is nothing to stream to
	if _, rpcErr := s.dispatchRPC("events.subscribe", nil); rpcErr == nil || rpcErr.Code != controlprotocol.Unavailable {
		t.Fatalf("no connection: %+v", rpcErr)
	}
	// every type, sorted, when none are named
	result, rpcErr, _ := rpcCall(t, reader, conn, "events.subscribe", `{"types":[]}`)
	want, _ := json.Marshal(struct {
		Subscribed bool     `json:"subscribed"`
		Types      []string `json:"types"`
	}{true, controlprotocol.EventTypes()})
	if rpcErr != nil || string(result) != string(want) {
		t.Fatalf("all types: %s %+v", result, rpcErr)
	}
}

func TestRPCEventsSecondSubscribeConflicts(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	batch := `[{"jsonrpc":"2.0","method":"events.subscribe","id":1},{"jsonrpc":"2.0","method":"events.subscribe","params":{"types":["window.opened"]},"id":2}]` + "\n"
	if _, err := conn.Write([]byte(batch)); err != nil {
		t.Fatal(err)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var responses []struct {
		ID     int                    `json:"id"`
		Result json.RawMessage        `json:"result"`
		Error  *controlprotocol.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &responses); err != nil || len(responses) != 2 {
		t.Fatalf("batch reply %s %v", line, err)
	}
	if responses[0].Error != nil || responses[1].Error == nil || responses[1].Error.Code != controlprotocol.Conflict {
		t.Fatalf("batch reply %s", line)
	}
}

func TestRPCEventsSubscriberCap(t *testing.T) {
	s, _, _, _ := rpcService(t)
	for i := 0; i < maxEventSubscribers; i++ {
		reader, conn := dialService(t)
		if _, rpcErr, _ := rpcCall(t, reader, conn, "events.subscribe", ""); rpcErr != nil {
			t.Fatalf("subscriber %d: %+v", i+1, rpcErr)
		}
	}
	reader, conn := dialService(t)
	if _, rpcErr, _ := rpcCall(t, reader, conn, "events.subscribe", ""); rpcErr == nil || rpcErr.Code != controlprotocol.Busy {
		t.Fatalf("subscriber past the cap: %+v", rpcErr)
	}
	if n := s.events.count(); n != maxEventSubscribers {
		t.Fatalf("hub holds %d subscribers", n)
	}
	// the refused connection still serves requests
	if _, rpcErr, _ := rpcCall(t, reader, conn, "service.status", ""); rpcErr != nil {
		t.Fatalf("status after Busy: %+v", rpcErr)
	}
}

func TestRPCEventsPeerCloseRemovesSubscriber(t *testing.T) {
	s, _, _, _ := rpcService(t)
	reader, conn := dialService(t)
	if _, rpcErr, _ := rpcCall(t, reader, conn, "events.subscribe", ""); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if n := s.events.count(); n != 1 {
		t.Fatalf("hub holds %d subscribers", n)
	}
	_ = conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.events.count() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("a closed subscriber was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRPCEventsDoNotBlockShutdown(t *testing.T) {
	paths := daemonPaths(t)
	s, err := Open(paths, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	reader, conn := dialService(t)
	if _, rpcErr, _ := rpcCall(t, reader, conn, "events.subscribe", ""); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	// the subscriber never reads: its queue and socket buffer fill
	for i := 0; i < 5000; i++ {
		s.events.publish(backend.ServiceEvent{Type: backend.EventSettingsChanged, Fields: []string{"relays"}})
	}
	done := make(chan struct{})
	go func() {
		s.BeginShutdown()
		_ = listener.Close()
		s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled subscriber blocked shutdown")
	}
	// the stream ends with the connection: what was written, then EOF
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("subscriber connection did not end with EOF: %v", err)
	}
}
