#!/usr/bin/env bash
# Linux user-service smoke test for Kwakore.
#
#   scripts/smoke-linux-service.sh --activation-only
#   scripts/smoke-linux-service.sh --bundle-only
#   scripts/smoke-linux-service.sh --install-only
#
# --activation-only builds kwakore-daemon and kwakore from the tracked backend
# sources into a temporary staging directory, renders
# packaging/systemd/user/*.{socket,service} against those binaries, and
# exercises them under the caller's real per-user systemd manager:
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
#
# --bundle-only needs no user manager. It builds this machine's bundle twice
# with scripts/build-linux-bundle.sh into a temporary output directory and
# checks:
#
#   bundle      both runs leave the same file set and the same archive bytes;
#               the archive holds exactly kwakore-daemon, kwakore, napplet and
#               libwebview.so as regular files in one directory and matches
#               SHA256SUMS; the unpacked files are owner-only-writable regular
#               files with the expected modes, ELF for this architecture, and
#               identical to the archive members
#   child       the bundled napplet child starts and its hardened loader
#               refuses a missing, relative or empty WEBVIEW_PATH instead of
#               falling back to a library search, and the child and library
#               resolve every shared object on this host
#
# --install-only builds the bundle the same way and installs it with
# scripts/install.sh under the caller's real user manager, using a prefix in
# a private staging directory below XDG_RUNTIME_DIR and --runtime-units, so
# nothing is written under the home directory and the units vanish at logout
# at the latest. A runtime drop-in points the installed service at staged,
# offline XDG config and data. It checks:
#
#   install     the helper refuses a prefix napplet windows would refuse;
#               otherwise it installs the four files side by side under the
#               rendered ExecStart, the units equal packaging/systemd/user
#               rendered the way a manual install would, only kwakore.socket is
#               enabled, and the installed CLI activates the daemon; a second
#               run changes no managed file, unit or activation state; a held
#               lock makes the helper wait and two concurrent runs end in the
#               same single layout; a tampered checksum, an extra ../ member
#               and a symlink member are refused before extraction without
#               disturbing the running install; a new archive swaps the
#               release in one rename, restarts the running daemon and prunes
#               releases older than the previous one
#
# A trap stops and disables the units, removes the unit files, drop-in and
# socket directory, reloads the manager and deletes the staging directory.

set -euo pipefail

mode=${1:-}
case "$mode" in
--activation-only | --bundle-only | --install-only) ;;
*)
	echo "usage: $0 --activation-only|--bundle-only|--install-only" >&2
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

[ "$(uname -s)" = Linux ] || fail "Linux is required"

# ─── bundle ─────────────────────────────────────────────────────────────────

bundle_version=0.0.0-smoke
bundle_files=(kwakore-daemon kwakore napplet libwebview.so)

host_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) fail "unsupported machine $(uname -m)" ;;
	esac
}

# file_set lists every path under $1 with its type, mode and content hash, so
# two runs can be compared byte for byte.
file_set() {
	(
		cd "$1"
		find . -mindepth 1 -printf '%y %m %p\n' | LC_ALL=C sort
		find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
	)
}

# elf_machine prints e_machine of an ELF file (little-endian targets only).
elf_machine() {
	[ "$(od -An -tx1 -N4 "$1" | tr -d ' \n')" = 7f454c46 ] || return 1
	od -An -tu2 -j18 -N2 "$1" | tr -d ' \n'
}

check_bundle() {
	local release=$1 arch=$2 name archive bundle f mode want_machine out
	name="kwakore-$bundle_version-linux-$arch"
	archive="kwakore-linux-$arch.tar.gz"
	bundle="$release/$name"
	case "$arch" in
	amd64) want_machine=62 ;;
	arm64) want_machine=183 ;;
	esac

	[ "$(cd "$release" && ls -A | LC_ALL=C sort | tr '\n' ' ')" = ".lock SHA256SUMS $name $archive " ] ||
		fail "unexpected release directory contents: $(cd "$release" && ls -A | tr '\n' ' ')"
	[ "$(wc -l <"$release/SHA256SUMS")" = 1 ] || fail "SHA256SUMS does not name exactly one archive"
	grep -Eq "^[0-9a-f]{64}  $archive\$" "$release/SHA256SUMS" || fail "SHA256SUMS does not name $archive"
	(cd "$release" && sha256sum --quiet --strict -c SHA256SUMS) || fail "archive does not match SHA256SUMS"

	out=$(tar -tzf "$release/$archive" | LC_ALL=C sort | tr '\n' ' ')
	[ "$out" = "$name/kwakore $name/kwakore-daemon $name/libwebview.so $name/napplet " ] ||
		fail "archive members are not exactly the four bundle files: $out"
	tar -tvzf "$release/$archive" | grep -qv '^-' && fail "archive holds a member that is not a regular file"
	mkdir "$stage/unpacked"
	tar -xzf "$release/$archive" -C "$stage/unpacked"

	[ "$(ls -A "$bundle" | LC_ALL=C sort | tr '\n' ' ')" = "kwakore kwakore-daemon libwebview.so napplet " ] ||
		fail "bundle directory is not exactly the four files: $(ls -A "$bundle" | tr '\n' ' ')"
	[ "$(stat -c '%a %u' "$bundle")" = "755 $uid" ] || fail "bundle directory is not 0755 owned by $uid"
	for f in "${bundle_files[@]}"; do
		[ -f "$bundle/$f" ] && [ ! -L "$bundle/$f" ] || fail "$f is not a regular file"
		mode=755
		[ "$f" = libwebview.so ] && mode=644
		[ "$(stat -c '%a %u %h' "$bundle/$f")" = "$mode $uid 1" ] ||
			fail "$f is not mode $mode, owned by $uid with one link: $(stat -c '%a %u %h' "$bundle/$f")"
		[ "$(elf_machine "$bundle/$f")" = "$want_machine" ] || fail "$f is not an ELF file for linux/$arch"
		cmp -s "$bundle/$f" "$stage/unpacked/$name/$f" || fail "$f differs from its archive member"
	done
	pass "bundle: archive holds exactly kwakore-daemon, kwakore, napplet and libwebview.so, matching SHA256SUMS and the unpacked directory"

	out=$("$bundle/kwakore-daemon" version) || fail "bundled daemon did not run: $out"
	[ "$out" = "$bundle_version" ] || fail "bundled daemon reports version $out, not $bundle_version"

	# The hardened loader (desktop/child/libcheck.go) must refuse to fall back
	# to a dlopen search when WEBVIEW_PATH does not name the library's
	# directory. Reaching each refusal also proves the child runs on this host.
	child_refuses() {
		local want=$1 status=0
		shift
		env "$@" VERDANA_NAPP_FORMAT=napplet "$bundle/napplet" </dev/null >/dev/null 2>"$stage/child.log" || status=$?
		[ "$status" = 1 ] && grep -qF "$want" "$stage/child.log" ||
			fail "napplet child (status $status) did not refuse with \"$want\": $(cat "$stage/child.log")"
		grep -qF 'window program started' "$stage/child.log" || fail "napplet child did not start: $(cat "$stage/child.log")"
	}
	child_refuses 'WEBVIEW_PATH is not set' -u WEBVIEW_PATH
	child_refuses 'is not absolute' WEBVIEW_PATH=relative/dir
	mkdir "$stage/nolib"
	child_refuses 'no such file or directory' WEBVIEW_PATH="$stage/nolib"
	pass "child: bundled napplet starts and refuses a missing, relative or empty WEBVIEW_PATH"

	if [ "$arch" = "$(host_arch)" ] && command -v ldd >/dev/null; then
		for f in napplet libwebview.so; do
			ldd "$bundle/$f" >"$stage/ldd.log" 2>&1 || fail "ldd $f failed: $(cat "$stage/ldd.log")"
			grep -q 'not found' "$stage/ldd.log" && fail "$f has unresolved libraries on this host: $(grep 'not found' "$stage/ldd.log")"
		done
		ldd "$bundle/libwebview.so" | grep -q 'libwebkit2gtk-4\.1\.so' ||
			fail "libwebview.so does not link WebKitGTK 4.1"
		pass "child: napplet and libwebview.so resolve every shared object, including WebKitGTK 4.1"
	else
		fail "ldd is required to check the bundled child's libraries"
	fi
}

run_bundle() {
	local arch out
	command -v go >/dev/null || fail "go not found; it is needed to build the bundle"
	for tool in tar gzip sha256sum flock od; do
		command -v "$tool" >/dev/null || fail "$tool not found"
	done
	arch=$(host_arch)
	uid=$(id -u)
	stage=$(mktemp -d "${TMPDIR:-/tmp}/kwakore-bundle-smoke.XXXXXX")
	trap 'rm -rf "$stage"' EXIT
	trap 'exit 130' INT TERM
	out="$stage/dist"
	bash "$repo_root/scripts/build-linux-bundle.sh" --arch "$arch" --version "$bundle_version" --out "$out" >/dev/null ||
		fail "first bundle build failed"
	file_set "$out/$bundle_version" >"$stage/run1"
	bash "$repo_root/scripts/build-linux-bundle.sh" --arch "$arch" --version "$bundle_version" --out "$out" >/dev/null ||
		fail "second bundle build failed"
	file_set "$out/$bundle_version" >"$stage/run2"
	diff -u "$stage/run1" "$stage/run2" >"$stage/run.diff" ||
		fail "consecutive bundle builds differ: $(cat "$stage/run.diff")"
	pass "bundle: two consecutive builds left the same file set, modes and archive bytes"
	check_bundle "$out/$bundle_version" "$arch"
}

if [ "$mode" = --bundle-only ]; then
	run_bundle
	exit 0
fi

# ─── environment preconditions ──────────────────────────────────────────────

command -v systemctl >/dev/null || fail "systemctl not found; a systemd user manager is required"
command -v go >/dev/null || fail "go not found; it is needed to build the staged binaries"
[ -n "${XDG_RUNTIME_DIR:-}" ] || fail "XDG_RUNTIME_DIR is unset; run inside a user session with a systemd user manager"
manager_state=$(systemctl --user is-system-running 2>/dev/null || true)
case "$manager_state" in
running | degraded) ;;
*) fail "systemd user manager unavailable (state: ${manager_state:-unreachable}); cannot run the ${mode#--} smoke" ;;
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
	out=$("$cli" status) || fail "kwakore status failed: $out"
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

# ─── install ────────────────────────────────────────────────────────────────

# managed_state prints every file the helper owns, the unit files and enable
# link, and the activation state, so repeated installs can be compared.
managed_state() {
	local f
	(
		cd "$install_prefix"
		find . -mindepth 1 -printf '%y %m %p -> %l\n' | LC_ALL=C sort
		find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
	)
	for f in "$runtime_units/kwakore.socket" "$runtime_units/kwakore.service" "$runtime_units/sockets.target.wants/kwakore.socket"; do
		printf '%s %s %s\n' "$f" "$(stat -c '%F %a' "$f" 2>/dev/null || echo missing)" "$(readlink "$f" || true)"
		if [ -f "$f" ]; then sha256sum <"$f"; fi
	done
	printf 'socket %s %s\n' "$(active_state kwakore.socket)" "$(systemctl --user show -p UnitFileState --value kwakore.socket)"
	printf 'service %s pid %s\n' "$(active_state kwakore.service)" "$(main_pid)"
	printf 'socket inode %s\n' "$(socket_identity)"
}

# check_child_path mirrors linuxhost checkProgram: every component of the
# resolved path is root- or user-owned, not group or world writable, and no
# symlink.
check_child_path() {
	local path=$1 current="" part mode owner
	IFS=/ read -ra parts <<<"${path#/}"
	for part in "${parts[@]}"; do
		current="$current/$part"
		[ ! -L "$current" ] || fail "$current is a symlink on the napplet path"
		read -r mode owner <<<"$(stat -c '%a %u' "$current")"
		[ "$owner" = 0 ] || [ "$owner" = "$uid" ] || fail "$current is owned by $owner"
		(((8#$mode & 8#022) == 0)) || fail "$current is group or world writable ($mode)"
	done
}

install_cleanup() {
	status=$?
	trap - EXIT INT TERM
	if [ -n "${holder:-}" ]; then kill "$holder" 2>/dev/null || true; fi
	wait 2>/dev/null || true
	# Stop while the unit files are still loaded so RemoveOnStop runs.
	systemctl --user stop kwakore.service kwakore.socket >/dev/null 2>&1 || true
	systemctl --user disable --runtime kwakore.socket >/dev/null 2>&1 || true
	# The preconditions refused to run if any of these existed beforehand.
	rm -f "$runtime_units/kwakore.socket" "$runtime_units/kwakore.service" "$runtime_units/kwakore.service.d/50-smoke.conf"
	rmdir "$runtime_units/kwakore.service.d" 2>/dev/null || true
	if [ "$wants_created" = 1 ]; then rmdir "$runtime_units/sockets.target.wants" 2>/dev/null || true; fi
	if [ "$units_dir_created" = 1 ]; then rmdir "$runtime_units" 2>/dev/null || true; fi
	systemctl --user daemon-reload >/dev/null 2>&1 || true
	systemctl --user reset-failed kwakore.service kwakore.socket >/dev/null 2>&1 || true
	rmdir "$runtime_child" 2>/dev/null || true
	rm -rf "$stage"
	exit "$status"
}

# repack writes the bundle's four files under another version as a new
# archive with its own SHA256SUMS, giving a distinct release cheaply.
repack() {
	local v=$1 dir="$stage/repack-$1" name="kwakore-$1-linux-$arch" f
	mkdir -p "$dir/src/$name"
	for f in "${bundle_files[@]}"; do cp -p "$bundle/$f" "$dir/src/$name/$f"; done
	tar --owner=0 --group=0 --numeric-owner -C "$dir/src" -czf "$dir/kwakore-linux-$arch.tar.gz" \
		"$name/kwakore-daemon" "$name/kwakore" "$name/napplet" "$name/libwebview.so"
	(cd "$dir" && sha256sum "kwakore-linux-$arch.tar.gz" >SHA256SUMS)
	echo "$dir/kwakore-linux-$arch.tar.gz"
}

# refuse runs the helper on a hostile archive and requires the given refusal
# with the previous install and its daemon untouched.
refuse() {
	local want=$1 file=$2
	if "${helper[@]}" --archive "$file" >"$stage/refuse.log" 2>&1; then
		fail "helper accepted a hostile archive ($want)"
	fi
	grep -qF "$want" "$stage/refuse.log" || fail "helper refusal lacks \"$want\": $(cat "$stage/refuse.log")"
	managed_state >"$stage/state.refused"
	diff -u "$stage/state1" "$stage/state.refused" >"$stage/state.diff" ||
		fail "a refused archive changed the install: $(cat "$stage/state.diff")"
	! ls -A "$root" | grep -q '^\.stage\.' || fail "a refused archive left a staging directory"
}

run_install() {
	local tool dist archive sha first_pid t0 elapsed r1 r2 v2 v3 sha2 sha3 releases_now
	for tool in tar gzip sha256sum flock od realpath; do
		command -v "$tool" >/dev/null || fail "$tool not found"
	done
	[ ! -e "$runtime_units/kwakore.service.d" ] || fail "$runtime_units/kwakore.service.d already exists"
	arch=$(host_arch)
	units_dir_created=0
	[ -d "$runtime_units" ] || units_dir_created=1
	wants_created=0
	[ -d "$runtime_units/sockets.target.wants" ] || wants_created=1
	# Below XDG_RUNTIME_DIR, not /tmp: the daemon runs a napplet child only
	# from a path with no group- or world-writable component, and the helper
	# refuses such a prefix up front.
	stage=$(mktemp -d "$XDG_RUNTIME_DIR/kwakore-install-smoke.XXXXXX")
	holder=""
	trap install_cleanup EXIT
	trap 'exit 130' INT TERM
	install_prefix="$stage/prefix"
	root="$install_prefix/lib/kwakore"
	cli="$install_prefix/bin/kwakore"
	helper=(bash "$repo_root/scripts/install.sh" --prefix "$install_prefix" --runtime-units)

	bash "$repo_root/scripts/build-linux-bundle.sh" --arch "$arch" --version "$bundle_version" --out "$stage/dist" >/dev/null ||
		fail "could not build the bundle"
	dist="$stage/dist/$bundle_version"
	archive="$dist/kwakore-linux-$arch.tar.gz"
	bundle="$dist/kwakore-$bundle_version-linux-$arch"
	sha=$(sha256sum <"$archive" | cut -d' ' -f1)

	mkdir -m 0700 "$stage/config" "$stage/data" "$stage/config/kwakore"
	# Offline settings keep the smoke deterministic and off the network.
	printf '%s\n' '{"relays":[],"blossom_servers":[],"discover_on_user_relays":false}' >"$stage/config/kwakore/config.json"
	mkdir -p "$runtime_units/kwakore.service.d"
	printf '%s\n' '[Service]' "Environment=\"XDG_CONFIG_HOME=$stage/config\" \"XDG_DATA_HOME=$stage/data\"" \
		>"$runtime_units/kwakore.service.d/50-smoke.conf"

	# A prefix below a group-writable directory is refused before any unit,
	# release or link is written.
	mkdir "$stage/shared"
	chmod 0775 "$stage/shared"
	if bash "$repo_root/scripts/install.sh" --prefix "$stage/shared/prefix" --runtime-units --archive "$archive" >"$stage/shared.log" 2>&1; then
		fail "helper installed below a group-writable directory"
	fi
	grep -q 'group or world writable' "$stage/shared.log" || fail "unsafe prefix refusal unclear: $(cat "$stage/shared.log")"
	[ ! -e "$runtime_units/kwakore.socket" ] && [ ! -e "$runtime_units/kwakore.service" ] ||
		fail "a refused prefix still wrote unit files"
	[ ! -e "$stage/shared/prefix/lib/kwakore/releases" ] || fail "a refused prefix still wrote a release"

	"${helper[@]}" --archive "$archive" >"$stage/install1.log" 2>&1 || fail "helper install failed: $(cat "$stage/install1.log")"

	# Units: exactly the shipped templates, with ExecStart rendered the way the
	# manual instructions render it, and the helper's copies match too.
	cmp -s "$runtime_units/kwakore.socket" "$units_src/kwakore.socket" || fail "installed kwakore.socket differs from the template"
	sed "s|@BINDIR@|$root/current|g" "$units_src/kwakore.service" | cmp -s - "$runtime_units/kwakore.service" ||
		fail "installed kwakore.service differs from the manually rendered template"
	bash "$repo_root/scripts/install.sh" --print-unit kwakore.socket | cmp -s - "$units_src/kwakore.socket" ||
		fail "the helper's socket template drifted from packaging/systemd/user"
	bash "$repo_root/scripts/install.sh" --print-unit kwakore.service | cmp -s - "$units_src/kwakore.service" ||
		fail "the helper's service template drifted from packaging/systemd/user"
	grep -qx "ExecStart=$root/current/kwakore-daemon" "$runtime_units/kwakore.service" || fail "ExecStart is not the installed daemon"

	# Payload: the ExecStart daemon resolves into the archive's release
	# directory, beside the napplet child and library, byte for byte the bundle.
	daemon=$(readlink -f "$root/current/kwakore-daemon")
	[ "$daemon" = "$(realpath "$root")/releases/$sha/kwakore-daemon" ] || fail "ExecStart resolves to $daemon, not the archive's release"
	release=$(dirname "$daemon")
	[ "$(ls -A "$release" | LC_ALL=C sort | tr '\n' ' ')" = "kwakore kwakore-daemon libwebview.so napplet " ] ||
		fail "release directory is not exactly the four files"
	for f in "${bundle_files[@]}"; do
		[ -f "$release/$f" ] && [ ! -L "$release/$f" ] || fail "installed $f is not a regular file"
		mode=755
		[ "$f" = libwebview.so ] && mode=644
		[ "$(stat -c '%a %u' "$release/$f")" = "$mode $uid" ] || fail "installed $f is not mode $mode owned by $uid"
		cmp -s "$release/$f" "$bundle/$f" || fail "installed $f differs from the bundle"
	done
	check_child_path "$release/napplet"
	check_child_path "$release/libwebview.so"
	[ "$(readlink "$cli")" = "$root/current/kwakore" ] || fail "CLI link does not point at the installed release"

	# Activation: only the socket is enabled and started.
	wait_state kwakore.socket active
	[ "$(systemctl --user show -p UnitFileState --value kwakore.socket)" = enabled-runtime ] || fail "kwakore.socket is not enabled"
	[ "$(active_state kwakore.service)" = inactive ] || fail "kwakore.service was active before the first client"
	[ "$(systemctl --user is-enabled kwakore.service 2>/dev/null || true)" != enabled ] || fail "kwakore.service is enabled"
	check_inode_policy
	cli_status
	wait_state kwakore.service active
	pass "install: helper put the four files beside ExecStart, installed the exact unit templates, enabled only kwakore.socket, and the installed CLI activated the daemon"

	managed_state >"$stage/state1"
	"${helper[@]}" --archive "$archive" >"$stage/install2.log" 2>&1 || fail "second helper run failed: $(cat "$stage/install2.log")"
	grep -q 'already installed; nothing changed' "$stage/install2.log" || fail "second run did not report an unchanged install"
	managed_state >"$stage/state2"
	diff -u "$stage/state1" "$stage/state2" >"$stage/state.diff" || fail "second run changed the install: $(cat "$stage/state.diff")"
	cli_status
	pass "install: a second run with the same archive changed no managed file, unit or activation state"

	# Serialization: a held lock makes the helper wait, then it completes.
	(
		flock 9
		: >"$stage/held"
		sleep 3
	) 9<>"$root/.install.lock" &
	holder=$!
	for _ in $(seq 1 50); do
		[ -e "$stage/held" ] && break
		sleep 0.1
	done
	[ -e "$stage/held" ] || fail "could not hold the install lock"
	t0=$(date +%s%N)
	"${helper[@]}" --archive "$archive" >"$stage/wait.log" 2>&1 || fail "helper failed behind a held lock: $(cat "$stage/wait.log")"
	elapsed=$((($(date +%s%N) - t0) / 1000000))
	wait "$holder"
	holder=""
	grep -q 'waiting for another kwakore install' "$stage/wait.log" || fail "helper did not wait for the held lock"
	[ "$elapsed" -ge 2000 ] || fail "helper finished in ${elapsed}ms while the lock was held"
	"${helper[@]}" --archive "$archive" >"$stage/race1.log" 2>&1 &
	r1=$!
	"${helper[@]}" --archive "$archive" >"$stage/race2.log" 2>&1 &
	r2=$!
	wait "$r1" || fail "first concurrent run failed: $(cat "$stage/race1.log")"
	wait "$r2" || fail "second concurrent run failed: $(cat "$stage/race2.log")"
	managed_state >"$stage/state3"
	diff -u "$stage/state1" "$stage/state3" >"$stage/state.diff" || fail "concurrent runs changed the install: $(cat "$stage/state.diff")"
	cli_status
	pass "install: a held lock made the helper wait, and two concurrent runs ended in the same single layout"

	# Hostile archives are refused before extraction.
	mkdir "$stage/tamper"
	cp "$archive" "$dist/SHA256SUMS" "$stage/tamper/"
	printf x >>"$stage/tamper/kwakore-linux-$arch.tar.gz"
	refuse 'checksum mismatch' "$stage/tamper/kwakore-linux-$arch.tar.gz"
	mkdir -p "$stage/escape/src/kwakore-9-linux-$arch"
	for f in "${bundle_files[@]}"; do cp -p "$bundle/$f" "$stage/escape/src/kwakore-9-linux-$arch/"; done
	: >"$stage/escape/src/escaped"
	tar -P -C "$stage/escape/src" -czf "$stage/escape/kwakore-linux-$arch.tar.gz" \
		"kwakore-9-linux-$arch/kwakore-daemon" "kwakore-9-linux-$arch/kwakore" "kwakore-9-linux-$arch/napplet" \
		"kwakore-9-linux-$arch/libwebview.so" "kwakore-9-linux-$arch/../escaped" 2>/dev/null
	tar -tzf "$stage/escape/kwakore-linux-$arch.tar.gz" 2>/dev/null | grep -qF '../escaped' || fail "could not build the ../ archive"
	(cd "$stage/escape" && sha256sum "kwakore-linux-$arch.tar.gz" >SHA256SUMS)
	refuse 'must hold exactly' "$stage/escape/kwakore-linux-$arch.tar.gz"
	[ ! -e "$root/escaped" ] && [ ! -e "$install_prefix/lib/escaped" ] || fail "a ../ member escaped"
	mkdir -p "$stage/link/src/kwakore-9-linux-$arch"
	for f in kwakore-daemon kwakore libwebview.so; do cp -p "$bundle/$f" "$stage/link/src/kwakore-9-linux-$arch/"; done
	ln -s /bin/sh "$stage/link/src/kwakore-9-linux-$arch/napplet"
	tar -C "$stage/link/src" -czf "$stage/link/kwakore-linux-$arch.tar.gz" \
		"kwakore-9-linux-$arch/kwakore-daemon" "kwakore-9-linux-$arch/kwakore" "kwakore-9-linux-$arch/napplet" "kwakore-9-linux-$arch/libwebview.so"
	(cd "$stage/link" && sha256sum "kwakore-linux-$arch.tar.gz" >SHA256SUMS)
	refuse 'not a regular file' "$stage/link/kwakore-linux-$arch.tar.gz"
	cli_status
	pass "install: a tampered checksum, an extra ../ member and a symlink member were refused before extraction, leaving the running install untouched"

	# Upgrade: a new archive swaps the release, restarts the running daemon,
	# keeps the previous release and prunes older ones.
	first_pid=$(main_pid)
	v2=$(repack 0.0.0-smoke2)
	sha2=$(sha256sum <"$v2" | cut -d' ' -f1)
	"${helper[@]}" --archive "$v2" >"$stage/upgrade.log" 2>&1 || fail "upgrade failed: $(cat "$stage/upgrade.log")"
	[ "$(readlink "$root/current")" = "releases/$sha2" ] || fail "upgrade did not switch current to the new release"
	wait_state kwakore.service active
	[ "$(main_pid)" != "$first_pid" ] || fail "upgrade did not restart the running daemon"
	cli_status
	v3=$(repack 0.0.0-smoke3)
	sha3=$(sha256sum <"$v3" | cut -d' ' -f1)
	"${helper[@]}" --archive "$v3" >"$stage/upgrade.log" 2>&1 || fail "second upgrade failed: $(cat "$stage/upgrade.log")"
	releases_now=$(ls -A "$root/releases" | LC_ALL=C sort | tr '\n' ' ')
	[ "$releases_now" = "$(printf '%s\n' "$sha2" "$sha3" | LC_ALL=C sort | tr '\n' ' ')" ] ||
		fail "releases after two upgrades are not exactly the previous and current ones: $releases_now"
	[ "$(readlink "$root/current")" = "releases/$sha3" ] || fail "second upgrade did not switch current"
	cli_status
	pass "install: a new archive swapped the release in one rename, restarted the running daemon, kept the previous release and pruned older ones"
}

if [ "$mode" = --install-only ]; then
	run_install
	exit 0
fi

# ─── staging and cleanup ────────────────────────────────────────────────────

stage=$(mktemp -d "${TMPDIR:-/tmp}/kwakore-smoke.XXXXXX")
cli="$stage/bin/kwakore"
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
