//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
	"verdana/backend/controlprotocol"
)

func run(args []string) error {
	method, params, socketOverride, err := command(args)
	if err != nil {
		return err
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if socketOverride == "" && (runtimeDir == "" || !filepath.IsAbs(runtimeDir)) {
		return errors.New("XDG_RUNTIME_DIR must name an absolute private 0700 directory")
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
	uid, err := peerUID(conn)
	if err != nil || uid != uint32(os.Geteuid()) {
		return errors.New("unauthorized server")
	}
	deadline := 5 * time.Second
	if method == "napplet.discover" && bytes.Contains(params, []byte(`"refresh":true`)) {
		deadline = 30 * time.Second
	}
	if method == "napplet.install" || method == "napplet.update" {
		deadline = 180 * time.Second
	}
	_ = conn.SetDeadline(time.Now().Add(deadline))
	request, err := json.Marshal(controlprotocol.Request{JSONRPC: "2.0", Method: method, Params: params, ID: json.RawMessage("1")})
	if err != nil {
		return errors.New("invalid command parameters")
	}
	request = append(request, '\n')
	if n, err := conn.Write(request); err != nil || n != len(request) {
		return errors.New("daemon unavailable")
	}
	line, err := bufio.NewReader(io.LimitReader(conn, controlprotocol.MaxResponseLine+1)).ReadBytes('\n')
	if err != nil {
		return errors.New("daemon unavailable")
	}
	if len(line) > controlprotocol.MaxResponseLine {
		return errors.New("invalid daemon response")
	}
	var response struct {
		JSONRPC string                 `json:"jsonrpc"`
		ID      json.RawMessage        `json:"id"`
		Result  json.RawMessage        `json:"result"`
		Error   *controlprotocol.Error `json:"error"`
	}
	if json.Unmarshal(line, &response) != nil || response.JSONRPC != "2.0" || !bytes.Equal(response.ID, []byte("1")) {
		return errors.New("invalid daemon response")
	}
	if response.Error != nil {
		return rpcFailure{RPC: *controlprotocol.FixedError(response.Error.Code)}
	}
	if len(response.Result) == 0 {
		return errors.New("invalid daemon response")
	}
	_, err = os.Stdout.Write(append(response.Result, '\n'))
	return err
}

type rpcFailure struct{ RPC controlprotocol.Error }

func (e rpcFailure) Error() string { return e.RPC.Message }

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
	return "", nil, "", inputFailure("usage: kwakore [--socket PATH] status|diagnostics|installed [--offset N --limit N]|discover [--query TEXT --refresh --offset N --limit N]|install ADDRESS|update ADDRESS|settings get|reload|set FIELD JSON_VALUE|clear FIELD")
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
	if errors.As(err, &remote) {
		rpcErr = controlprotocol.FixedError(remote.RPC.Code)
	} else if errors.As(err, &input) {
		rpcErr = controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Error *controlprotocol.Error `json:"error"`
	}{rpcErr})
}
