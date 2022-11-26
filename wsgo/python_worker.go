package wsgo

import (
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

/*
#include <Python.h>

// Python <3.7 doesn't include this by default
#include "pythread.h"

*/
import "C"

type PythonWorker struct {
	normalOnly    bool
	nonBackground bool

	number  int
	started time.Time
	stuck   bool
}

var workers []*PythonWorker

var lastRequestId int64

func AllWorkersAreStuck() bool {
	ret := true
	for _, w := range workers {
		if !w.stuck {
			ret = false
		}
	}
	return ret
}

func StartWorkers() {
	workers = make([]*PythonWorker, totalWorkers)

	log.Println("Process", strconv.Itoa(process), "starting", strconv.Itoa(totalWorkers), "workers.")

	for i := 0; i < totalWorkers; i++ {
		workers[i] = &PythonWorker{
			number:        i + 1,
			normalOnly:    (i >= heavyWorkers),
			nonBackground: (i >= backgroundWorkers),
		}
		go workers[i].Run()
	}
}

func (worker *PythonWorker) Run() {
	// It is important that this goroutine always uses the same OS thread, else
	// the python GIL will get very upset.
	runtime.LockOSThread()

	// Pin all the threads to the same CPU, which should reduce the 'convoy problem' caused by the GIL
	var cpuSet unix.CPUSet
	unix.SchedGetaffinity(0, &cpuSet)
	cpuCount := cpuSet.Count()
	cpuSet.Zero()
	cpuSet.Set(process % cpuCount)
	unix.SchedSetaffinity(0, &cpuSet)

	//log.Println("Worker", strconv.Itoa(process)+":"+strconv.Itoa(worker.number), "started!")

	for {
		var job *Job
		var backgroundJob *BackgroundJob

		worker.started = time.Time{}

		// Grab the next job (from one or several channels depending on the worker settings)
		if worker.normalOnly {
			job = <-normalJobs
		} else if worker.nonBackground {
			select {
			case j := <-normalJobs:
				job = j
			case j := <-heavyJobs:
				job = j
			}
		} else {
			select {
			case j := <-normalJobs:
				job = j
			case j := <-heavyJobs:
				job = j
			case bj := <-backgroundJobs:
				backgroundJob = bj
				// // Do background jobs separately
				// worker.started = time.Now()
				// gilState := C.PyGILState_Ensure()
				// worker.HandleBackgroundJob(bj)
				// C.PyGILState_Release(gilState)
				// continue
			}
		}

		worker.started = time.Now()
		cpu_start := GetThreadCpuTime()

		// Grab the GIL
		gilState := C.PyGILState_Ensure()

		pydone := make(chan bool, 1)
		thread_id := C.PyThread_get_thread_ident()

		if requestTimeout > 0 {
			exc := C.PyExc_InterruptedError

			//add a request timeout to kill the worker
			go func() {
				select {
				case <-pydone:
					return
				// case <-job.req.Context().Done():
				// 	// Other end hung up
				// 	fmt.Println("Other end disconnected!")

				// 	// Give the script a chance to exit cleanly
				// 	time.Sleep(200 * time.Millisecond)

				// 	select {
				// 		case <-pydone:
				// 			return
				// 		default:
				// 	}

				// 	exc = C.PyExc_BrokenPipeError

				case <-time.After(time.Duration(requestTimeout) * time.Second):
					fmt.Println("Timed out!")

					// flag worker as stuck, so we can detect whether our
					// attempt at interrupting it hasn't worked
					worker.stuck = true

					// trigger a fancy traceback
					syscall.Kill(syscall.Getpid(), syscall.SIGUSR1)
					// wait for the traceback
					time.Sleep(100 * time.Millisecond)
				}

				runtime.LockOSThread()
				gs := C.PyGILState_Ensure()

				if C.PyThreadState_SetAsyncExc(thread_id, exc) == 0 {
					log.Println("Failed to issue InterruptedError to stuck worker!")
				}

				C.PyGILState_Release(gs)
			}()
		}

		start := time.Now()
		if job != nil {
			worker.HandleJob(job)
		} else {
			worker.HandleBackgroundJob(backgroundJob)
		}

		worker.stuck = false
		pydone <- true

		C.PyGILState_Release(gilState)

		finish := time.Now()
		elapsed := finish.Sub(start).Milliseconds()
		cpu_elapsed := GetThreadCpuTime() - cpu_start

		if job == nil {
			// was a background task, skip the rest
			continue
		}

		job.done <- true

		// could move this off this thread?
		LogRequest(
		 	job.req,
		 	job.statusCode,
		 	finish,
		 	int(elapsed),
			int(cpu_elapsed),
		 	worker.number,
		)

		RecordPageStats(job.req.URL.Path+"?"+job.req.URL.RawQuery, elapsed)
	}
}

func (worker *PythonWorker) HandleJob(job *Job) {
	requestId := atomic.AddInt64(&lastRequestId, 1)

	AddWsgiRequestReader(requestId, job.r)
	defer RemoveWsgiRequestReader(requestId)

	ret := CallApplication(requestId, job.req)

	BadGateway := func() {
		job.w.WriteHeader(502)
		job.w.Write([]byte("Bad Gateway"))
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

	if CanAccelResponse(job) {
		// Is X-Sendfile or similar - the response will be done later, based
		// on the headers alone.
		return
	}

	job.w.WriteHeader(responseStart.status)

	if ReadWsgiResponseToWriter(ret, job.w) != nil {
		BadGateway()
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
