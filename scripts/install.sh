#!/usr/bin/env bash
set -euo pipefail

repo="hzrd149/verdana"
action="${VERDANA_ACTION:-install}"
version="${VERDANA_VERSION:-latest}"
install_dir="${VERDANA_INSTALL_DIR:-$HOME/.local/bin}"
purge=false

usage() {
  cat <<'EOF'
Usage: install.sh [install|uninstall] [options]

Options:
  --version VERSION    Install a release such as v1.2.3 (default: latest)
  --install-dir PATH   Binary directory (default: ~/.local/bin)
  --purge              Also remove Verdana's user data when uninstalling
  -h, --help           Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    install|uninstall) action="$1"; shift ;;
    --version) version="${2:?--version needs a value}"; shift 2 ;;
    --install-dir) install_dir="${2:?--install-dir needs a value}"; shift 2 ;;
    --purge) purge=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$(uname -s)" in
  Linux) os=linux; data_dir="${XDG_CONFIG_HOME:-$HOME/.config}/Verdana" ;;
  Darwin) os=darwin; data_dir="$HOME/Library/Application Support/Verdana" ;;
  *) echo "This installer supports Linux and macOS. Use install.ps1 on Windows." >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

path_line='export PATH="$HOME/.local/bin:$PATH" # Verdana installer'
shell_profile() {
  case "${SHELL:-}" in
    */zsh) printf '%s\n' "$HOME/.zshrc" ;;
    */bash)
      if [[ "$os" == darwin ]]; then printf '%s\n' "$HOME/.bash_profile"; else printf '%s\n' "$HOME/.bashrc"; fi
      ;;
    *) return 1 ;;
  esac
}

remove_path_line() {
  local profile tmp
  profile="$(shell_profile 2>/dev/null || true)"
  [[ -n "$profile" && -f "$profile" ]] || return 0
  tmp="${profile}.verdana.tmp"
  awk -v line="$path_line" '$0 != line' "$profile" > "$tmp"
  mv "$tmp" "$profile"
}

remove_integrations() {
  if [[ "$os" == linux ]]; then
    rm -f -- "${XDG_CONFIG_HOME:-$HOME/.config}/autostart/verdana.desktop"
    local data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
    local applications="$data_home/applications"
    rm -f -- \
      "$applications/com.verdana.Verdana.desktop" \
      "$data_home/gnome-shell/search-providers/com.verdana.Verdana.search-provider.ini" \
      "$data_home/dbus-1/services/com.verdana.Verdana.SearchProvider.service"
    local search_dir
    IFS=: read -ra search_dirs <<< "${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
    for search_dir in "${search_dirs[@]}"; do
      case "$search_dir/" in
        "$HOME/"*) rm -f -- "$search_dir/gnome-shell/search-providers/com.verdana.Verdana.search-provider.ini" ;;
      esac
    done
    if [[ -d "$applications" ]]; then
      find "$applications" -maxdepth 1 -type f \
        \( -name 'verdana-*.desktop' -o -name 'com.verdana.napp.*.desktop' \) \
        -delete
    fi
  else
    rm -f -- "$HOME/Library/LaunchAgents/com.verdana.launcher.plist"
    rm -rf -- "$HOME/Applications/Verdana Apps"
    local app
    shopt -s nullglob
    for app in "$HOME"/Applications/verdana-*.app; do rm -rf -- "$app"; done
    shopt -u nullglob
  fi
}

if [[ "$action" == uninstall ]]; then
  binary="$install_dir/verdana"
  if [[ -e "$binary" ]]; then
    rm -f -- "$binary"
    echo "Removed $binary"
  else
    echo "Verdana is not installed at $binary"
  fi
  if [[ "$install_dir" == "$HOME/.local/bin" ]]; then
    remove_path_line
  fi
  remove_integrations
  if $purge; then
    rm -rf -- "$data_dir"
    echo "Removed user data at $data_dir"
  else
    echo "User data was kept at $data_dir (use --purge to remove it)."
  fi
  exit 0
fi

command -v curl >/dev/null 2>&1 || { echo "curl is required." >&2; exit 1; }

asset="verdana-${os}-${arch}.tar.gz"
if [[ "$version" == latest ]]; then
  base_url="https://github.com/${repo}/releases/latest/download"
else
  [[ "$version" == v* ]] || version="v${version}"
  base_url="https://github.com/${repo}/releases/download/${version}"
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT

echo "Downloading $asset…"
curl --fail --location --retry 3 --silent --show-error \
  "$base_url/$asset" --output "$tmp_dir/$asset"
curl --fail --location --retry 3 --silent --show-error \
  "$base_url/SHA256SUMS" --output "$tmp_dir/SHA256SUMS"

expected="$(awk -v name="$asset" '$2 == name { print $1; exit }' "$tmp_dir/SHA256SUMS")"
[[ -n "$expected" ]] || { echo "No checksum found for $asset." >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp_dir/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp_dir/$asset" | awk '{print $1}')"
fi
[[ "$actual" == "$expected" ]] || { echo "Checksum verification failed." >&2; exit 1; }

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
mkdir -p -- "$install_dir"
install -m 0755 "$tmp_dir/verdana" "$install_dir/verdana"

if [[ "$os" == linux ]]; then
  data_home="${XDG_DATA_HOME:-$HOME/.local/share}"
  applications="$data_home/applications"
  providers=""
  IFS=: read -ra search_dirs <<< "${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
  for search_dir in "${search_dirs[@]}"; do
    case "$search_dir/" in
      "$HOME/"*) providers="$search_dir/gnome-shell/search-providers"; break ;;
    esac
  done
  if [[ -z "$providers" ]]; then
    echo "GNOME search integration needs a per-user directory in XDG_DATA_DIRS." >&2
    echo "Verdana was installed, but its GNOME search provider could not be registered." >&2
  fi
  services="$data_home/dbus-1/services"
  mkdir -p -- "$applications" "$services"
  [[ -z "$providers" ]] || mkdir -p -- "$providers"

  binary="$install_dir/verdana"
  escaped_binary="${binary//\\/\\\\}"
  escaped_binary="${escaped_binary//\"/\\\"}"
  escaped_binary="${escaped_binary//\$/\\$}"
  escaped_binary="${escaped_binary//\`/\\\`}"

  cat > "$applications/com.verdana.Verdana.desktop" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=Verdana
Comment=Discover and run Nostr applications
Exec="$escaped_binary"
Icon=applications-internet
Terminal=false
Categories=Network;
Keywords=Nostr;Napp;Napplet;
EOF

  if [[ -n "$providers" ]]; then
    cat > "$providers/com.verdana.Verdana.search-provider.ini" <<'EOF'
[Shell Search Provider]
DesktopId=com.verdana.Verdana.desktop
BusName=com.verdana.Verdana.SearchProvider
ObjectPath=/com/verdana/Verdana/SearchProvider
Version=2
EOF
  fi

  cat > "$services/com.verdana.Verdana.SearchProvider.service" <<EOF
[D-BUS Service]
Name=com.verdana.Verdana.SearchProvider
Exec="$escaped_binary" --background
EOF
fi

if [[ "$install_dir" == "$HOME/.local/bin" && ":$PATH:" != *":$install_dir:"* ]]; then
  profile="$(shell_profile 2>/dev/null || true)"
  if [[ -n "$profile" ]]; then
    touch "$profile"
    grep -Fqx "$path_line" "$profile" || printf '\n%s\n' "$path_line" >> "$profile"
    echo "Added $install_dir to PATH in $profile; open a new terminal to use it."
  else
    echo "Add $install_dir to your PATH to run Verdana from a terminal."
  fi
fi

echo "Installed Verdana at $install_dir/verdana"
