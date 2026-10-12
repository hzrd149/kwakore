//go:build linux

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

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
	if topic, requested := helpTopic(args); requested {
		if topic == "" {
			return inputFailure("unknown help topic; run 'kwak help' for commands")
		}
		_, err := io.WriteString(os.Stdout, topic)
		return err
	}
	if len(args) == 1 && args[0] == "tui" {
		if jsonOutput {
			return inputFailure("--json is not available with tui")
		}
		return runTUI(socketOverride, timeoutOverride)
	}
	method, params, _, err := command(args)
	var lookup snapshotLookup
	if errors.As(err, &lookup) {
		// a note1 or an author-less nevent names only the snapshot's event:
		// its canonical address comes from one cached catalog lookup on a
		// connection of its own, then the command is read again with it
		address, lookupErr := resolveSnapshot(socketOverride, timeoutOverride, lookup.ID)
		if lookupErr != nil {
			return lookupErr
		}
		resolved := slices.Clone(args)
		if i := slices.Index(resolved, lookup.Arg); i >= 0 {
			resolved[i] = address
		}
		method, params, _, err = command(resolved)
	}
	if err != nil {
		return err
	}
	// A desktop entry always reaches the standard user socket, the one
	// kwakore.socket owns, so systemd can start the daemon on demand.
	if len(args) > 0 && args[0] == "launch-token" && socketOverride != "" {
		return inputFailure("launch-token uses the standard user socket")
	}
	conn, err := dialDaemon(socketOverride)
	if err != nil {
		return err
	}
	defer conn.Close()
	if method == "events.subscribe" {
		return runEvents(conn, params, timeoutOverride, jsonOutput, os.Stdout)
	}
	result, err := exchange(conn, method, params, commandDeadline(method, timeoutOverride))
	if err != nil {
		return err
	}
	if strings.HasPrefix(method, "napplet.permissions.") && !validPermissionResponse(method, params, result) {
		return errors.New("invalid daemon response")
	}
	if method == "napplet.icon" {
		pngData, ok := validIconResult(result)
		if !ok {
			return errors.New("invalid daemon response")
		}
		if !jsonOutput {
			return writeIcon(pngData, iconOutPath(args))
		}
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

// dialDaemon connects to the daemon's socket: the --socket path, or the
// standard user socket under a private XDG_RUNTIME_DIR. The peer must run as
// the current user.
func dialDaemon(socketOverride string) (*net.UnixConn, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if socketOverride == "" && (runtimeDir == "" || !filepath.IsAbs(runtimeDir)) {
		return nil, errors.New("XDG_RUNTIME_DIR must name an absolute private 0700 directory")
	}
	if socketOverride == "" {
		info, err := os.Lstat(runtimeDir)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("XDG_RUNTIME_DIR must name an absolute private 0700 directory")
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) {
			return nil, errors.New("XDG_RUNTIME_DIR must be owned by the current user")
		}
	}
	socketPath := socketOverride
	if socketPath == "" {
		socketPath = filepath.Join(runtimeDir, "kwakore", "daemon.sock")
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, errors.New("daemon unavailable")
	}
	uid, err := serverPeerUID(conn)
	if err != nil || uid != uint32(os.Geteuid()) {
		conn.Close()
		return nil, errors.New("unauthorized server")
	}
	return conn, nil
}

// commandDeadline is how long the CLI waits for a method's answer.
func commandDeadline(method string, override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	switch method {
	case "napplet.install", "napplet.update", "napplet.select", "napplet.uninstall", "napplet.try", "signer.pair.wait":
		return 180 * time.Second
	case "napplet.discover", "napplet.versions", "napplet.snapshots":
		// a refresh is bounded by the 30s relay cap plus a short drain
		return 45 * time.Second
	case "napplet.updates.check":
		// the daemon bounds the relay lookups at 60s and the call at 75s
		return 90 * time.Second
	case "napplet.social":
		// the daemon bounds a refresh at 60s
		return 75 * time.Second
	case "napplet.vote", "napplet.react", "napplet.comment", "napplet.delete":
		// the daemon bounds a social action at 60s
		return 75 * time.Second
	}
	return 30 * time.Second
}

// exchange sends one request on conn and returns its result once the
// response line is a well-formed answer to it. A remote error becomes an
// rpcFailure with fixed text and only the data the method may carry.
func exchange(conn *net.UnixConn, method string, params json.RawMessage, deadline time.Duration) (json.RawMessage, error) {
	_ = conn.SetDeadline(time.Now().Add(deadline))
	request, err := json.Marshal(controlprotocol.Request{JSONRPC: "2.0", Method: method, Params: params, ID: json.RawMessage("1")})
	if err != nil {
		return nil, errors.New("invalid command parameters")
	}
	request = append(request, '\n')
	if n, err := conn.Write(request); err != nil || n != len(request) {
		if isTimeout(err) {
			return nil, timeoutFailure{}
		}
		return nil, errors.New("daemon unavailable")
	}
	line, err := bufio.NewReader(io.LimitReader(conn, controlprotocol.MaxResponseLine+2)).ReadBytes('\n')
	if len(line) > controlprotocol.MaxResponseLine+1 {
		return nil, errors.New("invalid daemon response")
	}
	if err != nil {
		if isTimeout(err) {
			return nil, timeoutFailure{}
		}
		if len(line) != 0 {
			return nil, errors.New("invalid daemon response")
		}
		return nil, errors.New("daemon unavailable")
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return nil, errors.New("invalid daemon response")
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(line, &response) != nil || response == nil ||
		!bytes.Equal(response["jsonrpc"], []byte(`"2.0"`)) || !bytes.Equal(response["id"], []byte("1")) {
		return nil, errors.New("invalid daemon response")
	}
	result, hasResult := response["result"]
	rpcError, hasError := response["error"]
	if hasResult == hasError || len(response) != 3 {
		return nil, errors.New("invalid daemon response")
	}
	if hasError {
		if strings.HasPrefix(method, "signer.") {
			var fields map[string]json.RawMessage
			if controlprotocol.ValidateNamedParams(rpcError, "code", "message") != nil || json.Unmarshal(rpcError, &fields) != nil || len(fields) != 2 || fields["code"] == nil || fields["message"] == nil {
				return nil, errors.New("invalid daemon response")
			}
		}
		var remote controlprotocol.Error
		if json.Unmarshal(rpcError, &remote) != nil || remote.Code == 0 || remote.Message == "" {
			return nil, errors.New("invalid daemon response")
		}
		fixed := controlprotocol.FixedError(remote.Code)
		if remote.Code == controlprotocol.PartialCleanup {
			var fields map[string]json.RawMessage
			if json.Unmarshal(rpcError, &fields) != nil {
				return nil, errors.New("invalid daemon response")
			}
			data, ok := parsePartialCleanupData(fields["data"], params)
			if method != "napplet.uninstall" || !ok {
				return nil, errors.New("invalid daemon response")
			}
			fixed.Data = data
		}
		if remote.Code == controlprotocol.Unavailable {
			var fields map[string]json.RawMessage
			if json.Unmarshal(rpcError, &fields) != nil {
				return nil, errors.New("invalid daemon response")
			}
			if raw, ok := fields["data"]; ok {
				data, valid := parseUnavailableData(raw)
				if !valid || data.Reason != unavailableReason(method) {
					return nil, errors.New("invalid daemon response")
				}
				fixed.Data = data
			}
		}
		return nil, rpcFailure{RPC: *fixed}
	}
	if len(result) == 0 {
		return nil, errors.New("invalid daemon response")
	}
	return result, nil
}

// snapshotLookup is an ADDRESS that names only a snapshot's event (a note1
// or an nevent without its author). ID is the event id hex and Arg the
// argument as given; neither is ever printed.
type snapshotLookup struct{ ID, Arg string }

func (snapshotLookup) Error() string { return "snapshot address needs a lookup" }

// resolveSnapshot finds the canonical address of the snapshot with event id
// id through one cached napplet.discover lookup on its own connection. It
// must name exactly one item, a canonical 5129 address ending in the id;
// anything else is the address reason unknown_snapshot.
func resolveSnapshot(socketOverride string, timeoutOverride time.Duration, id string) (string, error) {
	conn, err := dialDaemon(socketOverride)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	params, _ := json.Marshal(struct {
		Query string `json:"query"`
		Kind  int    `json:"kind"`
		Limit int    `json:"limit"`
	}{id, 5129, 2})
	result, err := exchange(conn, "napplet.discover", params, commandDeadline("napplet.discover", timeoutOverride))
	if err != nil {
		return "", err
	}
	var page struct {
		Items []struct {
			Address string `json:"address"`
		} `json:"items"`
	}
	if json.Unmarshal(result, &page) != nil || len(page.Items) != 1 {
		return "", addressFailure("unknown_snapshot")
	}
	address := page.Items[0].Address
	if desktopentry.CanonicalAddress(address) != nil || !strings.HasPrefix(address, "5129:") || !strings.HasSuffix(address, ":"+id) {
		return "", addressFailure("unknown_snapshot")
	}
	return address, nil
}

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

// parseUnavailableData is an Unavailable error's data: exactly one reason
// key holding a reason controlprotocol.UnavailableReason accepts, spelled
// plainly.
func parseUnavailableData(raw json.RawMessage) (controlprotocol.SessionUnavailableData, bool) {
	var data controlprotocol.SessionUnavailableData
	if len(raw) == 0 || raw[0] != '{' || controlprotocol.ValidateNamedParams(raw, "reason") != nil {
		return data, false
	}
	var fields map[string]json.RawMessage
	var reason string
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 1 || json.Unmarshal(fields["reason"], &reason) != nil ||
		!controlprotocol.UnavailableReason(reason) || !bytes.Equal(fields["reason"], []byte(`"`+reason+`"`)) {
		return data, false
	}
	data.Reason = reason
	return data, true
}

// socialActionCommand maps the vote, react, comment and unpublish words
// onto their methods. Positional arguments come first, then flags; ids are
// 64 lowercase hex.
func socialActionCommand(args []string) (string, json.RawMessage, error) {
	addr, err := commandAddress(args[1], false)
	if err != nil {
		return "", nil, err
	}
	validID := func(id string) bool {
		decoded, err := hex.DecodeString(id)
		return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == id
	}
	if args[0] == "unpublish" {
		if len(args) != 3 {
			return "", nil, inputFailure("usage: kwak unpublish ADDRESS EVENT_ID")
		}
		if !validID(args[2]) {
			return "", nil, inputFailure("invalid event ID")
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
			EventID string `json:"event_id"`
		}{addr.Canonical, args[2]})
		return "napplet.delete", params, nil
	}
	if len(args) < 3 {
		return "", nil, inputFailure("invalid " + args[0] + " parameters")
	}
	value := args[2]
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	idFlag := "event"
	if args[0] == "comment" {
		idFlag = "reply"
	}
	id := flags.String(idFlag, "", "event id")
	emojiURL := ""
	if args[0] == "react" {
		flags.StringVar(&emojiURL, "url", "", "emoji image url")
	}
	if flags.Parse(args[3:]) != nil || len(flags.Args()) != 0 {
		return "", nil, inputFailure("invalid " + args[0] + " parameters")
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set[idFlag] && !validID(*id) {
		return "", nil, inputFailure("invalid event ID")
	}
	switch args[0] {
	case "vote":
		if value != "up" && value != "down" && value != "none" {
			return "", nil, inputFailure("vote must be up, down or none")
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
			Vote    string `json:"vote"`
			EventID string `json:"event_id,omitempty"`
		}{addr.Canonical, value, *id})
		return "napplet.vote", params, nil
	case "react":
		if value == "" || value == "+" || value == "-" {
			return "", nil, inputFailure("use kwak vote for + and -")
		}
		params, _ := json.Marshal(struct {
			Address  string `json:"address"`
			Emoji    string `json:"emoji"`
			EmojiURL string `json:"emoji_url,omitempty"`
			EventID  string `json:"event_id,omitempty"`
		}{addr.Canonical, value, emojiURL, *id})
		return "napplet.react", params, nil
	}
	if strings.TrimSpace(value) == "" {
		return "", nil, inputFailure("comment text is empty")
	}
	params, _ := json.Marshal(struct {
		Address  string `json:"address"`
		Content  string `json:"content"`
		ParentID string `json:"parent_id,omitempty"`
	}{addr.Canonical, value, *id})
	return "napplet.comment", params, nil
}

// unavailableReason is the Unavailable reason method may carry: a launch
// without a graphical session, or a social action without a signer.
func unavailableReason(method string) string {
	switch method {
	case "napplet.launch", "napplet.try":
		return controlprotocol.ReasonSessionUnavailable
	case "napplet.vote", "napplet.react", "napplet.comment", "napplet.delete":
		return controlprotocol.ReasonSignerUnavailable
	}
	return ""
}

type inputFailure string

func (e inputFailure) Error() string { return string(e) }

// addressFailure is a refused ADDRESS argument. It holds one fixed reason
// (invalid_address, unsupported_nip19, unsafe_identifier or
// unknown_snapshot) and never the argument itself, which may be a secret
// key pasted by mistake.
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
	if errors.Is(err, napaddr.ErrSnapshotLookup) {
		return napaddr.Address{}, snapshotLookup{ID: addr.Identifier, Arg: arg}
	}
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
		if len(args) == 3 && args[1] == "switch" && args[2] == "system" {
			return "signer.switch", json.RawMessage(`{"mode":"system"}`), socketPath, nil
		}
		if len(args) == 5 && args[1] == "switch" && args[2] == "system" && args[3] == "--signer-socket" {
			if !filepath.IsAbs(args[4]) || filepath.Clean(args[4]) != args[4] {
				return "", nil, "", inputFailure("signer socket must be an absolute, clean path")
			}
			params, _ := json.Marshal(struct {
				Mode   string `json:"mode"`
				Socket string `json:"socket"`
			}{"system", args[4]})
			return "signer.switch", params, socketPath, nil
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
	if len(args) >= 2 && (args[0] == "versions" || args[0] == "snapshots") {
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		refresh := flags.Bool("refresh", false, "refresh observed history from relays")
		offset := flags.Int("offset", 0, "page offset")
		limit := flags.Int("limit", 100, "page size")
		if flags.Parse(args[2:]) != nil || len(flags.Args()) != 0 || *offset < 0 || *limit < 1 || *limit > 500 {
			return "", nil, "", inputFailure("invalid history parameters")
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
			Refresh bool   `json:"refresh"`
			Offset  int    `json:"offset"`
			Limit   int    `json:"limit"`
		}{addr.Canonical, *refresh, *offset, *limit})
		return "napplet." + args[0], params, socketPath, nil
	}
	if len(args) >= 2 && args[0] == "social" {
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		flags := flag.NewFlagSet("social", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		refresh := flags.Bool("refresh", false, "refresh from relays first")
		only := flags.String("only", "", "categories to refresh, comma separated")
		if flags.Parse(args[2:]) != nil || len(flags.Args()) != 0 {
			return "", nil, "", inputFailure("invalid social parameters")
		}
		var categories []string
		set := false
		flags.Visit(func(f *flag.Flag) { set = set || f.Name == "only" })
		if set {
			for _, name := range strings.Split(*only, ",") {
				if (name != "reactions" && name != "comments") || slices.Contains(categories, name) {
					return "", nil, "", inputFailure("invalid social categories")
				}
				categories = append(categories, name)
			}
		}
		params, _ := json.Marshal(struct {
			Address    string   `json:"address"`
			Refresh    bool     `json:"refresh"`
			Categories []string `json:"categories,omitempty"`
		}{addr.Canonical, *refresh, categories})
		return "napplet.social", params, socketPath, nil
	}
	if len(args) >= 2 && (args[0] == "reactions" || args[0] == "comments") {
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		idFlag, idField := "event", "event_id"
		if args[0] == "comments" {
			idFlag, idField = "parent", "parent_id"
		}
		id := flags.String(idFlag, "", "event id")
		offset := flags.Int("offset", 0, "page offset")
		limit := flags.Int("limit", 100, "page size")
		if flags.Parse(args[2:]) != nil || len(flags.Args()) != 0 || *offset < 0 || *limit < 1 || *limit > 500 {
			return "", nil, "", inputFailure("invalid " + args[0] + " parameters")
		}
		fields := map[string]any{"address": addr.Canonical, "offset": *offset, "limit": *limit}
		set := false
		flags.Visit(func(f *flag.Flag) { set = set || f.Name == idFlag })
		if set {
			decoded, err := hex.DecodeString(*id)
			if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != *id {
				return "", nil, "", inputFailure("invalid event ID")
			}
			fields[idField] = *id
		}
		params, _ := json.Marshal(fields)
		return "napplet." + args[0], params, socketPath, nil
	}
	if len(args) >= 2 && (args[0] == "vote" || args[0] == "react" || args[0] == "comment" || args[0] == "unpublish") {
		method, params, err := socialActionCommand(args)
		if err != nil {
			return "", nil, "", err
		}
		return method, params, socketPath, nil
	}
	if len(args) == 1 && args[0] == "windows" {
		return "napplet.windows", nil, socketPath, nil
	}
	if len(args) >= 1 && args[0] == "discover" {
		flags := flag.NewFlagSet("discover", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		query := flags.String("query", "", "catalog query")
		refresh := flags.Bool("refresh", false, "wait for relay refresh")
		offset := flags.Int("offset", 0, "page offset")
		limit := flags.Int("limit", 100, "page size")
		archetype := flags.String("archetype", "", "archetype")
		intent := flags.String("intent", "", "intent id")
		domain := flags.String("domain", "", "required or optional NAP domain")
		compatible := flags.Bool("compatible", false, "only napplets with no missing domain")
		kind := flags.Int("kind", 0, "manifest kind")
		if flags.Parse(args[1:]) != nil || len(flags.Args()) != 0 || len(*query) > 4096 || *offset < 0 || *limit < 1 || *limit > 500 {
			return "", nil, "", inputFailure("invalid discover parameters")
		}
		// only the filters given are sent, so a plain discover sends
		// exactly what it always did
		set := map[string]bool{}
		flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
		var filter struct {
			Query      string  `json:"query"`
			Refresh    bool    `json:"refresh"`
			Offset     int     `json:"offset"`
			Limit      int     `json:"limit"`
			Archetype  *string `json:"archetype,omitempty"`
			Intent     *string `json:"intent,omitempty"`
			Domain     *string `json:"domain,omitempty"`
			Compatible *bool   `json:"compatible,omitempty"`
			Kind       *int    `json:"kind,omitempty"`
		}
		filter.Query, filter.Refresh, filter.Offset, filter.Limit = *query, *refresh, *offset, *limit
		if set["archetype"] {
			if !discoverToken(*archetype) {
				return "", nil, "", inputFailure("invalid discover parameters")
			}
			filter.Archetype = archetype
		}
		if set["domain"] {
			if !discoverToken(*domain) {
				return "", nil, "", inputFailure("invalid discover parameters")
			}
			filter.Domain = domain
		}
		if set["intent"] {
			if !discoverIntent(*intent) {
				return "", nil, "", inputFailure("invalid discover parameters")
			}
			filter.Intent = intent
		}
		if set["compatible"] {
			filter.Compatible = compatible
		}
		if set["kind"] {
			switch *kind {
			case 5129, 15129, 35129, 35130:
				filter.Kind = kind
			default:
				return "", nil, "", inputFailure("invalid discover parameters")
			}
		}
		params, _ := json.Marshal(filter)
		return "napplet.discover", params, socketPath, nil
	}
	if len(args) >= 2 && (args[0] == "select" || args[0] == "install" && len(args) > 2) {
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		var id string
		if args[0] == "select" {
			if len(args) != 3 {
				return "", nil, "", inputFailure("select requires ADDRESS and EVENT_ID")
			}
			id = args[2]
		} else {
			flags := flag.NewFlagSet("install", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			eventID := flags.String("event", "", "exact manifest event ID")
			if flags.Parse(args[2:]) != nil || len(flags.Args()) != 0 {
				return "", nil, "", inputFailure("invalid install parameters")
			}
			id = *eventID
		}
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != id {
			return "", nil, "", inputFailure("invalid manifest event ID")
		}
		params := map[string]any{"address": addr.Canonical, "event_id": id}
		if args[0] == "install" && len(addr.Relays) > 0 {
			params["relays"] = addr.Relays
		}
		raw, _ := json.Marshal(params)
		return "napplet." + args[0], raw, socketPath, nil
	}
	if len(args) == 2 && (args[0] == "install" || args[0] == "update" || args[0] == "try") {
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		// Only install and try carry an naddr's relay hints (already
		// filtered, deduplicated and capped by napaddr); the daemon refuses
		// them on update, and running install again updates an installed
		// napplet.
		var relays []string
		if args[0] == "install" || args[0] == "try" {
			relays = addr.Relays
		}
		params, _ := json.Marshal(struct {
			Address string   `json:"address"`
			Relays  []string `json:"relays,omitempty"`
		}{addr.Canonical, relays})
		return "napplet." + args[0], params, socketPath, nil
	}
	if len(args) >= 1 && args[0] == "events" {
		types, err := eventTypeArgs(args[1:])
		if err != nil {
			return "", nil, "", err
		}
		if len(types) == 0 {
			return "events.subscribe", nil, socketPath, nil
		}
		params, _ := json.Marshal(struct {
			Types []string `json:"types"`
		}{types})
		return "events.subscribe", params, socketPath, nil
	}
	if len(args) >= 1 && args[0] == "updates" {
		switch len(args) {
		case 1:
			return "napplet.updates.check", nil, socketPath, nil
		case 2:
			addr, err := commandAddress(args[1], false)
			if err != nil {
				return "", nil, "", err
			}
			params, _ := json.Marshal(struct {
				Address string `json:"address"`
			}{addr.Canonical})
			return "napplet.updates.check", params, socketPath, nil
		}
		return "", nil, "", inputFailure("usage: kwak updates [ADDRESS]")
	}
	if len(args) >= 1 && args[0] == "detail" {
		if len(args) != 2 {
			return "", nil, "", inputFailure("usage: kwak detail ADDRESS")
		}
		addr, err := commandAddress(args[1], false)
		if err != nil {
			return "", nil, "", err
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
		}{addr.Canonical})
		return "napplet.detail", params, socketPath, nil
	}
	if len(args) >= 1 && args[0] == "icon" {
		arg, _, err := iconArgs(args[1:])
		if err != nil {
			return "", nil, "", err
		}
		addr, err := commandAddress(arg, false)
		if err != nil {
			return "", nil, "", err
		}
		params, _ := json.Marshal(struct {
			Address string `json:"address"`
		}{addr.Canonical})
		return "napplet.icon", params, socketPath, nil
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
		case "inspect":
			if len(args) == 2 {
				return "settings.inspect", nil, socketPath, nil
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
				// a bare word is taken as the sync_id or theme string, like
				// the URL scheme normalization below
				if (args[2] == "sync_id" || args[2] == "theme") && !strings.HasPrefix(args[3], `"`) {
					value, _ = json.Marshal(args[3])
				}
				if !validSettingValue(args[2], value) {
					return "", nil, "", inputFailure("invalid JSON setting value")
				}
				value = normalizeSettingURLs(args[2], value)
				params, _ := json.Marshal(struct {
					Field string          `json:"field"`
					Value json.RawMessage `json:"value"`
				}{args[2], value})
				return "settings.set", params, socketPath, nil
			}
		}
	}
	return "", nil, "", inputFailure("unknown or incomplete command; run 'kwak help' for usage")
}

// discoverToken is the daemon's archetype and domain filter rule: a
// lowercase token of at most 64 bytes.
func discoverToken(s string) bool {
	if s == "" || len(s) > 64 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// discoverIntent is the daemon's intent filter rule: 1–256 bytes of valid
// UTF-8 with no control or format characters.
func discoverIntent(s string) bool {
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

const iconUsage = "usage: kwak icon ADDRESS [--out FILE]"

// iconArgs reads `icon ADDRESS [--out FILE]`, with --out before or after
// the ADDRESS. FILE must be an absolute, clean path.
func iconArgs(args []string) (address, out string, err error) {
	var addresses []string
	outSet := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--out":
			if outSet || i+1 >= len(args) {
				return "", "", inputFailure(iconUsage)
			}
			out, outSet = args[i+1], true
			if !filepath.IsAbs(out) || filepath.Clean(out) != out {
				return "", "", inputFailure("--out requires an absolute, clean path")
			}
			i++
		case strings.HasPrefix(args[i], "-"):
			return "", "", inputFailure(iconUsage)
		default:
			addresses = append(addresses, args[i])
		}
	}
	if len(addresses) != 1 {
		return "", "", inputFailure(iconUsage)
	}
	return addresses[0], out, nil
}

// iconOutPath is the --out FILE of an icon command, or "".
func iconOutPath(args []string) string {
	if len(args) == 0 || args[0] != "icon" {
		return ""
	}
	_, out, err := iconArgs(args[1:])
	if err != nil {
		return ""
	}
	return out
}

// validIconResult checks a napplet.icon result before anything is printed
// or written, and returns the PNG it carries: an object of exactly sha256
// (64 lowercase hex), mime "image/png", integer width and height of 1–1024
// and data, standard base64 of bytes that decode as a PNG of that size.
func validIconResult(result json.RawMessage) ([]byte, bool) {
	if len(result) == 0 || result[0] != '{' || controlprotocol.ValidateNamedParams(result, "sha256", "mime", "width", "height", "data") != nil {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(result, &fields) != nil || len(fields) != 5 {
		return nil, false
	}
	var sha, mime, data string
	var width, height int
	if json.Unmarshal(fields["sha256"], &sha) != nil || json.Unmarshal(fields["mime"], &mime) != nil ||
		json.Unmarshal(fields["data"], &data) != nil ||
		json.Unmarshal(fields["width"], &width) != nil || json.Unmarshal(fields["height"], &height) != nil {
		return nil, false
	}
	decodedSha, err := hex.DecodeString(sha)
	if err != nil || len(decodedSha) != 32 || hex.EncodeToString(decodedSha) != sha || mime != "image/png" ||
		width < 1 || width > 1024 || height < 1 || height > 1024 {
		return nil, false
	}
	pngData, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil || !bytes.HasPrefix(pngData, []byte("\x89PNG\r\n\x1a\n")) {
		return nil, false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(pngData))
	if err != nil || format != "png" || cfg.Width != width || cfg.Height != height {
		return nil, false
	}
	return pngData, true
}

// writeIcon writes a validated icon: to FILE (owner-only) with --out, or to
// stdout when it is not a terminal.
func writeIcon(pngData []byte, out string) error {
	if out != "" {
		if err := os.WriteFile(out, pngData, 0o600); err != nil {
			return inputFailure("could not write the icon file")
		}
		return nil
	}
	if info, err := os.Stdout.Stat(); err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return inputFailure("icon output is binary; use --out FILE or redirect stdout")
	}
	_, err := os.Stdout.Write(pngData)
	return err
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
	if mode != "none" && mode != "nsec" && mode != "bunker" && mode != "system" {
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
	return field == "relays" || field == "blossom_servers" || field == "discover_on_user_relays" || field == "desktop_entries" || field == "gnome_search" || field == "sync_id" || field == "theme"
}

func validSettingValue(field string, value json.RawMessage) bool {
	if !json.Valid(value) || bytes.Equal(value, []byte("null")) {
		return false
	}
	if field == "discover_on_user_relays" || field == "desktop_entries" || field == "gnome_search" {
		var v bool
		return (bytes.Equal(value, []byte("true")) || bytes.Equal(value, []byte("false"))) && json.Unmarshal(value, &v) == nil
	}
	if field == "sync_id" {
		var v string
		return len(value) > 0 && value[0] == '"' && json.Unmarshal(value, &v) == nil
	}
	if field == "theme" {
		// a typo fails here, before dialing
		var v string
		return len(value) > 0 && value[0] == '"' && json.Unmarshal(value, &v) == nil && (v == "system" || v == "light" || v == "dark")
	}
	if len(value) == 0 || value[0] != '[' {
		return false
	}
	var v []string
	return json.Unmarshal(value, &v) == nil
}

// normalizeSettingURLs is a client convenience; the daemon and config file
// still validate and persist fully qualified canonical URLs.
func normalizeSettingURLs(field string, value json.RawMessage) json.RawMessage {
	if field != "relays" && field != "blossom_servers" {
		return value
	}
	var urls []string
	if json.Unmarshal(value, &urls) != nil {
		return value
	}
	scheme := "wss://"
	if field == "blossom_servers" {
		scheme = "https://"
	}
	for i, raw := range urls {
		if !strings.Contains(raw, "://") {
			urls[i] = scheme + raw
		}
	}
	result, err := json.Marshal(urls)
	if err != nil {
		return value
	}
	return result
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
		fmt.Fprintf(w, "Error: %s. Use KIND:PUBKEY_HEX:D, naddr1..., nostr:naddr1..., nevent1..., or note1...\n", err)
	case errors.As(err, &input):
		fmt.Fprintf(w, "Error: %s\n", err)
	case errors.As(err, &remote):
		fmt.Fprintf(w, "Error: %s\n", err)
		if data, ok := remote.RPC.Data.(controlprotocol.SessionUnavailableData); ok && data.Reason == controlprotocol.ReasonSessionUnavailable {
			fmt.Fprintln(w, "A graphical session is required to launch a napplet.")
		}
		if data, ok := remote.RPC.Data.(controlprotocol.SessionUnavailableData); ok && data.Reason == controlprotocol.ReasonSignerUnavailable {
			fmt.Fprintln(w, "A signer is required: set one up with kwak signer.")
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
			if data, ok := remote.RPC.Data.(controlprotocol.SessionUnavailableData); ok && controlprotocol.UnavailableReason(data.Reason) {
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
