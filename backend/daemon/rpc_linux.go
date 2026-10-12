//go:build linux

package daemon

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"kwakore/backend"
	"kwakore/backend/controlprotocol"
	"kwakore/backend/napaddr"
	"kwakore/backend/serviceconfig"
)

// serviceUninstall is replaceable by package tests to force a cleanup failure
// after record removal through the real socket path.
var serviceUninstall = backend.ServiceUninstall

// dispatchRPC is the allow-listed service boundary. Configuration persistence
// and change notifications remain owned by Service and serviceconfig.Manager.
func (s *Service) dispatchRPC(method string, params json.RawMessage) (any, *controlprotocol.Error) {
	return s.dispatchRPCContext(context.Background(), method, params)
}

func (s *Service) dispatchRPCContext(ctx context.Context, method string, params json.RawMessage) (any, *controlprotocol.Error) {
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return nil, controlprotocol.FixedError(controlprotocol.Closing)
	}
	switch method {
	case "events.subscribe":
		// the subscription is the connection's: the handler streams it once
		// this frame's response is written (events_linux.go)
		return s.subscribeEvents(ctx, params)
	case "signer.status":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		return s.signer.Status(), nil
	case "signer.switch":
		mode, secret, err := decodeSignerSwitch(params)
		if err != nil {
			return nil, err
		}
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		workCtx, timeoutCancel := context.WithTimeout(workCtx, 20*time.Second)
		defer timeoutCancel()
		status, switchErr := s.SwitchSigner(workCtx, mode, secret)
		if switchErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Unavailable)
		}
		return status, nil
	case "signer.pair.start":
		secret, err := decodePairStart(params)
		if err != nil {
			return nil, err
		}
		start, startErr := s.StartSignerPair(secret)
		if startErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Unavailable)
		}
		return start, nil
	case "signer.pair.wait":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		workCtx, timeoutCancel := context.WithTimeout(workCtx, 125*time.Second)
		defer timeoutCancel()
		status, waitErr := s.WaitSignerPair(workCtx)
		if waitErr != nil {
			if errors.Is(waitErr, context.DeadlineExceeded) {
				return nil, controlprotocol.FixedError(controlprotocol.Timeout)
			}
			return nil, controlprotocol.FixedError(controlprotocol.Unavailable)
		}
		return status, nil
	case "signer.pair.cancel":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		cancelled, cancelErr := s.CancelSignerPair()
		if cancelErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		return struct {
			Cancelled bool `json:"cancelled"`
		}{cancelled}, nil
	case "service.status":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		return struct {
			ProtocolVersion int    `json:"protocol_version"`
			Health          Health `json:"health"`
		}{controlprotocol.Version, s.Health()}, nil
	case "service.diagnostics":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		return s.Diagnostics(), nil
	case "settings.get":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		return s.Manager().Effective(), nil
	case "settings.inspect":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		settings, sources := s.Manager().Inspect()
		return struct {
			Settings serviceconfig.Effective `json:"settings"`
			Sources  map[string]string       `json:"sources"`
		}{settings, sources}, nil
	case "settings.reload":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		if err := s.Reload(); err != nil {
			return nil, settingsError(s, err)
		}
		return settingsResult{Settings: s.Manager().Effective()}, nil
	case "settings.set":
		field, value, err := decodeSettingParams(params)
		if err != nil {
			return nil, err
		}
		if e := s.SetSetting(field, value); e != nil {
			return nil, settingsError(s, e)
		}
		return settingsResult{Settings: s.Manager().Effective()}, nil
	case "settings.clear":
		field, err := decodeFieldParams(params)
		if err != nil {
			return nil, err
		}
		if e := s.ClearSetting(field); e != nil {
			return nil, settingsError(s, e)
		}
		return settingsResult{Settings: s.Manager().Effective()}, nil
	case "napplet.installed":
		offset, limit, err := decodePageParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		return backend.ServiceInstalled(offset, limit), nil
	case "napplet.updates.check":
		address, err := decodeOptionalAddressParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		workCtx, cancelTimeout := context.WithTimeout(workCtx, 75*time.Second)
		defer cancelTimeout()
		result, checkErr := backend.ServiceCheckUpdates(workCtx, address)
		if checkErr != nil {
			return nil, mutationError(checkErr)
		}
		return result, nil
	case "napplet.versions", "napplet.snapshots":
		address, refresh, offset, limit, err := decodeHistoryParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		result, historyErr := backend.ServiceHistory(workCtx, address, method == "napplet.snapshots", refresh, offset, limit)
		if historyErr != nil {
			return nil, mutationError(historyErr)
		}
		return result, nil
	case "napplet.detail":
		address, err := decodeAddressParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		result, detailErr := backend.ServiceDetail(address)
		if detailErr != nil {
			return nil, mutationError(detailErr)
		}
		return result, nil
	case "napplet.icon":
		address, err := decodeAddressParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		workCtx, cancelTimeout := context.WithTimeout(workCtx, 15*time.Second)
		defer cancelTimeout()
		result, iconErr := backend.ServiceIcon(workCtx, address)
		if iconErr != nil {
			return nil, mutationError(iconErr)
		}
		return result, nil
	case "napplet.windows":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		return struct {
			Items []backend.ServiceWindow `json:"items"`
		}{backend.ServiceWindows()}, nil
	case "napplet.permissions.get":
		address, err := decodeAddressParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		result, getErr := backend.ServicePermissionsGet(workCtx, address)
		if getErr != nil {
			return nil, mutationError(getErr)
		}
		return result, nil
	case "napplet.permissions.set":
		fallthrough
	case "napplet.permissions.clear":
		address, perm, subject, decision, err := decodePermissionParams(params, method == "napplet.permissions.set")
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		var result backend.ServicePermissionResult
		var mutationErr error
		if method == "napplet.permissions.set" {
			result, mutationErr = backend.ServicePermissionSet(workCtx, address, perm, subject, decision)
		} else {
			result, mutationErr = backend.ServicePermissionClear(workCtx, address, perm, subject)
		}
		if mutationErr != nil {
			return nil, mutationError(mutationErr)
		}
		return result, nil
	case "napplet.launch":
		address, err := decodeAddressParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancelWork := s.registryContext(ctx)
		defer cancelWork()
		workCtx, cancelDeadline := context.WithTimeout(workCtx, 12*time.Second)
		defer cancelDeadline()
		s.operationMu.Lock()
		defer s.operationMu.Unlock()
		result, launchErr := backend.ServiceLaunch(workCtx, address)
		if launchErr != nil {
			return nil, mutationError(launchErr)
		}
		return result, nil
	case "napplet.try":
		address, relays, err := decodeInstallParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		// the downloads (bounded at 120 seconds) run outside the operation
		// lock; ServiceTry takes it only to open the window
		workCtx, cancelWork := s.registryContext(ctx)
		defer cancelWork()
		workCtx, cancelDeadline := context.WithTimeout(workCtx, 150*time.Second)
		defer cancelDeadline()
		result, tryErr := backend.ServiceTry(workCtx, address, relays, &s.operationMu)
		if tryErr != nil {
			return nil, mutationError(tryErr)
		}
		return result, nil
	case "napplet.stop":
		windowID, err := decodeWindowIDParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancelWork := s.registryContext(ctx)
		defer cancelWork()
		workCtx, cancelDeadline := context.WithTimeout(workCtx, 12*time.Second)
		defer cancelDeadline()
		result, stopErr := backend.ServiceStop(workCtx, windowID)
		if stopErr != nil {
			return nil, mutationError(stopErr)
		}
		return result, nil
	case "napplet.discover":
		filter, refresh, offset, limit, err := decodeDiscoveryParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		result, discoverErr := backend.ServiceDiscover(workCtx, filter, refresh, offset, limit)
		if discoverErr != nil {
			switch {
			case errors.Is(discoverErr, backend.ErrDiscoveryUnavailable):
				return nil, controlprotocol.FixedError(controlprotocol.Unavailable)
			case errors.Is(discoverErr, backend.ErrDiscoveryTimeout):
				return nil, controlprotocol.FixedError(controlprotocol.Timeout)
			default:
				return nil, controlprotocol.FixedError(controlprotocol.Conflict)
			}
		}
		return result, nil
	case "napplet.install":
		address, relays, err := decodeInstallParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		result, installErr := backend.ServiceInstall(workCtx, address, relays)
		if installErr != nil {
			return nil, mutationError(installErr)
		}
		return result, nil
	case "napplet.update":
		address, err := decodeAddressParams(params)
		if err != nil {
			return nil, err
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		result, updateErr := backend.ServiceUpdate(workCtx, address)
		if updateErr != nil {
			return nil, mutationError(updateErr)
		}
		return result, nil
	case "napplet.uninstall":
		address, confirmed, err := decodeUninstallParams(params)
		if err != nil {
			return nil, err
		}
		if !confirmed {
			return nil, controlprotocol.FixedError(controlprotocol.ConfirmationRequired)
		}
		done, beginErr := s.Begin()
		if beginErr != nil {
			return nil, controlprotocol.FixedError(controlprotocol.Closing)
		}
		defer done()
		workCtx, cancel := s.registryContext(ctx)
		defer cancel()
		result, uninstallErr := serviceUninstall(workCtx, address)
		if errors.Is(uninstallErr, backend.ErrServicePartialCleanup) {
			rpcErr := controlprotocol.FixedError(controlprotocol.PartialCleanup)
			rpcErr.Data = controlprotocol.PartialCleanupData{Address: result.Address, RecordRemoved: result.RecordRemoved, CleanupComplete: result.CleanupComplete}
			return nil, rpcErr
		}
		if uninstallErr != nil {
			return nil, mutationError(uninstallErr)
		}
		return result, nil
	default:
		return nil, controlprotocol.FixedError(controlprotocol.MethodNotFound)
	}
}

func decodePairStart(params json.RawMessage) (string, *controlprotocol.Error) {
	invalid := controlprotocol.FixedError(controlprotocol.InvalidParams)
	if err := controlprotocol.ValidateNamedParams(params, "secret"); err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil || len(fields) != 1 || len(fields["secret"]) > 80 {
		return "", invalid
	}
	var secret string
	if json.Unmarshal(fields["secret"], &secret) != nil || len(secret) != 32 {
		return "", invalid
	}
	b, err := hex.DecodeString(secret)
	if err != nil || len(b) != 16 || hex.EncodeToString(b) != secret {
		return "", invalid
	}
	return secret, nil
}

func decodeSignerSwitch(params json.RawMessage) (string, string, *controlprotocol.Error) {
	invalid := controlprotocol.FixedError(controlprotocol.InvalidParams)
	if err := controlprotocol.ValidateNamedParams(params, "mode", "secret", "socket"); err != nil {
		return "", "", err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", "", invalid
	}
	var mode string
	if json.Unmarshal(fields["mode"], &mode) != nil {
		return "", "", invalid
	}
	if mode == "none" && len(fields) == 1 {
		return mode, "", nil
	}
	if mode == "system" {
		// The socket travels in the secret slot; it is not a secret.
		if err := controlprotocol.ValidateNamedParams(params, "mode", "socket"); err != nil {
			return "", "", err
		}
		if len(fields) == 1 {
			return mode, "", nil
		}
		var socket string
		if len(fields) != 2 || len(fields["socket"]) > 512 || json.Unmarshal(fields["socket"], &socket) != nil || !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || len(socket) > 107 {
			return "", "", invalid
		}
		return mode, socket, nil
	}
	if (mode != "nsec" && mode != "bunker") || len(fields) != 2 || len(fields["secret"]) > 4096 {
		return "", "", invalid
	}
	var secret string
	if json.Unmarshal(fields["secret"], &secret) != nil || secret == "" || len(secret) > 2048 || mode == "nsec" && len(secret) > 256 {
		return "", "", invalid
	}
	return mode, secret, nil
}

func decodeUninstallParams(params json.RawMessage) (string, bool, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "address", "confirm"); err != nil {
		return "", false, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", false, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	address, err := decodeAddressParams(json.RawMessage(`{"address":` + string(fields["address"]) + `}`))
	if err != nil {
		return "", false, err
	}
	return address, bytes.Equal(fields["confirm"], []byte("true")), nil
}

func decodeAddressParams(params json.RawMessage) (string, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "address"); err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	return addressField(fields)
}

// decodeOptionalAddressParams reads {address?}: an absent address is "",
// a present one must be a canonical address string.
func decodeOptionalAddressParams(params json.RawMessage) (string, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "address"); err != nil {
		return "", err
	}
	if len(params) == 0 {
		return "", nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	if _, ok := fields["address"]; !ok {
		return "", nil
	}
	return addressField(fields)
}

// decodeInstallParams reads napplet.install's params: the canonical address
// and optional relay hints, the usable ones an naddr carried. A client may
// send anything, so the hints are checked again here: at most
// napaddr.MaxRelayHints distinct ws(s) URLs with a host and no credentials,
// query or fragment. Whether a hint's host is public is decided when the
// relays are asked. Absent or empty relays is nil.
func decodeInstallParams(params json.RawMessage) (string, []string, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "address", "relays"); err != nil {
		return "", nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	address, err := addressField(fields)
	if err != nil {
		return "", nil, err
	}
	raw, ok := fields["relays"]
	if !ok {
		return address, nil, nil
	}
	var relays []string
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &relays) != nil || len(relays) > napaddr.MaxRelayHints {
		return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	for i, r := range relays {
		if !napaddr.ValidRelayHint(r) || slices.Contains(relays[:i], r) {
			return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
	}
	if len(relays) == 0 {
		relays = nil
	}
	return address, relays, nil
}

// addressField is the canonical address in decoded params, shared by every
// method that takes one so the checks cannot drift apart.
func addressField(fields map[string]json.RawMessage) (string, *controlprotocol.Error) {
	var address string
	if raw, ok := fields["address"]; !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &address) != nil {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	if _, err := backend.ParseCanonicalServiceAddress(address); err != nil {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	return address, nil
}

func decodePermissionParams(params json.RawMessage, set bool) (string, backend.Permission, string, backend.Decision, *controlprotocol.Error) {
	allowed := []string{"address", "permission", "subject"}
	if set {
		allowed = append(allowed, "decision")
	}
	invalid := controlprotocol.FixedError(controlprotocol.InvalidParams)
	if err := controlprotocol.ValidateNamedParams(params, allowed...); err != nil {
		return "", "", "", "", err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", "", "", "", invalid
	}
	address, err := decodeAddressParams(json.RawMessage(`{"address":` + string(fields["address"]) + `}`))
	if err != nil {
		return "", "", "", "", err
	}
	var permission string
	if raw, ok := fields["permission"]; !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &permission) != nil {
		return "", "", "", "", invalid
	}
	var subject string
	if raw, ok := fields["subject"]; ok && (bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &subject) != nil) {
		return "", "", "", "", invalid
	}
	var decision string
	if set {
		if raw, ok := fields["decision"]; !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &decision) != nil || (decision != "allow" && decision != "deny") {
			return "", "", "", "", invalid
		}
	}
	if !backend.ValidServicePermission(backend.Permission(permission), subject) {
		return "", "", "", "", invalid
	}
	return address, backend.Permission(permission), subject, backend.Decision(decision), nil
}

func decodeWindowIDParams(params json.RawMessage) (string, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "window_id"); err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	var id string
	if json.Unmarshal(fields["window_id"], &id) != nil || id == "" {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != id {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	return id, nil
}

func mutationError(err error) *controlprotocol.Error {
	switch {
	case errors.Is(err, backend.ErrServiceSessionUnavailable):
		rpcErr := controlprotocol.FixedError(controlprotocol.Unavailable)
		rpcErr.Data = controlprotocol.SessionUnavailableData{Reason: "session_unavailable"}
		return rpcErr
	case errors.Is(err, backend.ErrServiceInvalidAddress):
		return controlprotocol.FixedError(controlprotocol.InvalidParams)
	case errors.Is(err, backend.ErrServiceInvalidPermission):
		return controlprotocol.FixedError(controlprotocol.InvalidParams)
	case errors.Is(err, backend.ErrServiceNotFound):
		return controlprotocol.FixedError(controlprotocol.NotFound)
	case errors.Is(err, backend.ErrServiceBusy):
		return controlprotocol.FixedError(controlprotocol.Busy)
	case errors.Is(err, backend.ErrServiceNoUpdate):
		return controlprotocol.FixedError(controlprotocol.NoUpdate)
	case errors.Is(err, backend.ErrServiceTimeout):
		return controlprotocol.FixedError(controlprotocol.Timeout)
	case errors.Is(err, backend.ErrServiceConflict):
		return controlprotocol.FixedError(controlprotocol.Conflict)
	default:
		return controlprotocol.FixedError(controlprotocol.Unavailable)
	}
}

func decodePageParams(params json.RawMessage) (int, int, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "offset", "limit"); err != nil {
		return 0, 0, err
	}
	if len(params) == 0 {
		return 0, 100, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	return decodePageFields(fields)
}

func decodeHistoryParams(params json.RawMessage) (string, bool, int, int, *controlprotocol.Error) {
	invalid := controlprotocol.FixedError(controlprotocol.InvalidParams)
	if err := controlprotocol.ValidateNamedParams(params, "address", "refresh", "offset", "limit"); err != nil {
		return "", false, 0, 0, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", false, 0, 0, invalid
	}
	address, err := addressField(fields)
	if err != nil {
		return "", false, 0, 0, err
	}
	refresh := false
	if raw, ok := fields["refresh"]; ok && (bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &refresh) != nil) {
		return "", false, 0, 0, invalid
	}
	offset, limit, err := decodePageFields(fields)
	return address, refresh, offset, limit, err
}

func decodePageFields(fields map[string]json.RawMessage) (int, int, *controlprotocol.Error) {
	offset, limit := 0, 100
	if raw, ok := fields["offset"]; ok {
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &offset) != nil || offset < 0 {
			return 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
	}
	if raw, ok := fields["limit"]; ok {
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &limit) != nil || limit < 1 || limit > 500 {
			return 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
	}
	return offset, limit, nil
}

// discoverToken is the archetype and domain grammar of a discover filter.
var discoverToken = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func decodeDiscoveryParams(params json.RawMessage) (backend.ServiceDiscoverFilter, bool, int, int, *controlprotocol.Error) {
	var filter backend.ServiceDiscoverFilter
	invalid := func() (backend.ServiceDiscoverFilter, bool, int, int, *controlprotocol.Error) {
		return backend.ServiceDiscoverFilter{}, false, 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	if err := controlprotocol.ValidateNamedParams(params, "query", "refresh", "offset", "limit", "archetype", "intent", "domain", "compatible", "kind"); err != nil {
		return backend.ServiceDiscoverFilter{}, false, 0, 0, err
	}
	if len(params) == 0 {
		return filter, false, 0, 100, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return invalid()
	}
	offset, limit, pageErr := decodePageFields(fields)
	if pageErr != nil {
		return backend.ServiceDiscoverFilter{}, false, 0, 0, pageErr
	}
	if raw, ok := fields["query"]; ok {
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &filter.Query) != nil || len(filter.Query) > 4096 {
			return invalid()
		}
	}
	var refresh bool
	if raw, ok := fields["refresh"]; ok {
		if !literalBool(raw, &refresh) {
			return invalid()
		}
	}
	for name, dst := range map[string]*string{"archetype": &filter.Archetype, "domain": &filter.Domain} {
		if raw, ok := fields[name]; ok {
			if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, dst) != nil || len(*dst) > 64 || !discoverToken.MatchString(*dst) {
				return invalid()
			}
		}
	}
	if raw, ok := fields["intent"]; ok {
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &filter.Intent) != nil || !validIntentFilter(filter.Intent) {
			return invalid()
		}
	}
	if raw, ok := fields["compatible"]; ok {
		var compatible bool
		if !literalBool(raw, &compatible) {
			return invalid()
		}
		filter.Compatible = &compatible
	}
	if raw, ok := fields["kind"]; ok {
		kind, ok := discoverKind(raw)
		if !ok {
			return invalid()
		}
		filter.Kind = kind
	}
	return filter, refresh, offset, limit, nil
}

// literalBool reads a JSON true or false and nothing else.
func literalBool(raw json.RawMessage, dst *bool) bool {
	raw = bytes.TrimSpace(raw)
	switch {
	case bytes.Equal(raw, []byte("true")):
		*dst = true
	case bytes.Equal(raw, []byte("false")):
		*dst = false
	default:
		return false
	}
	return true
}

// validIntentFilter accepts 1–256 bytes of valid UTF-8 with no control or
// format runes.
func validIntentFilter(s string) bool {
	if s == "" || len(s) > 256 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// discoverKind reads a plain JSON integer naming a manifest kind the
// catalog holds: no fraction, exponent or string form.
func discoverKind(raw json.RawMessage) (int, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' || bytes.ContainsAny(raw, ".eE") {
		return 0, false
	}
	var kind int
	if json.Unmarshal(raw, &kind) != nil {
		return 0, false
	}
	switch kind {
	case 5129, 15129, 35129, 35130:
		return kind, true
	}
	return 0, false
}

type settingsResult struct {
	Settings serviceconfig.Effective `json:"settings"`
}

func settingsError(s *Service, err error) *controlprotocol.Error {
	if errors.Is(err, ErrClosing) {
		return controlprotocol.FixedError(controlprotocol.Closing)
	}
	if s.Manager().PersistenceError() != nil {
		return controlprotocol.FixedError(controlprotocol.Unavailable)
	}
	return controlprotocol.FixedError(controlprotocol.ConfigInvalid)
}

func decodeFieldParams(params json.RawMessage) (string, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "field"); err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	var field string
	if raw, ok := fields["field"]; !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &field) != nil || !serviceconfig.FieldName(field) {
		return "", controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	return field, nil
}

func decodeSettingParams(params json.RawMessage) (string, any, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "field", "value"); err != nil {
		return "", nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	var field string
	if raw, ok := fields["field"]; !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &field) != nil || !serviceconfig.FieldName(field) {
		return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	raw, ok := fields["value"]
	if !ok || bytes.Equal(raw, []byte("null")) {
		return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	if field == "discover_on_user_relays" || field == "desktop_entries" || field == "gnome_search" {
		var value bool
		if len(raw) == 0 || (raw[0] != 't' && raw[0] != 'f') || json.Unmarshal(raw, &value) != nil {
			return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
		return field, value, nil
	}
	// sync_id and theme are JSON strings; serviceconfig validates the value
	if field == "sync_id" || field == "theme" {
		var value string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
			return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
		return field, value, nil
	}
	if len(raw) == 0 || raw[0] != '[' {
		return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	var value []string
	if json.Unmarshal(raw, &value) != nil {
		return "", nil, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	return field, value, nil
}
