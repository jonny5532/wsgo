package wsgo

import (
	"errors"
	"net/http"
	"strconv"
)

type CacheWriter struct {
	writer        http.ResponseWriter
	cacheHeader   http.Header
	skipCaching   bool //don't bother trying to cache the response (wrong type or too big)
	doneBuffering bool
	finished      bool
	statusCode    int
	buf           []byte
}

var cacheWriterLimit = 1000000

func NewCacheWriter(w http.ResponseWriter) *CacheWriter {
	return &CacheWriter{
		writer:        w,
		doneBuffering: skipBuffering(),
	}
}

func NewCacheOnlyCacheWriter() *CacheWriter {
	return &CacheWriter{
		writer:        nil,
		cacheHeader:   make(http.Header),
		doneBuffering: true,
	}
}

func NewNonCachingCacheWriter(w http.ResponseWriter) *CacheWriter {
	return &CacheWriter{
		writer: w,
		skipCaching:   true,
		doneBuffering: skipBuffering(),
	}
}

func skipBuffering() bool {
	if responseBufferLength==0 {
		return true
	}
	return false
}

func (cw *CacheWriter) Header() http.Header {
	if cw.writer == nil {
		return cw.cacheHeader
	}
	return cw.writer.Header()
}

func (cw *CacheWriter) bufferedWrite(b []byte) (int, error, int) {
	// Write as much of b as possible to the internal buffer. 
	
	// When the buffer is full, write it out to the underlying writer (sending
	// the header first), and then write the rest of b out directly.

	// Returns (amount_written_if_error, error, buffered)

	// How much space is there left in the buffer?
	remainingBufferSpace := responseBufferLength - len(cw.buf)
	// How much will we buffer?
	toBuffer := min(len(b), remainingBufferSpace)

	if toBuffer > 0 {
		// Add it to the buffer!
		cw.buf = append(cw.buf, b[:toBuffer]...)
	}

	if len(cw.buf) >= responseBufferLength {
		// We have filled the buffer, now write it out

		cw.writer.WriteHeader(cw.statusCode)
		cw.doneBuffering = true
		if len(cw.buf) > 0 {
			_, err := cw.writer.Write(cw.buf)
			if err != nil {
				cw.skipCaching = true
				// Technically we didn't write any of b
				return 0, err, 0
			}
		}
	}
	if len(b) > toBuffer {
		// Still have more left to write out

		if len(cw.buf) + (len(b)-toBuffer) > cacheWriterLimit {
			// Will overflow cache, so stop caching
			cw.skipCaching = true
		}

		// Write out the remaining
		n, err := cw.writer.Write(b[toBuffer:len(b)])
		if err != nil {
			cw.skipCaching = true
			return n, err, 0
		}
	}

	return 0, nil, toBuffer
}

func (cw *CacheWriter) Write(b []byte) (int, error) {
	if cw.finished {
		return 0, errors.New("Response already finished.")
	}

    buffered := 0

    if !cw.doneBuffering {
		// We're still buffering, so write b to the buffer. May spill to the 
		// underlying writer if we exceed the buffer size.
		n, err, _buffered := cw.bufferedWrite(b)
		if err != nil {
			// Writing failed, bail now.
			return n, err
		}

		// Track how much of b got buffered (there might be some left that got 
		// written out instead).
		buffered = _buffered
	} else if cw.writer != nil {
		// Not buffering, just write out as normal.
		n, err := cw.writer.Write(b)
		if err != nil {
			cw.skipCaching = true
			return n, err
		}
	}

	if cw.skipCaching {
		// We've given up caching the response
	} else if len(cw.buf) + ( len(b)-buffered ) > cacheWriterLimit {
		// Got too big to cache, give up
		cw.skipCaching = true
	} else if len(b) - buffered > 0 {
		// We're still caching, and we didn't buffer the whole of b, so buffer 
		// the rest now for caching purposes.
		cw.buf = append(cw.buf, b[buffered:len(b)]...)
	}

	if cw.skipCaching && cw.doneBuffering && len(cw.buf)>0 {
		// Buffer no longer useful, release the memory now.
	 	cw.buf = []byte{}
	}

	return len(b), nil
}

func (cw *CacheWriter) Flush() error {
	if cw.finished {
		return nil
	}
	cw.finished = true

	if cw.writer == nil || cw.doneBuffering {
		return nil
	}

	if cw.statusCode==0 {
		// header was never written?
		return nil
	}

	cw.writer.WriteHeader(cw.statusCode)
	_, err := cw.writer.Write(cw.buf)
	cw.doneBuffering = true
	return err
}

func (cw *CacheWriter) WriteHeader(statusCode int) {
	if cw.finished {
		return
	}
	if cw.doneBuffering && cw.writer != nil {
		// Write header out now (if we were still buffering it'd get written 
		// when the buffer hits full).
		cw.writer.WriteHeader(statusCode)
	}
	contentLength, err := strconv.Atoi(cw.Header().Get("Content-length"))

	cw.statusCode = statusCode

	if statusCode != 200 && statusCode != 204 && statusCode != 301 {
		// Probably an error of some kind, skip
		cw.skipCaching = true
	} else if err == nil && contentLength > cacheWriterLimit {
		// Will be too big to cache, don't bother trying
		cw.skipCaching = true
	} else {
//		cw.statusCode = statusCode
		//if err == nil {
			//preallocate buf
			//cw.buf = make([]byte, 0, contentLength)
		//}
	}
}
