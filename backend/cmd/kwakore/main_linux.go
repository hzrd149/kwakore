//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"verdana/backend/controlprotocol"
)

func run(args []string) error {
	args, socketOverride, timeoutOverride, err := globalOptions(args)
	if err != nil {
		return err
	}
	method, params, _, err := command(args)
	if err != nil {
		return err
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if socketOverride == "" && (runtimeDir == "" || !filepath.IsAbs(runtimeDir)) {
		return errors.New("XDG_RUNTIME_DIR must name an absolute private 0700 directory")
	}
	if socketOverride == "" {
		info, err := os.Lstat(runtimeDir)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("XDG_RUNTIME_DIR must name an absolute private 0700 directory")
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) {
			return errors.New("XDG_RUNTIME_DIR must be owned by the current user")
		}
	}
	socketPath := socketOverride
	if socketPath == "" {
		socketPath = filepath.Join(runtimeDir, "kwakore", "daemon.sock")
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return errors.New("daemon unavailable")
	}
	defer conn.Close()
	uid, err := serverPeerUID(conn)
	if err != nil || uid != uint32(os.Geteuid()) {
		return errors.New("unauthorized server")
	}
	deadline := 30 * time.Second
	if method == "napplet.install" || method == "napplet.update" || method == "napplet.uninstall" {
		deadline = 180 * time.Second
	}
	if timeoutOverride > 0 {
		deadline = timeoutOverride
	}
	_ = conn.SetDeadline(time.Now().Add(deadline))
	request, err := json.Marshal(controlprotocol.Request{JSONRPC: "2.0", Method: method, Params: params, ID: json.RawMessage("1")})
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
	line, err := bufio.NewReader(io.LimitReader(conn, controlprotocol.MaxResponseLine+2)).ReadBytes('\n')
	if len(line) > controlprotocol.MaxResponseLine+1 {
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
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return errors.New("invalid daemon response")
	}
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
		var remote controlprotocol.Error
		if json.Unmarshal(rpcError, &remote) != nil || remote.Code == 0 || remote.Message == "" {
			return errors.New("invalid daemon response")
		}
		fixed := controlprotocol.FixedError(remote.Code)
		if remote.Code == controlprotocol.PartialCleanup {
			var fields map[string]json.RawMessage
			if json.Unmarshal(rpcError, &fields) != nil {
				return errors.New("invalid daemon response")
			}
			data, ok := parsePartialCleanupData(fields["data"], params)
			if method != "napplet.uninstall" || !ok {
				return errors.New("invalid daemon response")
			}
			fixed.Data = data
		}
		if remote.Code == controlprotocol.Unavailable {
			var fields map[string]json.RawMessage
			if json.Unmarshal(rpcError, &fields) != nil {
				return errors.New("invalid daemon response")
			}
			if raw, ok := fields["data"]; ok {
				data, valid := parseSessionUnavailableData(raw)
				if method != "napplet.launch" || !valid {
					return errors.New("invalid daemon response")
				}
				fixed.Data = data
			}
		}
		return rpcFailure{RPC: *fixed}
	}
	if len(result) == 0 {
		return errors.New("invalid daemon response")
	}
	_, err = os.Stdout.Write(append(result, '\n'))
	return err
}

var serverPeerUID = peerUID

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

type timeoutFailure struct{}

func (timeoutFailure) Error() string {
	return "client timeout; operation outcome unknown; check status or installed state"
}

func globalOptions(args []string) ([]string, string, time.Duration, error) {
	var socket string
	var timeout time.Duration
	for len(args) > 0 {
		switch args[0] {
		case "--socket", "--timeout":
			if len(args) < 2 {
				return nil, "", 0, inputFailure("missing global option value")
			}
			if args[0] == "--socket" {
				if socket != "" || !filepath.IsAbs(args[1]) {
					return nil, "", 0, inputFailure("--socket requires an absolute path")
				}
				socket = args[1]
			} else {
				value, err := time.ParseDuration(args[1])
				if err != nil || value <= 0 || timeout != 0 {
					return nil, "", 0, inputFailure("--timeout requires a positive duration")
				}
				timeout = value
			}
			args = args[2:]
		default:
			return args, socket, timeout, nil
		}
	}
	return args, socket, timeout, nil
}

type rpcFailure struct{ RPC controlprotocol.Error }

func (e rpcFailure) Error() string { return e.RPC.Message }

func parsePartialCleanupData(raw, params json.RawMessage) (controlprotocol.PartialCleanupData, bool) {
	var data controlprotocol.PartialCleanupData
	if len(raw) == 0 || raw[0] != '{' || controlprotocol.ValidateNamedParams(raw, "address", "record_removed", "cleanup_complete") != nil {
		return data, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 ||
		!bytes.Equal(bytes.TrimSpace(fields["record_removed"]), []byte("true")) ||
		!bytes.Equal(bytes.TrimSpace(fields["cleanup_complete"]), []byte("false")) ||
		json.Unmarshal(fields["address"], &data.Address) != nil || data.Address == "" {
		return data, false
	}
	var requested struct {
		Address string `json:"address"`
	}
	if json.Unmarshal(params, &requested) != nil || data.Address != requested.Address {
		return data, false
	}
	data.RecordRemoved = true
	return data, true
}

func parseSessionUnavailableData(raw json.RawMessage) (controlprotocol.SessionUnavailableData, bool) {
	var data controlprotocol.SessionUnavailableData
	if len(raw) == 0 || raw[0] != '{' || controlprotocol.ValidateNamedParams(raw, "reason") != nil {
		return data, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 1 || !bytes.Equal(fields["reason"], []byte(`"session_unavailable"`)) {
		return data, false
	}
	data.Reason = "session_unavailable"
	return data, true
}

type inputFailure string

func (e inputFailure) Error() string { return string(e) }

func command(args []string) (string, json.RawMessage, string, error) {
	socketPath := ""
	if len(args) >= 2 && args[0] == "--socket" {
		if !filepath.IsAbs(args[1]) {
			return "", nil, "", inputFailure("--socket requires an absolute path")
		}
		socketPath, args = args[1], args[2:]
	}
	if len(args) == 1 {
		switch args[0] {
		case "status":
			return "service.status", nil, socketPath, nil
		case "diagnostics":
			return "service.diagnostics", nil, socketPath, nil
		}
	}
	if len(args) >= 1 && args[0] == "installed" {
		flags := flag.NewFlagSet("installed", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		offset := flags.Int("offset", 0, "page offset")
		limit := flags.Int("limit", 100, "page size")
		if flags.Parse(args[1:]) != nil || len(flags.Args()) != 0 || *offset < 0 || *limit < 1 || *limit > 500 {
			return "", nil, "", inputFailure("invalid installed page")
		}
		params, _ := json.Marshal(struct {
			Offset int `json:"offset"`
			Limit  int `json:"limit"`
		}{*offset, *limit})
		return "napplet.installed", params, socketPath, nil
	}
	if len(args) >= 1 && args[0] == "discover" {
		flags := flag.NewFlagSet("discover", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		query := flags.String("query", "", "catalog query")
		refresh := flags.Bool("refresh", false, "wait for relay refresh")
		offset := flags.Int("offset", 0, "page offset")
		limit := flags.Int("limit", 100, "page size")
		if flags.Parse(args[1:]) != nil || len(flags.Args()) != 0 || len(*query) > 4096 || *offset < 0 || *limit < 1 || *limit > 500 {
			return "", nil, "", inputFailure("invalid discover parameters")
		}
		params, _ := json.Marshal(struct {
			Query   string `json:"query"`
			Refresh bool   `json:"refresh"`
			Offset  int    `json:"offset"`
			Limit   int    `json:"limit"`
		}{*query, *refresh, *offset, *limit})
		return "napplet.discover", params, socketPath, nil
	}
	if len(args) == 2 && (args[0] == "install" || args[0] == "update") {
		if len(args[1]) == 0 || len(args[1]) > 4096 {
			return "", nil, "", inputFailure("invalid address")
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
		}{args[1]})
		return "napplet." + args[0], params, socketPath, nil
	}
	if len(args) == 2 && args[0] == "launch" {
		if len(args[1]) == 0 || len(args[1]) > 4096 {
			return "", nil, "", inputFailure("invalid address")
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
		}{args[1]})
		return "napplet.launch", params, socketPath, nil
	}
	if len(args) == 2 && args[0] == "stop" {
		decoded, err := hex.DecodeString(args[1])
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != args[1] {
			return "", nil, "", inputFailure("invalid window ID")
		}
		params, _ := json.Marshal(struct {
			WindowID string `json:"window_id"`
		}{args[1]})
		return "napplet.stop", params, socketPath, nil
	}
	if len(args) >= 1 && args[0] == "uninstall" {
		flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		yes := flags.Bool("yes", false, "confirm removal")
		if flags.Parse(args[1:]) != nil || len(flags.Args()) != 1 || len(flags.Args()[0]) == 0 || len(flags.Args()[0]) > 4096 {
			return "", nil, "", inputFailure("usage: uninstall --yes ADDRESS")
		}
		if !*yes {
			return "", nil, "", inputFailure("uninstall requires --yes")
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
			Confirm bool   `json:"confirm"`
		}{flags.Args()[0], true})
		return "napplet.uninstall", params, socketPath, nil
	}
	if len(args) >= 2 && args[0] == "settings" {
		switch args[1] {
		case "get":
			if len(args) == 2 {
				return "settings.get", nil, socketPath, nil
			}
		case "reload":
			if len(args) == 2 {
				return "settings.reload", nil, socketPath, nil
			}
		case "clear":
			if len(args) == 3 && settingField(args[2]) {
				params, _ := json.Marshal(struct {
					Field string `json:"field"`
				}{args[2]})
				return "settings.clear", params, socketPath, nil
			}
		case "set":
			if len(args) == 4 && settingField(args[2]) {
				value := json.RawMessage(args[3])
				if !validSettingValue(args[2], value) {
					return "", nil, "", inputFailure("invalid JSON setting value")
				}
				params, _ := json.Marshal(struct {
					Field string          `json:"field"`
					Value json.RawMessage `json:"value"`
				}{args[2], value})
				return "settings.set", params, socketPath, nil
			}
		}
	}
	return "", nil, "", inputFailure("usage: kwakore [--socket PATH] [--timeout DURATION] status|diagnostics|installed [--offset N --limit N]|discover [--query TEXT --refresh --offset N --limit N]|install ADDRESS|update ADDRESS|uninstall --yes ADDRESS|launch ADDRESS|stop WINDOW_ID|settings get|reload|set FIELD JSON_VALUE|clear FIELD")
}

func settingField(field string) bool {
	return field == "relays" || field == "blossom_servers" || field == "discover_on_user_relays"
}

func validSettingValue(field string, value json.RawMessage) bool {
	if !json.Valid(value) || bytes.Equal(value, []byte("null")) {
		return false
	}
	if field == "discover_on_user_relays" {
		var v bool
		return (bytes.Equal(value, []byte("true")) || bytes.Equal(value, []byte("false"))) && json.Unmarshal(value, &v) == nil
	}
	if len(value) == 0 || value[0] != '[' {
		return false
	}
	var v []string
	return json.Unmarshal(value, &v) == nil
}

func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) { cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return 0, err
	}
	if sockErr != nil {
		return 0, sockErr
	}
	return cred.Uid, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		writeCLIError(os.Stderr, err)
		os.Exit(1)
	}
}

func writeCLIError(w io.Writer, err error) {
	rpcErr := controlprotocol.FixedError(controlprotocol.Unavailable)
	var remote rpcFailure
	var input inputFailure
	var timeout timeoutFailure
	if errors.As(err, &timeout) {
		rpcErr = &controlprotocol.Error{Code: controlprotocol.Timeout, Message: timeout.Error()}
	} else if errors.As(err, &remote) {
		rpcErr = controlprotocol.FixedError(remote.RPC.Code)
		if rpcErr.Code == controlprotocol.PartialCleanup {
			if data, ok := remote.RPC.Data.(controlprotocol.PartialCleanupData); ok && data.Address != "" && data.RecordRemoved && !data.CleanupComplete {
				rpcErr.Data = data
			}
		}
		if rpcErr.Code == controlprotocol.Unavailable {
			if data, ok := remote.RPC.Data.(controlprotocol.SessionUnavailableData); ok && data.Reason == "session_unavailable" {
				rpcErr.Data = data
			}
		}
	} else if errors.As(err, &input) {
		rpcErr = controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Error *controlprotocol.Error `json:"error"`
	}{rpcErr})
}
