// Package builtinhost connects a native application's unary extension handlers
// to an attached Kwakore window. Private broker frames never reach the child.
package builtinhost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

// Run opens a built-in and proxies the stock child until it closes. socket and
// child must be absolute paths. handler owns its domain business logic and
// must respect cancellation. Run does not replay requests or reconnect.
func Run(ctx context.Context, socket, child string, definition backend.BuiltinDefinition, handler backend.ExtensionHandler) error {
	if !filepath.IsAbs(socket) || !filepath.IsAbs(child) || handler == nil {
		return errors.New("absolute socket/child paths and handler required")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	return RunConnection(ctx, conn, child, definition, handler)
}

// RunConnection takes ownership of an already authenticated native connection.
// This supports inherited connections in sandboxed native applications.
func RunConnection(ctx context.Context, conn net.Conn, child string, definition backend.BuiltinDefinition, handler backend.ExtensionHandler) error {
	if conn == nil || !filepath.IsAbs(child) || handler == nil {
		return errors.New("native connection, absolute child path and handler required")
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(12 * time.Second))
	shutdown := context.AfterFunc(ctx, func() { conn.Close() })
	defer shutdown()
	var writeMu sync.Mutex
	send := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := conn.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return err
		}
		return json.NewEncoder(conn).Encode(value)
	}
	params, err := json.Marshal(definition)
	if err != nil {
		return err
	}
	request := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}{"2.0", 1, "runtime.builtin.open", params}
	encoded, err := json.Marshal(request)
	if err != nil || len(encoded) > controlprotocol.MaxRequestLine {
		return errors.New("registration exceeds control transport bound")
	}
	if err = send(request); err != nil {
		return err
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), backend.MaxInboundWireMsg+1)
	if !scanner.Scan() {
		return errors.New("missing registration response")
	}
	var response struct {
		Result backend.ServiceLaunchResult `json:"result"`
		Error  *controlprotocol.Error      `json:"error"`
	}
	if json.Unmarshal(scanner.Bytes(), &response) != nil {
		return errors.New("invalid registration response")
	}
	if response.Error != nil {
		return fmt.Errorf("registration rejected: %s", response.Error.Message)
	}
	_ = conn.SetReadDeadline(time.Time{})
	w := response.Result.Window
	if w == nil || w.Instance == "" || w.Format != "napplet" {
		return errors.New("missing attached window descriptor")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, child)
	cmd.Env = append(os.Environ(), "KWAKORE_NAPP_ID="+w.NappID, "KWAKORE_NAPP_NAME="+w.Name, "KWAKORE_INSTANCE_ID="+w.Instance, "KWAKORE_NAPP_FORMAT=napplet", "KWAKORE_THEME="+w.Theme, "KWAKORE_THEME_VARS="+w.ThemeVars, "KWAKORE_WINDOW_WIDTH="+strconv.Itoa(w.Width), "KWAKORE_WINDOW_HEIGHT="+strconv.Itoa(w.Height), "WEBVIEW_PATH="+filepath.Dir(child))
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() { cancel(); stdin.Close(); cmd.Wait() }()
	childDone := make(chan error, 1)
	go func() { childDone <- ForwardChild(stdout, send); conn.Close() }()
	var mu sync.Mutex
	pending := map[string]context.CancelFunc{}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, cancel := range pending {
			cancel()
		}
	}()
	closing := false
	for scanner.Scan() {
		var msg backend.WireMsg
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			return errors.New("invalid daemon frame")
		}
		if closing && msg.T != "extension.cancel" {
			continue
		}
		switch msg.T {
		case "extension.request":
			if msg.Extension == nil || msg.Extension.Instance != w.Instance || msg.RequestID == "" {
				return errors.New("invalid broker request")
			}
			mu.Lock()
			if _, exists := pending[msg.RequestID]; exists || len(pending) >= 64 {
				mu.Unlock()
				return errors.New("broker pending bound exceeded")
			}
			callCtx, callCancel := context.WithCancel(runCtx)
			pending[msg.RequestID] = callCancel
			mu.Unlock()
			go func(msg backend.WireMsg) {
				defer callCancel()
				defer func() { mu.Lock(); delete(pending, msg.RequestID); mu.Unlock() }()
				raw := invoke(callCtx, handler, *msg.Extension)
				if callCtx.Err() == nil {
					if send(backend.WireMsg{T: "extension.response", RequestID: msg.RequestID, Envelope: raw}) != nil {
						conn.Close()
					}
				}
			}(msg)
		case "extension.cancel":
			mu.Lock()
			cancel := pending[msg.RequestID]
			mu.Unlock()
			if cancel != nil {
				cancel()
			}
		default:
			if strings.HasPrefix(msg.T, "extension.") {
				return errors.New("unknown broker frame")
			}
			if msg.T == "close" {
				closing = true
				mu.Lock()
				for _, cancel := range pending {
					cancel()
				}
				mu.Unlock()
			}
			if err = json.NewEncoder(stdin).Encode(msg); err != nil {
				if closing {
					return nil
				}
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case err := <-childDone:
		return err
	default:
		return scanner.Err()
	}
}

func invoke(ctx context.Context, handler backend.ExtensionHandler, req backend.ExtensionRequest) (raw json.RawMessage) {
	failure := func() json.RawMessage {
		var head struct {
			Type string          `json:"type"`
			ID   json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(req.Envelope, &head)
		raw, _ := json.Marshal(map[string]any{"type": head.Type + ".result", "id": head.ID, "error": backend.ExtensionError{Code: -32000, Message: "Custom request failed", Reason: "internal-error"}})
		return raw
	}
	defer func() {
		if recover() != nil {
			raw = failure()
		}
	}()
	if ctx.Err() != nil {
		return nil
	}
	raw, err := handler(ctx, req)
	if err != nil {
		return failure()
	}
	return raw
}

// ForwardChild accepts only the existing child-to-daemon frame types. A child
// cannot send extension responses, registration calls or broker authority.
func ForwardChild(reader io.Reader, send func(any) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), backend.MaxInboundWireMsg+1)
	for scanner.Scan() {
		var msg backend.WireMsg
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			return errors.New("invalid child frame")
		}
		switch msg.T {
		case "rpc", "promptAnswer", "windowFailed":
		default:
			return errors.New("unauthorized child frame")
		}
		// Re-encode a clean frame: private fields never pass through this lane.
		clean := backend.WireMsg{T: msg.T, ID: msg.ID, Method: msg.Method, Params: msg.Params, Code: msg.Code}
		if err := send(clean); err != nil {
			return err
		}
	}
	return scanner.Err()
}
