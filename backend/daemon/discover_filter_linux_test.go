//go:build linux

package daemon

import (
	"strings"
	"testing"

	"kwakore/backend/controlprotocol"
)

func TestRPCDiscoverFilterParams(t *testing.T) {
	_, reader, conn, _ := rpcService(t)
	valid := `{"archetype":"feed","intent":"napplet:feed/edit","domain":"relay","compatible":true,"kind":35129}`
	if result, rpcErr, raw := rpcCall(t, reader, conn, "napplet.discover", valid); rpcErr != nil || len(result) == 0 {
		t.Fatalf("valid filters: %+v %s", rpcErr, raw)
	}
	for _, kind := range []string{"5129", "15129", "35130"} {
		if _, rpcErr, raw := rpcCall(t, reader, conn, "napplet.discover", `{"kind":`+kind+`,"compatible":false}`); rpcErr != nil {
			t.Fatalf("kind %s: %+v %s", kind, rpcErr, raw)
		}
	}
	for _, params := range []string{
		`{"archetype":"Feed"}`,
		`{"archetype":""}`,
		`{"archetype":"` + strings.Repeat("a", 65) + `"}`,
		`{"archetype":5}`,
		`{"intent":""}`,
		`{"intent":"` + strings.Repeat("a", 257) + `"}`,
		`{"intent":"napplet:feed/\u0007open"}`,
		`{"intent":null}`,
		`{"domain":"NAP-RELAY"}`,
		`{"domain":"relay.example"}`,
		`{"compatible":"yes"}`,
		`{"compatible":null}`,
		`{"compatible":1}`,
		`{"kind":1}`,
		`{"kind":30023}`,
		`{"kind":1.5}`,
		`{"kind":5129.0}`,
		`{"kind":5.129e3}`,
		`{"kind":"5129"}`,
		`{"kind":null}`,
		`{"kind":-5129}`,
		`{"filter":"feed"}`,
	} {
		_, rpcErr, raw := rpcCall(t, reader, conn, "napplet.discover", params)
		if rpcErr == nil || rpcErr.Code != controlprotocol.InvalidParams || strings.Contains(raw, "Feed") || strings.Contains(raw, "NAP-RELAY") {
			t.Fatalf("%s: %+v %s", params, rpcErr, raw)
		}
	}
}
