#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
dev_dir="$HOME/.cache/kwakore/dev"
arch=$(go env GOARCH)

case "$(go env GOOS)/$arch" in
linux/amd64 | linux/arm64) ;;
*) echo "just run supports linux/amd64 and linux/arm64" >&2; exit 1 ;;
esac

# The window host checks every component of its path. The checkout may be
# group-writable, so keep the binaries under the user's private cache.
umask 077
mkdir -p -- "$dev_dir"
if [ -L "$dev_dir" ]; then
    echo "development directory must not be a symlink: $dev_dir" >&2
    exit 1
fi

(cd "$repo_root/desktop" && env -u GOOS -u GOARCH go generate ./internal/webviewlib)
(cd "$repo_root/backend" && go build -o "$dev_dir/kwakore" ./cmd/kwakore-daemon)
(cd "$repo_root/backend" && go build -o "$dev_dir/kwak" ./cmd/kwakore)
(cd "$repo_root/desktop" && go build -o "$dev_dir/kwaklet" ./child)
cp -- "$repo_root/desktop/internal/webviewlib/lib/linux_$arch/libwebview.so" "$dev_dir/libwebview.so"
chmod 0755 "$dev_dir/kwakore" "$dev_dir/kwak" "$dev_dir/kwaklet"
chmod 0644 "$dev_dir/libwebview.so"

echo "Development CLI: $dev_dir/kwak" >&2
exec "$dev_dir/kwakore"
