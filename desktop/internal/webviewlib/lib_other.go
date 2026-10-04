//go:build !((linux || darwin || windows) && (amd64 || arm64))

package webviewlib

// This target has no prebuilt libwebview. Data is nil, which
// childbin.Ensure refuses, so a napp window fails closed instead of the
// child searching the system for a library of the same name.

// Name is empty: there is no library to look for.
const Name = ""

// Data is nil on this target.
var Data []byte
