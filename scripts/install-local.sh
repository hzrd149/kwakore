#!/usr/bin/env bash
# Build this checkout and install its bundle for the current user.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "kwakore install: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

bash "$repo_root/scripts/build-linux-bundle.sh" --version local --out "$repo_root/dist"
bash "$repo_root/scripts/install.sh" --archive "$repo_root/dist/local/kwakore-linux-$arch.tar.gz"
