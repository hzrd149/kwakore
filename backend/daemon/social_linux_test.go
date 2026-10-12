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

func TestRPCSocialStrictParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	address := "35129:" + strings.Repeat("a", 64) + ":unknown"
	addr := `"address":"` + address + `"`
	upper := strings.Repeat("A", 64)
	short := strings.Repeat("a", 63)
	nonHex := strings.Repeat("g", 64)
	common := []string{
		`{}`, `{"address":"bad"}`, `{"address":null}`, `{` + addr + `,"extra":1}`,
		`{` + addr + `,` + addr + `}`,
	}
	cases := map[string][]string{
		"napplet.social": append(cloneParams(common),
			`{`+addr+`,"refresh":null}`, `{`+addr+`,"refresh":"true"}`,
			`{`+addr+`,"categories":null}`, `{`+addr+`,"categories":"reactions"}`,
			`{`+addr+`,"categories":[]}`, `{`+addr+`,"categories":["zaps"]}`,
			`{`+addr+`,"categories":["reactions","reactions"]}`, `{`+addr+`,"categories":["reactions",null]}`,
			`{`+addr+`,"categories":[1]}`,
			`{`+addr+`,"offset":0}`, `{`+addr+`,"event_id":"`+strings.Repeat("a", 64)+`"}`,
		),
		"napplet.reactions": append(cloneParams(common),
			`{`+addr+`,"event_id":null}`, `{`+addr+`,"event_id":"`+upper+`"}`,
			`{`+addr+`,"event_id":"`+short+`"}`, `{`+addr+`,"event_id":"`+nonHex+`"}`,
			`{`+addr+`,"offset":-1}`, `{`+addr+`,"limit":0}`, `{`+addr+`,"limit":501}`, `{`+addr+`,"limit":null}`,
			`{`+addr+`,"categories":["reactions"]}`, `{`+addr+`,"parent_id":"`+strings.Repeat("a", 64)+`"}`,
			`{`+addr+`,"refresh":true}`,
		),
		"napplet.comments": append(cloneParams(common),
			`{`+addr+`,"parent_id":null}`, `{`+addr+`,"parent_id":"`+upper+`"}`,
			`{`+addr+`,"parent_id":"`+short+`"}`, `{`+addr+`,"parent_id":"`+nonHex+`"}`,
			`{`+addr+`,"offset":-1}`, `{`+addr+`,"limit":0}`, `{`+addr+`,"limit":501}`, `{`+addr+`,"limit":null}`,
			`{`+addr+`,"event_id":"`+strings.Repeat("a", 64)+`"}`, `{`+addr+`,"categories":["comments"]}`,
		),
	}
	for method, params := range cases {
		for _, p := range params {
			_, rpcErr, raw := rpcCall(t, reader, conn, method, p)
			if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams {
				t.Fatalf("%s %s: %+v %s", method, p, rpcErr, raw)
			}
			for _, echoed := range []string{"bad", "zaps", upper, short, nonHex, "extra"} {
				if strings.Contains(raw, echoed) {
					t.Fatalf("%s %s echoed %q: %s", method, p, echoed, raw)
				}
			}
		}
	}
}

func cloneParams(in []string) []string { return append([]string(nil), in...) }

func TestRPCSocialUnknownAddress(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	address := "35129:" + nostr.Generate().Public().Hex() + ":unknown"
	result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.social", `{"address":"`+address+`","refresh":false,"categories":["comments"]}`)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	var summary map[string]json.RawMessage
	if err := json.Unmarshal(result, &summary); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"address":  `"` + address + `"`,
		"sync":     `{"comments":{"status":"not_requested","synced_at":null},"mentions":{"status":"not_requested","synced_at":null},"reactions":{"status":"not_requested","synced_at":null}}`,
		"votes":    `{"up":0,"down":0,"score":0}`,
		"emoji":    `[]`,
		"by_event": `[]`,
		"comments": `{"count":0,"latest_at":null}`,
		"mentions": `{"count":0}`,
		"own":      `null`,
	}
	if len(summary) != len(want) {
		t.Fatalf("summary fields %s", raw)
	}
	for field, value := range want {
		if string(summary[field]) != value {
			t.Errorf("%s = %s, want %s", field, summary[field], value)
		}
	}
	for _, method := range []string{"napplet.reactions", "napplet.comments"} {
		result, rpcErr, _ := rpcCall(t, reader, conn, method, `{"address":"`+address+`"}`)
		if rpcErr != nil || string(result) != `{"items":[],"total":0,"next_offset":null}` {
			t.Fatalf("%s: %s %+v", method, result, rpcErr)
		}
	}
}

func TestRPCSocialWireHygiene(t *testing.T) {
	_, reader, conn, paths := rpcService(t)
	author := nostr.Generate()
	pk := author.Public().Hex()
	address := "35129:" + pk + ":app"
	hint := "wss://hint.example"
	sign := func(kind nostr.Kind, tags nostr.Tags, content string, at nostr.Timestamp) nostr.Event {
		evt := nostr.Event{Kind: kind, CreatedAt: at, Tags: tags, Content: content}
		if err := evt.Sign(nostr.Generate()); err != nil {
			t.Fatal(err)
		}
		if err := backend.Sys().Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
		return evt
	}
	sign(nostr.KindReaction, nostr.Tags{{"a", address, hint}, {"p", pk, hint}, {"emoji", "pepe", "https://cdn.example/pepe.png"}}, ":pepe:", 10)
	sign(nostr.KindReaction, nostr.Tags{{"a", address, hint}, {"emoji", "bad", "http://insecure.example/x.png"}}, ":bad:", 11)
	sign(nostr.KindReaction, nostr.Tags{{"a", address, hint}, {"e", strings.Repeat("c", 64), hint}, {"p", pk, hint}}, "+", 12)
	root := sign(nostr.KindComment, nostr.Tags{{"A", address, hint}, {"P", pk, hint}, {"a", address, hint}, {"p", pk, hint}}, "hello ‮world", 13)
	sign(nostr.KindComment, nostr.Tags{{"A", address, hint}, {"P", pk, hint}, {"e", root.ID.Hex(), hint}, {"p", root.PubKey.Hex(), hint}}, "reply", 14)

	calls := []struct{ method, params string }{
		{"napplet.social", `{"address":"` + address + `"}`},
		{"napplet.reactions", `{"address":"` + address + `"}`},
		{"napplet.comments", `{"address":"` + address + `"}`},
		{"napplet.comments", `{"address":"` + address + `","parent_id":"` + root.ID.Hex() + `"}`},
	}
	var seen strings.Builder
	for _, call := range calls {
		_, rpcErr, raw := rpcCall(t, reader, conn, call.method, call.params)
		if rpcErr != nil {
			t.Fatalf("%s: %+v", call.method, rpcErr)
		}
		seen.WriteString(raw)
		bare := strings.ReplaceAll(raw, "https://cdn.example/pepe.png", "")
		for _, leak := range []string{"wss://", "http://", "https://", paths.DataDir, `"sig"`, `"tags"`, "‮", `‮`} {
			if strings.Contains(bare, leak) {
				t.Fatalf("%s %s leaked %q: %s", call.method, call.params, leak, raw)
			}
		}
	}
	// the results still carry the data: the sanitized emoji url, the counts
	// and the reply
	for _, want := range []string{"https://cdn.example/pepe.png", `"shortcode":"bad"`, `"reply_count":1`, `"content":"reply"`, `"up":1`} {
		if !strings.Contains(seen.String(), want) {
			t.Errorf("results lack %s", want)
		}
	}
}
