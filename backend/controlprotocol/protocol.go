// Package controlprotocol defines the portable JSON-RPC wire contract.
package controlprotocol

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

const Version = 1
const MaxRequestLine = 1 << 20
const MaxResponseLine = 8 << 20
const MaxBatch = 64

// MethodNames is the version 1 public method catalog. A new entry requires
// matching daemon routing, CLI coverage, and control-protocol documentation.
func MethodNames() []string {
	return []string{
		"service.status", "service.diagnostics",
		"settings.get", "settings.reload", "settings.set", "settings.clear",
		"signer.status", "signer.switch",
		"napplet.discover", "napplet.installed", "napplet.install", "napplet.update", "napplet.uninstall", "napplet.launch", "napplet.stop",
		"napplet.permissions.get", "napplet.permissions.set", "napplet.permissions.clear",
	}
}

const (
	ParseError           = -32700
	InvalidRequest       = -32600
	MethodNotFound       = -32601
	InvalidParams        = -32602
	InternalError        = -32603
	Unauthorized         = 1001
	NotFound             = 1002
	Busy                 = 1003
	Unavailable          = 1004
	NoUpdate             = 1005
	ConfigInvalid        = 1006
	Closing              = 1007
	Timeout              = 1008
	Conflict             = 1009
	ConfirmationRequired = 1010
	PartialCleanup       = 1011
)

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// PartialCleanupData is the only error metadata allowed on the wire.
type PartialCleanupData struct {
	Address         string `json:"address"`
	RecordRemoved   bool   `json:"record_removed"`
	CleanupComplete bool   `json:"cleanup_complete"`
}

type SessionUnavailableData struct {
	Reason string `json:"reason"`
}
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}
type Dispatch func(method string, params json.RawMessage) (any, *Error)

func FixedError(code int) *Error {
	message := map[int]string{
		ParseError: "Parse error", InvalidRequest: "Invalid Request", MethodNotFound: "Method not found", InvalidParams: "Invalid params", InternalError: "Internal error",
		Unauthorized: "Unauthorized", NotFound: "Not found", Busy: "Busy", Unavailable: "Unavailable", NoUpdate: "No update", ConfigInvalid: "Invalid configuration", Closing: "Closing", Timeout: "Timeout", Conflict: "Conflict", ConfirmationRequired: "Confirmation required", PartialCleanup: "Partial cleanup",
	}[code]
	if message == "" {
		return &Error{Code: InternalError, Message: "Internal error"}
	}
	return &Error{Code: code, Message: message}
}

func ProcessFrame(frame []byte, dispatch Dispatch) []byte {
	if len(frame) > MaxRequestLine {
		return encodeError(InvalidRequest, nil)
	}
	if !json.Valid(frame) {
		return encodeError(ParseError, nil)
	}
	frame = bytes.TrimSpace(frame)
	if frame[0] == '[' {
		var members []json.RawMessage
		if json.Unmarshal(frame, &members) != nil {
			return encodeError(ParseError, nil)
		}
		if len(members) == 0 || len(members) > MaxBatch {
			return encodeError(InvalidRequest, nil)
		}
		responses := make([]json.RawMessage, 0, len(members))
		for _, member := range members {
			if response := processRequest(member, dispatch); response != nil {
				responses = append(responses, response)
			}
		}
		if len(responses) == 0 {
			return nil
		}
		out, _ := json.Marshal(responses)
		if len(out) > MaxResponseLine {
			return encodeError(InternalError, nil)
		}
		return out
	}
	return processRequest(frame, dispatch)
}

func processRequest(frame []byte, dispatch Dispatch) []byte {
	if len(frame) == 0 || frame[0] != '{' {
		return encodeError(InvalidRequest, nil)
	}
	fields, err := objectFields(frame)
	if err != nil {
		return encodeError(InvalidRequest, nil)
	}
	var request Request
	if json.Unmarshal(frame, &request) != nil || request.JSONRPC != "2.0" || request.Method == "" {
		return encodeError(InvalidRequest, nil)
	}
	if _, ok := fields["id"]; ok && !validID(request.ID) {
		return encodeError(InvalidRequest, nil)
	}
	for key := range fields {
		if key != "jsonrpc" && key != "method" && key != "params" && key != "id" {
			return encodeError(InvalidRequest, nil)
		}
	}
	if strings.HasPrefix(request.Method, "rpc.") {
		if _, ok := fields["id"]; !ok {
			return nil
		}
		return encodeError(MethodNotFound, request.ID)
	}
	if len(request.Params) > 0 && request.Params[0] != '{' {
		if _, ok := fields["id"]; !ok {
			return nil
		}
		return encodeError(InvalidParams, request.ID)
	}
	result, rpcErr := dispatch(request.Method, request.Params)
	if _, ok := fields["id"]; !ok {
		return nil
	}
	if rpcErr != nil {
		fixed := FixedError(rpcErr.Code)
		if fixed.Code == PartialCleanup && rpcErr.Code == PartialCleanup {
			if data, ok := rpcErr.Data.(PartialCleanupData); ok && data.Address != "" && data.RecordRemoved && !data.CleanupComplete {
				fixed.Data = data
			}
		}
		if fixed.Code == Unavailable && rpcErr.Code == Unavailable {
			if data, ok := rpcErr.Data.(SessionUnavailableData); ok && data.Reason == "session_unavailable" {
				fixed.Data = data
			}
		}
		rpcErr = fixed
	}
	response := Response{JSONRPC: "2.0", Error: rpcErr, ID: request.ID}
	if rpcErr == nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return encodeError(InternalError, request.ID)
		}
		response.Result = encoded
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return encodeError(InternalError, request.ID)
	}
	if len(encoded) > MaxResponseLine {
		return encodeError(InternalError, request.ID)
	}
	return encoded
}

// ValidateNamedParams rejects positional, unknown, and duplicate named values.
func ValidateNamedParams(raw json.RawMessage, allowed ...string) *Error {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] != '{' {
		return FixedError(InvalidParams)
	}
	fields, err := objectFields(raw)
	if err != nil {
		return FixedError(InvalidParams)
	}
	for key := range fields {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return FixedError(InvalidParams)
		}
	}
	return nil
}

func objectFields(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, io.ErrUnexpectedEOF
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		if _, exists := fields[key]; exists {
			return nil, io.ErrUnexpectedEOF
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, io.ErrUnexpectedEOF
	}
	return fields, nil
}

func validID(id json.RawMessage) bool {
	if bytes.Equal(id, []byte("null")) {
		return true
	}
	var s string
	if json.Unmarshal(id, &s) == nil {
		return true
	}
	if bytes.ContainsAny(id, ".eE") {
		return false
	}
	var n json.Number
	return json.Unmarshal(id, &n) == nil
}

func encodeError(code int, id json.RawMessage) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	b, _ := json.Marshal(Response{JSONRPC: "2.0", Error: FixedError(code), ID: id})
	return b
}

// ErrorResponse emits only catalogued messages and never includes internal data.
func ErrorResponse(code int, id json.RawMessage) []byte { return encodeError(code, id) }
