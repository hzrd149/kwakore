// Package gamepad supplies process-independent Linux controller snapshots.
package gamepad

import "context"

type Button struct {
	Value   float64 `json:"value"`
	Pressed bool    `json:"pressed"`
	Touched bool    `json:"touched"`
}
type Pad struct {
	Index     int       `json:"index"`
	ID        string    `json:"id"`
	Mapping   string    `json:"mapping"`
	Connected bool      `json:"connected"`
	Timestamp float64   `json:"timestamp"`
	Axes      []float64 `json:"axes"`
	Buttons   []Button  `json:"buttons"`
}
type Snapshot struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Pads      []*Pad `json:"pads"`
}

// Run owns the native monitor until cancellation. Callbacks receive detached
// snapshots and run serially. Only one monitor is active in this process.
func Run(ctx context.Context, emit func(Snapshot)) { run(ctx, emit) }
