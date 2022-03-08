package main

import (
	"net"
	"net/http"
	"strings"
	"unsafe"
)

/*
#include <Python.h>

extern PyObject *create_wsgi_input(long request_id);
*/
import "C"

func CreateWsgiEnvironment(requestId int64, req *http.Request) *C.PyObject {
	environ := C.PyDict_New()
	PyDictSet(environ, "REQUEST_METHOD", req.Method)
	PyDictSet(environ, "SCRIPT_NAME", "")
	PyDictSet(environ, "PATH_INFO", req.URL.Path)
	PyDictSet(environ, "QUERY_STRING", req.URL.RawQuery)
	PyDictSet(environ, "CONTENT_TYPE", req.Header.Get("Content-type"))
	PyDictSet(environ, "CONTENT_LENGTH", req.Header.Get("Content-length"))
	PyDictSet(environ, "SERVER_NAME", "wsgo.invalid")
	PyDictSet(environ, "SERVER_PORT", "8000")
	PyDictSet(environ, "SERVER_PROTOCOL", req.Proto)
	PyDictSet(environ, "HTTP_HOST", req.Host)
	host, port, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		PyDictSet(environ, "REMOTE_ADDR", host)
		PyDictSet(environ, "REMOTE_PORT", port)
	}
	scheme := req.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
	}
	PyDictSet(environ, "wsgi.url_scheme", scheme)
	multiprocess := C.Py_False
	if processes > 1 {
		multiprocess = C.Py_True
	}
	PyDictSetObject(environ, "wsgi.multiprocess", multiprocess)
	PyDictSetObject(environ, "wsgi.multithread", C.Py_True)
	PyDictSetObject(environ, "wsgi.run_once", C.Py_False)
	wsgi_version := C.PyTuple_New(2)
	C.PyTuple_SetItem(wsgi_version, 0, C.PyLong_FromLong(1)) //steals
	C.PyTuple_SetItem(wsgi_version, 1, C.PyLong_FromLong(0)) //steals
	PyDictSetObject(environ, "wsgi.version", wsgi_version)
	C.Py_DecRef(wsgi_version)

	//from golang cgi
	for k, v := range req.Header {
		k = strings.Map(upperCaseAndUnderscore, k)
		if k == "PROXY" {
			// golang cgi issue 16405
			continue
		}

		joinStr := ", "
		if k == "COOKIE" {
			joinStr = "; "
		}

		PyDictSet(environ, "HTTP_"+k, strings.Join(v, joinStr))
	}

	wsgi_input := C.create_wsgi_input(C.long(requestId))
	PyDictSetObject(environ, "wsgi.input", wsgi_input)
	C.Py_DecRef(wsgi_input)

	s := C.CString("stderr")
	stderr := C.PySys_GetObject(s) //borrowed
	C.free(unsafe.Pointer(s))
	PyDictSetObject(environ, "wsgi.errors", stderr)

	return environ
}

// from golang cgi
func upperCaseAndUnderscore(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z':
		return r - ('a' - 'A')
	case r == '-':
		return '_'
	case r == '=':
		// Maybe not part of the CGI 'spec' but would mess up
		// the environment in any case, as Go represents the
		// environment as a slice of "key=value" strings.
		return '_'
	}
	// TODO: other transformations in spec or practice?
	return r
}

func PyDictSet(dict *C.PyObject, key string, value string) {
	key_str := C.CString(key)
	defer C.free(unsafe.Pointer(key_str))
	key_obj := C.PyUnicode_FromString(key_str)
	defer C.Py_DecRef(key_obj)

	value_str := C.CString(value)
	defer C.free(unsafe.Pointer(value_str))
	value_obj := C.PyUnicode_FromString(value_str)
	defer C.Py_DecRef(value_obj)

	C.PyDict_SetItem(dict, key_obj, value_obj)
}

func PyDictSetObject(dict *C.PyObject, key string, obj *C.PyObject) {
	key_str := C.CString(key)
	defer C.free(unsafe.Pointer(key_str))
	key_obj := C.PyUnicode_FromString(key_str)
	defer C.Py_DecRef(key_obj)

	C.PyDict_SetItem(dict, key_obj, obj)
}
