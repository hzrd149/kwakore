# Vendored @napplet/shim prelude

`prelude.global.js` is `dist/prelude.global.js` from npm
[`@napplet/shim`](https://github.com/napplet/web/tree/main/packages/shim)
**0.30.0** (MIT), byte for byte, with no Verdana patches.

- Source commit: napplet/web `956135bfc41a2cff5e45d6c68d9f9a4d68c50531`
- npm tarball integrity:
  `sha512-aGkHuT6NI28rC1x8LXDKnF7XTv8RVf1WieItCJqKag14jQqxUOIuDUqFru1uzE5BIj6TnuqO3LRwvQzxuw7BwQ==`
- sha256: `25d6bb0e737e698e0c499e35c1ccd590ef6f87f7a025554d3af41bc6d5890753`
- Size: 136157 bytes, with no trailing newline

`webview.ShimVersion` and `webview.ShimSHA256` (`../embed.go`) state the same
version and hash, and `TestShimPreludeIsPristineUpstream` fails if the embedded
bytes differ from it by a single byte. `.gitattributes` marks the file `-text`
so git never rewrites it. Never open it in an editor: most add a final newline,
which breaks the hash.

The launcher inlines the prelude into every napplet's srcdoc and activates it
with `NappletShimPrelude.install({domains})`, so `window.napplet.*` exists
before the napplet's own scripts run.

The six Verdana patches of the former `0.30.0+verdana.2` build, plus the
NAP-CONFIG `schemaError` correlation, are dropped. Where Verdana still needs
the behavior it lives in Go or in `napplet-host.js`; see `spec/CONFORMANCE.md`
(Dropped shim patches P1–P7). Accepted intents, for example, now reach a
handler napplet as an `inc.event` on the convention topic.

## Upgrading

1. Fetch `https://registry.npmjs.org/@napplet/shim/-/shim-{version}.tgz` with
   curl into a scratch directory (no package manager, no install scripts).
2. Check the tarball's sha512 against the `integrity` npm publishes for that
   version.
3. Extract `package/dist/prelude.global.js` and `cp` it over this file.
4. Update `ShimVersion` and `ShimSHA256` in `../embed.go` and the version,
   commit, integrity, sha256 and size in this README.
5. Regenerate `backend/testdata/napplet-conformance-{version}-envelopes.json`
   as described in `backend/nap_conformance_test.go`.
6. Run the backend tests (`cd backend && go test ./...`) and re-check the Go NAP
   handlers (`backend/nap*.go`) against the shim's `nap/src/*/shim.ts`.
