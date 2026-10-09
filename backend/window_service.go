package backend

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
)

var ErrServiceSessionUnavailable = errors.New("graphical session unavailable")

type ServiceLaunchResult struct {
	Address  string `json:"address"`
	WindowID string `json:"window_id"`
	Outcome  string `json:"outcome"`
}

type ServiceStopResult struct {
	WindowID string `json:"window_id"`
	Closed   bool   `json:"closed"`
}

type ServiceWindow struct {
	WindowID string `json:"window_id"`
	Address  string `json:"address"`
	Name     string `json:"name"`
}

// ServiceWindows lists windows addressable by the public stop operation.
func ServiceWindows() []ServiceWindow {
	open := allInstances()
	out := make([]ServiceWindow, 0, len(open))
	for _, ci := range open {
		id := ci.ID()
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != id || !ci.napp.IsNapplet() {
			continue
		}
		out = append(out, ServiceWindow{WindowID: id, Address: ci.napp.Address(), Name: ci.napp.Label()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WindowID < out[j].WindowID })
	return out
}

// ServiceLaunch resolves only an installed canonical address. The host's
// context-aware open returns once its own child reports host-page readiness.
func ServiceLaunch(ctx context.Context, address string) (ServiceLaunchResult, error) {
	if _, err := ParseCanonicalServiceAddress(address); err != nil {
		return ServiceLaunchResult{}, err
	}
	if ctx.Err() != nil {
		return ServiceLaunchResult{}, ErrServiceTimeout
	}
	stateMu.Lock()
	var installed Napp
	for _, n := range state.InstalledNapps {
		if n.Address() == address && n.IsNapplet() {
			installed = n
			break
		}
	}
	stateMu.Unlock()
	if installed.ID == "" {
		return ServiceLaunchResult{}, ErrServiceNotFound
	}
	// A process-local serial can be reused after a daemon restart. Give
	// service windows a fresh opaque ID so a stale client cannot stop a
	// different window opened by the next daemon process.
	ci, err := launchWithInstance(ctx, installed, randomID())
	if err != nil {
		if errors.Is(err, ErrServiceSessionUnavailable) {
			return ServiceLaunchResult{}, err
		}
		if ctx.Err() != nil {
			return ServiceLaunchResult{}, ErrServiceTimeout
		}
		return ServiceLaunchResult{}, ErrServiceUnavailable
	}
	return ServiceLaunchResult{Address: address, WindowID: ci.ID(), Outcome: "opened"}, nil
}

// ServiceStop confirms WindowClosed for precisely the instance selected here.
func ServiceStop(ctx context.Context, windowID string) (ServiceStopResult, error) {
	decoded, err := hex.DecodeString(windowID)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != windowID {
		return ServiceStopResult{}, ErrServiceInvalidAddress
	}
	if ctx.Err() != nil {
		return ServiceStopResult{}, ErrServiceTimeout
	}
	ci := lookupInstance(windowID)
	if ci == nil {
		return ServiceStopResult{}, ErrServiceNotFound
	}
	ci.Close()
	select {
	case <-ci.gone:
		return ServiceStopResult{WindowID: windowID, Closed: true}, nil
	case <-ctx.Done():
		return ServiceStopResult{}, ErrServiceTimeout
	}
}
