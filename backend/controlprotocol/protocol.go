// Package controlprotocol defines the portable JSON-RPC wire contract.
package controlprotocol

import (
	"bytes"
	"encoding/json"
)

const Version = 1

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
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
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
	var request Request
	if !json.Valid(frame) {
		return encodeError(ParseError, nil)
	}
	if err := json.Unmarshal(frame, &request); err != nil || request.JSONRPC != "2.0" || request.Method == "" {
		return encodeError(InvalidRequest, nil)
	}
	if len(request.ID) == 0 {
		return nil
	}
	if !validID(request.ID) {
		return encodeError(InvalidRequest, nil)
	}
	result, rpcErr := dispatch(request.Method, request.Params)
	response := Response{JSONRPC: "2.0", Result: result, Error: rpcErr, ID: request.ID}
	encoded, err := json.Marshal(response)
	if err != nil {
		return encodeError(InternalError, request.ID)
	}
	return encoded
}

func validID(id json.RawMessage) bool {
	if bytes.Equal(id, []byte("null")) {
		return true
	}
	var s string
	if json.Unmarshal(id, &s) == nil {
		return true
	}
	var n json.Number
	if json.Unmarshal(id, &n) == nil {
		return true
	}
	return false
}

func encodeError(code int, id json.RawMessage) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	b, _ := json.Marshal(Response{JSONRPC: "2.0", Error: FixedError(code), ID: id})
	return b
}
