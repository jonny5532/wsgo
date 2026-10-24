package wsgo

import (
	"bytes"
	"io"
	"testing"
)

type fakeRequestReader struct {
	data []byte
	pos  int
}

func (f *fakeRequestReader) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *fakeRequestReader) Readline(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && f.pos < len(f.data) {
		p[n] = f.data[f.pos]
		f.pos++
		n++
		if p[n-1] == '\n' {
			break
		}
	}
	return n, nil
}

func (f *fakeRequestReader) Rewind() error { return nil }

func TestReadBufferSize(t *testing.T) {
	cases := []struct {
		toRead int64
		want   int
	}{
		{0, 0},  // read-to-EOF, handled incrementally
		{-1, 0}, // negative means read-to-EOF
		{1, 1},
		{4096, 4096},
		{maxWsgiReadSize, maxWsgiReadSize},
		{maxWsgiReadSize + 1, maxWsgiReadSize},
		{1 << 40, maxWsgiReadSize},
	}

	for _, c := range cases {
		if got := readBufferSize(c.toRead); got != c.want {
			t.Errorf("readBufferSize(%d) = %d, want %d", c.toRead, got, c.want)
		}
	}
}

func TestReadLineAll(t *testing.T) {
	// Simple line.
	r := &fakeRequestReader{data: []byte("hello\nworld\n")}
	if got := readLineAll(r, maxWsgiReadSize); !bytes.Equal(got, []byte("hello\n")) {
		t.Errorf("got %q, want %q", got, "hello\n")
	}

	// Line longer than one internal chunk.
	long := bytes.Repeat([]byte("a"), readlineChunkSize*2+17)
	r = &fakeRequestReader{data: append(long, []byte("\nafter")...)}
	if got := readLineAll(r, maxWsgiReadSize); !bytes.Equal(got, append(long, '\n')) {
		t.Errorf("long line: got %d bytes, want %d", len(got), len(long)+1)
	}

	// No trailing newline: EOF ends the line.
	r = &fakeRequestReader{data: []byte("no-newline")}
	if got := readLineAll(r, maxWsgiReadSize); !bytes.Equal(got, []byte("no-newline")) {
		t.Errorf("got %q, want %q", got, "no-newline")
	}

	// Respects the cap.
	r = &fakeRequestReader{data: bytes.Repeat([]byte("b"), 100)}
	if got := readLineAll(r, 10); len(got) != 10 {
		t.Errorf("cap: got %d bytes, want 10", len(got))
	}
}
