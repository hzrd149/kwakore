package controlprotocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONRPCEnvelopeErrors(t *testing.T) {
	cases := []struct {
		name, input string
		code        int
		id          string
	}{
		{"parse", `{"jsonrpc":`, ParseError, "null"},
		{"trailing", `{"jsonrpc":"2.0","method":"x","id":1} true`, ParseError, "null"},
		{"missing version", `{"method":"x","id":1}`, InvalidRequest, "null"},
		{"duplicate", `{"jsonrpc":"2.0","method":"x","method":"y","id":1}`, InvalidRequest, "null"},
		{"invalid id", `{"jsonrpc":"2.0","method":"x","id":true}`, InvalidRequest, "null"},
		{"fractional id", `{"jsonrpc":"2.0","method":"x","id":1.5}`, InvalidRequest, "null"},
		{"unknown", `{"jsonrpc":"2.0","method":"x","id":"a"}`, MethodNotFound, `"a"`},
		{"reserved", `{"jsonrpc":"2.0","method":"rpc.test","id":1}`, MethodNotFound, "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ProcessFrame([]byte(tc.input), func(string, json.RawMessage) (any, *Error) { return nil, FixedError(MethodNotFound) })
			var got struct {
				ID    json.RawMessage `json:"id"`
				Error *Error          `json:"error"`
			}
			if err := json.Unmarshal(out, &got); err != nil || got.Error == nil || got.Error.Code != tc.code || string(got.ID) != tc.id {
				t.Fatalf("response %s: %s, %v", tc.name, out, err)
			}
		})
	}
}

func TestJSONRPCBatchAndNotifications(t *testing.T) {
	calls := 0
	dispatch := func(method string, params json.RawMessage) (any, *Error) {
		calls++
		return map[string]int{"count": calls}, nil
	}
	if got := ProcessFrame([]byte(`{"jsonrpc":"2.0","method":"tick"}`), dispatch); got != nil || calls != 1 {
		t.Fatalf("notification: %s, calls=%d", got, calls)
	}
	out := ProcessFrame([]byte(`[{"jsonrpc":"2.0","method":"tick"},{"jsonrpc":"2.0","method":"tick","id":null},{"jsonrpc":"2.0","method":"tick","id":7}]`), dispatch)
	var batch []struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(out, &batch); err != nil || len(batch) != 2 || string(batch[0].ID) != "null" || string(batch[1].ID) != "7" || calls != 4 {
		t.Fatalf("batch: %s, calls=%d, err=%v", out, calls, err)
	}
	if got := ProcessFrame([]byte(`[{"jsonrpc":"2.0","method":"tick"}]`), dispatch); got != nil || calls != 5 {
		t.Fatalf("notification batch: %s", got)
	}
	if got := ProcessFrame([]byte(`[]`), dispatch); !bytes.Contains(got, []byte(`"code":-32600`)) {
		t.Fatalf("empty batch: %s", got)
	}
	if got := ProcessFrame([]byte(strings.Repeat(`{"jsonrpc":"2.0","method":"tick"},`, 65)), dispatch); got == nil {
		t.Fatal("oversized batch accepted")
	}
}
