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

// rpcInvalid calls method with each of params and requires a fixed Invalid
// params answer that echoes none of echoes.
func rpcInvalid(t *testing.T, method string, params []string, echoes ...string) {
	t.Helper()
	_, reader, conn, _ := rpcService(t)
	for _, p := range params {
		_, rpcErr, raw := rpcCall(t, reader, conn, method, p)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
			t.Fatalf("%s %s: %+v %s", method, p, rpcErr, raw)
		}
		for _, echoed := range echoes {
			if strings.Contains(raw, echoed) {
				t.Fatalf("%s %s echoed %q: %s", method, p, echoed, raw)
			}
		}
	}
}

func TestRPCMentionsStrictParams(t *testing.T) {
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	addr := `"address":"` + address + `"`
	id := strings.Repeat("a", 64)
	rpcInvalid(t, "napplet.mentions", []string{
		`{}`, `{"address":"bad"}`, `{"address":null}`, `{"address":1}`,
		`{` + addr + `,"extra":1}`, `{` + addr + `,` + addr + `}`,
		`{` + addr + `,"event_id":"` + id + `"}`, `{` + addr + `,"parent_id":"` + id + `"}`,
		`{` + addr + `,"refresh":true}`, `{` + addr + `,"categories":["mentions"]}`, `{` + addr + `,"content":"hi"}`,
		`{` + addr + `,"offset":-1}`, `{` + addr + `,"limit":0}`, `{` + addr + `,"limit":501}`, `{` + addr + `,"limit":null}`,
	}, "bad", "extra")
}

func TestRPCShareStrictParams(t *testing.T) {
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	addr := `"address":"` + address + `"`
	id := strings.Repeat("a", 64)
	rpcInvalid(t, "napplet.share", []string{
		`{}`, `{"address":"bad"}`, `{"address":null}`, `{"content":"hi"}`,
		`{` + addr + `,"extra":1}`, `{` + addr + `,` + addr + `}`,
		`{` + addr + `,"content":null}`, `{` + addr + `,"content":1}`, `{` + addr + `,"content":[]}`,
		`{` + addr + `,"vote":"up"}`, `{` + addr + `,"emoji":"x"}`,
		`{` + addr + `,"event_id":"` + id + `"}`, `{` + addr + `,"parent_id":"` + id + `"}`,
	}, "bad", "extra")
}

func TestRPCSocialAcceptsMentionsCategory(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.social", `{"address":"`+address+`","categories":["mentions"]}`)
	if rpcErr != nil || !strings.Contains(string(result), `"mentions":{"count":0}`) {
		t.Fatalf("mentions category: %s", raw)
	}
	rpcInvalid(t, "napplet.social", []string{
		`{"address":"` + address + `","categories":["tips"]}`,
		`{"address":"` + address + `","categories":["mentions","mentions"]}`,
	}, "tips")
}

func TestRPCShareSignerUnavailable(t *testing.T) {
	_, reader, conn, paths := rpcService(t)
	pk := nostr.Generate().Public().Hex()
	id := strings.Repeat("c", 64)
	want := `"error":{"code":1004,"message":"Unavailable","data":{"reason":"signer_unavailable"}}`
	for _, address := range []string{"35129:" + pk + ":app", "15129:" + pk + ":", "5129:" + pk + ":" + id} {
		addr := `"address":"` + address + `"`
		for _, params := range []string{`{` + addr + `}`, `{` + addr + `,"content":"hi"}`} {
			_, rpcErr, raw := rpcCall(t, reader, conn, "napplet.share", params)
			if rpcErr == nil || rpcErr.Code != controlprotocol.Unavailable || !strings.Contains(raw, want) {
				t.Fatalf("%s: %s", params, raw)
			}
			for _, leak := range []string{paths.DataDir, "/", "ws://", "wss://", "nsec", "bunker"} {
				if strings.Contains(raw, leak) {
					t.Fatalf("%s leaked %q: %s", params, leak, raw)
				}
			}
		}
	}
}

func TestRPCMentionsWireHygiene(t *testing.T) {
	_, reader, conn, paths := rpcService(t)
	author := nostr.Generate()
	pk := author.Public().Hex()
	address := "35129:" + pk + ":app"
	hint := "wss://hint.example"
	sign := func(tags nostr.Tags, content string, at nostr.Timestamp) nostr.Event {
		evt := nostr.Event{Kind: nostr.KindTextNote, CreatedAt: at, Tags: tags, Content: content}
		if err := evt.Sign(nostr.Generate()); err != nil {
			t.Fatal(err)
		}
		if err := backend.Sys().Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
		return evt
	}
	mention := sign(nostr.Tags{{"a", address, hint}, {"p", pk, hint}}, "hello ‮world", 10)
	quote := sign(nostr.Tags{{"q", address, hint}, {"p", pk, hint}}, "quoting https://example.com/x", 20)
	sign(nostr.Tags{{"a", "35129:" + pk + ":other", hint}}, "elsewhere", 30)

	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.mentions", `{"address":"`+address+`"}`)
	if rpcErr != nil {
		t.Fatalf("mentions: %+v", rpcErr)
	}
	var page struct {
		Items []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
			Quote   bool   `json:"quote"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].ID != quote.ID.Hex() || !page.Items[0].Quote ||
		page.Items[1].ID != mention.ID.Hex() || page.Items[1].Quote || page.Items[1].Content != "hello world" {
		t.Fatalf("page %s", raw)
	}
	if !strings.Contains(raw, `"total":2`) {
		t.Fatalf("no total: %s", raw)
	}
	// the note's own text may hold a url; nothing else on the line may
	bare := strings.ReplaceAll(raw, "https://example.com/x", "")
	for _, leak := range []string{"wss://", "http", paths.DataDir, `"sig"`, `"tags"`, "‮"} {
		if strings.Contains(bare, leak) {
			t.Fatalf("mentions leaked %q: %s", leak, raw)
		}
	}

	result, rpcErr, raw = rpcCall(t, reader, conn, "napplet.social", `{"address":"`+address+`"}`)
	if rpcErr != nil || !strings.Contains(string(result), `"mentions":{"count":2}`) {
		t.Fatalf("social: %s", raw)
	}

	unknown := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	result, rpcErr, _ = rpcCall(t, reader, conn, "napplet.mentions", `{"address":"`+unknown+`"}`)
	if rpcErr != nil || string(result) != `{"items":[],"total":0,"next_offset":null}` {
		t.Fatalf("unknown address: %s %+v", result, rpcErr)
	}
}
