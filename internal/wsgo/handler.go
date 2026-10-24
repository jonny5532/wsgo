package wsgo

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/projecthunt/reuseable"
)

/*
#include <Python.h>
*/
import "C"

type BackgroundJob struct {
	function *C.PyObject
}

var backgroundJobs chan *BackgroundJob
var backgroundJobActive sync.Mutex

var server *http.Server

// pythonFinalizing is set once Main has handed control back to Python, after
// which nothing else may call into the interpreter.
var pythonFinalizing atomic.Bool

func init() {
	backgroundJobs = make(chan *BackgroundJob, 0)
}

func Serve(w http.ResponseWriter, req *http.Request) {
	if TryBlocking(w, req) {
		return
	}

	if TryStatic(w, req) {
		return
	}

	try_cache_status := MISS
	if req.Method == "GET" || req.Method == "HEAD" || req.Method == "OPTIONS" {
		try_cache_status = TryCache(w, req)
	}
	if try_cache_status == HIT {
		LogRequest(
			req,
			200,
			time.Now(),
			0,
			0,
			0,
			0,
		)
		return
	}
	alreadyResponded := try_cache_status == HIT_BUT_EXPIRING_SOON

	if AllWorkersAreStuck() {
		// Last-resort safety valve: if no Python thread can make progress,
		// exit so the process manager can restart us.
		ExitIfAllWorkersStuck()
	}

	var cw *CacheWriter

	if maxAge > 0 && pageCacheLimit > 0 && (req.Method == "GET" || req.Method == "HEAD" || req.Method == "OPTIONS") {
		// We might be able to cache this
		if alreadyResponded {
			cw = NewCacheOnlyCacheWriter()
		} else {
			cw = NewCacheWriter(w)
		}
	} else if alreadyResponded {
		// We already responded, and are not going to cache, so bail
		return
	} else {
		// Response won't be cached
		cw = NewNonCachingCacheWriter(w)
	}

	var requestReader RequestReader

	if requestBufferLength > 0 && (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH") {
		reqBufLen, err := strconv.Atoi(req.Header.Get("Content-length"))
		if err != nil || reqBufLen > requestBufferLength {
			reqBufLen = requestBufferLength
		}
		// Read and buffer an initial chunk of the request body (good to do
		// this now before we tie up a Python worker, since it will block).
		requestReader = NewBufferingReader(req.Body, reqBufLen)
	} else {
		requestReader = NewBufferingReader(req.Body, 0)
	}

	job := &RequestJob{
		w:    cw,
		req:  req,
		r:    requestReader,
		done: make(chan bool, 1),
	}

	scheduler.HandleJob(job, time.Duration(requestTimeout)*time.Second)

	if ResolveAccel(job) {
		return
	}

	cw.Flush()

	if !cw.skipCaching {
		CachePage(cw, req)
	}
}

//export go_main
func go_main(args **C.char, arg_lengths *C.int, args_size C.int, executable *C.char, executable_len C.int) C.int {
	argPtrs := unsafe.Slice(args, int(args_size))
	argLengths := unsafe.Slice(arg_lengths, int(args_size))
	goArgs := make([]string, int(args_size))
	for i := range goArgs {
		goArgs[i] = C.GoStringN(argPtrs[i], argLengths[i])
	}
	return C.int(Main(goArgs, C.GoStringN(executable, executable_len)))
}

// Entry point for the Go process, which is called from Python with the GIL
// held, returns with the GIL held.
func Main(args []string, executable string) int {
	ParseFlags(args)

	pythonExecutable = executable
	serverArgs = args

	// We release and later reacquire the GIL with this thread's state, so it
	// must stay on this OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if process == 0 {
		// The manager never imports the application.
		threadState := C.PyEval_SaveThread()
		RunProcessManager()
		C.PyEval_RestoreThread(threadState)
		return 0
	}

	listener, err := reuseable.Listen("tcp", bindAddress)
	if err != nil {
		log.Fatalln(err)
	}

	LoadApplication(wsgiModule)

	threadState := C.PyEval_SaveThread()

	StartWorkers()

	go CronRoutine()
	go NewMonitor()

	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", Serve)

	// Go http's timeouts are rather crude.
	server = &http.Server{
		// Time to read the request body - 10 mins should be enough.
		ReadTimeout: 600 * time.Second,
		// Time to write the response. This includes the time to transfer
		// X-Sendfile downloads, so needs to be decent. After all, we're
		// time-limiting the scarce resource (Python threads) separately.
		WriteTimeout: 3600 * time.Second,
		// Time to keep Keep-alive sockets open
		IdleTimeout: 60 * time.Second,
		// Time to read the request header.
		ReadHeaderTimeout: 2 * time.Second,
		Handler:           serverMux,
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	shuttingDown := make(chan bool, 0)
	go func() {
		sig := <-sigs
		log.Println("Process", process, "got", sig, "signal, shutting down...")
		server.Shutdown(context.Background())

		// grab the background job mutex, to wait on any currently running job
		backgroundJobActive.Lock()

		shuttingDown <- true
	}()

	server.Serve(listener)

	shutdownTimedOut := false
	select {
	case <-shuttingDown:
		// Successfully shut down.
	case <-time.After(time.Duration(requestTimeout) * time.Second):
		// All requests should have completed by now, but something has hung.
		// We'll still let Python finalize though.
		shutdownTimedOut = true
	}

	// Stop the worker goroutines (and their Python thread states) before
	// we can let Python finalize.
	StopWorkers()

	if shutdownTimedOut {
		log.Println("Process", process, "shutdown timed out, exiting.")
	} else {
		log.Println("Process", process, "shut down gracefully.")
	}

	// Python finalizes itself once we return. If that hangs, exit uncleanly
	// so the process manager restarts us rather than treating this as a
	// successful shutdown. A clean finalization exits the process before
	// this fires.
	go func() {
		time.Sleep(15 * time.Second)
		log.Println("Process", process, "finalize timed out, exiting.")
		os.Exit(1)
	}()

	pythonFinalizing.Store(true)
	C.PyEval_RestoreThread(threadState)
	return 0
}
