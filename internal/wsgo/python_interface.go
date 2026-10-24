package wsgo

import (
	"errors"
	"io"
	"log"
	"net/http"
	"unsafe"
)

/*
#cgo pkg-config: python3
#cgo CFLAGS: -DPy_LIMITED_API=0x030A0000

#include "module.h"
*/
import "C"

var app_func *C.PyObject

func CreateStartResponseFunction(requestId int64) *C.PyObject {
	self := C.PyLong_FromLong((C.long)(requestId))
	start_response := C.PyCFunction_NewEx(&C.start_response_def, self, nil)
	C.Py_DecRef(self)
	return start_response
}

// Calls the WSGI application function. Returns a new reference to the output.
func CallApplication(requestId int64, req *http.Request) *C.PyObject {
	app_func_args := C.PyTuple_New(2)
	C.PyTuple_SetItem(app_func_args, 0, CreateWsgiEnvironment(requestId, req))  //steals
	C.PyTuple_SetItem(app_func_args, 1, CreateStartResponseFunction(requestId)) //steals

	ret := C.PyObject_CallObject(app_func, app_func_args)
	C.Py_DecRef(app_func_args)

	return ret
}

// GetWsgiResponseIter returns a new reference to an iterator over the WSGI
// response, or nil if the response isn't iterable. Any Python exception is
// printed.
func GetWsgiResponseIter(response *C.PyObject) *C.PyObject {
	iter := C.PyObject_GetIter(response)
	if iter == nil {
		C.PyErr_Print()
		return nil
	}

	if C.PyIter_Check(iter) == 0 {
		log.Println("Response isn't iterable")
		C.Py_DecRef(iter)
		return nil
	}

	return iter
}

// closeWsgiResponse calls .close() on the response if it has one, returning
// an error if the attribute can't be fetched or the call fails. Any Python
// exception is printed and cleared. Must be called with the GIL held.
func closeWsgiResponse(response *C.PyObject) error {
	closeString := C.CString("close")
	defer C.free(unsafe.Pointer(closeString))
	if C.PyObject_HasAttrString(response, closeString) == 1 {
		close := C.PyObject_GetAttrString(response, closeString)
		if close == nil {
			C.PyErr_Print()
			return errors.New("Bad Gateway")
		}
		ret := C.PyObject_CallObject(close, nil)
		C.Py_DecRef(close)
		if ret == nil {
			C.PyErr_Print()
			return errors.New("Bad Gateway")
		}
		C.Py_DecRef(ret)
	}
	return nil
}

// WriteWsgiResponseToWriter writes the items produced by iter to w, and then
// closes the response. The caller keeps ownership of iter. Must be called
// with the GIL held.
func WriteWsgiResponseToWriter(response *C.PyObject, iter *C.PyObject, w io.Writer) error {
	writeErr := writeWsgiResponseBody(iter, w)
	closeErr := closeWsgiResponse(response)
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// writeWsgiResponseBody iterates the response and writes each chunk. Must be
// called with the GIL held.
func writeWsgiResponseBody(iter *C.PyObject, w io.Writer) error {
	for {
		item := C.PyIter_Next(iter)
		if item == nil {
			// NULL is returned both at clean exhaustion and when the
			// iterator raised. Don't leave an exception pending on the
			// worker's thread state for the next request to trip over.
			if C.PyErr_Occurred() != nil {
				C.PyErr_Print()
				return errors.New("Bad Gateway")
			}
			break
		}

		var buf *C.char
		var size C.Py_ssize_t

		ret := C.PyBytes_AsStringAndSize(item, &buf, &size)
		if ret == -1 {
			C.PyErr_Print()
			C.Py_DecRef(item)
			return errors.New("Bad Gateway")
		}

		// safe version: (does a memcpy internally)
		v := C.GoBytes(unsafe.Pointer(buf), (C.int)(size))
		// Release the item immediately, so the whole response isn't retained
		// in memory for the duration of the write.
		C.Py_DecRef(item)

		// Release the GIL whilst we write (does the fact we're borrowing a reference above matter???)
		gilState := C.PyThreadState_Get()
		C.PyEval_SaveThread()

		n, err := w.Write(v)

		// Regrab the GIL
		C.PyEval_RestoreThread(gilState)

		if err != nil {
			log.Println("Failed to write response:", err)
			return err
		}
		if n != int(size) {
			log.Println("Only wrote", n, "of", size, "bytes!")
			return io.ErrShortWrite
		}
	}

	return nil
}

// LoadApplication imports the WSGI module and finds its 'application'
// callable. Must be called with the GIL held.
func LoadApplication(module_name string) {
	s := C.CString(module_name)
	defer C.free(unsafe.Pointer(s))
	app := C.PyImport_ImportModule(s)
	if app == nil {
		C.PyErr_Print()
		ExitProcessInvalid("Couldn't import module: " + module_name)
	}

	app_dict := C.PyModule_GetDict(app)
	if app_dict == nil {
		ExitProcessInvalid("Couldn't import module dict?")
	}

	s = C.CString("application")
	app_func = C.PyDict_GetItemString(app_dict, s)
	C.free(unsafe.Pointer(s))
	// app_dict is a borrowed reference from PyModule_GetDict and must not be
	// decref'd. The lookup result is borrowed too, so take our own reference.
	if app_func == nil {
		ExitProcessInvalid("Couldn't find 'application' entrypoint in module")
	}
	C.Py_IncRef(app_func)
}

// runPythonCode runs some Python code in the __main__ namespace, printing any
// exception. Must be called with the GIL held.
func runPythonCode(code string) {
	cmd := C.CString(code)
	defer C.free(unsafe.Pointer(cmd))
	C.run_python_code(cmd)
}

func GetThreadCpuTime() uint64 {
	return uint64(C.get_thread_cpu_time())
}
