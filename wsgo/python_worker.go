package wsgo

import (
	"log"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

/*
#include <Python.h>

// Python <3.7 doesn't include this by default
#include "pythread.h"

*/
import "C"

type PythonWorker struct {
	number      int
	stuck       atomic.Bool
	// This gets set once, the first time we run a task on a worker
	gilState    C.PyGILState_STATE
	// This is used to remember the threadstate between successive tasks
	threadState *C.PyThreadState
}

var workers []*PythonWorker

var lastRequestId int64

func AllWorkersAreStuck() bool {
	for _, w := range workers {
		if !w.stuck.Load() {
			return false
		}
	}
	return true
}

func GetRequestTimeoutException() (*C.PyObject) {
	// Returns a new reference.

	mod_name := C.CString("wsgo")
	defer C.free(unsafe.Pointer(mod_name))
	mod := C.PyImport_AddModule(mod_name) // borrowed reference
	if mod == nil {
		log.Fatalln("Failed to import wsgo module!")
	}

	exc_name := C.CString("RequestTimeoutException")
	defer C.free(unsafe.Pointer(exc_name))

	exc := C.PyObject_GetAttrString(mod, exc_name)
	if exc == nil {
		log.Fatalln("Failed to get RequestTimeoutException from wsgo module!")
	}

	return exc
}

func StartWorkers() {
	workers = make([]*PythonWorker, totalWorkers)

	log.Println("Process", strconv.Itoa(process), "starting", strconv.Itoa(totalWorkers), "workers.")

	for i := 0; i < totalWorkers; i++ {
		workers[i] = &PythonWorker{
			number:        i + 1,
		}
		go workers[i].Run()
	}

	// this should probably wait until cron actually registers something?
	backgroundWorker := &PythonWorker{}
	go backgroundWorker.BackgroundWorkerRun()
}

func (worker *PythonWorker) RunPythonTask(task func(), timeout int) (time.Time, int64, int64) {
	cpu_start := GetThreadCpuTime()

	if worker.threadState != nil {
		// We have a previously saved thread state, so we can just use that
		C.PyEval_RestoreThread(worker.threadState)
	} else {
		// This is the first time so use PyGILState_Ensure to make sure a new
		// threadstate gets created. We'll (maybe) call the corresponding
		// PyGILState_Release in each thread just before shutdown. (TODO)
		worker.gilState = C.PyGILState_Ensure()
	}


	// `finished` is set by the worker once the task has completed, whilst it
	// still holds the GIL. The timeout goroutine checks it whilst holding the
	// GIL, so once the task has finished, a stale timer can no longer inject a
	// RequestTimeoutException into a later request.
	var finished atomic.Bool
	pydone := make(chan struct{})
	thread_id := C.PyThread_get_thread_ident()

	if timeout > 0 {
		// Add a request timeout to interrupt the worker
		timer := time.NewTimer(time.Duration(timeout) * time.Second)
		go func() {
			defer timer.Stop()

			select {
			case <-pydone:
				return
			case <-timer.C:
			}

			log.Println("Task timed out!")

			// The GIL is held from here until the exception has been set,
			// which stops the worker from finishing the task underneath us.
			runtime.LockOSThread()
			gs := C.PyGILState_Ensure()

			if finished.Load() {
				// The task completed before we acquired the GIL; the timeout
				// is stale, and delivering it now would raise
				// RequestTimeoutException in a later request.
				C.PyGILState_Release(gs)
				runtime.UnlockOSThread()
				return
			}

			// Flag the worker as stuck, so we can detect whether our
			// attempt at interrupting it hasn't worked
			worker.stuck.Store(true)

			// Print a fancy traceback so we can see where it is stuck
			printPythonTracebackLocked()

			exc := GetRequestTimeoutException()
			if C.PyThreadState_SetAsyncExc(thread_id, exc) != 1 {
				log.Println("Failed to issue RequestTimeoutException to stuck worker!")
			}
			C.Py_DecRef(exc)

			C.PyGILState_Release(gs)
			runtime.UnlockOSThread()
		}()
	}

	start := time.Now()

	task()

	// The task has finished; record it (still holding the GIL) before the
	// timeout goroutine can check it.
	finished.Store(true)

	if worker.stuck.Load() {
		// Worker was stuck, so we need to clean up the side-effects of
		// unsticking it.
		worker.CleanupStuckWorker()
	}

	worker.stuck.Store(false)
	close(pydone)

	// We have to use PyEval_SaveThread rather than PyGILState_Release here because
	// we want to keep the thread state around for the next time we run a task.
	worker.threadState = C.PyEval_SaveThread()

	finish := time.Now()
	elapsed := finish.Sub(start).Milliseconds()
	cpu_elapsed := int64(GetThreadCpuTime() - cpu_start)

	return finish, elapsed, cpu_elapsed
}

func (worker *PythonWorker) Run() {
	// It is important that this goroutine always uses the same OS thread, else
	// the Python GIL will get very upset.
	runtime.LockOSThread()

	// Pin all the threads to the same CPU, which should reduce the 'convoy problem' caused by the GIL
	var cpuSet unix.CPUSet
	unix.SchedGetaffinity(0, &cpuSet)
	cpuCount := cpuSet.Count()
	cpuSet.Zero()
	cpuSet.Set(process % cpuCount)
	unix.SchedSetaffinity(0, &cpuSet)

	for {
		job := scheduler.GrabJob()

		job.worker = worker.number

		job.finish, job.elapsed, job.cpuElapsed = worker.RunPythonTask(func() {
			worker.HandleJob(job)
		}, requestTimeout)

		scheduler.JobFinished(job)
	}
}

func (worker *PythonWorker) BackgroundWorkerRun() {
	runtime.LockOSThread()

	// Pin all the threads to the same CPU, which should reduce the 'convoy problem' caused by the GIL
	var cpuSet unix.CPUSet
	unix.SchedGetaffinity(0, &cpuSet)
	cpuCount := cpuSet.Count()
	cpuSet.Zero()
	cpuSet.Set(process % cpuCount)
	unix.SchedSetaffinity(0, &cpuSet)

	for {
		backgroundJob := <-backgroundJobs
		backgroundJobActive.Lock()

		worker.RunPythonTask(func() {
			finished := make(chan bool, 1)

			go func() {
				select {
				case <-finished:
					return
				case <-time.After(time.Duration(backgroundTimeout + 10) * time.Second):
					log.Fatalln("Background worker permanently stuck, quitting!")
				}
			}()

			worker.HandleBackgroundJob(backgroundJob)

			finished <- true
		}, backgroundTimeout)

		backgroundJobActive.Unlock()
	}
}

func (worker *PythonWorker) HandleJob(job *RequestJob) {
	requestId := atomic.AddInt64(&lastRequestId, 1)

	AddWsgiRequestReader(requestId, job.r)
	defer RemoveWsgiRequestReader(requestId)

	ret := CallApplication(requestId, job.req)

	BadGateway := func() {
		job.w.WriteHeader(502)
		job.w.Write([]byte("Bad Gateway"))
		errorCount.Add(1)
	}

	if ret == nil {
		C.PyErr_Print()
		BadGateway()
		return
	}

	defer C.Py_DecRef(ret)

	responseStart := GetWsgiResponseStart(requestId)

	if responseStart.status == 0 {
		log.Println("start_response wasn't called")
		BadGateway()
		return
	}

	job.statusCode = responseStart.status

	for k, vv := range responseStart.headers {
		for _, v := range vv {
			job.w.Header().Add(k, v)
		}
	}

	UpdateBlocking(job)

	if CanAccelResponse(job) {
		// Is X-Sendfile or similar - the response will be done later, based
		// on the headers alone.
		return
	}

	// Obtain the response iterator before writing the header, so that a
	// non-iterable response can be reported as a 502 rather than being
	// appended to an already-started 200 response.
	iter := GetWsgiResponseIter(ret)
	if iter == nil {
		BadGateway()
		return
	}
	defer C.Py_DecRef(iter)

	job.w.WriteHeader(responseStart.status)

	if WriteWsgiResponseToWriter(ret, iter, job.w) != nil {
		// The header has already been sent, so the status can't be changed
		// now; the error has already been logged.
		errorCount.Add(1)
	}
}

func (worker *PythonWorker) HandleBackgroundJob(job *BackgroundJob) {
	app_func_args := C.PyTuple_New(0)
	defer C.Py_DecRef(app_func_args)

	ret := C.PyObject_CallObject((*C.PyObject)(job.function), app_func_args)
	if ret == nil {
		C.PyErr_Print()
	} else {
		C.Py_DecRef(ret)
	}
}

func (worker *PythonWorker) CleanupStuckWorker() {
	// When an async exception has been raised on a thread, there is the risk of
	// some locks not having been properly released, such as those in the
	// logging module.

	// We'll try to release such locks here, to avoid deadlocks on future
	// requests.

	// Must be called with the runtime thread locked and GIL held.

	cmd := C.CString(`
import logging

# Release the global logging lock, if held
try:
	# RLocks are re-entrant so might be held multiple times
	for i in range(50):
		logging._lock.release()
		print("Released leaked logging._lock!")
except:
	pass

# Release the individual logging handler locks, if held
for wr in reversed(logging._handlerList[:]):
	try:
		h = wr()
		if h.lock:
			for i in range(50):
				h.release()
				print('Released leaked %s lock!'%h)
	except:
		pass
`)
	defer C.free(unsafe.Pointer(cmd))
	C.PyRun_SimpleStringFlags(cmd, nil)
}