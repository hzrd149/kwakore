//go:build linux

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func attachedParams(ctx context.Context, params json.RawMessage, trial bool) (json.RawMessage, context.Context, *controlprotocol.Error) {
	names := []string{"address", "attach"}
	if trial {
		names = append(names, "relays")
	}
	if err := controlprotocol.ValidateNamedParams(params, names...); err != nil {
		return nil, ctx, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return nil, ctx, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	attach := false
	if raw, ok := fields["attach"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &attach) != nil {
			return nil, ctx, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
		delete(fields, "attach")
	}
	if attach {
		stream, ok := ctx.Value(connStreamKey{}).(*connStream)
		if !ok {
			return nil, ctx, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
		transport := &socketTransport{}
		stream.attached = transport
		ctx = backend.WithAttachedWindow(ctx, transport)
	}
	cleaned, _ := json.Marshal(fields)
	return cleaned, ctx, nil
}

// socketTransport holds all outbound frames until the peer sends its first
// wire frame. This makes buffered reads of the launch acknowledgement safe.
type socketTransport struct {
	mu       sync.Mutex
	conn     *net.UnixConn
	instance string
	queue    []backend.WireMsg
	active   bool
	closing  bool
	finished bool
	once     sync.Once
}

func (t *socketTransport) BindWindow(instance string) {
	t.mu.Lock()
	t.instance = instance
	t.mu.Unlock()
}

func (t *socketTransport) Send(msg backend.WireMsg) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	if !t.active {
		t.queue = append(t.queue, msg)
		t.mu.Unlock()
		return
	}
	_ = t.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	err := json.NewEncoder(t.conn).Encode(msg)
	t.mu.Unlock()
	if err != nil {
		t.finish()
	}
}
func (*socketTransport) Focus() {}
func (t *socketTransport) Close() {
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		return
	}
	t.closing = true
	t.mu.Unlock()
	go t.Send(backend.WireMsg{T: "close"})
	time.AfterFunc(2*time.Second, t.finish)
}
func (t *socketTransport) finish() {
	t.once.Do(func() {
		t.mu.Lock()
		t.finished = true
		t.queue = nil
		conn, instance := t.conn, t.instance
		t.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		if instance != "" {
			backend.WindowClosed(instance)
		}
	})
}
func (t *socketTransport) run(conn *net.UnixConn, reader *bufio.Reader) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		_ = conn.Close()
		return
	}
	t.conn = conn
	t.mu.Unlock()
	_ = conn.SetDeadline(time.Time{})
	_ = conn.SetReadDeadline(time.Now().Add(12 * time.Second))
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), backend.MaxInboundWireMsg+1)
	first := true
	for scanner.Scan() {
		if first {
			t.mu.Lock()
			t.active = true
			queued := t.queue
			t.queue = nil
			t.mu.Unlock()
			for _, msg := range queued {
				t.Send(msg)
			}
			first = false
		}
		var msg backend.WireMsg
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		backend.HandleMessage(t.instance, msg)
		if msg.T == "windowFailed" {
			break
		}
		if msg.T == "rpc" && msg.Method == "nap.start" && msg.ID > 0 && (msg.Params == "" || msg.Params == "null") {
			_ = conn.SetReadDeadline(time.Time{})
		}
	}
	t.finish()
}
