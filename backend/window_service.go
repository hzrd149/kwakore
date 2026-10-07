package backend

import (
	"context"
	"errors"
	"strconv"
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
	ci, err := launch(ctx, installed)
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
	id, err := strconv.ParseUint(windowID, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != windowID {
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
