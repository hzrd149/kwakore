//go:build linux && amd64

package webviewlib

import _ "embed"

// Name is the file name go-webview's loader looks for on this target.
const Name = "libwebview.so"

// Data is the library, copied from the go-webview module by go generate.
//
//go:embed lib/linux_amd64/libwebview.so
var Data []byte
