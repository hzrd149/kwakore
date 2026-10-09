#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf -- "$fixture"' EXIT
export KWAKORE_INSTALL_PREFIX="$fixture/prefix"
export XDG_CONFIG_HOME="$fixture/config"
export XDG_DATA_HOME="$fixture/data"
mkdir -p "$fixture/bin" "$KWAKORE_INSTALL_PREFIX/bin" \
  "$KWAKORE_INSTALL_PREFIX/lib/kwakore/releases/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$XDG_CONFIG_HOME/systemd/user" "$XDG_DATA_HOME/applications" "$XDG_DATA_HOME/kwakore"
cat >"$fixture/bin/systemctl" <<'SH'
#!/usr/bin/env bash
[ "$1" = --user ] && shift
if [ "$1" = show ]; then
  case "$3" in
    Version) echo 257 ;;
    FragmentPath) echo "$XDG_CONFIG_HOME/systemd/user/$5" ;;
  esac
fi
SH
chmod +x "$fixture/bin/systemctl"
export PATH="$fixture/bin:$PATH"

root="$KWAKORE_INSTALL_PREFIX/lib/kwakore"
release="$root/releases/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
touch "$release/kwak" "$release/kwaklet" "$release/kwakore" "$release/libwebview.so" "$root/.install.lock"
ln -s 'releases/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' "$root/current"
ln -s "$root/current/kwak" "$KWAKORE_INSTALL_PREFIX/bin/kwak"
printf 'ExecStart=%s/current/kwakore\n' "$root" >"$XDG_CONFIG_HOME/systemd/user/kwakore.service"
printf 'ListenStream=%%t/kwakore/daemon.sock\n' >"$XDG_CONFIG_HOME/systemd/user/kwakore.socket"
entry="$XDG_DATA_HOME/applications/kwakore-napplet-11111111111111111111111111111111.desktop"
printf 'Exec=%s/current/kwak launch-token abc\n' "$root" >"$entry"

bash "$repo_root/scripts/uninstall.sh"
test ! -e "$root"
test ! -e "$KWAKORE_INSTALL_PREFIX/bin/kwak"
test ! -e "$XDG_CONFIG_HOME/systemd/user/kwakore.service"
test ! -e "$entry"
test -d "$XDG_DATA_HOME/kwakore"
bash "$repo_root/scripts/uninstall.sh"

# A unit pointing at another installation must be left alone.
printf 'ExecStart=/usr/bin/true\n' >"$XDG_CONFIG_HOME/systemd/user/kwakore.service"
if bash "$repo_root/scripts/uninstall.sh" >"$fixture/refusal.log" 2>&1; then
  echo "uninstall accepted a foreign service unit" >&2
  exit 1
fi
test -f "$XDG_CONFIG_HOME/systemd/user/kwakore.service"
