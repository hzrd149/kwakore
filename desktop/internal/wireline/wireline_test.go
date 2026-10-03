package wireline

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func collect(t *testing.T, r io.Reader, max int) ([]string, error) {
	t.Helper()
	var got []string
	err := Read(r, max, func(line []byte) {
		got = append(got, string(line))
	})
	return got, err
}

func TestReadDeliversLinesInOrder(t *testing.T) {
	got, err := collect(t, strings.NewReader("{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"), 64)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`{"a":1}`, `{"b":2}`, `{"c":3}`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadDeliversLineOfExactlyMax(t *testing.T) {
	const max = 100
	line := strings.Repeat("a", max)
	got, err := collect(t, strings.NewReader(line+"\n"+"next\n"), max)
	if err != nil {
		t.Fatalf("a line of exactly max bytes was rejected: %v", err)
	}
	if len(got) != 2 || got[0] != line || got[1] != "next" {
		t.Fatalf("got %d lines (%q...), want the max-length line then next", len(got), got)
	}
}

func TestReadRejectsLineOverMax(t *testing.T) {
	const max = 100
	for _, tail := range []string{"\n", "", "\nafter\n"} {
		// the over-cap line comes after a valid one, which is still delivered
		in := "ok\n" + strings.Repeat("a", max+1) + tail
		got, err := collect(t, strings.NewReader(in), max)
		if !errors.Is(err, bufio.ErrTooLong) {
			t.Fatalf("tail %q: err = %v, want bufio.ErrTooLong", tail, err)
		}
		if len(got) != 1 || got[0] != "ok" {
			t.Fatalf("tail %q: fn saw %q; the overlong line (or part of it) must never be delivered", tail, got)
		}
	}
}

func TestReadRejectsOverMaxFinalLineReturnedWithEOF(t *testing.T) {
	// iotest.DataErrReader hands back the last bytes together with io.EOF,
	// the case where the scanner could otherwise emit a buffer-sized token
	const max = 100
	in := iotest.DataErrReader(strings.NewReader(strings.Repeat("a", max+1)))
	got, err := collect(t, in, max)
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want bufio.ErrTooLong", err)
	}
	if len(got) != 0 {
		t.Fatalf("fn saw %d lines, want none", len(got))
	}
}

func TestReadRejectsOverMaxPastInitialBuffer(t *testing.T) {
	// a cap above the 64 KiB starting buffer: the buffer grows, then stops
	const max = 200 << 10
	ok := strings.Repeat("b", max)
	got, err := collect(t, strings.NewReader(ok+"\n"+strings.Repeat("a", max+1)+"\n"), max)
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want bufio.ErrTooLong", err)
	}
	if len(got) != 1 || got[0] != ok {
		t.Fatalf("got %d lines, want only the max-length one", len(got))
	}
}

func TestReadDeliversFinalLineWithoutNewline(t *testing.T) {
	got, err := collect(t, strings.NewReader("first\nlast"), 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != "last" {
		t.Fatalf("got %q, want the unterminated final line delivered", got)
	}
}

func TestReadEmptyInput(t *testing.T) {
	calls := 0
	err := Read(bytes.NewReader(nil), 64, func([]byte) { calls++ })
	if err != nil || calls != 0 {
		t.Fatalf("err = %v, calls = %d; want nil and no calls", err, calls)
	}
}

func TestReadReturnsOtherErrors(t *testing.T) {
	boom := errors.New("boom")
	got, err := collect(t, io.MultiReader(strings.NewReader("one\n"), iotest.ErrReader(boom)), 64)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the reader's error", err)
	}
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("got %q, want the line before the error", got)
	}
}
