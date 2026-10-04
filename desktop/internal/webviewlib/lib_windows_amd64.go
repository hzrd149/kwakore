//go:build windows && amd64

package webviewlib

import _ "embed"

// Name is the file name go-webview's loader looks for on this target.
const Name = "webview.dll"

// Data is the library, copied from the go-webview module by go generate.
//
//go:embed lib/windows_amd64/webview.dll
var Data []byte
