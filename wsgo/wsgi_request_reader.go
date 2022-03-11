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

var requestReaders map[int64]io.Reader
var requestReadersMutex sync.Mutex

func init() {
	requestReaders = make(map[int64]io.Reader)
}

//export go_wsgi_read_request
func go_wsgi_read_request(request_id C.long, to_read C.long) *C.PyObject {
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

	n, _ := io.ReadFull(rr, buf)

	// safe version:
	// c_str := C.CString(string(buf[:n]))
	// defer C.free(unsafe.Pointer(c_str))

	// hacky version without the extra copy (by exploiting the layout of slices):
	c_str := *(**C.char)(unsafe.Pointer(&buf))

	ret := C.PyBytes_FromStringAndSize(c_str, C.long(n))

	//Regrab the GIL
	C.PyEval_RestoreThread(gilState)
	runtime.UnlockOSThread()

	return ret
}

func AddWsgiRequestReader(requestId int64, reader io.Reader) {
	requestReadersMutex.Lock()
	requestReaders[requestId] = reader
	requestReadersMutex.Unlock()
}

func RemoveWsgiRequestReader(requestId int64) {
	requestReadersMutex.Lock()
	delete(requestReaders, requestId)
	requestReadersMutex.Unlock()
}
