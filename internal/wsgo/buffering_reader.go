package wsgo

import (
	"errors"
	"io"
)

type BufferingReader struct {
	r        io.Reader
	buf      []byte
	initial  []byte
	// whether `initial` holds the entire request or not
	partial  bool
}

func NewBufferingReader(r io.Reader, bufLen int) *BufferingReader {
	b := &BufferingReader{
		r: r,
	}

	// Read and buffer an initial chunk
	if bufLen > 0 {
		buf := make([]byte, bufLen)
		n, _ := io.ReadFull(r, buf)
		b.buf = buf[:n]
		b.initial = b.buf
	}
	return b
}

func (b *BufferingReader) Read(p []byte) (int, error) {
	if len(b.buf) > 0 {
		to_read := min(len(b.buf), len(p))
		copy(p, b.buf[:to_read])
		b.buf = b.buf[to_read:len(b.buf)]

		if to_read < len(p) {
			// Still some room left in the buffer, so read some more
			n, err := b.r.Read(p[to_read:len(p)])
			if n > 0 {
				b.partial = true
			}
			return to_read + n, err
		}

		return to_read, nil
	}

	n, err := b.r.Read(p)
	if n > 0 {
		b.partial = true
	}
	return n, err
}

func (b *BufferingReader) Readline(p []byte) (n int, err error) {
	total := 0
	for {
		// does buffer need refilling?
		if len(b.buf)==0 {
			if cap(b.buf)<8192 {
				b.buf = make([]byte, 8192)
			}
			n, _ := io.ReadFull(b.r, b.buf)
			b.buf = b.buf[:n]
			if n==0 {
				return total, io.EOF
			}
			b.partial = true
		}

		var i int
		limit := min(len(p), len(b.buf))
		for i = 0; i < limit; i += 1 {
			p[i] = b.buf[i]
			if b.buf[i]==byte(10) {
				b.buf = b.buf[i+1:]
				return total + i + 1, nil
			}
		}

		b.buf = b.buf[i:]
		p = p[i:]
		total += i

		if len(p)==0 {
			// no output buffer left, return
			return total, nil
		}
	}
}

func (b *BufferingReader) Rewind() error {
	if b.partial {
		return errors.New("BufferingReader is not complete.")
	}

	b.buf = b.initial
	return nil
}
