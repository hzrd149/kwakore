#!/usr/bin/env bash
# Linux user-service smoke test for Kwakore.
#
#   scripts/smoke-linux-service.sh --activation-only
#
# Builds kwakore-daemon and kwakore from the tracked backend sources into a
# temporary staging directory, renders packaging/systemd/user/*.{socket,service}
# against those binaries, and exercises them under the caller's real per-user
# systemd manager:
#
#   activation  enabling only kwakore.socket leaves the service inactive; the
#               first `kwakore status` starts it and reports ready protocol 1
#   control     systemctl --user status/restart/stop/start stay coherent, the
#               manager-owned socket survives a daemon stop, repeated restarts
#               and a direct foreground start never replace the active socket,
#               inode owner and modes hold, and the user journal records the
#               daemon ready lines
#
# The units are linked and enabled with --runtime only, so nothing is written
# under ~/.config/systemd. The staged service runs with XDG_CONFIG_HOME and
# XDG_DATA_HOME inside the staging directory and an offline configuration, so
# real Kwakore data is never touched. A trap stops and disables both units,
# removes the runtime links and the socket directory, reloads the manager and
# deletes the staging directory on success and on failure.
#
# The script refuses to run when kwakore units or the runtime socket directory
# already exist, rather than disturbing a real installation. It exits non-zero
# when no user manager is reachable; it never reports a pass it did not observe.

set -euo pipefail

case "${1:-}" in
--activation-only) ;;
*)
	echo "usage: $0 --activation-only" >&2
	exit 2
	;;
esac

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

pass() {
	echo "PASS $*"
}

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
units_src="$repo_root/packaging/systemd/user"

# ─── environment preconditions ──────────────────────────────────────────────

[ "$(uname -s)" = Linux ] || fail "Linux is required"
command -v systemctl >/dev/null || fail "systemctl not found; a systemd user manager is required"
command -v go >/dev/null || fail "go not found; it is needed to build the staged binaries"
[ -n "${XDG_RUNTIME_DIR:-}" ] || fail "XDG_RUNTIME_DIR is unset; run inside a user session with a systemd user manager"
manager_state=$(systemctl --user is-system-running 2>/dev/null || true)
case "$manager_state" in
running | degraded) ;;
*) fail "systemd user manager unavailable (state: ${manager_state:-unreachable}); cannot run the activation smoke" ;;
esac

runtime_child="$XDG_RUNTIME_DIR/kwakore"
socket_path="$runtime_child/daemon.sock"
runtime_units="$XDG_RUNTIME_DIR/systemd/user"
uid=$(id -u)

for unit in kwakore.socket kwakore.service; do
	load=$(systemctl --user show -p LoadState --value "$unit" 2>/dev/null || true)
	if [ "$load" != "not-found" ]; then
		fail "$unit is already known to the user manager (LoadState=$load); refusing to modify an existing installation"
	fi
done
[ ! -e "$runtime_child" ] || fail "$runtime_child already exists; stop the running daemon before the smoke test"

# ─── staging and cleanup ────────────────────────────────────────────────────

stage=$(mktemp -d "${TMPDIR:-/tmp}/kwakore-smoke.XXXXXX")
linked=0
start_epoch=$(date +%s)

cleanup() {
	status=$?
	trap - EXIT INT TERM
	if [ "$linked" = 1 ]; then
		# Stop while the unit files are still loaded so RemoveOnStop runs;
		# disabling a linked unit also removes its link and reloads.
		systemctl --user stop kwakore.service kwakore.socket >/dev/null 2>&1 || true
		systemctl --user disable --runtime kwakore.socket >/dev/null 2>&1 || true
		for unit in kwakore.socket kwakore.service; do
			link="$runtime_units/$unit"
			if [ -L "$link" ] && [ "$(readlink "$link")" = "$stage/units/$unit" ]; then
				rm -f "$link"
			fi
		done
		systemctl --user daemon-reload >/dev/null 2>&1 || true
		systemctl --user reset-failed kwakore.service kwakore.socket >/dev/null 2>&1 || true
	fi
	# RemoveOnStop removes the inode; drop the directory the manager created.
	rmdir "$runtime_child" 2>/dev/null || true
	rm -rf "$stage"
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

mkdir -m 0700 "$stage/bin" "$stage/units" "$stage/config" "$stage/data"
(cd "$repo_root/backend" && go build -o "$stage/bin/kwakore-daemon" ./cmd/kwakore-daemon && go build -o "$stage/bin/kwakore" ./cmd/kwakore) ||
	fail "could not build the staged daemon and CLI"
mkdir -m 0700 "$stage/config/kwakore"
# Offline settings keep the smoke deterministic and off the network.
printf '%s\n' '{"relays":[],"blossom_servers":[],"discover_on_user_relays":false}' >"$stage/config/kwakore/config.json"

cp "$units_src/kwakore.socket" "$stage/units/kwakore.socket"
sed "s|@BINDIR@|$stage/bin|g" "$units_src/kwakore.service" >"$stage/units/kwakore.service"
grep -q '@' "$stage/units/kwakore.service" && fail "rendered service still contains a placeholder"
grep -qx "ExecStart=$stage/bin/kwakore-daemon" "$stage/units/kwakore.service" ||
	fail "rendered service ExecStart is not the staged absolute daemon path"
grep -qx 'ListenStream=%t/kwakore/daemon.sock' "$stage/units/kwakore.socket" ||
	fail "socket unit does not listen at the daemon socket path"
# Smoke-only isolation; the shipped template carries no data overrides.
printf '%s\n' "Environment=\"XDG_CONFIG_HOME=$stage/config\" \"XDG_DATA_HOME=$stage/data\"" >>"$stage/units/kwakore.service"
if command -v systemd-analyze >/dev/null; then
	(cd "$stage/units" && systemd-analyze --user verify ./kwakore.socket ./kwakore.service >/dev/null 2>"$stage/verify.log") ||
		{ cat "$stage/verify.log" >&2; fail "systemd-analyze rejected the rendered units"; }
fi

# ─── helpers ────────────────────────────────────────────────────────────────

active_state() {
	systemctl --user show -p ActiveState --value "$1"
}

main_pid() {
	systemctl --user show -p MainPID --value kwakore.service
}

wait_state() {
	local unit=$1 want=$2 i
	for i in $(seq 1 100); do
		[ "$(active_state "$unit")" = "$want" ] && return 0
		sleep 0.1
	done
	fail "$unit did not reach $want (now $(active_state "$unit"))"
}

cli_status() {
	local out
	out=$("$stage/bin/kwakore" status) || fail "kwakore status failed: $out"
	echo "$out" | grep -Eq '"protocol_version": *1([^0-9]|$)' || fail "status lacks protocol_version 1: $out"
	echo "$out" | grep -Eq '"ready": *true' || fail "status is not ready: $out"
}

socket_identity() {
	stat -c '%i %a %u %F' "$socket_path"
}

check_inode_policy() {
	[ "$(stat -c '%a %u %F' "$runtime_child")" = "700 $uid directory" ] ||
		fail "runtime directory is not a 0700 directory owned by $uid: $(stat -c '%a %u %F' "$runtime_child")"
	[ "$(stat -c '%a %u %F' "$socket_path")" = "600 $uid socket" ] ||
		fail "socket is not a 0600 socket owned by $uid: $(stat -c '%a %u %F' "$socket_path")"
}

# ─── activation ─────────────────────────────────────────────────────────────

systemctl --user --quiet link --runtime "$stage/units/kwakore.socket" "$stage/units/kwakore.service" >/dev/null
linked=1
systemctl --user --quiet enable --runtime --now kwakore.socket
wait_state kwakore.socket active
[ "$(active_state kwakore.service)" = inactive ] || fail "kwakore.service was active before the first client"
[ "$(systemctl --user is-enabled kwakore.service 2>/dev/null || true)" != enabled ] ||
	fail "kwakore.service is enabled; only the socket may be enabled"
check_inode_policy
socket_before=$(socket_identity)
cli_status
wait_state kwakore.service active
pass "activation: first kwakore status started kwakore.service through the 0600 user socket"

# ─── control ────────────────────────────────────────────────────────────────

# systemctl stop prints a fixed note while the socket can still re-trigger the
# service; keep it out of the PASS output.
stop_service() {
	systemctl --user stop kwakore.service 2>"$stage/stop.log" || fail "systemctl --user stop failed: $(cat "$stage/stop.log")"
	wait_state kwakore.service inactive
}

systemctl --user status kwakore.service --no-pager >"$stage/status.log" 2>&1 ||
	fail "systemctl --user status kwakore.service reported failure: $(cat "$stage/status.log")"
grep -q 'Active: active (running)' "$stage/status.log" || fail "status does not show active (running)"
first_pid=$(main_pid)
[ "$first_pid" -gt 0 ] || fail "kwakore.service has no main PID"

systemctl --user restart kwakore.service
wait_state kwakore.service active
second_pid=$(main_pid)
[ "$second_pid" -gt 0 ] && [ "$second_pid" != "$first_pid" ] || fail "restart did not replace the daemon process"
cli_status

stop_service
[ "$(active_state kwakore.socket)" = active ] || fail "stopping the daemon deactivated the manager-owned socket"
[ "$(socket_identity)" = "$socket_before" ] || fail "socket inode changed when the daemon stopped"
systemctl --user status kwakore.service --no-pager >/dev/null 2>&1 && fail "status of a stopped service reported success"
cli_status
wait_state kwakore.service active
pass "control: stopped daemon left the socket listening and the next client re-activated it"

stop_service
systemctl --user start kwakore.service
wait_state kwakore.service active
cli_status
[ "$(socket_identity)" = "$socket_before" ] || fail "explicit start replaced the socket inode"

# A direct foreground start beside the managed service must refuse, not rebind.
direct_out=$(XDG_CONFIG_HOME="$stage/config" XDG_DATA_HOME="$stage/data" timeout 10 "$stage/bin/kwakore-daemon" 2>&1) &&
	fail "a direct daemon started beside the managed service: $direct_out"
[ "$(socket_identity)" = "$socket_before" ] || fail "a direct daemon start replaced the managed socket"
[ "$(active_state kwakore.service)" = active ] || fail "a direct daemon start disturbed the managed service"
pass "control: systemctl --user status/restart/stop/start coherent; a direct start beside it refused"

# Rapid repeated restarts: the unit pins StartLimitBurst=5 in 10s. From a reset
# counter five restarts succeed and the sixth fails with systemd's fixed
# message; both units fail, the manager removes its inode instead of anything
# replacing it, and clients get the fixed Unavailable error until reset.
systemctl --user reset-failed kwakore.service
for n in 1 2 3 4 5; do
	systemctl --user restart kwakore.service 2>"$stage/restart.log" ||
		fail "restart $n of 5 failed inside the start limit: $(cat "$stage/restart.log")"
done
systemctl --user restart kwakore.service 2>"$stage/restart.log" && fail "a sixth restart in ten seconds was not rate limited"
grep -q 'start of the service was attempted too often' "$stage/restart.log" ||
	fail "rate-limited restart lacks the fixed message: $(cat "$stage/restart.log")"
wait_state kwakore.service failed
wait_state kwakore.socket failed
[ "$(systemctl --user show -p Result --value kwakore.service)" = start-limit-hit ] || fail "service result is not start-limit-hit"
[ "$(systemctl --user show -p Result --value kwakore.socket)" = service-start-limit-hit ] || fail "socket result is not service-start-limit-hit"
[ ! -e "$socket_path" ] || fail "a socket remained at the path after the manager released it: $(socket_identity)"
unavailable=$("$stage/bin/kwakore" status 2>&1 >/dev/null) && fail "status succeeded with no socket"
[ "$unavailable" = '{"error":{"code":1004,"message":"Unavailable"}}' ] || fail "status without a socket is not the fixed Unavailable error: $unavailable"
systemctl --user reset-failed kwakore.service kwakore.socket
systemctl --user start kwakore.socket
wait_state kwakore.socket active
[ "$(active_state kwakore.service)" = inactive ] || fail "recovered socket started the service without a client"
check_inode_policy
cli_status
wait_state kwakore.service active
pass "control: sixth rapid restart rate limited with fixed messages, no socket replaced, reset-failed recovers activation"

if command -v journalctl >/dev/null; then
	journal=$(journalctl --user -u kwakore.service --since "@$start_epoch" -o cat --no-pager 2>/dev/null || true)
	echo "$journal" | grep -q "kwakore-daemon .* ready (config: $stage/config/kwakore/config.json)" ||
		fail "user journal has no daemon ready line for the staged service"
	echo "$journal" | grep -q 'Stopped kwakore.service' || fail "user journal has no manager stop record"
	pass "control: user journal records daemon ready lines and manager stop records"
else
	fail "journalctl not found; cannot inspect the user journal"
fi
