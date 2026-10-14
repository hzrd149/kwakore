package builtinhost

import (
	"context"
	"encoding/json"
	"kwakore/backend"
	"strings"
	"testing"
)

func TestChildCannotForgeBrokerFrames(t *testing.T) {
	for _, typ := range []string{"extension.response", "extension.request", "extension.cancel", "eval"} {
		calls := 0
		err := ForwardChild(strings.NewReader(`{"t":"`+typ+`","request_id":"ext-1"}`+"\n"), func(any) error { calls++; return nil })
		if err == nil || calls != 0 {
			t.Fatalf("forwarded %s", typ)
		}
	}
	var got backend.WireMsg
	err := ForwardChild(strings.NewReader(`{"t":"rpc","id":1,"method":"nap.start","request_id":"ext-1","envelope":{"secret":true}}`+"\n"), func(value any) error { got = value.(backend.WireMsg); return nil })
	if err != nil || got.RequestID != "" || got.Envelope != nil || got.Method != "nap.start" {
		t.Fatal(got, err)
	}
}
func TestHandlerPanicAndCancellation(t *testing.T) {
	req := backend.ExtensionRequest{Envelope: json.RawMessage(`{"type":"example.request","id":"1"}`)}
	raw := invoke(context.Background(), func(context.Context, backend.ExtensionRequest) (json.RawMessage, error) { panic("private") }, req)
	if !strings.Contains(string(raw), `"code":-32000`) || strings.Contains(string(raw), "private") {
		t.Fatal(string(raw))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	invoke(ctx, func(context.Context, backend.ExtensionRequest) (json.RawMessage, error) {
		called = true
		return nil, nil
	}, req)
	if called {
		t.Fatal("cancelled work executed")
	}
}
