package wsgo

import (
	"errors"
	"io"
	"log"
	"net/http"
	"runtime"
	"unsafe"
)

/*
#cgo pkg-config: python-3-embed

#include <Python.h>
#include <time.h>

// Is a macro in Python <3.8 so needs concreting
int _PyIter_Check(PyObject *o) {
	return PyIter_Check(o);
}


extern void go_wsgi_start_response(long request_id, const char* status, int status_len, const char** header_parts, int* header_part_lengths, int headers_size);
extern PyObject *go_wsgi_read_request(long request_id, long to_read);
extern PyObject *go_wsgi_read_request_line(long request_id);
extern void go_add_cron(PyObject *func, long period, long min, long hour, long day, long mon, long wday);
extern void go_notify_parked(const char* parked_id, int parked_id_len, int action, const char* param, int param_len);


// _PyCFunctionFast signature
PyObject *py_wsgi_start_response(PyObject *self, PyObject **args, Py_ssize_t nargs) {
	if(nargs==2) {
		Py_ssize_t status_len;
		const char *status = PyUnicode_AsUTF8AndSize(args[0], &status_len); // don't need to free

		if(status==0) {
			PyErr_Print();
			Py_IncRef(Py_None);
			return Py_None;
		}

		long request_id = PyLong_AsLong(self);

		int headers_size = PyList_Size(args[1]);
		if(headers_size<0) {
			PyErr_Print();
			Py_IncRef(Py_None);
			return Py_None;
		}

		const char* header_parts[headers_size*2];
		int header_part_lengths[headers_size*2];

		for(int i=0; i<headers_size; i++) {
			PyObject *tup = PyList_GetItem(args[1], i);
			if(tup==NULL) {
				PyErr_Print();
				Py_IncRef(Py_None);
				return Py_None;
			}

			int tup_size = PyTuple_Size(tup);
			if(tup_size<0) {
				PyErr_Print();
				Py_IncRef(Py_None);
				return Py_None;
			}

			if(tup_size!=2) {
				Py_IncRef(Py_None);
				return Py_None;
			}

			Py_ssize_t key_len;
			const char *key = PyUnicode_AsUTF8AndSize(PyTuple_GetItem(tup, 0), &key_len);

			Py_ssize_t val_len;
			const char *val = PyUnicode_AsUTF8AndSize(PyTuple_GetItem(tup, 1), &val_len);

			header_parts[i*2] = (char*)key;
			header_part_lengths[i*2] = (int)key_len;

			header_parts[1 + i*2] = (char*)val;
			header_part_lengths[1 + i*2] = (int)val_len;
		}

		go_wsgi_start_response(request_id, status, (int)status_len, header_parts, header_part_lengths, headers_size);
	}

	Py_IncRef(Py_None);
	return Py_None;
}

typedef struct wsgo_WsgiInput {
	PyObject_HEAD
	long request_id;
} wsgo_WsgiInput;

Py_ssize_t size_of_wsgi_input() {
	return sizeof(PyObject);
}

void wsgi_input_free(PyObject *self) {
	//printf("wsgi_input_free\n");
	PyObject_Del(self);
}

PyObject *wsgi_input_iter(PyObject *self) {
	printf("wsgi_input_iter\n");
	Py_INCREF(self);
	return self;
}

PyObject *wsgi_input_next(PyObject* self) {
	printf("wsgi_input_next\n");
	PyErr_SetNone(PyExc_StopIteration);
	return NULL;
}

PyObject *wsgi_input_read(wsgo_WsgiInput* self, PyObject* args) {
	long to_read = 0;
	if (!PyArg_ParseTuple(args, "|l:read", &to_read)) {
		return NULL;
	}

	return go_wsgi_read_request(self->request_id, to_read);
}

PyObject *wsgi_input_readline(wsgo_WsgiInput* self, PyObject* args) {
	return go_wsgi_read_request_line(self->request_id);
}

// TODO - implement more than just read
static PyMethodDef wsgi_input_methods[] = {
	{ "read",      (PyCFunction)wsgi_input_read,      METH_VARARGS, 0 },
	{ "readline",  (PyCFunction)wsgi_input_readline,  METH_VARARGS, 0 },
	// { "readlines", (PyCFunction)wsgi_input_readlines, METH_VARARGS, 0 },
	// { "close",     (PyCFunction)wsgi_input_close,     METH_VARARGS, 0 },
	// { "seek",     (PyCFunction)wsgi_input_seek,     METH_VARARGS, 0 },
	// { "tell",     (PyCFunction)wsgi_input_tell,     METH_VARARGS, 0 },
	// { "fileno",     (PyCFunction)wsgi_input_fileno,     METH_VARARGS, 0 },
	{ NULL, NULL }
};


PyTypeObject wsgi_input_type = {
		PyVarObject_HEAD_INIT(NULL, 0)
		"wsgo.WsgiInput",  //tp_name
		sizeof(wsgo_WsgiInput),     // tp_basicsize
		0,                      // tp_itemsize
		(destructor) wsgi_input_free,	// tp_dealloc
		0,                      // tp_print
		0,                      // tp_getattr
		0,                      // tp_setattr
		0,                      // tp_compare
		0,                      // tp_repr
		0,                      // tp_as_number
		0,                      // tp_as_sequence
		0,                      // tp_as_mapping
		0,                      // tp_hash
		0,                      // tp_call
		0,                      // tp_str
		0,                      // tp_getattr
		0,                      // tp_setattr
		0,                      // tp_as_buffer
#if defined(Py_TPFLAGS_HAVE_ITER)
		Py_TPFLAGS_DEFAULT | Py_TPFLAGS_HAVE_ITER,
#else
		Py_TPFLAGS_DEFAULT,
#endif
		"wsgi input object.",      // tp_doc
		0,                      // tp_traverse
		0,                      // tp_clear
		0,                      // tp_richcompare
		0,                      // tp_weaklistoffset
		wsgi_input_iter,        // tp_iter: __iter__() method
		wsgi_input_next,        // tp_iternext: next() method
		wsgi_input_methods,
		0,0,0,0,0,0,0,0,0,0,0,0
};

PyObject *create_wsgi_input(long request_id) {
	PyObject *inp = (PyObject *) PyObject_New(wsgo_WsgiInput, &wsgi_input_type);
	((wsgo_WsgiInput*)inp)->request_id = request_id;
	return inp;
}


// _PyCFunctionFast signature
static PyObject* wsgo_add_cron(PyObject *self, PyObject **args, Py_ssize_t nargs)
{
	if(nargs==7) {
		PyObject *func               = args[0];
		long period    = PyLong_AsLong(args[1]);
		long min       = PyLong_AsLong(args[2]);
		long hour      = PyLong_AsLong(args[3]);
		long day       = PyLong_AsLong(args[4]);
		long mon       = PyLong_AsLong(args[5]);
		long wday      = PyLong_AsLong(args[6]);

		go_add_cron(func, period, min, hour, day, mon, wday);
	}

	Py_IncRef(Py_None);
	return Py_None;
}

// _PyCFunctionFast signature
static PyObject* wsgo_notify_parked(PyObject *self, PyObject **args, Py_ssize_t nargs)
{
	if(nargs==3) {
		Py_ssize_t parked_id_len;
		const char *parked_id = PyUnicode_AsUTF8AndSize(args[0], &parked_id_len);

		int action = PyLong_AsLong(args[1]);

		Py_ssize_t param_len;
		const char *param;

		if(PyUnicode_Check(args[2])) {
			param = PyUnicode_AsUTF8AndSize(args[2], &param_len);
		} else {
			param_len = 0;
			param = 0;
		}

		go_notify_parked(parked_id, parked_id_len, action, param, param_len);
	}

	Py_IncRef(Py_None);
	return Py_None;
}

static PyMethodDef WsgoMethods[] = {
	{"add_cron", (PyCFunction)wsgo_add_cron, METH_FASTCALL, "Registers a cron handler"},
	{"notify_parked", (PyCFunction)wsgo_notify_parked, METH_FASTCALL, "Notifies a parked job"},
	{NULL, NULL, 0, NULL}
};

static PyModuleDef WsgoModule = {
	PyModuleDef_HEAD_INIT, "wsgo", NULL, -1, WsgoMethods,
	NULL, NULL, NULL, NULL
};

PyMODINIT_FUNC
PyInit_wsgo(void) {
	return PyModule_Create(&WsgoModule);
}

void register_wsgo_module() {
	PyImport_AppendInittab("wsgo", &PyInit_wsgo);
}

void initialise_python() {

#if PY_VERSION_HEX < 0x03080000

	// Before Python 3.8.0
	Py_UnbufferedStdioFlag = 1;
	Py_Initialize();

#else

	PyStatus status;

	PyConfig config;
	PyConfig_InitPythonConfig(&config);
	config.buffered_stdio = 0;

	status = Py_InitializeFromConfig(&config);
	if (PyStatus_Exception(status)) {
		PyConfig_Clear(&config);
		Py_ExitStatusException(status);
	}

	PyConfig_Clear(&config);

#endif

}

uint64_t get_thread_cpu_time() {
	struct timespec t;
	clock_gettime(CLOCK_THREAD_CPUTIME_ID, &t);
	return (t.tv_sec*1000) + (t.tv_nsec/1000000);
}


*/
import "C"

var app_func *C.PyObject
var start_response_def C.PyMethodDef

func CreateStartResponseFunction(requestId int64) *C.PyObject {
	self := C.PyLong_FromLong((C.long)(requestId))
	start_response := C.PyCFunction_NewEx(&start_response_def, self, nil)
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

func ReadWsgiResponseToWriter(response *C.PyObject, w io.Writer) error {
	iter := C.PyObject_GetIter(response)
	defer C.Py_DecRef(iter)

	if iter == nil {
		C.PyErr_Print()
		return errors.New("Bad Gateway")
	}

	if C._PyIter_Check(iter) == 0 {
		log.Println("Response isn't iterable")
		return errors.New("Bad Gateway")
	}

	for {
		item := C.PyIter_Next(iter)
		if item == nil {
			break
		}
		defer C.Py_DecRef(item)

		var buf *C.char
		var size C.Py_ssize_t

		ret := C.PyBytes_AsStringAndSize(item, &buf, &size)
		if ret == -1 {
			C.PyErr_Print()
			break
		}

		// safe version: (does a memcpy internally)
		v := C.GoBytes(unsafe.Pointer(buf), (C.int)(size))
		// unsafe version: (doesn't, we trust that the writer won't keep a reference)
		//v := unsafe.Slice((*byte)(unsafe.Pointer(buf)), size)

		// Release the GIL whilst we write (does the fact we're borrowing a reference above matter???)
		gilState := C.PyThreadState_Get()
		C.PyEval_SaveThread()

		n, err := w.Write(v)

		// Regrab the GIL
		C.PyEval_RestoreThread(gilState)

		if err != nil {
			log.Println("Failed to write response:", err)
			break
		}
		if n != int(size) {
			log.Println("Only wrote", n, "of", size, "bytes!")
			break
		}
	}

	// We must call .close() on the response if present
	closeString := C.CString("close")
	defer C.free(unsafe.Pointer(closeString))
	if C.PyObject_HasAttrString(response, closeString) == 1 {
		close := C.PyObject_GetAttrString(response, closeString)
		C.PyObject_CallObject(close, nil)
		C.Py_DecRef(close)
	}

	return nil
}

func InitPythonInterpreter(module_name string) {
	// We need to lock the calling thread, since we want to be able to deinit
	// later from the same thread.
	runtime.LockOSThread()

	C.register_wsgo_module() // must happen before Py_Initialize

	C.initialise_python()

	s := C.CString("path")
	sys_path := C.PySys_GetObject(s)
	C.free(unsafe.Pointer(s))

	s = C.CString(".")
	C.PyList_Append(sys_path, C.PyUnicode_FromString(s))
	C.free(unsafe.Pointer(s))

	// Note - faulthandler can segfault when printing tracebacks in response to
	// signals since the GIL doesn't get acquired, and stack state can change
	// underneath it. Thus we will handle the SIGUSR1-triggered stack traces
	// from our own signal handler elsewhere, grabbing the GIL ourselves.

	cmd := C.CString(`
import faulthandler
faulthandler.enable() # dump all thread tracebacks on error signals

import wsgo
def _cron_decorator(*args):
	def cron(func):
		wsgo.add_cron(func, 0, *args)
		return func
	return cron
wsgo.cron = _cron_decorator

def _timer_decorator(period_seconds):
	def timer(func):
		wsgo.add_cron(func, period_seconds, 0, 0, 0, 0, 0)
		return func
	return timer
wsgo.timer = _timer_decorator

wsgo.RETRY = 1
wsgo.HTTP_204 = 2
wsgo.HTTP_504 = 3

`)
	defer C.free(unsafe.Pointer(cmd))
	C.PyRun_SimpleStringFlags(cmd, nil)

	s = C.CString(module_name)
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
	C.Py_IncRef(app_func)
	C.Py_DecRef(app_dict)

	start_response_def.ml_name = C.CString("start_response")
	start_response_def.ml_meth = C.PyCFunction(C.py_wsgi_start_response)
	start_response_def.ml_flags = C.METH_FASTCALL

	C.PyType_Ready(&C.wsgi_input_type)

	C.PyEval_SaveThread()
}

func DeinitPythonInterpreter() {
	// Grab the gil
	C.PyGILState_Ensure()

	// Finalize, so that we call any atexit handlers
	C.Py_Finalize()
}

func PyEval(code string) (*C.PyObject, func()) {
	runtime.LockOSThread()
	gilState := C.PyGILState_Ensure()

	cmd := C.CString(code)
	defer C.free(unsafe.Pointer(cmd))

	m := C.CString("__main__")
	defer C.free(unsafe.Pointer(m))
	globals := C.PyModule_GetDict(C.PyImport_AddModule(m))
	obj := C.PyRun_StringFlags(cmd, C.Py_eval_input, globals, globals, nil)
	log.Println(obj)

	done := func() {
		C.Py_DecRef(obj)
		C.PyGILState_Release(gilState)
		runtime.UnlockOSThread()
	}

	return obj, done
}

func GetThreadCpuTime() uint64 {
	return uint64(C.get_thread_cpu_time());
}