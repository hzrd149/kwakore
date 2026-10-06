package controlprotocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

func TestProtocolDocsMethods(t *testing.T) {
	docs, err := os.ReadFile(filepath.Join("..", "..", "docs", "control-protocol.md"))
	if err != nil {
		t.Fatal(err)
	}
	router, err := os.ReadFile(filepath.Join("..", "daemon", "rpc_linux.go"))
	if err != nil {
		t.Fatal(err)
	}
	cases := regexp.MustCompile(`case "((?:service|settings|napplet)\.[a-z]+)":`).FindAllSubmatch(router, -1)
	var routed []string
	for _, match := range cases {
		routed = append(routed, string(match[1]))
	}
	catalog := MethodNames()
	slices.Sort(catalog)
	slices.Sort(routed)
	if !slices.Equal(catalog, routed) {
		t.Fatalf("catalog %v differs from routed %v", catalog, routed)
	}
	for _, method := range catalog {
		if !bytes.Contains(docs, []byte("`"+method+"`")) {
			t.Errorf("documentation missing %s", method)
		}
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
	members := make([]string, 65)
	for i := range members {
		members[i] = `{"jsonrpc":"2.0","method":"tick"}`
	}
	if got := ProcessFrame([]byte("["+strings.Join(members, ",")+"]"), dispatch); !bytes.Contains(got, []byte(`"code":-32600`)) || calls != 5 {
		t.Fatalf("oversized batch: %s, calls=%d", got, calls)
	}
}

func TestJSONRPCNamedParams(t *testing.T) {
	for _, input := range []string{`{"extra":1}`, `{"name":1,"name":2}`, `[]`, `null`} {
		if err := ValidateNamedParams(json.RawMessage(input), "name"); err == nil || err.Code != InvalidParams {
			t.Fatalf("accepted %s: %+v", input, err)
		}
	}
	if err := ValidateNamedParams(json.RawMessage(`{"name":"ok"}`), "name"); err != nil {
		t.Fatal(err)
	}
}

func TestProcessFramePartialCleanupData(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	request := `{"jsonrpc":"2.0","method":"napplet.uninstall","id":1}`
	for _, tc := range []struct {
		name     string
		code     int
		data     any
		wantData bool
	}{
		{"valid", PartialCleanup, PartialCleanupData{address, true, false}, true},
		{"wrong code", Unavailable, PartialCleanupData{address, true, false}, false},
		{"wrong type", PartialCleanup, map[string]any{"address": address, "record_removed": true, "cleanup_complete": false, "private": "secret"}, false},
		{"empty address", PartialCleanup, PartialCleanupData{"", true, false}, false},
		{"record retained", PartialCleanup, PartialCleanupData{address, false, false}, false},
		{"cleanup complete", PartialCleanup, PartialCleanupData{address, true, true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dispatch := func(string, json.RawMessage) (any, *Error) {
				return nil, &Error{Code: tc.code, Message: "private failure text", Data: tc.data}
			}
			for _, frame := range []string{request, "[" + request + "]"} {
				out := ProcessFrame([]byte(frame), dispatch)
				if strings.HasPrefix(frame, "[") {
					out = bytes.TrimSuffix(bytes.TrimPrefix(out, []byte("[")), []byte("]"))
				}
				var got map[string]json.RawMessage
				if err := json.Unmarshal(out, &got); err != nil {
					t.Fatal(err)
				}
				if _, ok := got["result"]; ok {
					t.Fatalf("error has result: %s", out)
				}
				var rpcErr map[string]json.RawMessage
				if err := json.Unmarshal(got["error"], &rpcErr); err != nil {
					t.Fatal(err)
				}
				if string(rpcErr["message"]) != `"`+FixedError(tc.code).Message+`"` {
					t.Fatalf("unsafe message: %s", out)
				}
				if _, ok := rpcErr["data"]; ok != tc.wantData {
					t.Fatalf("data presence: %s", out)
				}
				if tc.wantData && string(rpcErr["data"]) != `{"address":"`+address+`","record_removed":true,"cleanup_complete":false}` {
					t.Fatalf("data: %s", out)
				}
				if bytes.Contains(out, []byte("private")) || bytes.Contains(out, []byte("secret")) {
					t.Fatalf("leak: %s", out)
				}
			}
		})
	}
}
