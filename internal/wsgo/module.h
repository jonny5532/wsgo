// Declarations for the C side of the _wsgo extension module (module.c).

#ifndef WSGO_MODULE_H
#define WSGO_MODULE_H

#include <Python.h>
#include <stdint.h>

extern PyMethodDef start_response_def;

PyObject *create_wsgi_input(long request_id);
int run_python_code(const char *code);
uint64_t get_thread_cpu_time();

#endif
