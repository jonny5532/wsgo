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

type RequestReader interface {
	Read([]byte) (n int, err error);
	Readline([]byte) (n int, err error);
	Rewind() error;
}

var requestReaders map[int64]RequestReader
var requestReadersMutex sync.Mutex

func init() {
	requestReaders = make(map[int64]RequestReader)
}

func doReadRequest(request_id C.long, to_read C.long, read_line bool) *C.PyObject {
	// We should already be in a locked thread but we'll do this again to be safe
	runtime.LockOSThread()

	// Release the GIL
	gilState := C.PyThreadState_Get()
	C.PyEval_SaveThread()

	buf_len := 1047586 * 32 //32mb maximum read in one go
	if to_read > 0 {
		buf_len = int(to_read)
	}

	buf := make([]byte, buf_len)

	requestReadersMutex.Lock()
	rr := requestReaders[int64(request_id)]
	requestReadersMutex.Unlock()

	var n int
	if read_line {
		n, _ = rr.Readline(buf)
	} else {
		n, _ = io.ReadFull(rr, buf)
	}

	// safe version:
	// c_str := C.CString(string(buf[:n]))
	// defer C.free(unsafe.Pointer(c_str))

	// unsafe version: (relies on buf staying in scope)
	c_str := (*C.char)(unsafe.Pointer(&buf[0]))

	//Regrab the GIL
	C.PyEval_RestoreThread(gilState)

	ret := C.PyBytes_FromStringAndSize(c_str, C.long(n))

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
