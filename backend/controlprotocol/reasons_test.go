package controlprotocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestUnavailableReason(t *testing.T) {
	for reason, want := range map[string]bool{
		"session_unavailable": true,
		"signer_unavailable":  true,
		"":                    false,
		"Signer_unavailable":  false,
		"not_zappable":        true,
		"wallet_unavailable":  false,
		"private":             false,
	} {
		if got := UnavailableReason(reason); got != want {
			t.Errorf("UnavailableReason(%q) = %v, want %v", reason, got, want)
		}
	}
}

func TestProcessFrameUnavailableReasons(t *testing.T) {
	frame := []byte(`{"jsonrpc":"2.0","method":"napplet.vote","id":1}`)
	for _, tc := range []struct {
		name string
		code int
		data any
		want string
	}{
		{"signer", Unavailable, SessionUnavailableData{Reason: "signer_unavailable"},
			`{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"signer_unavailable"}},"id":1}`},
		{"session", Unavailable, SessionUnavailableData{Reason: "session_unavailable"},
			`{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}},"id":1}`},
		{"unknown reason", Unavailable, SessionUnavailableData{Reason: "private"},
			`{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable"},"id":1}`},
		{"not zappable", Unavailable, SessionUnavailableData{Reason: "not_zappable"},
			`{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"not_zappable"}},"id":1}`},
		{"unreserved reason", Unavailable, SessionUnavailableData{Reason: "wallet_unavailable"},
			`{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable"},"id":1}`},
		{"extra key", Unavailable, map[string]string{"reason": "signer_unavailable", "extra": "secret"},
			`{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable"},"id":1}`},
		{"other code", NotFound, SessionUnavailableData{Reason: "signer_unavailable"},
			`{"jsonrpc":"2.0","error":{"code":1002,"message":"Not found"},"id":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := ProcessFrame(frame, func(string, json.RawMessage) (any, *Error) {
				return nil, &Error{Code: tc.code, Message: "private secret extra", Data: tc.data}
			})
			if string(out) != tc.want {
				t.Fatalf("frame\n got %s\nwant %s", out, tc.want)
			}
			for _, leak := range []string{"private", "secret", "extra"} {
				if bytes.Contains(out, []byte(leak)) {
					t.Fatalf("leak %q: %s", leak, out)
				}
			}
		})
	}
}
