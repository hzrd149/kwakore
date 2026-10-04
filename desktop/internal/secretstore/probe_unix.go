//go:build !darwin && !windows

package secretstore

import (
	"context"
	"slices"

	"github.com/godbus/dbus/v5"
)

// secretsBus is the well-known name of the freedesktop Secret Service.
const secretsBus = "org.freedesktop.secrets"

// serviceAvailable reports whether the session bus has a Secret Service
// running or able to start on demand. It uses its own private connection,
// never the shared dbus.SessionBus() that go-keyring uses, and closes it
// before returning; ctx bounds every bus call.
func serviceAvailable(ctx context.Context) bool {
	conn, err := dbus.SessionBusPrivate(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return false
	}
	if err := conn.Hello(); err != nil {
		return false
	}
	bus := conn.BusObject()
	var owned bool
	if err := bus.CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, secretsBus).Store(&owned); err == nil && owned {
		return true
	}
	var activatable []string
	if err := bus.CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return false
	}
	return slices.Contains(activatable, secretsBus)
}
