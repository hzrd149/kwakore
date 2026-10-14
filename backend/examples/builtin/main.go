// A native-owned page with a private API and standard theme/storage/gamepad.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"kwakore/backend"
	"kwakore/backend/builtinhost"
)

const binding = `window.napplet.example = {call(method) { return napExtension.call('example.request',{method},6000); }};`
const page = `<!doctype html><html><body><h1>Native-owned napplet</h1>
<button id="hello">Call native API</button><pre id="output"></pre><p id="pads"></p>
<script>
const output=document.getElementById('output');
document.getElementById('hello').onclick=async()=>{
 try {
  const result=await napplet.example.call('hello');
  await napplet.storage.setItem('lastGreeting',result.greeting);
  const theme=await napplet.theme.get();
  output.textContent=JSON.stringify({result,theme},null,2);
 } catch(error) {output.textContent=JSON.stringify(error)}
};
setInterval(()=>{document.getElementById('pads').textContent='Controllers: '+napplet.gamepad.getGamepads().filter(Boolean).length},500);
</script></body></html>`

func main() {
	socket := flag.String("socket", os.Getenv("XDG_RUNTIME_DIR")+"/kwakore/daemon.sock", "absolute Kwakore control socket")
	child := flag.String("child", "", "absolute path to stock napplet executable beside libwebview.so")
	flag.Parse()
	sum := sha256.Sum256([]byte(page))
	definition := backend.BuiltinDefinition{Key: "org.example.native-store", Name: "Native API example", HTML: page, SHA256: hex.EncodeToString(sum[:]), Domains: []string{"theme", "storage", "gamepad"}, Extensions: []backend.ExtensionDomain{{Name: "example", Version: "1", Prelude: binding, Routes: []backend.ExtensionRoute{{Type: "example.request", MaxRequestBytes: 4096, MaxResponseBytes: 4096, Concurrency: 4, RequestsPerSecond: 10, DeadlineMS: 5000}}}}}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	handler := func(ctx context.Context, req backend.ExtensionRequest) (json.RawMessage, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var input struct {
			Type   string          `json:"type"`
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(req.Envelope, &input) != nil {
			return nil, fmt.Errorf("invalid input")
		}
		response := map[string]any{"type": input.Type + ".result", "id": input.ID}
		if input.Method != "hello" {
			response["error"] = backend.ExtensionError{Code: 400, Message: "Unknown method"}
		} else {
			response["result"] = map[string]any{"greeting": "Hello from the native application"}
		}
		return json.Marshal(response)
	}
	if err := builtinhost.Run(ctx, *socket, *child, definition, handler); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
