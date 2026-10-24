// The _wsgo extension module

#include "module.h"

#include <stdio.h>
#include <time.h>

extern void go_wsgi_start_response(long request_id, const char* status, int status_len, const char** header_parts, int* header_part_lengths, int headers_size);
extern PyObject *go_wsgi_read_request(long request_id, long to_read);
extern PyObject *go_wsgi_read_request_line(long request_id);
extern void go_add_cron(PyObject *func, long period, long min, long hour, long day, long mon, long wday);
extern void go_notify_parked(const char* parked_id, int parked_id_len, int action, const char* param, int param_len);
extern int go_main(const char** args, int* arg_lengths, int args_size, const char* executable, int executable_len);

// Upper bound on the number of response headers accepted from an
// application. Protects against unbounded allocations/stack use when an
// application (or a library it uses) builds headers from untrusted input.
#define WSGO_MAX_RESPONSE_HEADERS 1024

// Sends the status line and headers to the Go side. Returns Py_None on
// success, or NULL with a Python exception set if the arguments are not
// valid WSGI (status string plus a list of 2-tuples of strings).
// METH_FASTCALL signature
static PyObject *py_wsgi_start_response(PyObject *self, PyObject *const *args, Py_ssize_t nargs) {
	if(nargs!=2) {
		PyErr_SetString(PyExc_TypeError, "start_response() takes exactly 2 arguments");
		return NULL;
	}

	Py_ssize_t status_len;
	const char *status = PyUnicode_AsUTF8AndSize(args[0], &status_len); // don't need to free
	if(status==NULL) {
		// Exception set by PyUnicode_AsUTF8AndSize
		return NULL;
	}
	if(status_len > 1024) {
		PyErr_SetString(PyExc_ValueError, "start_response() status line too long");
		return NULL;
	}

	long request_id = PyLong_AsLong(self);
	if(request_id==-1 && PyErr_Occurred()) {
		return NULL;
	}

	if(!PyList_Check(args[1])) {
		PyErr_SetString(PyExc_TypeError, "start_response() headers must be a list");
		return NULL;
	}

	Py_ssize_t headers_size = PyList_Size(args[1]);
	if(headers_size<0) {
		// Exception set by PyList_Size
		return NULL;
	}
	if(headers_size > WSGO_MAX_RESPONSE_HEADERS) {
		PyErr_SetString(PyExc_ValueError, "start_response() received too many headers");
		return NULL;
	}

	size_t parts_count = (size_t)headers_size * 2;
	const char **header_parts = PyMem_Malloc(parts_count * sizeof(char*));
	int *header_part_lengths = PyMem_Malloc(parts_count * sizeof(int));
	if(header_parts==NULL || header_part_lengths==NULL) {
		PyMem_Free(header_parts);
		PyMem_Free(header_part_lengths);
		PyErr_NoMemory();
		return NULL;
	}

	for(Py_ssize_t i=0; i<headers_size; i++) {
		// Pre-zero the slots so a failure part-way through can never leave
		// uninitialised data to be passed to Go.
		header_parts[i*2] = NULL;
		header_part_lengths[i*2] = 0;
		header_parts[1 + i*2] = NULL;
		header_part_lengths[1 + i*2] = 0;

		PyObject *tup = PyList_GetItem(args[1], i);
		if(tup==NULL) {
			goto error;
		}
		if(!PyTuple_Check(tup) || PyTuple_Size(tup)!=2) {
			PyErr_SetString(PyExc_TypeError, "each response header must be a (name, value) 2-tuple");
			goto error;
		}

		PyObject *key_obj = PyTuple_GetItem(tup, 0);
		PyObject *val_obj = PyTuple_GetItem(tup, 1);
		if(key_obj==NULL || val_obj==NULL) {
			goto error;
		}

		Py_ssize_t key_len = 0;
		const char *key = PyUnicode_AsUTF8AndSize(key_obj, &key_len);
		if(key==NULL) {
			goto error;
		}

		Py_ssize_t val_len = 0;
		const char *val = PyUnicode_AsUTF8AndSize(val_obj, &val_len);
		if(val==NULL) {
			goto error;
		}

		header_parts[i*2] = key;
		header_part_lengths[i*2] = (int)key_len;

		header_parts[1 + i*2] = val;
		header_part_lengths[1 + i*2] = (int)val_len;
	}

	go_wsgi_start_response(request_id, status, (int)status_len, header_parts, header_part_lengths, (int)headers_size);

	PyMem_Free(header_parts);
	PyMem_Free(header_part_lengths);

	Py_IncRef(Py_None);
	return Py_None;

error:
	PyMem_Free(header_parts);
	PyMem_Free(header_part_lengths);
	return NULL;
}

PyMethodDef start_response_def = {
	"start_response", (PyCFunction)(void(*)(void))py_wsgi_start_response, METH_FASTCALL, NULL
};

typedef struct wsgo_WsgiInput {
	PyObject_HEAD
	long request_id;
} wsgo_WsgiInput;

// Created once in PyInit__wsgo. The module uses single-phase init, so there
// is only ever one copy.
static PyTypeObject *wsgi_input_type = NULL;

static void wsgi_input_free(PyObject *self) {
	// Instances of a heap type hold a reference to their type.
	PyTypeObject *tp = Py_TYPE(self);
	PyObject_Free(self);
	Py_DECREF(tp);
}

static PyObject *wsgi_input_iter(PyObject *self) {
	printf("wsgi_input_iter\n");
	Py_INCREF(self);
	return self;
}

static PyObject *wsgi_input_next(PyObject* self) {
	printf("wsgi_input_next\n");
	PyErr_SetNone(PyExc_StopIteration);
	return NULL;
}

static PyObject *wsgi_input_read(wsgo_WsgiInput* self, PyObject* args) {
	long to_read = 0;
	if (!PyArg_ParseTuple(args, "|l:read", &to_read)) {
		return NULL;
	}

	return go_wsgi_read_request(self->request_id, to_read);
}

static PyObject *wsgi_input_readline(wsgo_WsgiInput* self, PyObject* args) {
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

static PyType_Slot wsgi_input_slots[] = {
	{ Py_tp_dealloc,  wsgi_input_free },
	{ Py_tp_iter,     wsgi_input_iter },
	{ Py_tp_iternext, wsgi_input_next },
	{ Py_tp_methods,  wsgi_input_methods },
	{ Py_tp_doc,      "wsgi input object." },
	{ 0, NULL }
};

static PyType_Spec wsgi_input_spec = {
	"wsgo._wsgo.WsgiInput",
	sizeof(wsgo_WsgiInput),
	0,
	Py_TPFLAGS_DEFAULT,
	wsgi_input_slots
};

PyObject *create_wsgi_input(long request_id) {
	PyObject *inp = (PyObject *) PyObject_New(wsgo_WsgiInput, wsgi_input_type);
	if(inp==NULL) {
		return NULL;
	}
	((wsgo_WsgiInput*)inp)->request_id = request_id;
	return inp;
}


// METH_FASTCALL signature
static PyObject* wsgo_add_cron(PyObject *self, PyObject *const *args, Py_ssize_t nargs)
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

// METH_FASTCALL signature
static PyObject* wsgo_notify_parked(PyObject *self, PyObject *const *args, Py_ssize_t nargs)
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

// main(argv: list[str], executable: str) -> int
// Runs the server (or process manager) until it shuts down, and returns the
// process exit code. Must be called from the main thread.
// METH_FASTCALL signature
static PyObject* wsgo_main(PyObject *self, PyObject *const *args, Py_ssize_t nargs)
{
	if(nargs!=2) {
		PyErr_SetString(PyExc_TypeError, "main() takes exactly 2 arguments");
		return NULL;
	}
	if(!PyList_Check(args[0])) {
		PyErr_SetString(PyExc_TypeError, "main() argv must be a list");
		return NULL;
	}

	Py_ssize_t executable_len;
	const char *executable = PyUnicode_AsUTF8AndSize(args[1], &executable_len);
	if(executable==NULL) {
		return NULL;
	}

	// Hold our own reference to the list so the borrowed UTF-8 buffers
	// stay valid for the duration of the call.
	PyObject *argv = args[0];
	Py_INCREF(argv);

	Py_ssize_t argc = PyList_Size(argv);
	const char **arg_parts = PyMem_Malloc((argc + 1) * sizeof(char*));
	int *arg_lengths = PyMem_Malloc((argc + 1) * sizeof(int));
	if(arg_parts==NULL || arg_lengths==NULL) {
		PyErr_NoMemory();
		goto error;
	}

	for(Py_ssize_t i=0; i<argc; i++) {
		Py_ssize_t len;
		const char *arg = PyUnicode_AsUTF8AndSize(PyList_GetItem(argv, i), &len);
		if(arg==NULL) {
			goto error;
		}
		arg_parts[i] = arg;
		arg_lengths[i] = (int)len;
	}

	int ret = go_main(arg_parts, arg_lengths, (int)argc, executable, (int)executable_len);

	PyMem_Free(arg_parts);
	PyMem_Free(arg_lengths);
	Py_DECREF(argv);

	return PyLong_FromLong(ret);

error:
	PyMem_Free(arg_parts);
	PyMem_Free(arg_lengths);
	Py_DECREF(argv);
	return NULL;
}

static PyMethodDef WsgoMethods[] = {
	{"main", (PyCFunction)(void(*)(void))wsgo_main, METH_FASTCALL, "Runs the wsgo server"},
	{"add_cron", (PyCFunction)(void(*)(void))wsgo_add_cron, METH_FASTCALL, "Registers a cron handler"},
	{"notify_parked", (PyCFunction)(void(*)(void))wsgo_notify_parked, METH_FASTCALL, "Notifies a parked job"},
	{NULL, NULL, 0, NULL}
};

// Single-phase init (m_size = -1), so that Python 3.12+ refuses to load the
// module into subinterpreters with their own GIL.
static PyModuleDef WsgoModule = {
	PyModuleDef_HEAD_INIT, "wsgo._wsgo", NULL, -1, WsgoMethods,
	NULL, NULL, NULL, NULL
};

PyMODINIT_FUNC
PyInit__wsgo(void) {
	PyObject *m = PyModule_Create(&WsgoModule);
	if(m==NULL) {
		return NULL;
	}

	if(wsgi_input_type==NULL) {
		wsgi_input_type = (PyTypeObject *)PyType_FromSpec(&wsgi_input_spec);
		if(wsgi_input_type==NULL) {
			Py_DECREF(m);
			return NULL;
		}
	}

	if(PyModule_AddObjectRef(m, "WsgiInput", (PyObject *)wsgi_input_type) < 0) {
		Py_DECREF(m);
		return NULL;
	}

	return m;
}

// Compiles and runs some Python code in the __main__ namespace, printing any
// exception. Must be called with the GIL held.
int run_python_code(const char *code) {
	PyObject *main_module = PyImport_AddModule("__main__"); // borrowed
	if(main_module==NULL) {
		PyErr_Print();
		return -1;
	}
	PyObject *globals = PyModule_GetDict(main_module); // borrowed

	PyObject *co = Py_CompileString(code, "<wsgo>", Py_file_input);
	if(co==NULL) {
		PyErr_Print();
		return -1;
	}

	PyObject *ret = PyEval_EvalCode(co, globals, globals);
	Py_DECREF(co);
	if(ret==NULL) {
		PyErr_Print();
		return -1;
	}
	Py_DECREF(ret);
	return 0;
}

uint64_t get_thread_cpu_time() {
	struct timespec t;
	clock_gettime(CLOCK_THREAD_CPUTIME_ID, &t);
	return (t.tv_sec*1000) + (t.tv_nsec/1000000);
}
