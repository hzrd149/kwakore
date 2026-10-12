//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sort"
	"strings"
	"time"

	"kwakore/backend/controlprotocol"
)

// eventTypeArgs reads `--type TYPE` pairs: known, distinct event types.
func eventTypeArgs(args []string) ([]string, error) {
	var types []string
	known := controlprotocol.EventTypes()
	for len(args) > 0 {
		if args[0] != "--type" || len(args) < 2 {
			return nil, inputFailure("usage: kwak events [--type TYPE]...")
		}
		if !slices.Contains(known, args[1]) {
			return nil, inputFailure("unknown event type; run 'kwak events --help' for the list")
		}
		if slices.Contains(types, args[1]) {
			return nil, inputFailure("event type given more than once")
		}
		types = append(types, args[1])
		args = args[2:]
	}
	return types, nil
}

var errLineTooLong = errors.New("line too long")

// readEventLine reads one newline-terminated line of at most
// controlprotocol.MaxResponseLine bytes, like the daemon's readFrame.
func readEventLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > controlprotocol.MaxResponseLine+1 {
			return nil, errLineTooLong
		}
		line = append(line, part...)
		if err == nil {
			return line, nil
		}
		if err != bufio.ErrBufferFull {
			return line, err
		}
	}
}

// runEvents subscribes and prints every notification until the daemon ends
// the stream (an error) or --timeout passes after the acknowledgement (exit
// 0). Without --timeout only the acknowledgement has a 30-second deadline.
func runEvents(conn *net.UnixConn, params json.RawMessage, timeout time.Duration, jsonOutput bool, stdout io.Writer) error {
	deadline := 30 * time.Second
	if timeout > 0 {
		deadline = timeout
	}
	_ = conn.SetDeadline(time.Now().Add(deadline))
	request, err := json.Marshal(controlprotocol.Request{JSONRPC: "2.0", Method: "events.subscribe", Params: params, ID: json.RawMessage("1")})
	if err != nil {
		return errors.New("invalid command parameters")
	}
	request = append(request, '\n')
	if n, err := conn.Write(request); err != nil || n != len(request) {
		if isTimeout(err) {
			return timeoutFailure{}
		}
		return errors.New("daemon unavailable")
	}
	reader := bufio.NewReader(conn)
	line, err := readEventLine(reader)
	if errors.Is(err, errLineTooLong) {
		return errors.New("invalid daemon response")
	}
	if err != nil {
		if isTimeout(err) {
			return timeoutFailure{}
		}
		if len(line) != 0 {
			return errors.New("invalid daemon response")
		}
		return errors.New("daemon unavailable")
	}
	if err := checkEventsAck(line); err != nil {
		return err
	}
	if timeout <= 0 {
		_ = conn.SetDeadline(time.Time{})
	}
	out := bufio.NewWriter(stdout)
	for {
		line, err := readEventLine(reader)
		if errors.Is(err, errLineTooLong) {
			return errors.New("invalid daemon response")
		}
		if err != nil {
			if isTimeout(err) && timeout > 0 {
				return nil
			}
			if len(line) != 0 {
				return errors.New("invalid daemon response")
			}
			return errors.New("event stream ended")
		}
		params, ok := eventParams(line)
		if !ok {
			return errors.New("invalid daemon response")
		}
		if jsonOutput {
			_, err = out.Write(append(append([]byte(nil), params...), '\n'))
		} else {
			_, err = io.WriteString(out, eventLine(params)+"\n")
		}
		if err == nil {
			err = out.Flush()
		}
		if err != nil {
			return err
		}
	}
}

// checkEventsAck validates the events.subscribe response the way run()
// validates any other, and its result: subscribed true and known types.
func checkEventsAck(line []byte) error {
	var response map[string]json.RawMessage
	if json.Unmarshal(line, &response) != nil || response == nil ||
		!bytes.Equal(response["jsonrpc"], []byte(`"2.0"`)) || !bytes.Equal(response["id"], []byte("1")) {
		return errors.New("invalid daemon response")
	}
	result, hasResult := response["result"]
	rpcError, hasError := response["error"]
	if hasResult == hasError || len(response) != 3 {
		return errors.New("invalid daemon response")
	}
	if hasError {
		var fields map[string]json.RawMessage
		var remote controlprotocol.Error
		if json.Unmarshal(rpcError, &remote) != nil || remote.Code == 0 || remote.Message == "" || json.Unmarshal(rpcError, &fields) != nil {
			return errors.New("invalid daemon response")
		}
		if _, ok := fields["data"]; ok && remote.Code == controlprotocol.Unavailable {
			return errors.New("invalid daemon response")
		}
		return rpcFailure{RPC: *controlprotocol.FixedError(remote.Code)}
	}
	var ack struct {
		Subscribed bool     `json:"subscribed"`
		Types      []string `json:"types"`
	}
	if controlprotocol.ValidateNamedParams(result, "subscribed", "types") != nil || json.Unmarshal(result, &ack) != nil || !ack.Subscribed || len(ack.Types) == 0 {
		return errors.New("invalid daemon response")
	}
	for _, t := range ack.Types {
		if !slices.Contains(controlprotocol.EventTypes(), t) {
			return errors.New("invalid daemon response")
		}
	}
	return nil
}

// eventParams returns the params of a valid event notification: exactly
// jsonrpc "2.0", method "event" and a params object with a string type and
// a string time, and no id.
func eventParams(line []byte) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(line, &fields) != nil || len(fields) != 3 ||
		!bytes.Equal(fields["jsonrpc"], []byte(`"2.0"`)) || !bytes.Equal(fields["method"], []byte(`"`+controlprotocol.EventMethod+`"`)) {
		return nil, false
	}
	params, ok := fields["params"]
	if !ok {
		return nil, false
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(params, &body) != nil || body == nil {
		return nil, false
	}
	var typ, at string
	if json.Unmarshal(body["type"], &typ) != nil || typ == "" || json.Unmarshal(body["time"], &at) != nil || at == "" {
		return nil, false
	}
	var compact bytes.Buffer
	if json.Compact(&compact, params) != nil {
		return nil, false
	}
	return compact.Bytes(), true
}

// eventLine is the human form of an event: time, type, then the other
// fields as sorted key=value pairs, with nested values as compact JSON.
func eventLine(params json.RawMessage) string {
	var body map[string]json.RawMessage
	_ = json.Unmarshal(params, &body)
	var typ, at string
	_ = json.Unmarshal(body["type"], &typ)
	_ = json.Unmarshal(body["time"], &at)
	keys := make([]string, 0, len(body))
	for key := range body {
		if key != "type" && key != "time" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := []string{displayValue(at), displayValue(typ)}
	for _, key := range keys {
		var value any
		_ = json.Unmarshal(body[key], &value)
		var text string
		switch value.(type) {
		case map[string]any, []any:
			var compact bytes.Buffer
			_ = json.Compact(&compact, body[key])
			text = compact.String()
		default:
			text = displayValue(value)
		}
		parts = append(parts, fmt.Sprintf("%s=%s", displayValue(key), text))
	}
	return strings.Join(parts, " ")
}
