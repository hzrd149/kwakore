//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"verdana/backend/daemon"
	"verdana/backend/serviceconfig"
)

var version = "development"

func run(args []string) error {
	paths, err := serviceconfig.ResolvePaths()
	if err != nil {
		return err
	}
	if len(args) > 0 {
		switch args[0] {
		case "version":
			fmt.Fprintln(os.Stdout, version)
			return nil
		case "validate", "status", "diagnostics":
			manager, err := serviceconfig.Load(paths)
			if err != nil {
				return err
			}
			if args[0] == "validate" {
				fmt.Fprintln(os.Stdout, "valid")
				return nil
			}
			result := struct {
				ObservedFrom  string                  `json:"observed_from"`
				Ready         *bool                   `json:"ready"`
				UptimeSeconds *float64                `json:"uptime_seconds"`
				ActiveWindows *int                    `json:"active_windows"`
				ConfigStatus  string                  `json:"config_status"`
				Settings      serviceconfig.Effective `json:"settings"`
			}{ObservedFrom: "files", ConfigStatus: "valid", Settings: manager.Effective()}
			return json.NewEncoder(os.Stdout).Encode(result)
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// SIGHUP is serviced on the running Service below; the foreground run
	// helper remains available to integration callers.
	service, err := daemon.Open(paths, version)
	if err != nil {
		return err
	}
	defer service.Close()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	fmt.Fprintf(os.Stdout, "kwakore-daemon %s ready (config: %s)\n", version, paths.ConfigFile)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-hup:
			if err := service.Reload(); err == nil {
				fmt.Fprintln(os.Stderr, "configuration reloaded")
			}
		}
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
