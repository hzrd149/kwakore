//go:build linux

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"kwakore/backend/controlprotocol"
	"kwakore/backend/desktopentry"
	"kwakore/backend/napaddr"
)

func run(args []string) error {
	args, socketOverride, timeoutOverride, jsonOutput, err := globalOptions(args)
	if err != nil {
		return err
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")) {
		_, err := io.WriteString(os.Stdout, cliHelp)
		return err
	}
	method, params, _, err := command(args)
	if err != nil {
		return err
	}
	// A desktop entry always reaches the standard user socket, the one
	// kwakore.socket owns, so systemd can start the daemon on demand.
	if len(args) > 0 && args[0] == "launch-token" && socketOverride != "" {
		return inputFailure("launch-token uses the standard user socket")
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
	if method == "napplet.install" || method == "napplet.update" || method == "napplet.uninstall" || method == "signer.pair.wait" {
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
		if strings.HasPrefix(method, "signer.") {
			var fields map[string]json.RawMessage
			if controlprotocol.ValidateNamedParams(rpcError, "code", "message") != nil || json.Unmarshal(rpcError, &fields) != nil || len(fields) != 2 || fields["code"] == nil || fields["message"] == nil {
				return errors.New("invalid daemon response")
			}
		}
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
	if strings.HasPrefix(method, "napplet.permissions.") && !validPermissionResponse(method, params, result) {
		return errors.New("invalid daemon response")
	}
	if method == "signer.pair.start" {
		start, ok := validPairStartResult(result)
		if !ok {
			return errors.New("invalid daemon response")
		}
		var offered struct {
			Secret string `json:"secret"`
		}
		if json.Unmarshal(params, &offered) != nil || len(offered.Secret) != 32 {
			return errors.New("invalid command parameters")
		}
		uri := localPairURI(start.ClientPublicKey, start.Relay, offered.Secret)
		out, _ := json.Marshal(struct {
			PairingURI string `json:"pairing_uri"`
			Notice     string `json:"notice"`
		}{uri, "Private pairing token: share only with your signer"})
		return writeCLIResult(os.Stdout, method, out, jsonOutput)
	}
	if method == "signer.pair.cancel" {
		if !validPairCancelResult(result) {
			return errors.New("invalid daemon response")
		}
	} else if strings.HasPrefix(method, "signer.") && !validSignerResponse(result) {
		return errors.New("invalid daemon response")
	}
	return writeCLIResult(os.Stdout, method, result, jsonOutput)
}

var serverPeerUID = peerUID

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

type timeoutFailure struct{}

func (timeoutFailure) Error() string {
	return "client timeout; operation outcome unknown; check service, signer, or installed state"
}

func globalOptions(args []string) ([]string, string, time.Duration, bool, error) {
	var socket string
	var timeout time.Duration
	var jsonOutput bool
	for len(args) > 0 {
		switch args[0] {
		case "--json":
			if jsonOutput {
				return nil, "", 0, false, inputFailure("--json specified more than once")
			}
			jsonOutput = true
			args = args[1:]
		case "--socket", "--timeout":
			if len(args) < 2 {
				return nil, "", 0, jsonOutput, inputFailure("missing global option value")
			}
			if args[0] == "--socket" {
				if socket != "" || !filepath.IsAbs(args[1]) {
					return nil, "", 0, jsonOutput, inputFailure("--socket requires an absolute path")
				}
				socket = args[1]
			} else {
				value, err := time.ParseDuration(args[1])
				if err != nil || value <= 0 || timeout != 0 {
					return nil, "", 0, jsonOutput, inputFailure("--timeout requires a positive duration")
				}
				timeout = value
			}
			args = args[2:]
		default:
			return args, socket, timeout, jsonOutput, nil
		}
	}
	return args, socket, timeout, jsonOutput, nil
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

// addressFailure is a refused ADDRESS argument. It holds one fixed reason
// (invalid_address, unsupported_nip19 or unsafe_identifier) and never the
// argument itself, which may be a secret key pasted by mistake.
type addressFailure string

func (e addressFailure) Error() string { return "invalid address: " + string(e) }

// addressErrorData is the data object of a refused ADDRESS argument.
type addressErrorData struct {
	Reason   string   `json:"reason"`
	Accepted []string `json:"accepted"`
}

// commandAddress normalizes an ADDRESS argument to the canonical coordinate
// the control protocol requires. A d tag with control, format or separator
// characters is refused unless allowUnsafeIdentifier is set, which only
// uninstall does so such a record can always be removed.
func commandAddress(arg string, allowUnsafeIdentifier bool) (napaddr.Address, error) {
	addr, err := napaddr.Parse(arg)
	if errors.Is(err, napaddr.ErrUnsupported) {
		return napaddr.Address{}, addressFailure("unsupported_nip19")
	}
	if err != nil {
		return napaddr.Address{}, addressFailure("invalid_address")
	}
	if !allowUnsafeIdentifier && !napaddr.SafeIdentifier(addr.Identifier) {
		return napaddr.Address{}, addressFailure("unsafe_identifier")
	}
	return addr, nil
}

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
	if len(args) >= 2 && args[0] == "signer" {
		if len(args) == 2 && args[1] == "status" {
			return "signer.status", nil, socketPath, nil
		}
		if len(args) == 3 && args[1] == "switch" && args[2] == "none" {
			return "signer.switch", json.RawMessage(`{"mode":"none"}`), socketPath, nil
		}
		if len(args) == 3 && args[1] == "pair" && args[2] == "start" {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err != nil {
				return "", nil, "", inputFailure("could not create pairing token")
			}
			params, _ := json.Marshal(struct {
				Secret string `json:"secret"`
			}{hex.EncodeToString(raw[:])})
			return "signer.pair.start", params, socketPath, nil
		}
		if len(args) == 3 && args[1] == "pair" && args[2] == "wait" {
			return "signer.pair.wait", nil, socketPath, nil
		}
		if len(args) == 3 && args[1] == "pair" && args[2] == "cancel" {
			return "signer.pair.cancel", nil, socketPath, nil
		}
		if len(args) == 5 && args[1] == "switch" && (args[2] == "nsec" || args[2] == "bunker") {
			mode := args[2]
			limit := 256
			if mode == "bunker" {
				limit = 2048
			}
			var secret string
			var err error
			switch args[3] {
			case "--secret-stdin":
				return "", nil, "", inputFailure("invalid signer secret source")
			case "--secret-file":
				secret, err = readSignerSecretFile(args[4], limit)
			default:
				return "", nil, "", inputFailure("invalid signer secret source")
			}
			if err != nil {
				return "", nil, "", inputFailure("invalid signer secret source")
			}
			params, _ := json.Marshal(struct {
				Mode   string `json:"mode"`
				Secret string `json:"secret"`
			}{mode, secret})
			return "signer.switch", params, socketPath, nil
		}
		if len(args) == 4 && args[1] == "switch" && (args[2] == "nsec" || args[2] == "bunker") && args[3] == "--secret-stdin" {
			mode := args[2]
			limit := 256
			if mode == "bunker" {
				limit = 2048
			}
			secret, err := readSignerSecret(os.Stdin, limit)
			if err != nil {
				return "", nil, "", inputFailure("invalid signer secret source")
			}
			params, _ := json.Marshal(struct {
				Mode   string `json:"mode"`
				Secret string `json:"secret"`
			}{mode, secret})
			return "signer.switch", params, socketPath, nil
		}
		return "", nil, "", inputFailure("invalid signer command")
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
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		// Only install carries an naddr's relay hints (already filtered,
		// deduplicated and capped by napaddr); the daemon refuses them on
		// update, and running install again updates an installed napplet.
		var relays []string
		if args[0] == "install" {
			relays = addr.Relays
		}
		params, _ := json.Marshal(struct {
			Address string   `json:"address"`
			Relays  []string `json:"relays,omitempty"`
		}{addr.Canonical, relays})
		return "napplet." + args[0], params, socketPath, nil
	}
	if len(args) == 2 && args[0] == "launch" {
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
		}{addr.Canonical})
		return "napplet.launch", params, socketPath, nil
	}
	if len(args) == 2 && args[0] == "launch-token" {
		// The token comes from a generated desktop entry's Exec line. It is
		// decoded and checked here, before any dial, and then becomes the
		// same napplet.launch request as `launch ADDRESS`.
		address, err := desktopentry.DecodeToken(args[1])
		if err != nil {
			return "", nil, "", inputFailure("invalid launch token")
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
		}{address})
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
	if len(args) >= 2 && args[0] == "permissions" {
		verb := args[1]
		if verb == "get" && len(args) == 3 {
			addr, err := commandAddress(args[2], false)
			if err != nil {
				return "", nil, "", err
			}
			params, _ := json.Marshal(struct {
				Address string `json:"address"`
			}{addr.Canonical})
			return "napplet.permissions.get", params, socketPath, nil
		}
		if (verb == "set" && (len(args) == 5 || len(args) == 7)) ||
			(verb == "clear" && (len(args) == 4 || len(args) == 6)) {
			addr, err := commandAddress(args[2], false)
			if err != nil {
				return "", nil, "", err
			}
			if !permissionField(args[3]) {
				return "", nil, "", inputFailure("invalid permission command")
			}
			var subject string
			base := 4
			if verb == "set" {
				if args[4] != "allow" && args[4] != "deny" {
					return "", nil, "", inputFailure("invalid permission decision")
				}
				base = 5
			}
			if len(args) == base+2 {
				if args[base] != "--subject" || args[base+1] == "" || strings.HasPrefix(args[base+1], "--") || len(args[base+1]) > 256 {
					return "", nil, "", inputFailure("invalid permission subject")
				}
				subject = args[base+1]
			}
			if args[3] == "dispatch" && subject == "" {
				return "", nil, "", inputFailure("dispatch requires --subject")
			}
			if verb == "set" {
				params, _ := json.Marshal(struct {
					Address    string `json:"address"`
					Permission string `json:"permission"`
					Decision   string `json:"decision"`
					Subject    string `json:"subject,omitempty"`
				}{addr.Canonical, args[3], args[4], subject})
				return "napplet.permissions.set", params, socketPath, nil
			}
			params, _ := json.Marshal(struct {
				Address    string `json:"address"`
				Permission string `json:"permission"`
				Subject    string `json:"subject,omitempty"`
			}{addr.Canonical, args[3], subject})
			return "napplet.permissions.clear", params, socketPath, nil
		}
		return "", nil, "", inputFailure("invalid permission command")
	}
	if len(args) >= 1 && args[0] == "uninstall" {
		flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		yes := flags.Bool("yes", false, "confirm removal")
		if flags.Parse(args[1:]) != nil || len(flags.Args()) != 1 {
			return "", nil, "", inputFailure("usage: uninstall --yes ADDRESS")
		}
		if !*yes {
			return "", nil, "", inputFailure("uninstall requires --yes")
		}
		// A d tag another socket client installed may hold any character;
		// removing it must still work, so the identifier check is skipped.
		addr, err := commandAddress(flags.Args()[0], true)
		if err != nil {
			return "", nil, "", err
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
			Confirm bool   `json:"confirm"`
		}{addr.Canonical, true})
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
	return "", nil, "", inputFailure("unknown or incomplete command; run 'kwakore help' for usage")
}

func readSignerSecret(r io.Reader, limit int) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(limit+2)))
	if err != nil || len(b) > limit+1 {
		return "", errors.New("invalid signer secret")
	}
	secret := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if secret == "" || strings.ContainsAny(secret, "\r\n\x00") {
		return "", errors.New("invalid signer secret")
	}
	if len(secret) > limit {
		return "", errors.New("invalid signer secret")
	}
	return secret, nil
}

func readSignerSecretFile(path string, limit int) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("invalid signer secret file")
	}
	// Walk with directory descriptors so no path component follows a symlink.
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/")
	dirFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", errors.New("invalid signer secret file")
	}
	defer func() { unix.Close(dirFD) }()
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(dirFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return "", errors.New("invalid signer secret file")
		}
		unix.Close(dirFD)
		dirFD = next
	}
	fd, err := unix.Openat(dirFD, parts[len(parts)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", errors.New("invalid signer secret file")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || info.Size() > int64(limit+1) {
		return "", errors.New("invalid signer secret file")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return "", errors.New("invalid signer secret file")
	}
	return readSignerSecret(f, limit)
}

type pairStartResult struct {
	ClientPublicKey string `json:"client_public_key"`
	Relay           string `json:"relay"`
}

func validPairStartResult(result json.RawMessage) (pairStartResult, bool) {
	var out pairStartResult
	if len(result) == 0 || result[0] != '{' || controlprotocol.ValidateNamedParams(result, "client_public_key", "relay") != nil {
		return out, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 2 || json.Unmarshal(fields["client_public_key"], &out.ClientPublicKey) != nil || json.Unmarshal(fields["relay"], &out.Relay) != nil {
		return out, false
	}
	b, err := hex.DecodeString(out.ClientPublicKey)
	if err != nil || len(b) != 32 || bytes.Equal(b, make([]byte, 32)) || hex.EncodeToString(b) != out.ClientPublicKey {
		return out, false
	}
	u, err := url.Parse(out.Relay)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host != strings.ToLower(u.Host) || u.String() != out.Relay {
		return out, false
	}
	return out, true
}

func validPairCancelResult(result json.RawMessage) bool {
	if len(result) == 0 || result[0] != '{' || controlprotocol.ValidateNamedParams(result, "cancelled") != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 1 {
		return false
	}
	return bytes.Equal(fields["cancelled"], []byte("true")) || bytes.Equal(fields["cancelled"], []byte("false"))
}

func localPairURI(clientPublicKey, relay, secret string) string {
	q := url.Values{}
	q.Set("relay", relay)
	q.Set("secret", secret)
	q.Set("perms", "get_public_key,sign_event,nip44_encrypt,nip44_decrypt,nip04_encrypt,nip04_decrypt")
	q.Set("name", "Kwakore")
	return "nostrconnect://" + clientPublicKey + "?" + q.Encode()
}

func validSignerResponse(result json.RawMessage) bool {
	if len(result) == 0 || result[0] != '{' || controlprotocol.ValidateNamedParams(result, "mode", "public_key", "connection_state") != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 3 {
		return false
	}
	var mode, pubkey, state string
	if json.Unmarshal(fields["mode"], &mode) != nil || json.Unmarshal(fields["public_key"], &pubkey) != nil || json.Unmarshal(fields["connection_state"], &state) != nil {
		return false
	}
	if mode != "none" && mode != "nsec" && mode != "bunker" {
		return false
	}
	if state != "connected" && state != "disconnected" {
		return false
	}
	if state == "disconnected" {
		return pubkey == ""
	}
	if mode == "none" {
		return false
	}
	decoded, err := hex.DecodeString(pubkey)
	return len(decoded) == 32 && err == nil && hex.EncodeToString(decoded) == pubkey
}

func permissionField(field string) bool {
	switch field {
	case "sign", "encrypt", "decrypt", "publish", "open_link", "save_file", "copy_text", "upload", "fetch", "notify", "media", "dispatch":
		return true
	}
	return false
}

func validPermissionResponse(method string, params, result json.RawMessage) bool {
	if len(result) == 0 || result[0] != '{' {
		return false
	}
	var requested struct{ Address, Permission, Decision, Subject string }
	if json.Unmarshal(params, &requested) != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil {
		return false
	}
	var address string
	if bytes.Equal(fields["address"], []byte("null")) || json.Unmarshal(fields["address"], &address) != nil || address != requested.Address {
		return false
	}
	if method == "napplet.permissions.get" {
		if len(fields) != 4 || controlprotocol.ValidateNamedParams(result, "address", "required_domains", "optional_domains", "saved_rules") != nil {
			return false
		}
		var domains []string
		for _, name := range []string{"required_domains", "optional_domains"} {
			if raw := fields[name]; len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &domains) != nil {
				return false
			}
		}
		var rules []json.RawMessage
		raw := fields["saved_rules"]
		if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &rules) != nil {
			return false
		}
		for _, rawRule := range rules {
			if len(rawRule) == 0 || rawRule[0] != '{' || controlprotocol.ValidateNamedParams(rawRule, "permission", "subject", "decision") != nil {
				return false
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(rawRule, &fields) != nil || len(fields) != 3 {
				return false
			}
			var permission, subject, decision string
			if bytes.Equal(fields["permission"], []byte("null")) || bytes.Equal(fields["subject"], []byte("null")) || bytes.Equal(fields["decision"], []byte("null")) ||
				json.Unmarshal(fields["permission"], &permission) != nil ||
				json.Unmarshal(fields["subject"], &subject) != nil ||
				json.Unmarshal(fields["decision"], &decision) != nil ||
				!permissionField(permission) || (decision != "allow" && decision != "deny") {
				return false
			}
		}
		return true
	}
	var permission, subject string
	if bytes.Equal(fields["permission"], []byte("null")) || bytes.Equal(fields["subject"], []byte("null")) ||
		json.Unmarshal(fields["permission"], &permission) != nil || permission != requested.Permission ||
		json.Unmarshal(fields["subject"], &subject) != nil || subject != requested.Subject {
		return false
	}
	if method == "napplet.permissions.set" {
		var decision string
		return len(fields) == 4 && controlprotocol.ValidateNamedParams(result, "address", "permission", "subject", "decision") == nil &&
			!bytes.Equal(fields["decision"], []byte("null")) && json.Unmarshal(fields["decision"], &decision) == nil && decision == requested.Decision
	}
	var cleared bool
	return len(fields) == 4 && controlprotocol.ValidateNamedParams(result, "address", "permission", "subject", "cleared") == nil &&
		json.Unmarshal(fields["cleared"], &cleared) == nil
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
		if wantsJSON(os.Args[1:]) {
			writeCLIError(os.Stderr, err)
		} else {
			writeHumanError(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func wantsJSON(args []string) bool {
	for len(args) > 0 {
		switch args[0] {
		case "--json":
			return true
		case "--socket", "--timeout":
			if len(args) < 2 {
				return false
			}
			args = args[2:]
		default:
			return false
		}
	}
	return false
}

func writeHumanError(w io.Writer, err error) {
	var remote rpcFailure
	var address addressFailure
	var input inputFailure
	switch {
	case errors.As(err, &address):
		fmt.Fprintf(w, "Error: %s. Use KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...\n", err)
	case errors.As(err, &input):
		fmt.Fprintf(w, "Error: %s\n", err)
	case errors.As(err, &remote):
		fmt.Fprintf(w, "Error: %s\n", err)
		if data, ok := remote.RPC.Data.(controlprotocol.SessionUnavailableData); ok && data.Reason == "session_unavailable" {
			fmt.Fprintln(w, "A graphical session is required to launch a napplet.")
		}
		if data, ok := remote.RPC.Data.(controlprotocol.PartialCleanupData); ok {
			fmt.Fprintf(w, "Record removed for %s, but cleanup is incomplete.\n", displayValue(data.Address))
		}
	default:
		fmt.Fprintf(w, "Error: %s\n", err)
	}
}

func writeCLIError(w io.Writer, err error) {
	rpcErr := controlprotocol.FixedError(controlprotocol.Unavailable)
	var remote rpcFailure
	var input inputFailure
	var address addressFailure
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
	} else if errors.As(err, &address) {
		rpcErr = controlprotocol.FixedError(controlprotocol.InvalidParams)
		rpcErr.Data = addressErrorData{Reason: string(address), Accepted: napaddr.AcceptedForms}
	} else if errors.As(err, &input) {
		rpcErr = controlprotocol.FixedError(controlprotocol.InvalidParams)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Error *controlprotocol.Error `json:"error"`
	}{rpcErr})
}
