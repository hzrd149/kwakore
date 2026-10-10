//go:build !linux

package gamepad

import "context"

func run(ctx context.Context, emit func(Snapshot)) {
	if ctx.Err() == nil {
		emit(Snapshot{Reason: "unavailable", Pads: []*Pad{}})
	}
}
