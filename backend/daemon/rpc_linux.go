//go:build linux

package daemon

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"verdana/backend"
	"verdana/backend/controlprotocol"
	"verdana/backend/serviceconfig"
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
		query, refresh, offset, limit, err := decodeDiscoveryParams(params)
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
		result, discoverErr := backend.ServiceDiscover(workCtx, query, refresh, offset, limit)
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
		result, installErr := backend.ServiceInstall(workCtx, address)
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
	if err := controlprotocol.ValidateNamedParams(params, "mode", "secret"); err != nil {
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

func decodeDiscoveryParams(params json.RawMessage) (string, bool, int, int, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "query", "refresh", "offset", "limit"); err != nil {
		return "", false, 0, 0, err
	}
	if len(params) == 0 {
		return "", false, 0, 100, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", false, 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	offset, limit, pageErr := decodePageFields(fields)
	if pageErr != nil {
		return "", false, 0, 0, pageErr
	}
	var query string
	if raw, ok := fields["query"]; ok {
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &query) != nil || len(query) > 4096 {
			return "", false, 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
	}
	var refresh bool
	if raw, ok := fields["refresh"]; ok {
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &refresh) != nil || (len(raw) > 0 && raw[0] != 't' && raw[0] != 'f') {
			return "", false, 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
		}
	}
	return query, refresh, offset, limit, nil
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
	if field == "discover_on_user_relays" {
		var value bool
		if len(raw) == 0 || (raw[0] != 't' && raw[0] != 'f') || json.Unmarshal(raw, &value) != nil {
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
