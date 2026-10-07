# Linux service bundle: kwakore-daemon, kwakore, the napplet child and
# libwebview.so side by side in dist/<version>/kwakore-<version>-linux-<arch>/,
# plus dist/<version>/kwakore-linux-<arch>.tar.gz and SHA256SUMS. The daemon
# runs the napplet sibling of its own executable, so the four files ship and
# install together (scripts/install.sh, packaging/systemd/user). Extra
# arguments go to the script: --version VERSION, --out DIR. Building the other
# architecture needs CC set to a C cross compiler for it.
#
# build the Linux service bundle and checksummed archive for this machine
bundle *args:
    bash scripts/build-linux-bundle.sh {{args}}

# build the linux/amd64 service bundle and archive
bundle-linux-amd64 *args:
    bash scripts/build-linux-bundle.sh --arch amd64 {{args}}

# build the linux/arm64 service bundle and archive (cross builds need CC)
bundle-linux-arm64 *args:
    bash scripts/build-linux-bundle.sh --arch arm64 {{args}}

# The archive and SHA256SUMS carry the names the release publishes and
# scripts/install.sh downloads (kwakore-linux-ARCH.tar.gz), so a local bundle
# installs the same way: scripts/install.sh --archive dist/VERSION/kwakore-linux-ARCH.tar.gz
# verifies it against the SHA256SUMS beside it before unpacking.
#
# build this machine's bundle twice and check it the way CI does: identical
# bytes, exactly the four files, matching SHA256SUMS, and a child that starts
bundle-check:
    bash scripts/smoke-linux-service.sh --bundle-only

# The napplet child loads libwebview.so from the directory it is installed in
# (WEBVIEW_PATH, set by the daemon). The copies come from the pinned go-webview
# module and are git-ignored; the bundle and child tests need them. GOOS/GOARCH
# must be unset here: go generate builds its copy program for this machine.
webview-libs:
    cd desktop && go generate ./internal/webviewlib

# the ui kit carries the launcher's own face (desktop/assets/*.ttf is
# Verdana, in three faces) as woff2, inlined by backend/webview/embed.go.
# Regenerate them when those ttf files change.
fonts:
	(cd desktop/assets && woff2_compress v.TTF && woff2_compress vb.ttf && woff2_compress vi.ttf)
	cp desktop/assets/v.woff2 desktop/assets/vb.woff2 desktop/assets/vi.woff2 backend/webview/fonts/
	rm -f desktop/assets/*.woff2
