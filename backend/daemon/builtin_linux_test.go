//go:build linux

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kwakore/backend"
	"kwakore/backend/builtinhost"
)

func daemonBuiltin(html string) backend.BuiltinDefinition {
	sum := sha256.Sum256([]byte(html))
	return backend.BuiltinDefinition{Key: "org.example.test", Name: "Custom native window", HTML: html, SHA256: hex.EncodeToString(sum[:]), Domains: []string{"gamepad", "theme", "storage"}, Extensions: []backend.ExtensionDomain{{Name: "example", Version: "1", Prelude: `window.napplet.example = {call(){return napExtension.call("example.request",{},6000)}};`, Routes: []backend.ExtensionRoute{{Type: "example.request", MaxRequestBytes: 4096, MaxResponseBytes: 4096, Concurrency: 4, RequestsPerSecond: 10, DeadlineMS: 5000}}}}}
}
func TestRPCBuiltinAttached(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	d := daemonBuiltin("<p>native</p>")
	params, _ := json.Marshal(d)
	result, rpcErr, _ := rpcCall(t, reader, conn, "runtime.builtin.open", string(params))
	var launch backend.ServiceLaunchResult
	if rpcErr != nil || json.Unmarshal(result, &launch) != nil || launch.Window == nil {
		t.Fatal(string(result), rpcErr)
	}
	send := func(msg backend.WireMsg) {
		t.Helper()
		if err := json.NewEncoder(conn).Encode(msg); err != nil {
			t.Fatal(err)
		}
	}
	read := func() backend.WireMsg {
		t.Helper()
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var msg backend.WireMsg
		if json.Unmarshal(line, &msg) != nil {
			t.Fatal(string(line))
		}
		return msg
	}
	send(backend.WireMsg{T: "rpc", ID: 1, Method: "nap.start"})
	if msg := read(); msg.T != "resp" {
		t.Fatal(msg)
	}
	send(backend.WireMsg{T: "rpc", ID: 2, Method: "nap.msg", Params: `{"type":"example.request","id":"x"}`})
	var request backend.WireMsg
	for request.T != "extension.request" {
		request = read()
	}
	if request.Extension == nil || request.Extension.Instance != launch.WindowID || request.Extension.Generation < 1 {
		t.Fatal(request)
	}
	send(backend.WireMsg{T: "extension.response", RequestID: "unknown", Envelope: json.RawMessage(`{"type":"example.request.result","id":"x","result":"forged"}`)})
	send(backend.WireMsg{T: "extension.response", RequestID: request.RequestID, Envelope: json.RawMessage(`{"type":"example.request.result","id":"x","result":"hello"}`)})
	var msg backend.WireMsg
	for msg.T != "eval" {
		msg = read()
	}
	if !strings.Contains(msg.Code, "hello") || strings.Contains(msg.Code, "forged") {
		t.Fatal(msg)
	}
	// Duplicate replies are discarded, and the next session cancels old work.
	send(backend.WireMsg{T: "extension.response", RequestID: request.RequestID, Envelope: json.RawMessage(`{"type":"example.request.result","id":"x","result":"duplicate"}`)})
	send(backend.WireMsg{T: "rpc", ID: 3, Method: "nap.msg", Params: `{"type":"example.request","id":"pending"}`})
	request = backend.WireMsg{}
	for request.T != "extension.request" {
		request = read()
	}
	send(backend.WireMsg{T: "rpc", ID: 4, Method: "nap.reset"})
	for {
		msg = read()
		if msg.T == "extension.cancel" {
			break
		}
		if strings.Contains(msg.Code, "duplicate") {
			t.Fatal("duplicate delivered")
		}
	}
	if msg.RequestID != request.RequestID {
		t.Fatal(msg)
	}
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for len(backend.ServiceWindows()) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(backend.ServiceWindows()) > 0 {
		t.Fatal("disconnect retained built-in")
	}
}

// Exercises the full native proxy + daemon + actual sandboxed WebKit frame.
func TestRPCBuiltinRealChildGraphical(t *testing.T) {
	if os.Getenv("KWAKORE_REQUIRE_GRAPHICS") != "1" {
		t.Skip("local-only real built-in test: set KWAKORE_REQUIRE_GRAPHICS=1")
	}
	child := requiredGraphicalFile(t, "KWAKORE_WINDOW_BIN", true)
	library := requiredGraphicalFile(t, "KWAKORE_WEBVIEW_LIB", false)
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-builtin-child-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	program := filepath.Join(dir, "napplet")
	copyGraphicalFile(t, child, program, 0700)
	copyGraphicalFile(t, library, filepath.Join(dir, "libwebview.so"), 0600)
	_, _, _, paths := rpcService(t)
	d := daemonBuiltin(`<body><script>
(async()=>{
 try {
  const result=await napplet.example.call();
  const theme=await napplet.theme.get();
  const pads=napplet.gamepad.getGamepads();
  await napplet.storage.setItem('verified',JSON.stringify({result,theme:!!theme.colors,pads:Array.isArray(pads),private:!!napplet.example}));
 }catch(error){await napplet.storage.setItem('failed',JSON.stringify(error))}
})();
</script></body>`)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- builtinhost.Run(ctx, filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "kwakore", "daemon.sock"), program, d, func(ctx context.Context, req backend.ExtensionRequest) (json.RawMessage, error) {
			var head struct {
				ID json.RawMessage `json:"id"`
			}
			_ = json.Unmarshal(req.Envelope, &head)
			return json.Marshal(map[string]any{"type": "example.request.result", "id": head.ID, "result": "native-result"})
		})
	}()
	key := sha256.Sum256([]byte(d.Key))
	file := filepath.Join(paths.DataDir, "builtin-storage", hex.EncodeToString(key[:])+".json")
	verified := false
	for ctx.Err() == nil {
		raw, _ := os.ReadFile(file)
		var store map[string]string
		_ = json.Unmarshal(raw, &store)
		if store["failed"] != "" {
			t.Fatal(store)
		}
		if value := store["verified"]; value != "" {
			for _, want := range []string{`"result":"native-result"`, `"theme":true`, `"pads":true`, `"private":true`} {
				if !strings.Contains(value, want) {
					t.Fatal(value)
				}
			}
			verified = true
			break
		}
		select {
		case err := <-done:
			t.Fatalf("proxy exited before verification: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !verified {
		t.Fatal("actual WebKit custom/stock calls did not complete")
	}
	windows := backend.ServiceWindows()
	if len(windows) != 1 || windows[0].Address != "" {
		t.Fatal(windows)
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if _, err := backend.ServiceStop(stopCtx, windows[0].WindowID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-stopCtx.Done():
		t.Fatal("proxy did not close")
	}
	// A document.open replacement has no load event. Its ordinary messages
	// may reach the old stock session, but it must not acquire the private lane.
	replacementHTML := `<body><script>
 napplet.example.call().catch(()=>{});
 setTimeout(()=>{
  document.open();
  document.write('<script>parent.postMessage({type:"example.request",id:"forged",method:"forged"},"*");try{napplet.example.call().catch(()=>{})}catch(error){}<'+ '/script>');
 },100);
 </script></body>`
	d = daemonBuiltin(replacementHTML)
	ended := make(chan struct{}, 8)
	var forged atomic.Bool
	replacementCtx, replacementCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer replacementCancel()
	replacementDone := make(chan error, 1)
	go func() {
		replacementDone <- builtinhost.Run(replacementCtx, filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "kwakore", "daemon.sock"), program, d, func(ctx context.Context, req backend.ExtensionRequest) (json.RawMessage, error) {
			if strings.Contains(string(req.Envelope), "forged") {
				forged.Store(true)
			}
			<-ctx.Done()
			ended <- struct{}{}
			return nil, ctx.Err()
		})
	}()
	select {
	case <-ended:
	case err := <-replacementDone:
		t.Fatalf("replacement proxy ended early: %v", err)
	case <-replacementCtx.Done():
		t.Fatal("replacement did not cancel private work")
	}
	if forged.Load() {
		t.Fatal("replacement invoked a private handler")
	}
	windows = backend.ServiceWindows()
	if len(windows) != 1 {
		t.Fatal(windows)
	}
	if _, err := backend.ServiceStop(stopCtx, windows[0].WindowID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-replacementDone:
	case <-replacementCtx.Done():
		t.Fatal("replacement proxy did not close")
	}

}

func TestRPCBuiltinCannotRegisterInBatch(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	d := daemonBuiltin("<p>native</p>")
	params, _ := json.Marshal(d)
	frame := `[{"jsonrpc":"2.0","id":1,"method":"runtime.builtin.open","params":` + string(params) + `}]` + "\n"
	if _, err := conn.Write([]byte(frame)); err != nil {
		t.Fatal(err)
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(line), `"error"`) || len(backend.ServiceWindows()) != 0 {
		t.Fatal("batch created native authority", string(line))
	}
}
