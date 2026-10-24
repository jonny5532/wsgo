package wsgo

/*
#include <Python.h>
*/
import "C"

import (
	"io"
	"runtime"
	"sync"
	"unsafe"
)

const (
	// Largest single read buffer. Callers that need more data can loop, as
	// with any buffered file object.
	maxWsgiReadSize = 1047586 * 32 // 32mb maximum read in one go
	// Chunk size used when assembling a line, so a short line doesn't need a
	// multi-megabyte allocation.
	readlineChunkSize = 64 * 1024
)

type RequestReader interface {
	Read([]byte) (n int, err error)
	Readline([]byte) (n int, err error)
	Rewind() error
}

var requestReaders map[int64]RequestReader
var requestReadersMutex sync.Mutex

func init() {
	requestReaders = make(map[int64]RequestReader)
}

// readBufferSize returns the number of bytes to allocate for a read(n) call.
// A non-positive size means "read to EOF" and is handled separately (with a
// buffer that grows as needed), so callers must not allocate a buffer of this
// size when it returns zero.
func readBufferSize(toRead int64) int {
	if toRead <= 0 {
		return 0
	}
	if toRead > maxWsgiReadSize {
		return maxWsgiReadSize
	}
	return int(toRead)
}

// readLineAll reads a single line, growing the result as needed up to max
// bytes. rr.Readline returns as soon as it finds a newline or fills the
// supplied buffer, so keep reading until a newline, EOF, or the cap.
func readLineAll(rr RequestReader, max int) []byte {
	var out []byte
	chunk := make([]byte, readlineChunkSize)
	for len(out) < max {
		if space := max - len(out); space < len(chunk) {
			chunk = chunk[:space]
		}
		n, _ := rr.Readline(chunk)
		out = append(out, chunk[:n]...)
		if n == 0 || chunk[n-1] == '\n' || n < len(chunk) {
			break
		}
	}
	return out
}

func doReadRequest(request_id C.long, to_read C.long, read_line bool) *C.PyObject {
	// We should already be in a locked thread but we'll do this again to be safe
	runtime.LockOSThread()

	// Release the GIL
	gilState := C.PyThreadState_Get()
	C.PyEval_SaveThread()

	requestReadersMutex.Lock()
	rr := requestReaders[int64(request_id)]
	requestReadersMutex.Unlock()

	if rr == nil {
		// The request has finished and its reader has been removed. Raise a
		// Python exception rather than dereferencing a nil reader and
		// crashing, since applications may have kept wsgi.input alive on
		// another thread.
		C.PyEval_RestoreThread(gilState)
		runtime.UnlockOSThread()
		msg := C.CString("wsgi.input is no longer available (request has finished)")
		defer C.free(unsafe.Pointer(msg))
		C.PyErr_SetString(C.PyExc_OSError, msg)
		return nil
	}

	var buf []byte
	if read_line {
		buf = readLineAll(rr, maxWsgiReadSize)
	} else if to_read > 0 {
		buf = make([]byte, readBufferSize(int64(to_read)))
		n, _ := io.ReadFull(rr, buf)
		buf = buf[:n]
	} else {
		// read() with no size: read to EOF, bounded so a huge body can't
		// consume unbounded memory. io.ReadAll grows the buffer
		// incrementally, so small bodies don't pay for a 32MB allocation.
		buf, _ = io.ReadAll(io.LimitReader(rr, maxWsgiReadSize))
	}

	var c_str *C.char
	if len(buf) > 0 {
		// unsafe version: (relies on buf staying in scope)
		c_str = (*C.char)(unsafe.Pointer(&buf[0]))
	}

	//Regrab the GIL
	C.PyEval_RestoreThread(gilState)

	ret := C.PyBytes_FromStringAndSize(c_str, C.long(len(buf)))

	runtime.UnlockOSThread()

	return ret
}

//export go_wsgi_read_request
func go_wsgi_read_request(request_id C.long, to_read C.long) *C.PyObject {
	return doReadRequest(request_id, to_read, false)
}

//export go_wsgi_read_request_line
func go_wsgi_read_request_line(request_id C.long) *C.PyObject {
	return doReadRequest(request_id, 0, true)
}

func AddWsgiRequestReader(requestId int64, reader RequestReader) {
	requestReadersMutex.Lock()
	requestReaders[requestId] = reader
	requestReadersMutex.Unlock()
}

func RemoveWsgiRequestReader(requestId int64) {
	requestReadersMutex.Lock()
	delete(requestReaders, requestId)
	requestReadersMutex.Unlock()
}
