//go:build linux

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"verdana/backend"
	"verdana/backend/controlprotocol"
	"verdana/backend/serviceconfig"
)

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
		result, uninstallErr := backend.ServiceUninstall(workCtx, address)
		if errors.Is(uninstallErr, backend.ErrServicePartialCleanup) {
			rpcErr := controlprotocol.FixedError(controlprotocol.PartialCleanup)
			rpcErr.Data = struct {
				Address         string `json:"address"`
				RecordRemoved   bool   `json:"record_removed"`
				CleanupComplete bool   `json:"cleanup_complete"`
			}{result.Address, result.RecordRemoved, result.CleanupComplete}
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

func mutationError(err error) *controlprotocol.Error {
	switch {
	case errors.Is(err, backend.ErrServiceInvalidAddress):
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
