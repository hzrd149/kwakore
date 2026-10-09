#!/usr/bin/env bash
# Remove a helper-managed Kwakore installation for the current user.
# Configuration, installed napplets, and other user data are retained.
set -euo pipefail
shopt -s nullglob

fail() { echo "kwakore uninstall: $*" >&2; exit 1; }

[ "$(uname -s)" = Linux ] || fail "Linux is required"
[ "$(id -u)" != 0 ] || fail "run as the user who installed Kwakore, not root"
case "${HOME:-}" in /*) ;; *) fail "HOME must be absolute" ;; esac
prefix="${KWAKORE_INSTALL_PREFIX:-$HOME/.local}"
case "$prefix" in /*) ;; *) fail "KWAKORE_INSTALL_PREFIX must be absolute" ;; esac
prefix="${prefix%/}"
root="$prefix/lib/kwakore"
bin_dir="$prefix/bin"
config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
case "$config_home" in /*) ;; *) fail "XDG_CONFIG_HOME must be absolute" ;; esac
unit_dir="$config_home/systemd/user"
socket_unit="$unit_dir/kwakore.socket"
service_unit="$unit_dir/kwakore.service"
cli_link="$bin_dir/kwak"
old_cli_link="$bin_dir/kwakore"

command -v systemctl >/dev/null || fail "systemctl is required"
command -v flock >/dev/null || fail "flock is required"
[ -n "$(systemctl --user show -p Version --value 2>/dev/null || true)" ] ||
  fail "no systemd user manager is reachable"

# Refuse to stop units loaded from another installation, such as a Nix profile.
for unit in kwakore.socket kwakore.service; do
  loaded=$(systemctl --user show -p FragmentPath --value "$unit" 2>/dev/null || true)
  if [ -n "$loaded" ] && [ "$loaded" != "$unit_dir/$unit" ]; then
    fail "$unit is loaded from $loaded; expected $unit_dir/$unit"
  fi
done

for unit in "$socket_unit" "$service_unit"; do
  if [ -e "$unit" ] || [ -L "$unit" ]; then
    [ -f "$unit" ] && [ ! -L "$unit" ] || fail "$unit is not a regular file"
    [ "$(stat -c '%u' -- "$unit")" = "$(id -u)" ] || fail "$unit is not owned by you"
  fi
done
if [ -f "$service_unit" ]; then
  grep -Fqx "ExecStart=$root/current/kwakore" "$service_unit" ||
    grep -Fqx "ExecStart=$root/current/kwakore-daemon" "$service_unit" ||
    fail "$service_unit does not start this installation"
fi
if [ -f "$socket_unit" ]; then
  grep -Fqx 'ListenStream=%t/kwakore/daemon.sock' "$socket_unit" ||
    fail "$socket_unit is not the Kwakore control socket"
fi

for link in "$cli_link" "$old_cli_link"; do
  if [ -e "$link" ] || [ -L "$link" ]; then
    [ -L "$link" ] || fail "$link is not a helper-created symlink"
    case "$(readlink -- "$link")" in
      "$root/current/kwak" | "$root/current/kwakore") ;;
      *) fail "$link points outside this installation" ;;
    esac
  fi
done

if [ -e "$root" ] || [ -L "$root" ]; then
  [ -d "$root" ] && [ ! -L "$root" ] || fail "$root is not a directory"
  [ "$(stat -c '%u' -- "$root")" = "$(id -u)" ] || fail "$root is not owned by you"
  lock="$root/.install.lock"
  [ -f "$lock" ] && [ ! -L "$lock" ] || fail "$lock is not the helper lock"
  exec 9<>"$lock"
  flock 9
  if [ -e "$root/current" ] || [ -L "$root/current" ]; then
    [ -L "$root/current" ] || fail "$root/current is not a symlink"
    [[ "$(readlink -- "$root/current")" =~ ^releases/[0-9a-f]{64}$ ]] ||
      fail "$root/current is not a helper release link"
  fi
  [ -d "$root/releases" ] && [ ! -L "$root/releases" ] || fail "$root/releases is not a directory"
  for release in "$root/releases"/* "$root/releases"/.[!.]*; do
    [ -d "$release" ] && [ ! -L "$release" ] || fail "$release is not a directory"
    [[ "${release##*/}" =~ ^[0-9a-f]{64}$ ]] || fail "$release has an unexpected name"
    files=$(cd "$release" && LC_ALL=C ls -A | tr '\n' ' ')
    case "$files" in
      'kwak kwaklet kwakore libwebview.so ' | 'kwakore kwakore-daemon libwebview.so napplet ') ;;
      *) fail "$release has unexpected files: $files" ;;
    esac
  done
  for item in "$root"/* "$root"/.[!.]*; do
    case "${item##*/}" in current | releases | .install.lock) ;; *) fail "$root contains unexpected item $item" ;; esac
  done
fi

if [ ! -f "$service_unit" ] && [ ! -f "$socket_unit" ] &&
   [ ! -e "$root" ] && [ ! -L "$cli_link" ] && [ ! -L "$old_cli_link" ]; then
  echo "Kwakore is already uninstalled"
  exit 0
fi

if [ -f "$socket_unit" ]; then
  systemctl --user disable --now kwakore.socket >/dev/null ||
    fail "could not disable and stop kwakore.socket"
fi
if [ -f "$service_unit" ]; then
  systemctl --user stop kwakore.service >/dev/null ||
    fail "could not stop kwakore.service"
fi
rm -f -- "$socket_unit" "$service_unit"
systemctl --user daemon-reload
systemctl --user reset-failed kwakore.service kwakore.socket >/dev/null 2>&1 || true
rm -f -- "$cli_link" "$old_cli_link"

# Remove only desktop entries generated for this installation.
apps_dir="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
if [ -d "$apps_dir" ] && [ ! -L "$apps_dir" ]; then
  for entry in "$apps_dir"/kwakore-napplet-*.desktop; do
    [[ "${entry##*/}" =~ ^kwakore-napplet-[0-9a-f]{32}\.desktop$ ]] || continue
    [ -f "$entry" ] && [ ! -L "$entry" ] || continue
    grep -Fq "$root/current/kwak" "$entry" ||
      grep -Fq "$root/current/kwakore" "$entry" || continue
    rm -f -- "$entry"
  done
fi

if [ -d "$root" ]; then
  rm -f -- "$root/current"
  for release in "$root/releases"/*; do rm -rf -- "$release"; done
  rmdir -- "$root/releases"
  rm -f -- "$root/.install.lock"
  rmdir -- "$root"
fi
echo "Removed Kwakore service and binaries; configuration and app data are kept"
