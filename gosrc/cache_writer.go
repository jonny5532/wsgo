package main

import (
	"net/http"
	"strconv"
)

type CacheWriter struct {
	writer        http.ResponseWriter
	cacheHeader   http.Header
	skipCaching   bool //don't bother trying to cache the response (wrong type or too big)
	doneBuffering bool
	statusCode    int
	buf           []byte
}

var cacheWriterLimit = 1000000

func NewCacheWriter(w http.ResponseWriter) *CacheWriter {
	return &CacheWriter{
		writer: w,
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
	}
}

func (cw *CacheWriter) Header() http.Header {
	if cw.writer == nil {
		return cw.cacheHeader
	}
	return cw.writer.Header()
}

func (cw *CacheWriter) WriteOld(b []byte) (int, error) {
	var n int
	var err error
	if cw.writer != nil {
		n, err = cw.writer.Write(b)
	} else {
		n, err = len(b), nil
	}

	if cw.skipCaching {
		// we've given up caching the response
	} else if err != nil || len(cw.buf)+n > cacheWriterLimit {
		// either erroring or got too big to cache, give up
		cw.skipCaching = true
		cw.buf = []byte{}
	} else {
		cw.buf = append(cw.buf, b[:n]...)
	}

	return n, err
}

func (cw *CacheWriter) Write(b []byte) (int, error) {
	// TODO - make this simpler

	// algorithm:

	// the first responseBufferLength bytes go straight into buf
	// once we hit responseBufferLength, we:
	//  - output the entire responseBufferLength
	//  - write the remainder of b directly
	// then if we're skipCaching, empty buf and go into straight-through mode
    // then check whether we've hit cacheWriterLimit, if so, empty buf and enable skipCaching
    // if not, keep filling buf whilst outputting directly

    cacheStartOffset := 0

    if !cw.doneBuffering {
	    remainingBufferSpace := responseBufferLength - len(cw.buf)
	    //if remainingBufferSpace > 0 {
	    	toBuffer := min(len(b), remainingBufferSpace)
	    	cw.buf = append(cw.buf, b[:toBuffer]...)
	    	cacheStartOffset = toBuffer
	    	if len(cw.buf) >= responseBufferLength {
	    		// have filled the buffer, now write it out
				cw.writer.WriteHeader(cw.statusCode)
	    		_, err := cw.writer.Write(cw.buf)
	    		cw.doneBuffering = true
	    		if err != nil {
					cw.skipCaching = true
		    		// technically we didn't write any of b
		    		return 0, err
		    	}
	    	}
	    	if len(b) > toBuffer {
	    		// need to keep caching potentially??
	    		if len(cw.buf) + (len(b)-toBuffer) > cacheWriterLimit {
	    			// will overflow cache, stop
	    			cw.skipCaching = true
	    		}

	    		// still some unsent b remaining
	    		n, err := cw.writer.Write(b[toBuffer:len(b)])
	    		if err != nil {
					cw.skipCaching = true
	    			return n, err
	    		}
	    	}
	    //} else {
    	//	cw.doneBuffering = true
 		//	cw.writer.WriteHeader(cw.statusCode) //???
	    //}
	} else if cw.writer != nil {
		n, err := cw.writer.Write(b)
		if err != nil {
			cw.skipCaching = true
			return n, err
		}
	}

	if cw.skipCaching {
		// we've given up caching the response
	} else if len(cw.buf) + ( len(b)-cacheStartOffset ) > cacheWriterLimit {
		// got too big to cache, give up
		cw.skipCaching = true
	} else if len(b) - cacheStartOffset > 0 {
		//is some remaining in the buffer to cache
		cw.buf = append(cw.buf, b[cacheStartOffset:len(b)]...)
	}

	if cw.skipCaching && cw.doneBuffering && len(cw.buf)>0 {
	 	cw.buf = []byte{}
	}

	return len(b), nil
}

// func (cw *CacheWriter) Write2(b []byte) (int, error) {
// 	toBuffer := 0
// 	if cacheWriterLimit > 0 && !cw.skipCaching {
// 		toCache = cacheWriterLimit
// 	}
// 	if responseBufferLength > 0 && cw.writer!=nil && responseBufferLength > toBuffer {
// 		toBuffer = responseBufferLength
// 	}

// 	toBuffer = min(
// 		toBuffer - len(cw.buf),
// 		len(b),
// 	)

// 	if toBuffer > 0 {
// 		cw.buf = append(cw.buf, b[:toBuffer]...)
// 	}



// }

func (cw *CacheWriter) Flush() error {
	if cw.writer == nil || cw.doneBuffering {
		return nil
	}


	cw.writer.WriteHeader(cw.statusCode)
	_, err := cw.writer.Write(cw.buf)
	cw.doneBuffering = true
	return err
}

func (cw *CacheWriter) WriteHeader(statusCode int) {
	if cw.doneBuffering && cw.writer != nil {
//	if cw.writer != nil {
		cw.writer.WriteHeader(statusCode)
	}
	contentLength, err := strconv.Atoi(cw.Header().Get("Content-length"))

	cw.statusCode = statusCode

	if statusCode != 200 && statusCode != 204 && statusCode != 301 {
		// probably an error of some kind, skip
		cw.skipCaching = true
	} else if err == nil && contentLength > cacheWriterLimit {
		// too big to cache, skip
		cw.skipCaching = true
	} else {
//		cw.statusCode = statusCode
		//if err == nil {
			//preallocate buf
			//cw.buf = make([]byte, 0, contentLength)
		//}
	}
}
