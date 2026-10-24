package wsgo

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// A CacheWriter passes a response through to the client, keeping a copy of
// it if its headers allow it to be stored in the page cache.
type CacheWriter struct {
	writer      http.ResponseWriter // nil if there's no client to send to
	header      http.Header         // used instead of the writer's if there's no client
	limit       *transferRateLimit
	statusCode  int
	wroteHeader bool
	finished    bool

	// Whether the response is being copied into buf for the page cache, and
	// (once the header has been written) how it would be cached.
	caching bool
	policy  cachePolicy
	buf     []byte

	// From the X-WSGo-Vary-Cookies and X-WSGo-Ignore-Params headers.
	varyCookies  string
	ignoreParams string
}

// Responses larger than this aren't cached.
var cacheWriterLimit = 1000000

// NewCacheWriter returns a writer for a response which might be cached.
func NewCacheWriter(w http.ResponseWriter) *CacheWriter {
	return &CacheWriter{
		writer:  w,
		limit:   newTransferRateLimit(http.NewResponseController(w).SetWriteDeadline),
		caching: true,
	}
}

// NewCacheOnlyCacheWriter returns a writer which only caches the response,
// for refreshing a cache entry after the client has been sent the old one.
func NewCacheOnlyCacheWriter() *CacheWriter {
	return &CacheWriter{header: make(http.Header), caching: true}
}

// NewNonCachingCacheWriter returns a writer which only passes the response
// through to the client.
func NewNonCachingCacheWriter(w http.ResponseWriter) *CacheWriter {
	return &CacheWriter{
		writer: w,
		limit:  newTransferRateLimit(http.NewResponseController(w).SetWriteDeadline),
	}
}

func (cw *CacheWriter) Header() http.Header {
	if cw.writer == nil {
		return cw.header
	}
	return cw.writer.Header()
}

// takeCacheDirectives removes the headers that control the page cache from the
// response, so that they aren't sent to the client, keeping their values for
// CachePage.
func (cw *CacheWriter) takeCacheDirectives() {
	header := cw.Header()
	cw.varyCookies = strings.Join(splitHeaderList(header.Values("X-WSGo-Vary-Cookies")), ", ")
	cw.ignoreParams = strings.Join(splitHeaderList(header.Values("X-WSGo-Ignore-Params")), ", ")
	header.Del("X-WSGo-Vary-Cookies")
	header.Del("X-WSGo-Ignore-Params")
}

// stopCaching gives up on caching the response, releasing the copy so far.
func (cw *CacheWriter) stopCaching() {
	cw.caching = false
	cw.buf = nil
}

func (cw *CacheWriter) WriteHeader(statusCode int) {
	if cw.finished || cw.wroteHeader {
		return
	}
	cw.wroteHeader = true
	cw.statusCode = statusCode

	// The headers are complete now, so it's known whether the response can
	// be cached, and it needn't be copied if not.
	if cw.caching {
		policy, ok := parseCachePolicy(statusCode, cw.Header())
		if ok {
			cw.policy = policy
		} else {
			cw.stopCaching()
		}
	}

	if cw.writer != nil {
		cw.writer.WriteHeader(statusCode)
	}
}

func (cw *CacheWriter) Write(b []byte) (int, error) {
	if cw.finished {
		return 0, errors.New("Response already finished.")
	}
	if !cw.wroteHeader {
		cw.WriteHeader(http.StatusOK)
	}

	if cw.caching {
		if len(cw.buf)+len(b) > cacheWriterLimit {
			cw.stopCaching()
		} else {
			cw.buf = append(cw.buf, b...)
		}
	}

	if cw.writer == nil {
		return len(b), nil
	}
	n, err := cw.writeToClient(b)
	if err != nil {
		// An incomplete response mustn't be cached.
		cw.stopCaching()
	}
	return n, err
}

// writeToClient writes to the client in pieces, enforcing the minimum
// transfer rate.
func (cw *CacheWriter) writeToClient(b []byte) (int, error) {
	if len(b) == 0 {
		return cw.writer.Write(b)
	}

	written := 0
	for written < len(b) {
		piece := b[written:min(len(b), written+transferChunkSize)]
		n, err := cw.limit.do(func() (int, error) {
			return cw.writer.Write(piece)
		})
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// Finish marks the response as complete, after which any further writes fail.
func (cw *CacheWriter) Finish() {
	cw.finished = true

	if cw.writer != nil {
		// The end of the response is sent once the handler returns, which
		// may be long after the last write if the application paused, so
		// the last write's deadline may already have passed.
		http.NewResponseController(cw.writer).SetWriteDeadline(time.Now().Add(transferGrace))
	}
}
