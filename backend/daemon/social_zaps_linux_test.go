//go:build linux

package daemon

import (
	"testing"

	"kwakore/backend"
)

func TestMutationErrorNotZappable(t *testing.T) {
	want := `{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"not_zappable"}},"id":1}`
	if got := framed(mutationError(backend.ErrServiceNotZappable)); got != want {
		t.Fatalf("not zappable\n got %s\nwant %s", got, want)
	}
}
