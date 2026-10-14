//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"kwakore/backend/windowprogram"
)

func runAttached(args []string, timeout time.Duration) error {
	if len(args) != 2 {
		return inputFailure("usage: kwak run ADDRESS")
	}
	_, params, _, err := command([]string{"launch", args[1]})
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(params, &fields)
	fields["attach"] = json.RawMessage("true")
	params, _ = json.Marshal(fields)
	program := windowprogram.DefaultProgramPath()
	if err := windowprogram.CheckProgram(program); err != nil {
		return errors.New("the napp window program is missing or was modified")
	}
	conn, err := dialDaemon("")
	if err != nil {
		return err
	}
	defer conn.Close()
	result, err := exchange(conn, "napplet.launch", params, commandDeadline("napplet.launch", timeout))
	if err != nil {
		return err
	}
	var reply attachedReply
	if json.Unmarshal(result, &reply) != nil || reply.Outcome != "attached" || reply.Window == nil || reply.Window.Instance != reply.WindowID || reply.WindowID == "" || reply.Window.Format != "napplet" {
		return errors.New("invalid daemon response")
	}
	w := reply.Window
	values := map[string]string{
		"KWAKORE_NAPP_ID": w.NappID, "KWAKORE_NAPP_NAME": windowprogram.WindowTitleText(w.Name),
		"KWAKORE_INSTANCE_ID": w.Instance, "KWAKORE_WINDOW_WIDTH": strconv.Itoa(w.Width),
		"KWAKORE_WINDOW_HEIGHT": strconv.Itoa(w.Height), "KWAKORE_NAPP_FORMAT": w.Format,
		"KWAKORE_THEME": w.Theme, "KWAKORE_THEME_VARS": w.ThemeVars, "WEBVIEW_PATH": filepath.Dir(program),
	}
	for key, value := range values {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	_ = conn.SetDeadline(time.Time{})
	file, err := conn.File()
	if err != nil {
		return err
	}
	defer file.Close()
	if err := unix.Dup2(int(file.Fd()), 0); err != nil {
		return err
	}
	if err := unix.Dup2(int(file.Fd()), 1); err != nil {
		return err
	}
	return syscall.Exec(program, []string{program}, os.Environ())
}

type attachedReply struct {
	Outcome  string `json:"outcome"`
	WindowID string `json:"window_id"`
	Window   *struct {
		NappID    string `json:"napp_id"`
		Name      string `json:"name"`
		Instance  string `json:"instance_id"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Format    string `json:"format"`
		Theme     string `json:"theme"`
		ThemeVars string `json:"theme_vars"`
	} `json:"window"`
}
