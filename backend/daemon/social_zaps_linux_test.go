//go:build linux

package daemon

import (
	"strings"
	"testing"

	"fiatjaf.com/nostr"
	"kwakore/backend"
	"kwakore/backend/controlprotocol"
)

func TestMutationErrorNotZappable(t *testing.T) {
	want := `{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"not_zappable"}},"id":1}`
	if got := framed(mutationError(backend.ErrServiceNotZappable)); got != want {
		t.Fatalf("not zappable\n got %s\nwant %s", got, want)
	}
}

func TestRPCZapsStrictParams(t *testing.T) {
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	addr := `"address":"` + address + `"`
	id := strings.Repeat("a", 64)
	rpcInvalid(t, "napplet.zaps", []string{
		`{}`, `{"address":"bad"}`, `{"address":null}`, `{` + addr + `,"extra":1}`, `{` + addr + `,` + addr + `}`,
		`{` + addr + `,"event_id":"` + id + `"}`, `{` + addr + `,"parent_id":"` + id + `"}`,
		`{` + addr + `,"refresh":true}`, `{` + addr + `,"categories":["zaps"]}`,
		`{` + addr + `,"offset":-1}`, `{` + addr + `,"limit":0}`, `{` + addr + `,"limit":501}`, `{` + addr + `,"limit":null}`,
	}, "bad", "extra")
}

func TestRPCZapStrictParams(t *testing.T) {
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	addr := `"address":"` + address + `"`
	amount := `"amount_msats":21000`
	upper := strings.Repeat("A", 64)
	short := strings.Repeat("a", 63)
	params := []string{
		`{` + amount + `}`, `{"address":"bad",` + amount + `}`, `{"address":null,` + amount + `}`,
		`{` + addr + `,` + amount + `,"extra":1}`, `{` + addr + `,` + addr + `,` + amount + `}`,
		`{` + addr + `}`, `{` + addr + `,"amount_msats":null}`, `{` + addr + `,"amount_msats":"21"}`,
		`{` + addr + `,"amount_msats":1.5}`, `{` + addr + `,"amount_msats":1e3}`, `{` + addr + `,"amount_msats":0}`,
		`{` + addr + `,"amount_msats":-1}`, `{` + addr + `,"amount_msats":9223372036854775808}`,
		`{` + addr + `,` + amount + `,"comment":null}`, `{` + addr + `,` + amount + `,"comment":1}`,
		`{` + addr + `,` + amount + `,"event_id":null}`, `{` + addr + `,` + amount + `,"event_id":"` + upper + `"}`,
		`{` + addr + `,` + amount + `,"event_id":"` + short + `"}`,
		`{` + addr + `,` + amount + `,"vote":"up"}`, `{` + addr + `,` + amount + `,"content":"hi"}`,
	}
	rpcInvalid(t, "napplet.zap", params, "bad", "extra", upper, short, "9223372036854775808", "1e3")
}

func TestRPCZapSignerUnavailable(t *testing.T) {
	_, reader, conn, paths := rpcService(t)
	pk := nostr.Generate().Public().Hex()
	id := strings.Repeat("c", 64)
	want := `"error":{"code":1004,"message":"Unavailable","data":{"reason":"signer_unavailable"}}`
	for _, address := range []string{"35129:" + pk + ":app", "15129:" + pk + ":", "5129:" + pk + ":" + id} {
		_, rpcErr, raw := rpcCall(t, reader, conn, "napplet.zap", `{"address":"`+address+`","amount_msats":21000,"comment":"thanks"}`)
		if rpcErr == nil || rpcErr.Code != controlprotocol.Unavailable || !strings.Contains(raw, want) {
			t.Fatalf("%s: %s", address, raw)
		}
		for _, leak := range []string{paths.DataDir, "/", "ws://", "wss://", "https://", "lnurl", "nsec", "bunker"} {
			if strings.Contains(raw, leak) {
				t.Fatalf("%s leaked %q: %s", address, leak, raw)
			}
		}
	}
}

func TestRPCZapsEmptyPage(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.zaps", `{"address":"`+address+`"}`)
	if rpcErr != nil || string(result) != `{"items":[],"total":0,"total_msats":0,"next_offset":null}` {
		t.Fatalf("zaps page: %s", raw)
	}
	result, rpcErr, raw = rpcCall(t, reader, conn, "napplet.social", `{"address":"`+address+`","categories":["comments"]}`)
	if rpcErr != nil || !strings.Contains(string(result), `"zaps":{"count":0,"total_msats":0,"top":[]}`) {
		t.Fatalf("social: %s", raw)
	}
}
