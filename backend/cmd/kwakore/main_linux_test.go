//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"kwakore/backend/controlprotocol"
	"kwakore/backend/desktopentry"
)

func TestCLISettingsCommands(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, tc := range []struct {
		args           []string
		method, params string
	}{
		{[]string{"settings", "reload"}, "settings.reload", ""},
		{[]string{"settings", "set", "relays", `[]`}, "settings.set", `{"field":"relays","value":[]}`},
		{[]string{"settings", "set", "blossom_servers", `[]`}, "settings.set", `{"field":"blossom_servers","value":[]}`},
		{[]string{"settings", "set", "discover_on_user_relays", `false`}, "settings.set", `{"field":"discover_on_user_relays","value":false}`},
		{[]string{"settings", "clear", "relays"}, "settings.clear", `{"field":"relays"}`},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			seen := make(chan string, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- err.Error()
					return
				}
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					seen <- err.Error()
					return
				}
				var request struct {
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				_ = json.Unmarshal(line, &request)
				seen <- request.Method + " " + string(request.Params)
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"settings":{"relays":[],"blossom_servers":[],"discover_on_user_relays":false}}}` + "\n"))
			}()
			old := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			callErr := run(append([]string{"--json"}, tc.args...))
			_ = w.Close()
			os.Stdout = old
			out, _ := io.ReadAll(r)
			_ = r.Close()
			if callErr != nil || !strings.Contains(string(out), `"settings":`) {
				t.Fatalf("run: %s %v", out, callErr)
			}
			want := tc.method + " " + tc.params
			if got := <-seen; got != want {
				t.Fatalf("request %q, want %q", got, want)
			}
		})
	}
}

func TestCLISecretInputFileAndArgvBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("nsec-private-sentinel\n"), 0600); err != nil {
		t.Fatal(err)
	}
	method, params, _, err := command([]string{"signer", "switch", "nsec", "--secret-file", path})
	if err != nil || method != "signer.switch" || !strings.Contains(string(params), "nsec-private-sentinel") {
		t.Fatalf("file input: %s %s %v", method, params, err)
	}
	if _, _, _, err := command([]string{"signer", "switch", "nsec", "nsec-private-sentinel"}); err == nil || strings.Contains(err.Error(), "nsec-private-sentinel") {
		t.Fatalf("argv accepted/leaked: %v", err)
	}
	bunker := "bunker://" + strings.Repeat("a", 64) + "?relay=wss%3A%2F%2Fexample.com&secret=private-sentinel"
	if err := os.WriteFile(path, []byte(bunker+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	method, params, _, err = command([]string{"signer", "switch", "bunker", "--secret-file", path})
	if err != nil || method != "signer.switch" || !strings.Contains(string(params), "private-sentinel") {
		t.Fatalf("bunker file input: %s %s %v", method, params, err)
	}
	if _, _, _, err := command([]string{"signer", "switch", "bunker", bunker}); err == nil || strings.Contains(err.Error(), "private-sentinel") {
		t.Fatalf("bunker argv accepted/leaked: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := command([]string{"signer", "switch", "nsec", "--secret-file", path}); err == nil {
		t.Fatal("public file accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "linked")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := command([]string{"signer", "switch", "nsec", "--secret-file", link}); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestSignerLeakMalformedPeerResult(t *testing.T) {
	for _, result := range []string{
		`{"mode":"nsec","public_key":"","connection_state":"disconnected","secret":"private-sentinel"}`,
		`{"mode":"nsec","public_key":"private-sentinel","connection_state":"connected"}`,
		`{"mode":"none","public_key":"","connection_state":"connected"}`,
	} {
		if validSignerResponse(json.RawMessage(result)) {
			t.Fatalf("accepted private/malformed result: %s", result)
		}
	}
}

func TestCLIContractPairStartLocalToken(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "kwakore")
	if out, err := exec.Command("go", "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "pair.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	pub := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, result string
		valid        bool
	}{
		{"valid", `{"client_public_key":"` + pub + `","relay":"wss://example.com"}`, true},
		{"extra secret", `{"client_public_key":"` + pub + `","relay":"wss://example.com","secret":"private-sentinel"}`, false},
		{"query relay", `{"client_public_key":"` + pub + `","relay":"wss://example.com/?secret=private-sentinel"}`, false},
		{"zero key", `{"client_public_key":"` + strings.Repeat("0", 64) + `","relay":"wss://example.com"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan string, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- ""
					return
				}
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadBytes('\n')
				var req controlprotocol.Request
				_ = json.Unmarshal(line, &req)
				var p struct {
					Secret string `json:"secret"`
				}
				_ = json.Unmarshal(req.Params, &p)
				seen <- p.Secret
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + tc.result + `}` + "\n"))
			}()
			cmd := exec.Command(cli, "--json", "--socket", listener.Addr().String(), "signer", "pair", "start")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			secret := <-seen
			if len(secret) != 32 {
				t.Fatalf("CLI did not generate a 16-byte secret: %q", secret)
			}
			if !tc.valid {
				if err == nil || stdout.Len() != 0 || strings.Contains(stderr.String(), secret) || strings.Contains(stderr.String(), "private-sentinel") {
					t.Fatalf("accepted/leaked invalid peer result: %q %q %v", stdout.String(), stderr.String(), err)
				}
				return
			}
			var output struct {
				PairingURI string `json:"pairing_uri"`
				Notice     string `json:"notice"`
			}
			if err != nil || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &output) != nil || !strings.Contains(output.PairingURI, secret) || !strings.Contains(output.PairingURI, pub) || !strings.Contains(output.Notice, "Private") {
				t.Fatalf("local token: %q %q %v", stdout.String(), stderr.String(), err)
			}
		})
	}
}

func TestCLIContract(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "kwakore")
	build := exec.Command("go", "build", "-o", cli, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	windowID := strings.Repeat("b", 32)
	for _, tc := range []struct {
		name           string
		args           []string
		method, params string
	}{
		{"status", []string{"status"}, "service.status", ""},
		{"diagnostics", []string{"diagnostics"}, "service.diagnostics", ""},
		{"get", []string{"settings", "get"}, "settings.get", ""},
		{"reload", []string{"settings", "reload"}, "settings.reload", ""},
		{"signer status", []string{"signer", "status"}, "signer.status", ""},
		{"signer none", []string{"signer", "switch", "none"}, "signer.switch", `{"mode":"none"}`},
		{"signer pair wait", []string{"signer", "pair", "wait"}, "signer.pair.wait", ""},
		{"signer pair cancel", []string{"signer", "pair", "cancel"}, "signer.pair.cancel", ""},
		{"set", []string{"settings", "set", "relays", `[]`}, "settings.set", `{"field":"relays","value":[]}`},
		{"clear", []string{"settings", "clear", "relays"}, "settings.clear", `{"field":"relays"}`},
		{"discover", []string{"discover", "--query", "hello", "--refresh", "--offset", "2", "--limit", "3"}, "napplet.discover", `{"query":"hello","refresh":true,"offset":2,"limit":3}`},
		{"installed", []string{"installed", "--offset", "2", "--limit", "3"}, "napplet.installed", `{"offset":2,"limit":3}`},
		{"install", []string{"install", address}, "napplet.install", `{"address":"` + address + `"}`},
		{"launch", []string{"launch", address}, "napplet.launch", `{"address":"` + address + `"}`},
		{"stop", []string{"stop", windowID}, "napplet.stop", `{"window_id":"` + windowID + `"}`},
		{"permissions get", []string{"permissions", "get", address}, "napplet.permissions.get", `{"address":"` + address + `"}`},
		{"permissions set", []string{"permissions", "set", address, "dispatch", "deny", "--subject", "view"}, "napplet.permissions.set", `{"address":"` + address + `","permission":"dispatch","decision":"deny","subject":"view"}`},
		{"permissions clear", []string{"permissions", "clear", address, "dispatch", "--subject", "view"}, "napplet.permissions.clear", `{"address":"` + address + `","permission":"dispatch","subject":"view"}`},
		{"update", []string{"update", address}, "napplet.update", `{"address":"` + address + `"}`},
		{"uninstall", []string{"uninstall", "--yes", address}, "napplet.uninstall", `{"address":"` + address + `","confirm":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan []byte, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- nil
					return
				}
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadBytes('\n')
				seen <- line
				result := cliContractResult(tc.method, address)
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + result + `}` + "\n"))
			}()
			cmd := exec.Command(cli, append([]string{"--json", "--socket", listener.Addr().String()}, tc.args...)...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			wantResult := cliContractResult(tc.method, address)
			if err != nil || stderr.Len() != 0 || string(out) != wantResult+"\n" {
				t.Fatalf("out=%q stderr=%q err=%v", out, stderr.String(), err)
			}
			var request controlprotocol.Request
			if err := json.Unmarshal(<-seen, &request); err != nil || request.JSONRPC != "2.0" || string(request.ID) != "1" || request.Method != tc.method || string(request.Params) != tc.params {
				t.Fatalf("request=%+v, err=%v", request, err)
			}
		})
	}
	for _, tc := range []struct{ name, response string }{
		{"wrong ID", `{"jsonrpc":"2.0","id":2,"result":true}`},
		{"missing version", `{"id":1,"result":true}`},
		{"both fields", `{"jsonrpc":"2.0","id":1,"result":true,"error":{"code":1004,"message":"Unavailable"}}`},
		{"malformed", `{`},
		{"remote error", `{"jsonrpc":"2.0","id":1,"error":{"code":1006,"message":"secret"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = bufio.NewReader(conn).ReadBytes('\n')
				_, _ = conn.Write([]byte(tc.response + "\n"))
			}()
			cmd := exec.Command(cli, "--json", "--socket", listener.Addr().String(), "status")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err == nil || stdout.Len() != 0 {
				t.Fatalf("accepted response: %q %v", stdout.String(), err)
			}
			var got struct {
				Error controlprotocol.Error `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &got); err != nil || got.Error.Code == 0 || bytes.Contains(stderr.Bytes(), []byte("secret")) || bytes.Count(stderr.Bytes(), []byte{'\n'}) != 1 {
				t.Fatalf("stderr=%q err=%v", stderr.String(), err)
			}
		})
	}
	for _, tc := range []struct {
		name, response string
		wantReason     bool
	}{
		{"headless", `{"jsonrpc":"2.0","id":1,"error":{"code":1004,"message":"private","data":{"reason":"session_unavailable"}}}`, true},
		{"host leak", `{"jsonrpc":"2.0","id":1,"error":{"code":1004,"message":"private","data":{"reason":"session_unavailable","path":"secret"}}}`, false},
		{"bad reason", `{"jsonrpc":"2.0","id":1,"error":{"code":1004,"message":"private","data":{"reason":"secret"}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = bufio.NewReader(conn).ReadBytes('\n')
				_, _ = conn.Write([]byte(tc.response + "\n"))
			}()
			cmd := exec.Command(cli, "--json", "--socket", listener.Addr().String(), "launch", address)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err == nil || stdout.Len() != 0 || bytes.Contains(stderr.Bytes(), []byte("secret")) || bytes.Contains(stderr.Bytes(), []byte("private")) || bytes.Contains(stderr.Bytes(), []byte(`"reason":"session_unavailable"`)) != tc.wantReason {
				t.Fatalf("unsafe launch error: %q %v", stderr.String(), err)
			}
		})
	}
	for _, response := range []string{
		`{"jsonrpc":"2.0","id":2,"result":{"address":"` + address + `","required_domains":[],"optional_domains":[],"saved_rules":[]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"address":"other","required_domains":[],"optional_domains":[],"saved_rules":[]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"address":"` + address + `","required_domains":[],"optional_domains":[],"saved_rules":[{"permission":"sign","decision":"secret"}]}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"address":"` + address + `","required_domains":[],"optional_domains":[],"saved_rules":[{"permission":"sign","subject":null,"decision":"allow"}]}}`,
	} {
		go func() {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			defer conn.Close()
			_, _ = bufio.NewReader(conn).ReadBytes('\n')
			_, _ = conn.Write([]byte(response + "\n"))
		}()
		cmd := exec.Command(cli, "--json", "--socket", listener.Addr().String(), "permissions", "get", address)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":1004`) || strings.Contains(stderr.String(), "secret") {
			t.Fatalf("permission peer response accepted: %q %q %v", stdout.String(), stderr.String(), err)
		}
	}
	for _, args := range [][]string{{"--timeout", "0s", "status"}, {"--timeout", "garbage", "status"}, {"status"}} {
		cmd := exec.Command(cli, append([]string{"--json"}, args...)...)
		cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR=")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err == nil || stdout.Len() != 0 {
			t.Fatalf("accepted %v: %v", args, err)
		}
		var got struct {
			Error controlprotocol.Error `json:"error"`
		}
		if err := json.Unmarshal(stderr.Bytes(), &got); err != nil || got.Error.Code == 0 {
			t.Fatalf("stderr=%q err=%v", stderr.String(), err)
		}
	}
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		time.Sleep(100 * time.Millisecond)
	}()
	cmd := exec.Command(cli, "--json", "--socket", listener.Addr().String(), "--timeout", "10ms", "install", address)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil || stdout.Len() != 0 {
		t.Fatalf("timeout accepted: %v, %q", err, stdout.String())
	}
	var timed struct {
		Error controlprotocol.Error `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &timed); err != nil || timed.Error.Code != controlprotocol.Timeout || !strings.Contains(strings.ToLower(timed.Error.Message), "unknown") || !strings.Contains(strings.ToLower(timed.Error.Message), "installed") {
		t.Fatalf("timeout stderr=%q err=%v", stderr.String(), err)
	}
}

func cliContractResult(method, address string) string {
	switch method {
	case "signer.status", "signer.switch", "signer.pair.wait":
		return `{"mode":"none","public_key":"","connection_state":"disconnected"}`
	case "signer.pair.cancel":
		return `{"cancelled":true}`
	case "napplet.permissions.get":
		return `{"address":"` + address + `","required_domains":[],"optional_domains":[],"saved_rules":[]}`
	case "napplet.permissions.set":
		return `{"address":"` + address + `","permission":"dispatch","subject":"view","decision":"deny"}`
	case "napplet.permissions.clear":
		return `{"address":"` + address + `","permission":"dispatch","subject":"view","cleared":true}`
	}
	return `{"method":"` + method + `"}`
}

func TestCLIContractPeerUIDOverride(t *testing.T) {
	root := t.TempDir()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	old := serverPeerUID
	serverPeerUID = func(*net.UnixConn) (uint32, error) { return uint32(os.Geteuid() + 1), nil }
	defer func() { serverPeerUID = old }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err == nil {
			defer conn.Close()
		}
	}()
	if err := run([]string{"--socket", listener.Addr().String(), "status"}); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("foreign peer accepted: %v", err)
	}
	<-done
}

func TestCLIContractCatalog(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	windowID := strings.Repeat("b", 32)
	commands := [][]string{
		{"status"}, {"diagnostics"}, {"settings", "get"}, {"settings", "reload"},
		{"signer", "status"}, {"signer", "switch", "none"}, {"signer", "pair", "start"}, {"signer", "pair", "wait"}, {"signer", "pair", "cancel"},
		{"settings", "set", "relays", `[]`}, {"settings", "clear", "relays"},
		{"discover"}, {"installed"}, {"install", address}, {"update", address},
		{"uninstall", "--yes", address}, {"launch", address}, {"stop", windowID},
		{"permissions", "get", address},
		{"permissions", "set", address, "sign", "allow"},
		{"permissions", "clear", address, "sign"},
	}
	methods := make([]string, 0, len(commands))
	for _, args := range commands {
		method, _, _, err := command(args)
		if err != nil {
			t.Fatalf("command %v: %v", args, err)
		}
		methods = append(methods, method)
	}
	catalog := controlprotocol.MethodNames()
	slices.Sort(methods)
	slices.Sort(catalog)
	if !slices.Equal(methods, catalog) {
		t.Fatalf("CLI methods %v differ from protocol catalog %v", methods, catalog)
	}
}

func TestCLIPermissionsRejectAmbiguousSyntax(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	for _, args := range [][]string{
		{"permissions", "get", address, "extra"},
		{"permissions", "set", address, "sign", "ask"},
		{"permissions", "set", address, "dispatch", "allow"},
		{"permissions", "set", address, "sign", "allow", "--subject"},
		{"permissions", "set", address, "sign", "allow", "view", "--subject"},
		{"permissions", "clear", address, "sign", "extra"},
		{"permissions", "clear", address, "sign", "--subject", "--other"},
	} {
		if _, _, _, err := command(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestCLIInstalledCommand(t *testing.T) {
	method, params, _, err := command([]string{"installed", "--offset", "2", "--limit", "50"})
	if err != nil || method != "napplet.installed" || string(params) != `{"offset":2,"limit":50}` {
		t.Fatalf("installed command: %s %s %v", method, params, err)
	}
	for _, args := range [][]string{{"installed", "--offset", "-1"}, {"installed", "--limit", "501"}, {"installed", "--limit", "1.5"}, {"installed", "extra"}} {
		if _, _, _, err := command(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestCLIInstallCommand(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	method, params, _, err := command([]string{"install", address})
	if err != nil || method != "napplet.install" || string(params) != `{"address":"`+address+`"}` {
		t.Fatalf("install: %s %s %v", method, params, err)
	}
	if _, _, _, err := command([]string{"install"}); err == nil {
		t.Fatal("missing address accepted")
	}
}

func TestCLIUpdateCommand(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	method, params, _, err := command([]string{"update", address})
	if err != nil || method != "napplet.update" || string(params) != `{"address":"`+address+`"}` {
		t.Fatalf("update: %s %s %v", method, params, err)
	}
	if _, _, _, err := command([]string{"update"}); err == nil {
		t.Fatal("missing address accepted")
	}
}

func TestCLIUninstallRequiresYes(t *testing.T) {
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	if _, _, _, err := command([]string{"uninstall", address}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unconfirmed: %v", err)
	}
	method, params, _, err := command([]string{"uninstall", "--yes", address})
	if err != nil || method != "napplet.uninstall" || string(params) != `{"address":"`+address+`","confirm":true}` {
		t.Fatalf("confirmed: %s %s %v", method, params, err)
	}
}

func TestCLIDiscoveryCommand(t *testing.T) {
	method, params, _, err := command([]string{"discover", "--query", "hello", "--refresh", "--offset", "2", "--limit", "50"})
	if err != nil || method != "napplet.discover" || string(params) != `{"query":"hello","refresh":true,"offset":2,"limit":50}` {
		t.Fatalf("discover command: %s %s %v", method, params, err)
	}
	for _, args := range [][]string{{"discover", "--offset", "-1"}, {"discover", "--limit", "501"}, {"discover", "--refresh=maybe"}, {"discover", "extra"}} {
		if _, _, _, err := command(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestCLISettingsStructuredErrors(t *testing.T) {
	for _, args := range [][]string{
		{"settings", "set", "relays", `null`},
		{"settings", "set", "discover_on_user_relays", `[]`},
		{"settings", "set", "blossom_servers", `[true]`},
	} {
		err := run(args)
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
		var out bytes.Buffer
		writeCLIError(&out, err)
		if !strings.Contains(out.String(), `"code":-32602`) {
			t.Fatalf("error %v: %s", args, out.String())
		}
	}
}

func TestCLISettingsRemoteErrorIsFixed(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":1006,"message":"private-url.invalid/secret"}}` + "\n"))
	}()
	err = run([]string{"settings", "reload"})
	<-done
	if err == nil {
		t.Fatal("remote error accepted")
	}
	var out bytes.Buffer
	writeCLIError(&out, err)
	if !strings.Contains(out.String(), `"code":1006`) || strings.Contains(out.String(), "private-url.invalid") {
		t.Fatalf("unsafe error: %s", out.String())
	}
}

func TestCLIPartialCleanupErrorData(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "kwakore")
	if out, err := exec.Command("go", "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := "35129:" + strings.Repeat("a", 64) + ":app"
	for _, tc := range []struct {
		name, data string
		code       int
		wantCode   int
		wantData   bool
	}{
		{"valid", `{"address":"` + address + `","record_removed":true,"cleanup_complete":false}`, controlprotocol.PartialCleanup, controlprotocol.PartialCleanup, true},
		{"wrong address", `{"address":"35129:` + strings.Repeat("b", 64) + `:app","record_removed":true,"cleanup_complete":false}`, controlprotocol.PartialCleanup, controlprotocol.Unavailable, false},
		{"extra field", `{"address":"` + address + `","record_removed":true,"cleanup_complete":false,"private":"/home/user/secret"}`, controlprotocol.PartialCleanup, controlprotocol.Unavailable, false},
		{"missing field", `{"address":"` + address + `","record_removed":true}`, controlprotocol.PartialCleanup, controlprotocol.Unavailable, false},
		{"false removal", `{"address":"` + address + `","record_removed":false,"cleanup_complete":false}`, controlprotocol.PartialCleanup, controlprotocol.Unavailable, false},
		{"completed cleanup", `{"address":"` + address + `","record_removed":true,"cleanup_complete":true}`, controlprotocol.PartialCleanup, controlprotocol.Unavailable, false},
		{"wrong type", `{"address":"` + address + `","record_removed":"true","cleanup_complete":false}`, controlprotocol.PartialCleanup, controlprotocol.Unavailable, false},
		{"null data", `null`, controlprotocol.PartialCleanup, controlprotocol.Unavailable, false},
		{"other code", `{"address":"` + address + `","record_removed":true,"cleanup_complete":false,"private":"/home/user/secret"}`, controlprotocol.ConfigInvalid, controlprotocol.ConfigInvalid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan []byte, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- nil
					return
				}
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadBytes('\n')
				seen <- line
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":` + fmt.Sprint(tc.code) + `,"message":"private cleanup path /home/user/secret","data":` + tc.data + `}}` + "\n"))
			}()
			cmd := exec.Command(cli, "--json", "--socket", listener.Addr().String(), "uninstall", "--yes", address)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err == nil || stdout.Len() != 0 {
				t.Fatalf("status=%v stdout=%q", err, stdout.String())
			}
			var request controlprotocol.Request
			if err := json.Unmarshal(<-seen, &request); err != nil || request.Method != "napplet.uninstall" || string(request.Params) != `{"address":"`+address+`","confirm":true}` {
				t.Fatalf("request=%+v err=%v", request, err)
			}
			var outer map[string]json.RawMessage
			if err := json.Unmarshal(stderr.Bytes(), &outer); err != nil || len(outer) != 1 || bytes.Count(stderr.Bytes(), []byte{'\n'}) != 1 {
				t.Fatalf("stderr=%q err=%v", stderr.String(), err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(outer["error"], &got); err != nil {
				t.Fatal(err)
			}
			if string(got["code"]) != fmt.Sprint(tc.wantCode) || string(got["message"]) != `"`+controlprotocol.FixedError(tc.wantCode).Message+`"` {
				t.Fatalf("error fields: %s", stderr.String())
			}
			_, hasData := got["data"]
			if hasData != tc.wantData || len(got) != 2+boolInt(tc.wantData) {
				t.Fatalf("error data: %s", stderr.String())
			}
			if tc.wantData && string(got["data"]) != tc.data {
				t.Fatalf("error data mismatch: %s", stderr.String())
			}
			if bytes.Contains(stderr.Bytes(), []byte("private")) || bytes.Contains(stderr.Bytes(), []byte("/home/user/secret")) {
				t.Fatalf("leak: %s", stderr.String())
			}
		})
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestCLIReadMethods(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, tc := range []struct {
		args           []string
		method, result string
	}{
		{[]string{"diagnostics"}, "service.diagnostics", `{"observed_from":"live","warning":"safe"}`},
		{[]string{"settings", "get"}, "settings.get", `{"relays":[],"blossom_servers":[],"discover_on_user_relays":false}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			seen := make(chan string, 1)
			go func() {
				conn, err := listener.AcceptUnix()
				if err != nil {
					seen <- err.Error()
					return
				}
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					seen <- err.Error()
					return
				}
				var request struct {
					Method string `json:"method"`
				}
				_ = json.Unmarshal(line, &request)
				seen <- request.Method
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + tc.result + "}\n"))
			}()
			old := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			callErr := run(append([]string{"--json"}, tc.args...))
			_ = w.Close()
			os.Stdout = old
			out, _ := io.ReadAll(r)
			_ = r.Close()
			if callErr != nil || string(out) != tc.result+"\n" || <-seen != tc.method {
				t.Fatalf("run %v: output=%s err=%v", tc.args, out, callErr)
			}
		})
	}
}

func TestStatusRejectsMismatchedResponseID(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	child := filepath.Join(runtimeDir, "kwakore")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(child, "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		request := make([]byte, 256)
		_, _ = conn.Read(request)
		_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{"protocol_version":1}}` + "\n"))
	}()
	if err := run([]string{"status"}); err == nil || !strings.Contains(err.Error(), "invalid daemon response") {
		t.Fatalf("mismatched ID: %v", err)
	}
	<-done
}

// ─── desktop entry launch tokens ───────────────────────────────────

func TestCLILaunchToken(t *testing.T) {
	root, err := os.MkdirTemp("", "kwl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cli := filepath.Join(root, "kwakore")
	if out, err := exec.Command("go", "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	runtimeDir := filepath.Join(root, "run")
	if err := os.MkdirAll(filepath.Join(runtimeDir, "kwakore"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(runtimeDir, "kwakore", "daemon.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var accepted atomic.Int32
	requests := make(chan string, 4)
	responses := make(chan string, 4)
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				requests <- string(line)
				select {
				case response := <-responses:
					_, _ = conn.Write([]byte(response + "\n"))
				case <-time.After(5 * time.Second):
				}
			}()
		}
	}()
	runCLI := func(args ...string) (string, string, error) {
		cmd := exec.Command(cli, append([]string{"--json"}, args...)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	// The d tag carries everything a desktop entry must never see raw.
	pubkey := "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
	address := "35129:" + pubkey + ":notes\n%f $(id) \"q\" ✓"
	token, err := desktopentry.EncodeToken(address)
	if err != nil {
		t.Fatal(err)
	}
	wantRequest, _ := json.Marshal(controlprotocol.Request{
		JSONRPC: "2.0", Method: "napplet.launch",
		Params: json.RawMessage(mustJSON(t, map[string]string{"address": address})), ID: json.RawMessage("1"),
	})

	t.Run("dispatches one canonical napplet.launch", func(t *testing.T) {
		before := accepted.Load()
		result := `{"address":` + mustJSON(t, address) + `,"window_id":"` + strings.Repeat("ab", 16) + `","outcome":"opened"}`
		responses <- `{"jsonrpc":"2.0","id":1,"result":` + result + `}`
		stdout, stderr, err := runCLI("launch-token", token)
		if err != nil || stderr != "" || stdout != result+"\n" {
			t.Fatalf("launch-token: stdout=%q stderr=%q err=%v", stdout, stderr, err)
		}
		if got := <-requests; got != string(wantRequest)+"\n" {
			t.Fatalf("request %q, want %q", got, wantRequest)
		}
		if n := accepted.Load() - before; n != 1 {
			t.Fatalf("dialed %d times, want 1", n)
		}
	})

	t.Run("headless error is fixed JSON on stderr", func(t *testing.T) {
		responses <- `{"jsonrpc":"2.0","id":1,"error":{"code":1004,"message":"no DISPLAY at /run/user/1000","data":{"reason":"session_unavailable"}}}`
		stdout, stderr, err := runCLI("launch-token", token)
		want := `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}` + "\n"
		if err == nil || stdout != "" || stderr != want {
			t.Fatalf("headless: stdout=%q stderr=%q err=%v", stdout, stderr, err)
		}
		if got := <-requests; got != string(wantRequest)+"\n" {
			t.Fatalf("request %q, want %q", got, wantRequest)
		}
	})

	t.Run("invalid tokens never dial", func(t *testing.T) {
		before := accepted.Load()
		invalid := map[string][]string{
			"raw address":  {"launch-token", address},
			"padded":       {"launch-token", token + "=="},
			"std alphabet": {"launch-token", strings.NewReplacer("-", "+", "_", "/").Replace(token) + "+/"},
			"truncated":    {"launch-token", token[:len(token)-1]},
			"oversized":    {"launch-token", strings.Repeat("A", desktopentry.MaxTokenLen+4)},
			"noncanonical": {"launch-token", base64.RawURLEncoding.EncodeToString([]byte("nostr:" + address))},
			"upper hex":    {"launch-token", base64.RawURLEncoding.EncodeToString([]byte(strings.ToUpper(address)))},
			"bad kind":     {"launch-token", base64.RawURLEncoding.EncodeToString([]byte("1:" + pubkey + ":notes"))},
			"empty":        {"launch-token", ""},
			"missing":      {"launch-token"},
			"extra":        {"launch-token", token, token},
			"socket":       {"--socket", listener.Addr().String(), "launch-token", token},
		}
		wantErr := `{"error":{"code":-32602,"message":"Invalid params"}}` + "\n"
		for name, args := range invalid {
			stdout, stderr, err := runCLI(args...)
			if err == nil || stdout != "" || stderr != wantErr {
				t.Fatalf("%s: stdout=%q stderr=%q err=%v", name, stdout, stderr, err)
			}
		}
		if n := accepted.Load() - before; n != 0 {
			t.Fatalf("invalid tokens dialed %d times", n)
		}
	})

	t.Run("server uid still checked", func(t *testing.T) {
		old := serverPeerUID
		serverPeerUID = func(*net.UnixConn) (uint32, error) { return uint32(os.Geteuid() + 1), nil }
		defer func() { serverPeerUID = old }()
		if err := run([]string{"launch-token", token}); err == nil || err.Error() != "unauthorized server" {
			t.Fatalf("foreign server accepted: %v", err)
		}
		var stderr bytes.Buffer
		writeCLIError(&stderr, errors.New("unauthorized server"))
		if stderr.String() != `{"error":{"code":1004,"message":"Unavailable"}}`+"\n" {
			t.Fatalf("unauthorized stderr %q", stderr.String())
		}
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ─── address forms ───────────────────────────────────────────────────────

const addressErrorJSON = `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":%q,"accepted":["KIND:PUBKEY_HEX:D","naddr1...","nostr:naddr1..."]}}}` + "\n"

func testNaddr(t *testing.T, kind nostr.Kind, d string, relays []string) (canonical, naddr string) {
	t.Helper()
	pk := nostr.MustPubKeyFromHex(strings.Repeat("a", 64))
	return fmt.Sprintf("%d:%s:%s", kind, pk.Hex(), d), nip19.EncodeNaddr(pk, kind, d, relays)
}

// addressCommands are every ADDRESS-taking command with X in place of the
// address, and the params each must send for the canonical address.
func addressCommands(canonical string) []struct {
	args   []string
	method string
	params string
} {
	q := mustJSONString(canonical)
	return []struct {
		args   []string
		method string
		params string
	}{
		{[]string{"install", "X"}, "napplet.install", `{"address":` + q + `}`},
		{[]string{"update", "X"}, "napplet.update", `{"address":` + q + `}`},
		{[]string{"launch", "X"}, "napplet.launch", `{"address":` + q + `}`},
		{[]string{"uninstall", "--yes", "X"}, "napplet.uninstall", `{"address":` + q + `,"confirm":true}`},
		{[]string{"permissions", "get", "X"}, "napplet.permissions.get", `{"address":` + q + `}`},
		{[]string{"permissions", "set", "X", "sign", "allow"}, "napplet.permissions.set", `{"address":` + q + `,"permission":"sign","decision":"allow"}`},
		{[]string{"permissions", "clear", "X", "sign"}, "napplet.permissions.clear", `{"address":` + q + `,"permission":"sign"}`},
	}
}

func withAddress(args []string, address string) []string {
	out := slices.Clone(args)
	out[slices.Index(out, "X")] = address
	return out
}

func mustJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestCLIAddressForms(t *testing.T) {
	canonical, naddr := testNaddr(t, 35129, "n-143146b0d6f", nil)
	root, rootNaddr := testNaddr(t, 15129, "", nil)
	for _, pair := range [][2]string{{canonical, naddr}, {root, rootNaddr}} {
		forms := []string{pair[0], pair[1], "nostr:" + pair[1], "NOSTR:" + pair[1], strings.ToUpper(pair[1]), "nostr:" + strings.ToUpper(pair[1])}
		for _, tc := range addressCommands(pair[0]) {
			for _, form := range forms {
				method, params, _, err := command(withAddress(tc.args, form))
				if err != nil || method != tc.method || string(params) != tc.params {
					t.Fatalf("%v with %q: %s %s %v", tc.args, form, method, params, err)
				}
			}
		}
	}
}

func TestCLIAddressErrors(t *testing.T) {
	canonical, naddr := testNaddr(t, 35129, "notes", nil)
	var id nostr.ID
	id[0] = 7
	pk := nostr.MustPubKeyFromHex(strings.Repeat("a", 64))
	_, tabNaddr := testNaddr(t, 35129, "a\tb", nil)
	rloCanonical, _ := testNaddr(t, 35129, "a\u202eb", nil)
	cases := map[string]struct{ input, reason string }{
		"empty":       {"", "invalid_address"},
		"garbage":     {"hello", "invalid_address"},
		"oversized":   {strings.Repeat("a", 4097), "invalid_address"},
		"mixed case":  {"N" + naddr[1:], "invalid_address"},
		"web link":    {"https://njump.me/" + naddr, "invalid_address"},
		"padded":      {" " + canonical, "invalid_address"},
		"wrong kind":  {strings.Replace(canonical, "35129", "30023", 1), "invalid_address"},
		"npub":        {nip19.EncodeNpub(pk), "unsupported_nip19"},
		"nprofile":    {nip19.EncodeNprofile(pk, nil), "unsupported_nip19"},
		"note":        {"note1" + strings.Repeat("q", 58), "unsupported_nip19"},
		"nevent":      {nip19.EncodeNevent(id, nil, pk), "unsupported_nip19"},
		"nsec":        {nip19.EncodeNsec(nostr.KeyOne), "unsupported_nip19"},
		"nostr nsec":  {"nostr:" + strings.ToUpper(nip19.EncodeNsec(nostr.KeyOne)), "unsupported_nip19"},
		"tab d":       {tabNaddr, "unsafe_identifier"},
		"rlo d":       {rloCanonical, "unsafe_identifier"},
		"nostr tab d": {"nostr:" + tabNaddr, "unsafe_identifier"},
	}
	for name, tc := range cases {
		for _, cmd := range addressCommands(canonical) {
			args := withAddress(cmd.args, tc.input)
			_, _, _, err := command(args)
			want := tc.reason
			if want == "unsafe_identifier" && cmd.method == "napplet.uninstall" {
				continue
			}
			var stderr bytes.Buffer
			writeCLIError(&stderr, err)
			if err == nil || stderr.String() != fmt.Sprintf(addressErrorJSON, want) {
				t.Fatalf("%s %v: err=%v stderr=%q", name, cmd.args, err, stderr.String())
			}
			if tc.input != "" && strings.Contains(stderr.String(), tc.input) {
				t.Fatalf("%s: stderr echoes the input", name)
			}
		}
	}
	// uninstall still removes a record whose d tag holds anything
	for _, input := range []string{tabNaddr, rloCanonical} {
		addr, err := commandAddress(input, true)
		if err != nil {
			t.Fatal(err)
		}
		method, params, _, err := command([]string{"uninstall", "--yes", input})
		if err != nil || method != "napplet.uninstall" || string(params) != `{"address":`+mustJSONString(addr.Canonical)+`,"confirm":true}` {
			t.Fatalf("uninstall %q: %s %s %v", input, method, params, err)
		}
	}
	// a bad address on a well-formed permission command is an address error
	_, _, _, err := command([]string{"permissions", "set", "hello", "nonsense", "allow"})
	var stderr bytes.Buffer
	writeCLIError(&stderr, err)
	if stderr.String() != fmt.Sprintf(addressErrorJSON, "invalid_address") {
		t.Fatalf("permissions: %q", stderr.String())
	}
}

func TestCLIAddressExec(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "kwakore")
	if out, err := exec.Command("go", "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var accepted atomic.Int32
	requests := make(chan string, 4)
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			accepted.Add(1)
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				requests <- string(line)
				_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}` + "\n"))
			}()
		}
	}()
	runCLI := func(args ...string) (string, string, error) {
		cmd := exec.Command(cli, append([]string{"--json", "--socket", listener.Addr().String()}, args...)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	canonical, naddr := testNaddr(t, 35129, "n-143146b0d6f", nil)
	stdout, stderr, err := runCLI("install", "nostr:"+strings.ToUpper(naddr))
	if err != nil || stderr != "" || stdout != "{}\n" {
		t.Fatalf("install: %q %q %v", stdout, stderr, err)
	}
	want, _ := json.Marshal(controlprotocol.Request{
		JSONRPC: "2.0", Method: "napplet.install",
		Params: json.RawMessage(`{"address":` + mustJSONString(canonical) + `}`), ID: json.RawMessage("1"),
	})
	if got := <-requests; got != string(want)+"\n" {
		t.Fatalf("request %q, want %q", got, want)
	}

	before := accepted.Load()
	nsec := nip19.EncodeNsec(nostr.KeyOne)
	for _, tc := range []struct{ input, reason string }{
		{nsec, "unsupported_nip19"},
		{nip19.EncodeNpub(nostr.KeyOne.Public()), "unsupported_nip19"},
		{"hello", "invalid_address"},
		{strings.Repeat("a", 4097), "invalid_address"},
	} {
		stdout, stderr, err := runCLI("install", tc.input)
		if err == nil || stdout != "" || stderr != fmt.Sprintf(addressErrorJSON, tc.reason) || strings.Contains(stderr, tc.input) {
			t.Fatalf("%q: stdout=%q stderr=%q err=%v", tc.input[:min(len(tc.input), 12)], stdout, stderr, err)
		}
	}
	if n := accepted.Load() - before; n != 0 {
		t.Fatalf("refused addresses dialed %d times", n)
	}
}

// The CLI stays small: address parsing must not drag in the backend root
// or the nostr library.
func TestCLIDoesNotLinkBackendOrNostr(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "kwakore/backend" || strings.HasPrefix(line, "fiatjaf.com/nostr") {
			t.Fatalf("CLI links %s", line)
		}
	}
	if !strings.Contains(string(out), "kwakore/backend/napaddr\n") {
		t.Fatal("CLI does not use napaddr")
	}
}

func TestCLIInstallForwardsRelayHints(t *testing.T) {
	canonical, naddr := testNaddr(t, 35129, "n-143146b0d6f", []string{"wss://relay.napplet.soy", "relay.damus.io"})
	q := mustJSONString(canonical)
	for _, form := range []string{naddr, "nostr:" + strings.ToUpper(naddr)} {
		method, params, _, err := command([]string{"install", form})
		if err != nil || method != "napplet.install" || string(params) != `{"address":`+q+`,"relays":["wss://relay.napplet.soy"]}` {
			t.Fatalf("install %q: %s %s %v", form, method, params, err)
		}
	}
	// canonical input and an naddr without usable hints send address only
	_, bare := testNaddr(t, 35129, "n-143146b0d6f", []string{"relay.damus.io"})
	for _, form := range []string{canonical, bare} {
		_, params, _, err := command([]string{"install", form})
		if err != nil || string(params) != `{"address":`+q+`}` {
			t.Fatalf("install %q: %s %v", form, params, err)
		}
	}
	// every other command sends address-only params
	for _, tc := range addressCommands(canonical) {
		if tc.method == "napplet.install" {
			continue
		}
		method, params, _, err := command(withAddress(tc.args, naddr))
		if err != nil || method != tc.method || string(params) != tc.params {
			t.Fatalf("%v: %s %s %v", tc.args, method, params, err)
		}
	}

	root := t.TempDir()
	cli := filepath.Join(root, "kwakore")
	if out, err := exec.Command("go", "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	seen := make(chan string, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			seen <- ""
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadBytes('\n')
		seen <- string(line)
		_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}` + "\n"))
	}()
	cmd := exec.Command(cli, "--json", "--socket", listener.Addr().String(), "install", naddr)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || stderr.Len() != 0 || string(out) != "{}\n" {
		t.Fatalf("out=%q stderr=%q err=%v", out, stderr.String(), err)
	}
	want, _ := json.Marshal(controlprotocol.Request{
		JSONRPC: "2.0", Method: "napplet.install",
		Params: json.RawMessage(`{"address":` + q + `,"relays":["wss://relay.napplet.soy"]}`), ID: json.RawMessage("1"),
	})
	if got := <-seen; got != string(want)+"\n" {
		t.Fatalf("request %q, want %q", got, want)
	}
}

func TestCLIHumanOutputAndJSONOption(t *testing.T) {
	raw := json.RawMessage(`{"items":[{"name":"Notes","address":"35129:abc:notes"}],"total":1,"next_offset":null}`)
	var out bytes.Buffer
	if err := writeCLIResult(&out, "napplet.installed", raw, false); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"Napplet installed\n", "name: Notes", "address: 35129:abc:notes", "total: 1", "next offset: none"} {
		if !strings.Contains(out.String(), part) {
			t.Fatalf("missing %q in %q", part, out.String())
		}
	}
	out.Reset()
	if err := writeCLIResult(&out, "napplet.installed", raw, true); err != nil || out.String() != string(raw)+"\n" {
		t.Fatalf("json: %q %v", out.String(), err)
	}
	args, socket, timeout, jsonOutput, err := globalOptions([]string{"--timeout", "2s", "--json", "--socket", "/tmp/test.sock", "status"})
	if err != nil || !jsonOutput || socket != "/tmp/test.sock" || timeout != 2*time.Second || !slices.Equal(args, []string{"status"}) {
		t.Fatalf("options: %v %q %s %t %v", args, socket, timeout, jsonOutput, err)
	}
	if _, _, _, _, err := globalOptions([]string{"--json", "--json", "status"}); err == nil {
		t.Fatal("duplicate --json accepted")
	}
	out.Reset()
	writeHumanError(&out, addressFailure("invalid_address"))
	if !strings.Contains(out.String(), "naddr1...") || strings.HasPrefix(out.String(), "{") {
		t.Fatalf("human error: %q", out.String())
	}
	out.Reset()
	if err := writeCLIResult(&out, "napplet.installed", json.RawMessage(`{"name":"bad\nname"}`), false); err != nil || strings.Contains(out.String(), "bad\nname") {
		t.Fatalf("control output: %q %v", out.String(), err)
	}
}
