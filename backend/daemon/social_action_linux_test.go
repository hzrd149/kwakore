//go:build linux

package daemon

import (
	"encoding/json"
	"testing"

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
