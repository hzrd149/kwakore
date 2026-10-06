//go:build linux

package daemon

import (
	"bytes"
	"encoding/json"
	"errors"

	"verdana/backend"
	"verdana/backend/controlprotocol"
	"verdana/backend/serviceconfig"
)

// dispatchRPC is the allow-listed service boundary. Configuration persistence
// and change notifications remain owned by Service and serviceconfig.Manager.
func (s *Service) dispatchRPC(method string, params json.RawMessage) (any, *controlprotocol.Error) {
	switch method {
	case "service.status":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		return struct {
			ProtocolVersion int    `json:"protocol_version"`
			Health          Health `json:"health"`
		}{controlprotocol.Version, s.Health()}, nil
	case "service.diagnostics":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
		return s.Diagnostics(), nil
	case "settings.get":
		if err := controlprotocol.ValidateNamedParams(params); err != nil {
			return nil, err
		}
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
	default:
		return nil, controlprotocol.FixedError(controlprotocol.MethodNotFound)
	}
}

func decodePageParams(params json.RawMessage) (int, int, *controlprotocol.Error) {
	if err := controlprotocol.ValidateNamedParams(params, "offset", "limit"); err != nil {
		return 0, 0, err
	}
	offset, limit := 0, 100
	if len(params) == 0 {
		return offset, limit, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return 0, 0, controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
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
