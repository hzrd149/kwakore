//go:build linux && arm64

package webviewlib

import _ "embed"

// Name is the file name go-webview's loader looks for on this target.
const Name = "libwebview.so"

// Data is the library, copied from the go-webview module by go generate.
//
//go:embed lib/linux_arm64/libwebview.so
var Data []byte
