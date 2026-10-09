#!/usr/bin/env bash
# Install the Kwakore Linux service for the current user.
#
#   curl -fsSL https://raw.githubusercontent.com/hzrd149/kwakore/master/scripts/install.sh | bash
#   scripts/install.sh --archive dist/VERSION/kwakore-linux-amd64.tar.gz
#
# The helper verifies kwakore-linux-ARCH.tar.gz against SHA256SUMS before it
# reads anything inside it, accepts only an archive holding exactly
# kwakore, kwak, kwaklet and libwebview.so as regular files in one
# directory, and installs them side by side:
#
#   PREFIX/lib/kwakore/releases/SHA256/   the four files, one directory per archive
#   PREFIX/lib/kwakore/current            symlink to the installed release
#   PREFIX/bin/kwak                       symlink to current/kwak
#   UNITDIR/kwakore.socket                packaging/systemd/user/kwakore.socket
#   UNITDIR/kwakore.service               ExecStart=PREFIX/lib/kwakore/current/kwakore
#
# PREFIX defaults to ~/.local and UNITDIR to ${XDG_CONFIG_HOME:-~/.config}/systemd/user.
# It then reloads the user manager and enables and starts only kwakore.socket;
# the first client connection starts kwakore.service (D-01).
#
# A new release is staged inside PREFIX/lib/kwakore and becomes live through a
# single rename of the "current" symlink, so the daemon, child and library
# always change together and an interrupted or rejected install leaves the
# previous one usable. Running the helper again with the same archive changes
# nothing. Concurrent runs for one user serialize on an owner-only lock file.
# The previous release is kept; older ones are pruned. Kwakore data,
# configuration, other units and files from earlier products are never touched.
#
# Manual installation is equivalent: unpack the archive into one directory,
# copy kwakore.socket and kwakore.service (with @BINDIR@ replaced by that
# directory) into ~/.config/systemd/user, then run
#   systemctl --user daemon-reload && systemctl --user enable --now kwakore.socket

set -euo pipefail
umask 022

repo="hzrd149/kwakore"

usage() {
	cat <<'EOF'
Usage: install.sh [options]

Installs the Kwakore service for the current user and enables kwakore.socket.

Source (default: the latest GitHub release):
  --version VERSION     download release VERSION, such as v0.2.0
  --archive FILE        install a local kwakore-linux-ARCH.tar.gz (from `just bundle`)
  --sha256sums FILE     checksum list for --archive (default: SHA256SUMS beside it)

Layout:
  --prefix DIR          install under DIR/lib/kwakore and DIR/bin (default: ~/.local)
  --runtime-units       put the units in $XDG_RUNTIME_DIR/systemd/user and enable
                        them with --runtime, so they last only until logout

Other:
  --print-unit NAME     print the kwakore.socket or kwakore.service template
  -h, --help            show this help
EOF
}

fail() {
	echo "kwakore install: $*" >&2
	exit 1
}

note() {
	echo "kwakore install: $*" >&2
}

# ─── unit templates ──────────────────────────────────────────────────────────
# Verbatim copies of packaging/systemd/user/*; the smoke test fails when they
# drift. @BINDIR@ is rendered at install time.

socket_template() {
	cat <<'EOF'
# Kwakore control socket for the per-user systemd manager.
#
# Enable only this unit (systemctl --user enable --now kwakore.socket). The
# first client connection starts kwakore.service, which adopts this listener.
# The path, 0700 directory and 0600 inode must match what kwakore
# validates before serving an inherited socket.
[Unit]
Description=Kwakore control socket
Documentation=https://github.com/hzrd149/kwakore/blob/master/docs/service.md

[Socket]
ListenStream=%t/kwakore/daemon.sock
SocketMode=0600
DirectoryMode=0700
Accept=no
RemoveOnStop=yes

[Install]
WantedBy=sockets.target
EOF
}

service_template() {
	cat <<'EOF'
# Kwakore daemon for the per-user systemd manager.
#
# Started on demand by kwakore.socket. Installers render @BINDIR@ to the
# absolute directory holding kwakore, the napplet child and
# libwebview.so; the daemon resolves its child beside its own executable.
[Unit]
Description=Kwakore napplet daemon
Documentation=https://github.com/hzrd149/kwakore/blob/master/docs/service.md
Requires=kwakore.socket
After=kwakore.socket
# Explicit so the burst edge is the same on every distribution: a sixth start
# within ten seconds fails this unit with start-limit-hit and the socket with
# service-start-limit-hit. Clients then get the fixed Unavailable error until
# `systemctl --user reset-failed kwakore.service kwakore.socket` and
# `systemctl --user start kwakore.socket`. The limit also stops a daemon that
# exits without accepting from being re-triggered forever by a queued client.
StartLimitIntervalSec=10s
StartLimitBurst=5

[Service]
Type=simple
ExecStart=@BINDIR@/kwakore
ExecReload=kill -HUP $MAINPID
KillMode=mixed
TimeoutStopSec=15s
UMask=0077
EOF
}

# ─── arguments ───────────────────────────────────────────────────────────────

version=latest
archive=""
sums=""
prefix=""
runtime_units=false
while [ $# -gt 0 ]; do
	case "$1" in
	--version) version="${2:?--version needs a value}"; shift 2 ;;
	--archive) archive="${2:?--archive needs a file}"; shift 2 ;;
	--sha256sums) sums="${2:?--sha256sums needs a file}"; shift 2 ;;
	--prefix) prefix="${2:?--prefix needs a directory}"; shift 2 ;;
	--runtime-units) runtime_units=true; shift ;;
	--print-unit)
		case "${2:-}" in
		kwakore.socket) socket_template ;;
		kwakore.service) service_template ;;
		*) echo "--print-unit takes kwakore.socket or kwakore.service" >&2; exit 2 ;;
		esac
		exit 0
		;;
	-h | --help) usage; exit 0 ;;
	*) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
	esac
done
[ -z "$sums" ] || [ -n "$archive" ] || fail "--sha256sums needs --archive"
if [ -n "$archive" ] && [ "$version" != latest ]; then
	fail "use either --archive or --version, not both"
fi

# ─── environment ─────────────────────────────────────────────────────────────

[ "$(uname -s)" = Linux ] || fail "Kwakore runs on Linux with a systemd user manager"
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "unsupported architecture $(uname -m)" ;;
esac
asset="kwakore-linux-$arch.tar.gz"
uid=$(id -u)
[ "$uid" != 0 ] || fail "run the helper as the user who will use Kwakore, not as root"

for tool in tar gzip sha256sum flock systemctl od stat; do
	command -v "$tool" >/dev/null || fail "$tool is required"
done
[ -n "$archive" ] || command -v curl >/dev/null || fail "curl is required to download a release"

case "${HOME:-}" in
/*) ;;
*) fail "HOME must be an absolute path" ;;
esac
[ -n "$prefix" ] || prefix="$HOME/.local"
case "$prefix" in
/*) ;;
*) fail "--prefix must be an absolute path" ;;
esac
# Paths end up in unit files and symlinks; refuse control characters outright.
if [[ "$prefix" =~ [[:cntrl:]] ]]; then
	fail "--prefix must not contain control characters"
fi
prefix="${prefix%/}"
root="$prefix/lib/kwakore"
bin_dir="$prefix/bin"

if $runtime_units; then
	case "${XDG_RUNTIME_DIR:-}" in
	/*) ;;
	*) fail "--runtime-units needs XDG_RUNTIME_DIR" ;;
	esac
	unit_dir="$XDG_RUNTIME_DIR/systemd/user"
	enable_scope=(--runtime)
else
	config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
	case "$config_home" in
	/*) ;;
	*) fail "XDG_CONFIG_HOME must be an absolute path" ;;
	esac
	unit_dir="$config_home/systemd/user"
	enable_scope=()
fi
if [[ "$unit_dir" =~ [[:cntrl:]] ]]; then
	fail "unit directory must not contain control characters"
fi

# Nothing is written before the user manager is known to be reachable.
manager_version=$(systemctl --user show -p Version --value 2>/dev/null || true)
[ -n "$manager_version" ] ||
	fail "no systemd user manager is reachable; run the helper inside your login session (manual steps: docs/service.md)"

# ─── payload directory and lock ──────────────────────────────────────────────

# The daemon only runs a napplet child whose every path component is owned by
# root or this user and is not group or world writable, and it refuses
# symlinks along the resolved path (linuxhost checkProgram). Check the
# directory the payload will live in up front, so a layout napplet windows
# would refuse never replaces a working one.
check_path_policy() {
	local path current part mode owner
	path=$(realpath -e -- "$1") || fail "cannot resolve $1"
	current=""
	IFS=/ read -ra parts <<<"${path#/}"
	for part in "" "${parts[@]}"; do
		if [ -z "$current" ] && [ -z "$part" ]; then
			current=/
		elif [ "$current" = / ]; then
			current="/$part"
		else
			current="$current/$part"
		fi
		read -r mode owner <<<"$(stat -c '%a %u' -- "$current")"
		if [ "$owner" != 0 ] && [ "$owner" != "$uid" ]; then
			fail "$current is owned by uid $owner; napplet windows would refuse to start from below it"
		fi
		if (((8#$mode & 8#022) != 0)); then
			fail "$current is group or world writable (mode $mode); napplet windows would refuse to start from below it. Run: chmod go-w '$current'"
		fi
	done
}

mkdir -p -- "$root"
[ -d "$root" ] && [ ! -L "$root" ] || fail "$root is not a directory"
[ "$(stat -c '%u' -- "$root")" = "$uid" ] || fail "$root is not owned by you"
chmod 0755 -- "$root"
check_path_policy "$root"

lock="$root/.install.lock"
[ ! -L "$lock" ] || fail "$lock is a symlink"
(umask 077 && : >>"$lock")
[ -f "$lock" ] && [ "$(stat -c '%u' -- "$lock")" = "$uid" ] || fail "$lock is not a file owned by you"
chmod 0600 -- "$lock"
exec 9<>"$lock"
if ! flock -n 9; then
	note "waiting for another kwakore install to finish"
	flock 9
fi

stage=$(mktemp -d "$root/.stage.XXXXXX")
link_tmp=""
cleanup() {
	rm -rf -- "$stage"
	[ -z "$link_tmp" ] || rm -f -- "$link_tmp"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# ─── fetch and verify ────────────────────────────────────────────────────────

if [ -n "$archive" ]; then
	[ -f "$archive" ] || fail "$archive is not a file"
	[ "$(basename -- "$archive")" = "$asset" ] ||
		fail "this machine needs $asset, not $(basename -- "$archive")"
	[ -n "$sums" ] || sums="$(dirname -- "$archive")/SHA256SUMS"
	[ -f "$sums" ] || fail "no checksum list at $sums (pass --sha256sums)"
	# Verify and unpack private copies, so the files cannot change in between.
	cp -- "$archive" "$stage/$asset"
	cp -- "$sums" "$stage/SHA256SUMS"
	source_desc="$archive"
else
	if [ "$version" = latest ]; then
		base_url="https://github.com/$repo/releases/latest/download"
	else
		[[ "$version" == v* ]] || version="v$version"
		[[ "$version" =~ ^v[A-Za-z0-9._+-]{1,64}$ ]] || fail "invalid version $version"
		base_url="https://github.com/$repo/releases/download/$version"
	fi
	note "downloading $asset ($version)"
	for f in "$asset" SHA256SUMS; do
		curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --silent --show-error \
			--output "$stage/$f" "$base_url/$f" || fail "could not download $base_url/$f"
	done
	source_desc="$base_url/$asset"
fi

expected=$(awk -v name="$asset" '{ f = $2; sub(/^\*/, "", f) } NF == 2 && f == name { print $1 }' "$stage/SHA256SUMS")
[ -n "$expected" ] || fail "SHA256SUMS has no entry for $asset"
[ "$(printf '%s\n' "$expected" | wc -l)" = 1 ] || fail "SHA256SUMS has more than one entry for $asset"
[[ "$expected" =~ ^[0-9a-f]{64}$ ]] || fail "SHA256SUMS entry for $asset is not a sha256"
actual=$(sha256sum -- "$stage/$asset" | cut -d' ' -f1)
[ "$actual" = "$expected" ] || fail "checksum mismatch for $asset; nothing was installed"

# ─── inspect and unpack ──────────────────────────────────────────────────────

files=(kwakore kwak kwaklet libwebview.so)
tar -tzf "$stage/$asset" >"$stage/members" 2>"$stage/tar.log" || fail "$asset is not a readable archive"
top=$(head -n1 "$stage/members")
top=${top%%/*}
[[ "$top" =~ ^kwakore-[A-Za-z0-9][A-Za-z0-9._+-]{0,63}-linux-$arch$ ]] ||
	fail "$asset does not hold a kwakore-VERSION-linux-$arch directory"
printf '%s\n' "${files[@]/#/$top/}" | LC_ALL=C sort >"$stage/members.want"
LC_ALL=C sort "$stage/members" >"$stage/members.got"
cmp -s "$stage/members.want" "$stage/members.got" ||
	fail "$asset must hold exactly ${files[*]} in $top/; refusing it"
# Every member is a plain file: no links, devices or directories.
tar -tvzf "$stage/$asset" >"$stage/types"
[ "$(wc -l <"$stage/types")" = "${#files[@]}" ] || fail "$asset has duplicate members"
if grep -qv '^-' "$stage/types"; then
	fail "$asset holds a member that is not a regular file; refusing it"
fi

mkdir -- "$stage/unpack"
mapfile -t members <"$stage/members.want"
tar -xzf "$stage/$asset" -C "$stage/unpack" --no-same-owner --no-same-permissions -- "${members[@]}" ||
	fail "could not unpack $asset"
payload="$stage/unpack/$top"
[ "$(ls -A -- "$payload" | wc -l)" = "${#files[@]}" ] || fail "unexpected files after unpacking $asset"
for f in "${files[@]}"; do
	[ -f "$payload/$f" ] && [ ! -L "$payload/$f" ] && [ "$(stat -c '%h' -- "$payload/$f")" = 1 ] ||
		fail "$f in $asset is not a regular file"
	[ "$(od -An -tx1 -N4 -- "$payload/$f" | tr -d ' \n')" = 7f454c46 ] || fail "$f in $asset is not an ELF file"
done
chmod 0755 -- "$payload" "$payload/kwakore" "$payload/kwak" "$payload/kwaklet"
chmod 0644 -- "$payload/libwebview.so"
installed_version=${top#kwakore-}
installed_version=${installed_version%-linux-"$arch"}

# ─── place the release ───────────────────────────────────────────────────────

# same_release reports whether directory $1 already holds exactly this payload.
same_release() {
	local dir=$1 f
	[ -d "$dir" ] && [ ! -L "$dir" ] || return 1
	[ "$(ls -A -- "$dir" | wc -l)" = "${#files[@]}" ] || return 1
	[ "$(stat -c '%a' -- "$dir")" = 755 ] || return 1
	for f in "${files[@]}"; do
		[ -f "$dir/$f" ] && [ ! -L "$dir/$f" ] || return 1
		[ "$(stat -c '%a %h' -- "$dir/$f")" = "$(stat -c '%a %h' -- "$payload/$f")" ] || return 1
		cmp -s -- "$dir/$f" "$payload/$f" || return 1
	done
}

releases="$root/releases"
mkdir -p -- "$releases"
[ -d "$releases" ] && [ ! -L "$releases" ] || fail "$releases is not a directory"
chmod 0755 -- "$releases"
release="$releases/$actual"
if ! same_release "$release"; then
	# Releases are named by archive hash and never edited, so a mismatch here
	# means damage: set it aside and put the verified payload in its place.
	if [ -e "$release" ] || [ -L "$release" ]; then
		mv -- "$release" "$stage/damaged-release"
	fi
	mv -- "$payload" "$release"
fi
check_path_policy "$release"

current="$root/current"
want="releases/$actual"
previous=""
if [ -L "$current" ]; then
	previous=$(readlink -- "$current")
elif [ -e "$current" ]; then
	fail "$current exists and is not a symlink; move it away and run the helper again"
fi
release_changed=false
if [ "$previous" != "$want" ]; then
	ln -s -- "$want" "$stage/current"
	mv -T -- "$stage/current" "$current"
	release_changed=true
fi

# ─── units ───────────────────────────────────────────────────────────────────

# systemd_path renders an absolute path as one ExecStart word: bare when it
# is plain, otherwise double-quoted with specifiers and variables escaped.
systemd_path() {
	local p=$1
	if [[ "$p" =~ ^[A-Za-z0-9._/+@:-]+$ ]]; then
		printf '%s' "$p"
		return
	fi
	p=${p//\\/\\\\}
	p=${p//\"/\\\"}
	p=${p//%/%%}
	p=${p//\$/\$\$}
	printf '"%s"' "$p"
}

daemon_path="$current/kwakore"
exec_line="ExecStart=$(systemd_path "$daemon_path")"
render_service() {
	local line
	while IFS= read -r line; do
		if [ "$line" = 'ExecStart=@BINDIR@/kwakore' ]; then
			printf '%s\n' "$exec_line"
		else
			printf '%s\n' "${line//@BINDIR@/"$current"}"
		fi
	done < <(service_template)
}
socket_template >"$stage/kwakore.socket"
render_service >"$stage/kwakore.service"
[ "$(grep -c '^ExecStart=' "$stage/kwakore.service")" = 1 ] &&
	grep -qxF "$exec_line" "$stage/kwakore.service" &&
	! grep -qF '@BINDIR@' "$stage/kwakore.service" || fail "could not render kwakore.service"
grep -qx 'ListenStream=%t/kwakore/daemon.sock' "$stage/kwakore.socket" || fail "socket template is damaged"

mkdir -p -- "$unit_dir"
units_changed=false
for unit in kwakore.socket kwakore.service; do
	target="$unit_dir/$unit"
	if [ -f "$target" ] && [ ! -L "$target" ] && cmp -s -- "$stage/$unit" "$target"; then
		continue
	fi
	tmp=$(mktemp "$unit_dir/.$unit.XXXXXX")
	cat -- "$stage/$unit" >"$tmp"
	chmod 0644 -- "$tmp"
	mv -f -- "$tmp" "$target"
	units_changed=true
done

# ─── activation ──────────────────────────────────────────────────────────────

systemctl --user daemon-reload || fail "systemctl --user daemon-reload failed"
# A socket left failed by the start limit cannot be started until reset.
if [ "$(systemctl --user show -p ActiveState --value kwakore.socket)" = failed ]; then
	systemctl --user reset-failed kwakore.socket kwakore.service || true
fi
systemctl --user enable "${enable_scope[@]}" --now --quiet kwakore.socket ||
	fail "could not enable and start kwakore.socket"
if $release_changed && [ "$(systemctl --user show -p ActiveState --value kwakore.service)" = active ]; then
	note "restarting the running daemon on the new release"
	systemctl --user try-restart kwakore.service || fail "could not restart kwakore.service"
fi
[ "$(systemctl --user show -p ActiveState --value kwakore.socket)" = active ] ||
	fail "kwakore.socket is not active; see: systemctl --user status kwakore.socket"

# ─── CLI link ────────────────────────────────────────────────────────────────

mkdir -p -- "$bin_dir"
cli_target="$current/kwak"
cli_link="$bin_dir/kwak"
old_cli_link="$bin_dir/kwakore"
removed_old_cli=false
if [ -L "$old_cli_link" ] && [ "$(readlink -- "$old_cli_link")" = "$current/kwakore" ]; then
	rm -- "$old_cli_link"
	removed_old_cli=true
fi
if [ -L "$cli_link" ] && [ "$(readlink -- "$cli_link")" = "$cli_target" ]; then
	:
elif [ -L "$cli_link" ] || [ ! -e "$cli_link" ]; then
	link_tmp="$bin_dir/.kwak.$$"
	ln -sfn -- "$cli_target" "$link_tmp"
	mv -T -- "$link_tmp" "$cli_link"
	link_tmp=""
else
	note "left $cli_link alone because it is not a symlink; remove it to use $cli_target"
fi

if $removed_old_cli && [ -L "$cli_link" ] && [ "$(readlink -- "$cli_link")" = "$cli_target" ]; then
	# Activate an upgraded idle service so it rewrites existing desktop entries
	# with the new CLI path after removing the old command.
	"$cli_link" --json status >/dev/null || note "could not refresh desktop entries; run: $cli_link status"
fi

# ─── prune ───────────────────────────────────────────────────────────────────

# Keep the live release and the one it replaced (a daemon that has not
# restarted yet may still run from it); drop older releases. Only
# hash-named directories this helper created are considered. Prune only when
# this run swapped the release: on a re-run with the installed archive,
# $previous is the live release itself, and pruning against it would delete
# the kept rollback release.
if $release_changed; then
	for dir in "$releases"/*; do
		name=$(basename -- "$dir")
		[[ "$name" =~ ^[0-9a-f]{64}$ ]] || continue
		[ -d "$dir" ] && [ ! -L "$dir" ] || continue
		[ "releases/$name" = "$want" ] && continue
		[ "releases/$name" = "$previous" ] && continue
		rm -rf -- "$dir"
	done
fi

# ─── report ──────────────────────────────────────────────────────────────────

if $release_changed || $units_changed; then
	echo "Installed Kwakore $installed_version from $source_desc"
else
	echo "Kwakore $installed_version is already installed; nothing changed"
fi
echo "  release  $current -> $want"
echo "  CLI      $cli_link"
echo "  units    $unit_dir/kwakore.socket, kwakore.service"
echo "  enabled  kwakore.socket (the first client starts kwakore.service)"
case ":${PATH:-}:" in
*":$bin_dir:"*) ;;
*) echo "Add $bin_dir to PATH to run kwak from a terminal." ;;
esac
echo "Try: kwak status"
