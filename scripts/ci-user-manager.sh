#!/usr/bin/env bash
# Run a command against a private, throwaway systemd user manager.
#
#   scripts/ci-user-manager.sh COMMAND [ARG...]
#
# CI runners have no login session, so there is no per-user manager for
# `systemctl --user` to reach. This helper starts one for the calling user
# that shares nothing with the runner's own setup, runs COMMAND against it
# and tears it down again:
#
#   /run/kwakore-ci.XXXXXX/            0700, owned by the caller
#       runtime/                       0700, the manager's XDG_RUNTIME_DIR
#       home/                          0700, the manager's HOME and XDG dirs
#   /run/systemd/system/user@kwakore-ci-XXXXXX.service
#                                      runtime unit running `systemd --user`
#                                      as the caller, with its cgroup delegated
#
# The unit is named like the user@UID.service instances logind starts, so
# journald attributes the manager's services to their user units and
# `journalctl --user -u UNIT` finds their output. It lives under /run only,
# is never enabled, and replaces no packaged unit; the runner's home, its
# lingering setting and every other unit are left alone. The parent of the
# runtime directory is root-owned /run, so a napplet child installed below
# XDG_RUNTIME_DIR passes the daemon's path ownership checks.
#
# COMMAND runs as the caller with XDG_RUNTIME_DIR pointing at the private
# runtime directory, DBUS_SESSION_BUS_ADDRESS at its session bus when the
# distribution ships one for user managers (unset otherwise), and
# KWAKORE_CI_MANAGER_HOME at the manager's HOME. HOME and the rest of the
# environment are the caller's own, so build caches keep working; a command
# that must write user units or data where the manager reads them uses
# --runtime units or KWAKORE_CI_MANAGER_HOME.
#
# On exit, success or failure, the helper stops the manager (which stops every
# unit it ran), removes the runtime unit and reloads, and deletes the private
# directory. It exits with COMMAND's status, or 1 when the manager could not
# be set up. Needs Linux with systemd as PID 1, the unified cgroup hierarchy,
# and passwordless sudo for the caller (as on GitHub-hosted runners). It is
# meant for disposable CI machines; locally, run the smoke scripts against
# your own session's user manager instead.

set -euo pipefail

fail() {
	echo "ci-user-manager: $*" >&2
	exit 1
}

note() {
	echo "ci-user-manager: $*" >&2
}

if [ $# -eq 0 ] || [ "$1" = -h ] || [ "$1" = --help ]; then
	echo "usage: $0 COMMAND [ARG...]" >&2
	exit 2
fi

[ "$(uname -s)" = Linux ] || fail "Linux is required"
uid=$(id -u)
gid=$(id -g)
[ "$uid" != 0 ] || fail "run as the unprivileged user whose manager is wanted, not as root"
[ "$(cat /proc/1/comm 2>/dev/null)" = systemd ] || fail "PID 1 is not systemd"
[ -f /sys/fs/cgroup/cgroup.controllers ] || fail "the unified cgroup hierarchy (cgroup v2) is required"
for tool in systemctl sudo mktemp; do
	command -v "$tool" >/dev/null || fail "$tool not found"
done
systemd_bin=""
for candidate in /usr/lib/systemd/systemd /lib/systemd/systemd; do
	if [ -x "$candidate" ]; then
		systemd_bin=$candidate
		break
	fi
done
[ -n "$systemd_bin" ] || fail "the systemd manager binary was not found"
sudo -n true 2>/dev/null || fail "passwordless sudo is required to start the isolated manager"

# ─── private directories ────────────────────────────────────────

base=""
unit=""
unit_file=""
unit_written=0
started=0

cleanup() {
	local status=$?
	trap - EXIT INT TERM
	if [ "$started" = 1 ] && [ "$status" != 0 ]; then
		note "manager log (last 50 lines):"
		sudo -n journalctl -u "$unit" --no-pager -n 50 -o cat >&2 2>/dev/null || true
	fi
	if [ -n "$unit" ]; then
		sudo -n systemctl stop "$unit" >/dev/null 2>&1 || true
	fi
	if [ "$unit_written" = 1 ]; then
		sudo -n rm -f -- "$unit_file"
		sudo -n systemctl daemon-reload >/dev/null 2>&1 || true
		sudo -n systemctl reset-failed "$unit" >/dev/null 2>&1 || true
	fi
	# Only the directory this run created, recognised by its exact shape.
	if [[ "$base" =~ ^/run/kwakore-ci\.[A-Za-z0-9]{6}$ ]] && [ -d "$base" ] && [ ! -L "$base" ]; then
		sudo -n rm -rf --one-file-system -- "$base"
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

base=$(sudo -n mktemp -d /run/kwakore-ci.XXXXXX) || fail "could not create a directory under /run"
[[ "$base" =~ ^/run/kwakore-ci\.[A-Za-z0-9]{6}$ ]] || fail "unexpected private directory $base"
sudo -n chown "$uid:$gid" -- "$base"
sudo -n chmod 0700 -- "$base"
runtime="$base/runtime"
home="$base/home"
mkdir -m 0700 -- "$runtime" "$home"
mkdir -p -- "$home/.config" "$home/.local/share" "$home/.local/state" "$home/.cache"
[ "$(stat -c '%a %u' -- "$runtime")" = "700 $uid" ] || fail "$runtime is not 0700 and owned by $uid"

# ─── manager unit ───────────────────────────────────────────────

unit="user@kwakore-ci-${base##*.}.service"
unit_file="/run/systemd/system/$unit"
[ ! -e "$unit_file" ] || fail "$unit_file already exists"
[ "$(systemctl show -p LoadState --value "$unit" 2>/dev/null)" != loaded ] || fail "$unit is already loaded"

# basic.target brings up the manager's sockets (and its session bus where
# the distribution enables one) without starting desktop session services.
sudo -n tee "$unit_file" >/dev/null <<EOF
# Throwaway user manager for scripts/ci-user-manager.sh; removed on exit.
[Unit]
Description=Kwakore CI user manager ($base)

[Service]
Type=notify
User=$uid
Group=$gid
ExecStart=$systemd_bin --user --unit=basic.target
Environment="XDG_RUNTIME_DIR=$runtime" "HOME=$home"
Environment="XDG_CONFIG_HOME=$home/.config" "XDG_DATA_HOME=$home/.local/share"
Environment="XDG_STATE_HOME=$home/.local/state" "XDG_CACHE_HOME=$home/.cache"
Slice=user.slice
Delegate=yes
KillMode=mixed
TasksMax=infinity
TimeoutStartSec=90s
TimeoutStopSec=30s
EOF
unit_written=1
sudo -n systemctl daemon-reload || fail "could not reload the system manager"
started=1
sudo -n systemctl start "$unit" || fail "could not start the isolated user manager $unit"

export XDG_RUNTIME_DIR="$runtime"
for _ in $(seq 1 100); do
	[ -S "$runtime/systemd/private" ] && break
	sleep 0.1
done
[ -S "$runtime/systemd/private" ] || fail "the user manager did not create $runtime/systemd/private"
state=$(timeout 60 systemctl --user is-system-running --wait 2>/dev/null || true)
case "$state" in
running | degraded) ;;
*) fail "the isolated user manager is not running (state: ${state:-unreachable})" ;;
esac

# The manager hands its environment to the units it starts; check that it
# really runs on the private directories, not on the runner's.
env_block=$(systemctl --user show-environment)
for want in "XDG_RUNTIME_DIR=$runtime" "HOME=$home" "XDG_CONFIG_HOME=$home/.config" "XDG_DATA_HOME=$home/.local/share"; do
	printf '%s\n' "$env_block" | grep -qxF -- "$want" || fail "the isolated manager lacks $want"
done
if [ "$(stat -c '%a %u' -- "$runtime")" != "700 $uid" ]; then
	fail "$runtime changed mode or owner after the manager started"
fi

if [ -S "$runtime/bus" ]; then
	export DBUS_SESSION_BUS_ADDRESS="unix:path=$runtime/bus"
else
	unset DBUS_SESSION_BUS_ADDRESS
fi
export KWAKORE_CI_MANAGER_HOME="$home"
note "user manager $unit ($state) at XDG_RUNTIME_DIR=$runtime"

status=0
"$@" || status=$?
exit "$status"
