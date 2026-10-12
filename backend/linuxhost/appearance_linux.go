//go:build linux

package linuxhost

import (
	"context"
	"errors"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	portalBus             = "org.freedesktop.portal.Desktop"
	portalPath            = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	portalSettings        = "org.freedesktop.portal.Settings"
	appearanceNamespace   = "org.freedesktop.appearance"
	appearanceColorScheme = "color-scheme"
)

// WatchColorScheme follows the desktop's light/dark preference through the
// XDG desktop portal on the session bus. It reads the one public key
// org.freedesktop.appearance color-scheme, calls changed with the initial
// value and with every later change, and sends nothing else. It blocks until
// ctx ends and then returns nil. An error means there is no session bus or
// the portal's change signal could not be followed; changed is then never
// called again.
func WatchColorScheme(ctx context.Context, changed func(dark bool)) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	// subscribe first, so a change between the read and the match is not lost
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(portalPath),
		dbus.WithMatchInterface(portalSettings),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		return err
	}
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	if dark, ok := readColorScheme(ctx, conn.Object(portalBus, portalPath)); ok {
		changed(dark)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, open := <-signals:
			if !open {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("session bus closed")
			}
			if sig == nil || sig.Path != portalPath || sig.Name != portalSettings+".SettingChanged" || len(sig.Body) != 3 {
				continue
			}
			namespace, _ := sig.Body[0].(string)
			key, _ := sig.Body[1].(string)
			if namespace != appearanceNamespace || key != appearanceColorScheme {
				continue
			}
			if dark, ok := colorSchemeDark(sig.Body[2]); ok {
				changed(dark)
			}
		}
	}
}

// readColorScheme asks the portal once, through ReadOne, else through the
// older Read that wraps the value in one more variant.
func readColorScheme(ctx context.Context, obj dbus.BusObject) (dark, ok bool) {
	for _, method := range []string{"ReadOne", "Read"} {
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var v dbus.Variant
		err := obj.CallWithContext(callCtx, portalSettings+"."+method, 0, appearanceNamespace, appearanceColorScheme).Store(&v)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return false, false
			}
			continue
		}
		return colorSchemeDark(v)
	}
	return false, false
}

// colorSchemeDark maps the portal's color-scheme value to dark or light. The
// value is a uint32: 1 prefers dark, 0 (no preference) and 2 prefer light.
// Up to two variant layers are unwrapped; anything else is not ok.
func colorSchemeDark(v any) (dark, ok bool) {
	for i := 0; i < 2; i++ {
		variant, isVariant := v.(dbus.Variant)
		if !isVariant {
			break
		}
		v = variant.Value()
	}
	n, isUint := v.(uint32)
	if !isUint {
		return false, false
	}
	switch n {
	case 1:
		return true, true
	case 0, 2:
		return false, true
	}
	return false, false
}
