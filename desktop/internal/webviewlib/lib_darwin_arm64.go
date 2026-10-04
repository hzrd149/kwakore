//go:build darwin && arm64

package webviewlib

import _ "embed"

// Name is the file name go-webview's loader looks for on this target.
const Name = "libwebview.dylib"

// Data is the library, copied from the go-webview module by go generate.
//
//go:embed lib/darwin_arm64/libwebview.dylib
var Data []byte
