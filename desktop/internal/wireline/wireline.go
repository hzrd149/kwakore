// Package wireline frames the pipe between the launcher and its webview
// child: one JSON wire message per line, in both directions. Every line is
// bounded, so a runaway or hostile side cannot make the other buffer without
// limit.
//
// Lengths are counted in raw bytes on the pipe, before any JSON decoding:
// escaping only ever makes a line longer, so an oversize message cannot slip
// under the cap by being encoded differently.
package wireline

import (
	"bufio"
	"io"
)

// initialBuf is where the line buffer starts; it grows (doubling) up to the
// cap only when a line actually needs it.
const initialBuf = 64 << 10

// Read calls fn with each newline-terminated line read from r, without the
// line terminator, until r ends. A final line with no trailing newline is
// still delivered. The slice handed to fn is only valid until fn returns;
// json.Unmarshal copies whatever it keeps.
//
// A line longer than max bytes is never delivered, not even in part: Read
// stops and returns bufio.ErrTooLong. Any other read error is returned as is,
// and a clean end of input returns nil.
func Read(r io.Reader, max int, fn func(line []byte)) error {
	sc := bufio.NewScanner(r)
	// the buffer must also hold the '\n', so a line of exactly max bytes
	// needs max+1 of room
	size := initialBuf
	if max+1 < size {
		size = max + 1
	}
	sc.Buffer(make([]byte, size), max+1)
	for sc.Scan() {
		line := sc.Bytes()
		// a reader that returns its last bytes together with io.EOF can
		// hand the scanner a final unterminated token as long as the whole
		// buffer, one byte over the cap
		if len(line) > max {
			return bufio.ErrTooLong
		}
		fn(line)
	}
	return sc.Err()
}
