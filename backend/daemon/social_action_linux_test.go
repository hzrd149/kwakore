//go:build linux

package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

// framed is err's wire form through ProcessFrame.
func framed(err *controlprotocol.Error) string {
	return string(controlprotocol.ProcessFrame([]byte(`{"jsonrpc":"2.0","method":"napplet.vote","id":1}`), func(string, json.RawMessage) (any, *controlprotocol.Error) {
		return nil, err
	}))
}

func TestMutationErrorSignerUnavailable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{backend.ErrServiceSignerUnavailable, `{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"signer_unavailable"}},"id":1}`},
		{backend.ErrServiceUnavailable, `{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable"},"id":1}`},
		{backend.ErrServiceSessionUnavailable, `{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}},"id":1}`},
	} {
		if got := framed(mutationError(tc.err)); got != tc.want {
			t.Errorf("%v\n got %s\nwant %s", tc.err, got, tc.want)
		}
	}
}

func TestRPCSocialActionStrictParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	// a valid address, so each case fails on the field under test
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	addr := `"address":"` + address + `"`
	id := strings.Repeat("a", 64)
	upper := strings.Repeat("A", 64)
	short := strings.Repeat("a", 63)
	common := func(required string) []string {
		return []string{
			`{` + required + `}`, `{"address":"bad",` + required + `}`, `{"address":null,` + required + `}`,
			`{` + addr + `,` + required + `,"extra":1}`, `{` + addr + `,` + addr + `,` + required + `}`,
		}
	}
	vote := `"vote":"up"`
	cases := map[string][]string{
		"napplet.vote": append(common(vote),
			`{`+addr+`}`, `{`+addr+`,"vote":null}`, `{`+addr+`,"vote":1}`, `{`+addr+`,"vote":"maybe"}`, `{`+addr+`,"vote":"UP"}`,
			`{`+addr+`,`+vote+`,"event_id":null}`, `{`+addr+`,`+vote+`,"event_id":"`+upper+`"}`,
			`{`+addr+`,`+vote+`,"event_id":"`+short+`"}`,
			`{`+addr+`,`+vote+`,"parent_id":"`+id+`"}`, `{`+addr+`,`+vote+`,"emoji":"x"}`,
		),
		"napplet.react": append(common(`"emoji":"x"`),
			`{`+addr+`}`, `{`+addr+`,"emoji":null}`, `{`+addr+`,"emoji":""}`, `{`+addr+`,"emoji":1}`,
			`{`+addr+`,"emoji":"x","emoji_url":null}`, `{`+addr+`,"emoji":"x","emoji_url":1}`,
			`{`+addr+`,"emoji":"x","event_id":"`+short+`"}`,
			`{`+addr+`,"emoji":"x","vote":"up"}`, `{`+addr+`,"emoji":"x","content":"hi"}`,
		),
		"napplet.comment": append(common(`"content":"hi"`),
			`{`+addr+`}`, `{`+addr+`,"content":null}`, `{`+addr+`,"content":1}`,
			`{`+addr+`,"content":"hi","parent_id":null}`, `{`+addr+`,"content":"hi","parent_id":"`+upper+`"}`,
			`{`+addr+`,"content":"hi","event_id":"`+id+`"}`,
		),
		"napplet.delete": append(common(`"event_id":"`+id+`"`),
			`{`+addr+`}`, `{`+addr+`,"event_id":null}`, `{`+addr+`,"event_id":"`+short+`"}`,
			`{`+addr+`,"event_id":"`+upper+`"}`, `{`+addr+`,"event_id":"`+id+`","vote":"up"}`,
		),
	}
	for method, params := range cases {
		for _, p := range params {
			_, rpcErr, raw := rpcCall(t, reader, conn, method, p)
			if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
				t.Fatalf("%s %s: %+v %s", method, p, rpcErr, raw)
			}
			for _, echoed := range []string{"bad", "maybe", "UP", upper, short, "extra"} {
				if strings.Contains(raw, echoed) {
					t.Fatalf("%s %s echoed %q: %s", method, p, echoed, raw)
				}
			}
		}
	}
}

func TestRPCSocialActionSignerUnavailable(t *testing.T) {
	_, reader, conn, paths := rpcService(t)
	pk := nostr.Generate().Public().Hex()
	id := strings.Repeat("c", 64)
	want := `"error":{"code":1004,"message":"Unavailable","data":{"reason":"signer_unavailable"}}`
	for _, address := range []string{"35129:" + pk + ":app", "15129:" + pk + ":", "5129:" + pk + ":" + id} {
		addr := `"address":"` + address + `"`
		for _, call := range []struct{ method, params string }{
			{"napplet.vote", `{` + addr + `,"vote":"up"}`},
			{"napplet.vote", `{` + addr + `,"vote":"none"}`},
			{"napplet.react", `{` + addr + `,"emoji":"🔥"}`},
			{"napplet.comment", `{` + addr + `,"content":"hi"}`},
			{"napplet.delete", `{` + addr + `,"event_id":"` + id + `"}`},
		} {
			_, rpcErr, raw := rpcCall(t, reader, conn, call.method, call.params)
			if rpcErr == nil || rpcErr.Code != controlprotocol.Unavailable || !strings.Contains(raw, want) {
				t.Fatalf("%s %s: %s", call.method, call.params, raw)
			}
			for _, leak := range []string{paths.DataDir, "/", "ws://", "wss://", "nsec", "bunker"} {
				if strings.Contains(raw, leak) {
					t.Fatalf("%s leaked %q: %s", call.method, leak, raw)
				}
			}
		}
	}
}
