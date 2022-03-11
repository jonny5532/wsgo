package wsgo

import (
	"io"
)

type BufferingReader struct {
	r io.Reader
	buf []byte
}

func NewBufferingReader(r io.Reader, bufLen int) *BufferingReader {
	b := &BufferingReader{
		r: r,
	}

	// Read and buffer an initial chunk
	buf := make([]byte, bufLen)
	n, _ := io.ReadFull(r, buf)
	b.buf = buf[:n]
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
			return to_read + n, err
		}

		return to_read, nil
	}

	n, err := b.r.Read(p)
	return n, err
}
