package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"

	"fiatjaf.com/verdana/desktop/internal/instanceipc"
	"verdana/backend"
)

// Single-instance forwarding: the first launcher listens on a channel only
// its own OS user can reach (a 0600 Unix socket in a 0700 directory with the
// peer uid checked, or an owner-only named pipe on Windows; see instanceipc).
// Later invocations hand a command to that process and exit: a plain launch
// opens the manager while a shortcut launch opens its napps.
//
// The protocol is one JSON line {"v":2,"cmd":...,"token":...} answered with
// "ok\n" or "err\n". Anything else (another version, an unknown command, the
// token-only message of the old TCP channel, oversize input) is refused and
// runs nothing.

type instanceCommand struct {
	V       int    `json:"v"`
	Command string `json:"cmd"`
	Token   string `json:"token,omitempty"`
}

const (
	commandOpenManager   = "open-manager"
	commandRunShortcut   = "run-shortcut"
	commandEnsureRunning = "ensure-running"
	commandTryNapplet    = "try-napplet"
)

const (
	// instanceProtocol is the only accepted value of instanceCommand.V.
	instanceProtocol = 2
	// maxInstanceLine caps one request in bytes, the newline included.
	maxInstanceLine = 64 << 10
	// maxInstanceToken caps the token in bytes, after JSON decoding.
	maxInstanceToken = 16 << 10
	// instanceDeadline bounds a whole exchange on either side.
	instanceDeadline = 5 * time.Second
)

// forwardToInstance sends a command to a launcher already running, if there
// is one, answering true when it accepted it (the caller should exit).
func forwardToInstance(dataDir string, msg instanceCommand) bool {
	msg.V = instanceProtocol
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := instanceipc.Dial(ctx, dataDir)
	if err != nil {
		if !errors.Is(err, instanceipc.ErrNoInstance) {
			log.Warn().Err(err).Msg("could not reach the running launcher")
		}
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(instanceDeadline))
	line, err := json.Marshal(msg)
	if err != nil {
		log.Warn().Err(err).Msg("could not encode the instance command")
		return false
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		log.Warn().Err(err).Msg("could not send the instance command")
		return false
	}
	reply, err := bufio.NewReader(io.LimitReader(conn, 16)).ReadString('\n')
	if err != nil {
		log.Warn().Err(err).Msg("the running launcher did not answer")
		return false
	}
	if reply != "ok\n" {
		log.Warn().Str("command", msg.Command).Msg("the running launcher refused the command")
		return false
	}
	return true
}

// startInstanceListener opens the instance channel, so later verdana
// invocations have someone to talk to. main calls it only while holding the
// instancelock. Commands arriving while the backend is still starting wait
// for it (see runInstanceCommand) instead of running into a launcher with no
// napp registry and no host.
func startInstanceListener(dataDir string) func() {
	return listenInstance(dataDir, runInstanceCommand)
}

// listenInstance serves the instance channel, handing every accepted command
// to handle on its own goroutine.
func listenInstance(dataDir string, handle func(instanceCommand)) func() {
	// the TCP channel of older versions published its port here, readable by
	// everyone; nothing reads it any more
	if err := os.Remove(filepath.Join(dataDir, "launcher.port")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn().Err(err).Msg("could not remove the old launcher port file")
	}
	ln, err := instanceipc.Listen(dataDir, func(err error) {
		log.Warn().Err(err).Msg("refused an instance connection from another user")
	})
	if err != nil {
		log.Warn().Err(err).Msg("no instance listener: shortcuts will start a new launcher")
		return func() {}
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if err != nil {
				// e.g. out of file descriptors: back off instead of spinning
				log.Warn().Err(err).Msg("instance listener accept failed")
				time.Sleep(100 * time.Millisecond)
				continue
			}
			go serveForward(conn, handle)
		}
	}()
	return func() { ln.Close() }
}

// serveForward reads one request from conn, answers it and, when accepted,
// runs it through handle.
func serveForward(conn net.Conn, handle func(instanceCommand)) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(instanceDeadline))
	msg, err := readInstanceCommand(conn)
	if err != nil {
		log.Warn().Err(err).Msg("refused an instance command")
		conn.Write([]byte("err\n"))
		return
	}
	if _, err := conn.Write([]byte("ok\n")); err != nil {
		// the sender gave up and will retry or start its own launcher;
		// running it here too would run it twice
		return
	}
	go handle(msg)
}

// readInstanceCommand reads and validates exactly one v2 request line.
func readInstanceCommand(r io.Reader) (instanceCommand, error) {
	var msg instanceCommand
	line, err := bufio.NewReader(io.LimitReader(r, maxInstanceLine)).ReadBytes('\n')
	if err != nil {
		// no newline within the cap: cut short, closed early or oversize
		return msg, fmt.Errorf("no complete request line: %w", err)
	}
	line = line[:len(line)-1]
	if len(line) == 0 {
		return msg, errors.New("empty request")
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return msg, fmt.Errorf("malformed request: %w", err)
	}
	if msg.V != instanceProtocol {
		return msg, fmt.Errorf("unsupported protocol version %d", msg.V)
	}
	switch msg.Command {
	case commandOpenManager, commandRunShortcut, commandEnsureRunning, commandTryNapplet:
	default:
		return msg, fmt.Errorf("unknown command %q", msg.Command)
	}
	if len(msg.Token) > maxInstanceToken {
		return msg, fmt.Errorf("token of %d bytes is over the %d byte cap", len(msg.Token), maxInstanceToken)
	}
	return msg, nil
}

// launcherReady is closed by main once the backend is up. The listener takes
// tokens from the moment it binds its port, which is before that, so a token
// handed over while this launcher is still starting waits here instead of
// running into a backend that has no napp registry, no stores and no host to
// open windows with.
var launcherReady = make(chan struct{})

// runBundleToken opens a bundle token's napps as soon as this launcher can.
// Each token gets its own goroutine, so a second shortcut click is never stuck
// behind a slow first one.
func runBundleToken(token string) {
	<-launcherReady
	runBundleTokenReady(token)
}

func runInstanceCommand(msg instanceCommand) {
	<-launcherReady
	switch msg.Command {
	case commandOpenManager:
		showPrimary()
	case commandRunShortcut:
		runBundleTokenReady(msg.Token)
	case commandEnsureRunning:
		// The caller only wanted to make sure the background process exists.
	case commandTryNapplet:
		backend.TryNappletFromDiscovery(msg.Token)
	default:
		log.Warn().Str("command", msg.Command).Msg("unknown instance command")
	}
}

func tryNappletWhenReady(id string) {
	<-launcherReady
	backend.TryNappletFromDiscovery(id)
}

func runBundleTokenReady(token string) {
	if err := backend.RunShortcutToken(token); err != nil {
		log.Warn().Err(err).Str("token", previewToken(token)).Msg("bundle invocation failed")
		backend.SetFetchErr("shortcut failed: " + err.Error())
	}
}
