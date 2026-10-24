package wsgo

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

/*
#include <Python.h>
*/
import "C"

type PythonWorker struct {
	number int
	stuck  atomic.Bool
	// This gets set once, the first time we run a task on a worker
	gilState C.PyGILState_STATE
	// This is used to remember the threadstate between successive tasks
	threadState *C.PyThreadState
}

var workers []*PythonWorker

var lastRequestId int64

func AllWorkersAreStuck() bool {
	if len(workers) == 0 {
		// With no workers there is nothing to be stuck; the config
		// validation rejects --workers 0 before startup anyway.
		return false
	}
	for _, w := range workers {
		if !w.stuck.Load() {
			return false
		}
	}
	return true
}

var exitOnStuckOnce sync.Once

// ExitIfAllWorkersStuck terminates the process if every worker is stuck, so
// that the process manager can restart it. Exiting uncleanly (rather than a
// graceful shutdown, which would wait forever for the stuck workers) is
// deliberate, and only one caller performs the exit.
func ExitIfAllWorkersStuck() {
	if !AllWorkersAreStuck() {
		return
	}
	exitOnStuckOnce.Do(func() {
		log.Println("All worker threads are stuck, quitting!")
		os.Exit(1)
	})
}

func GetRequestTimeoutException() *C.PyObject {
	// Returns a new reference.

	mod_name := C.CString("wsgo")
	defer C.free(unsafe.Pointer(mod_name))
	mod := C.PyImport_ImportModule(mod_name)
	if mod == nil {
		C.PyErr_Print()
		log.Fatalln("Failed to import wsgo module!")
	}
	defer C.Py_DecRef(mod)

	exc_name := C.CString("RequestTimeoutException")
	defer C.free(unsafe.Pointer(exc_name))

	exc := C.PyObject_GetAttrString(mod, exc_name)
	if exc == nil {
		log.Fatalln("Failed to get RequestTimeoutException from wsgo module!")
	}

	return exc
}

var (
	// workerStop is closed by StopWorkers to tell worker goroutines to exit.
	workerStop       = make(chan struct{})
	workerStopClosed atomic.Bool
	workersWg        sync.WaitGroup
)

// StopWorkers signals the worker goroutines to exit and waits for them (with
// a short timeout) so that Py_Finalize is not run while worker Python thread
// states are still alive.
func StopWorkers() bool {
	if workerStopClosed.CompareAndSwap(false, true) {
		close(workerStop)
	}

	done := make(chan struct{})
	go func() {
		workersWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		log.Println("Workers didn't stop in time.")
		return false
	}
}

func StartWorkers() {
	workers = make([]*PythonWorker, totalWorkers)

	log.Println("Process", strconv.Itoa(process), "starting", strconv.Itoa(totalWorkers), "workers.")

	workersWg.Add(totalWorkers + 1)

	for i := 0; i < totalWorkers; i++ {
		workers[i] = &PythonWorker{
			number: i + 1,
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

	// Record that we are finished whilst we still have the GIL
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
	defer workersWg.Done()

	// Pin all the threads to the same CPU, which should reduce the 'convoy problem' caused by the GIL
	var cpuSet unix.CPUSet
	unix.SchedGetaffinity(0, &cpuSet)
	cpuCount := cpuSet.Count()
	cpuSet.Zero()
	cpuSet.Set(process % cpuCount)
	unix.SchedSetaffinity(0, &cpuSet)

	for {
		job := scheduler.GrabJob()
		if job == nil {
			// Shutdown has been requested.
			return
		}

		job.worker = worker.number

		job.finish, job.elapsed, job.cpuElapsed = worker.RunPythonTask(func() {
			worker.HandleJob(job)
		}, requestTimeout)

		scheduler.JobFinished(job)
	}
}

func (worker *PythonWorker) BackgroundWorkerRun() {
	runtime.LockOSThread()
	defer workersWg.Done()

	// Pin all the threads to the same CPU, which should reduce the 'convoy problem' caused by the GIL
	var cpuSet unix.CPUSet
	unix.SchedGetaffinity(0, &cpuSet)
	cpuCount := cpuSet.Count()
	cpuSet.Zero()
	cpuSet.Set(process % cpuCount)
	unix.SchedSetaffinity(0, &cpuSet)

	for {
		var backgroundJob *BackgroundJob
		select {
		case <-workerStop:
			return
		case backgroundJob = <-backgroundJobs:
		}

		backgroundJobActive.Lock()

		worker.RunPythonTask(func() {
			finished := make(chan bool, 1)

			go func() {
				select {
				case <-finished:
					return
				case <-time.After(time.Duration(backgroundTimeout+10) * time.Second):
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
	job.w.takeCacheDirectives()

	if CanAccelResponse(job) {
		// Is X-Sendfile or similar - the response will be done later, based
		// on the headers alone. The application's response body isn't used,
		// but WSGI still requires .close() to be called on it.
		if closeWsgiResponse(ret) != nil {
			errorCount.Add(1)
		}
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

	runPythonCode(`
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
}
