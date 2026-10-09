#!/usr/bin/env bash
# Build the Kwakore Linux service bundle.
#
#   scripts/build-linux-bundle.sh [--arch amd64|arm64] [--version VERSION] [--out DIR]
#
# Produces, under DIR/VERSION (DIR defaults to dist/ in the repository):
#
#   kwakore-VERSION-linux-ARCH/   the four runtime files in one directory:
#       kwakore            foreground daemon (systemd ExecStart target)
#       kwak                      control CLI
#       kwaklet                  hardened napplet child (desktop/child)
#       libwebview.so             the pinned go-webview library the child loads
#   kwakore-linux-ARCH.tar.gz     exactly those four files under that directory
#   SHA256SUMS                    sha256 of every kwakore-linux-*.tar.gz there
#
# The daemon finds its child as the sibling "napplet" of its own resolved
# executable (linuxhost.DefaultProgramPath) and hands the child WEBVIEW_PATH
# set to that same directory, so the four files only work side by side.
#
# Repeatable: every run builds into a fresh staging directory beside the
# output and then replaces exactly the named bundle directory, archive and
# SHA256SUMS for this version. Nothing else under DIR is touched, and there is
# no wildcard cleanup. Builds are -trimpath with an empty build id and the
# archive uses fixed owners, sorted names and a fixed mtime, so identical
# inputs give identical archive bytes. Parallel builds of different
# architectures into one version directory serialize on DIR/VERSION/.lock.
#
# Both architectures build with cgo, as the release CI always has (the amd64
# event store is the cgo LMDB one). Building the other architecture needs CC
# set to a C cross compiler for it, such as aarch64-linux-gnu-gcc.
#
# Generated files are git-ignored (dist/); never commit them.

set -euo pipefail

usage() {
	echo "usage: $0 [--arch amd64|arm64] [--version VERSION] [--out DIR]" >&2
	exit 2
}

fail() {
	echo "build-linux-bundle: $*" >&2
	exit 1
}

arch=""
version=""
out=""
while [ $# -gt 0 ]; do
	case "$1" in
	--arch) [ $# -ge 2 ] || usage; arch=$2; shift 2 ;;
	--version) [ $# -ge 2 ] || usage; version=$2; shift 2 ;;
	--out) [ $# -ge 2 ] || usage; out=$2; shift 2 ;;
	-h | --help) usage ;;
	*) usage ;;
	esac
done

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
command -v go >/dev/null || fail "go not found"
command -v flock >/dev/null || fail "flock not found (util-linux)"

host_arch=$(env -u GOARCH go env GOHOSTARCH)
[ -n "$arch" ] || arch=$host_arch
case "$arch" in
amd64 | arm64) ;;
*) fail "unsupported architecture $arch (amd64 or arm64)" ;;
esac
if [ "$arch" != "$host_arch" ] && [ -z "${CC:-}" ]; then
	fail "building linux/$arch on a $host_arch host needs CC set to a linux/$arch C compiler"
fi

if [ -z "$version" ]; then
	version=$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo development)
fi
# The version names a directory and an archive member; keep it inert.
if ! [[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$ ]]; then
	fail "version must match [A-Za-z0-9][A-Za-z0-9._+-]{0,63}: $version"
fi

epoch=${SOURCE_DATE_EPOCH:-$(git -C "$repo_root" log -1 --format=%ct 2>/dev/null || echo 0)}
[[ "$epoch" =~ ^[0-9]+$ ]] || fail "SOURCE_DATE_EPOCH must be a whole number of seconds"

[ -n "$out" ] || out="$repo_root/dist"
mkdir -p -- "$out"
out=$(cd "$out" && pwd)
release_dir="$out/$version"
mkdir -p -- "$release_dir"

umask 022
name="kwakore-$version-linux-$arch"
archive="kwakore-linux-$arch.tar.gz"
stage=$(mktemp -d "$release_dir/.stage-$arch.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT
bundle="$stage/$name"
mkdir -- "$bundle"

# libwebview copies come from the pinned go-webview module. go generate builds
# its copy program for this machine, so GOOS/GOARCH must be unset for it.
(cd "$repo_root/desktop" && env -u GOOS -u GOARCH go generate ./internal/webviewlib) ||
	fail "could not generate the webview library copies"
lib="$repo_root/desktop/internal/webviewlib/lib/linux_$arch/libwebview.so"
[ -f "$lib" ] && [ ! -L "$lib" ] || fail "missing generated $lib"

export GOOS=linux GOARCH="$arch" CGO_ENABLED=1
ldflags="-s -w -buildid= -X kwakore/backend.Version=$version"
build() {
	go build -trimpath -buildvcs=false "$@"
}
(cd "$repo_root/backend" && build -ldflags "$ldflags -X main.version=$version" -o "$bundle/kwakore" ./cmd/kwakore-daemon) ||
	fail "could not build kwakore"
(cd "$repo_root/backend" && build -ldflags "$ldflags" -o "$bundle/kwak" ./cmd/kwakore) ||
	fail "could not build kwak"
# D-10: the child program is the napplet window host, the only window kind.
(cd "$repo_root/desktop" && build -ldflags "-s -w -buildid=" -o "$bundle/kwaklet" ./child) ||
	fail "could not build the napplet child"
cp -- "$lib" "$bundle/libwebview.so"
chmod 0755 "$bundle/kwakore" "$bundle/kwak" "$bundle/kwaklet"
chmod 0644 "$bundle/libwebview.so"
chmod 0755 "$bundle"

members=("$name/kwakore" "$name/kwak" "$name/kwaklet" "$name/libwebview.so")
tar --sort=name --format=gnu --owner=0 --group=0 --numeric-owner \
	--mtime="@$epoch" -C "$stage" -cf - "${members[@]}" | gzip -n -9 >"$stage/$archive"

# Publish under the version lock: replace exactly this bundle directory and
# archive, then recompute SHA256SUMS from the archives present.
exec 9>>"$release_dir/.lock"
flock 9
if [ -e "$release_dir/$name" ]; then
	mv -- "$release_dir/$name" "$stage/previous-bundle"
fi
mv -- "$bundle" "$release_dir/$name"
mv -f -- "$stage/$archive" "$release_dir/$archive"
(
	cd "$release_dir"
	for a in amd64 arm64; do
		if [ -f "kwakore-linux-$a.tar.gz" ]; then
			sha256sum "kwakore-linux-$a.tar.gz"
		fi
	done
) >"$stage/SHA256SUMS"
mv -f -- "$stage/SHA256SUMS" "$release_dir/SHA256SUMS"
flock -u 9

echo "$release_dir/$name"
echo "$release_dir/$archive"
echo "$release_dir/SHA256SUMS"
