package wsgo

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

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

	try_cache_status := TryCache(w, req)
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
		log.Fatalln("All worker threads are stuck, quitting!")
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

	if requestBufferLength>0 && (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH") {
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
		w:        cw,
		req:      req,
		r:	      requestReader,
		done:     make(chan bool, 1),
	}

	scheduler.HandleJob(job, time.Duration(requestTimeout) * time.Second)

	if ResolveAccel(job) {
		return
	}

	cw.Flush()

	if !cw.skipCaching {
		CachePage(cw, req)
	}
}

func StartupWsgo(initMux func(*http.ServeMux)) {
	ParseFlags()

	if process == 0 {
		RunProcessManager()
		return
	}

	listener, err := reuseable.Listen("tcp", bindAddress)
	if err != nil {
		log.Fatalln(err)
	}

	InitPythonInterpreter(wsgiModule)

	StartWorkers()

	go CronRoutine()
	go NewMonitor()

	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/", Serve)
	initMux(serverMux)

	// Go http's timeouts are rather crude.
	server = &http.Server{
		// Time to read the request body - 10 mins should be enough.
		ReadTimeout:       600 * time.Second,
		// Time to write the response. This includes the time to transfer
		// X-Sendfile downloads, so needs to be decent. After all, we're
		// time-limiting the scarce resource (Python threads) separately.
		WriteTimeout:      3600 * time.Second,
		// Time to keep Keep-alive sockets open
		IdleTimeout:       60 * time.Second,
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
			// We'll still try and finalize the Python interpreter though.
			shutdownTimedOut = true
	}

	// Now try to shut down the Python interpreter.

	finalized := make(chan bool, 0)
	go func() {
		select {
			case <-finalized:
				// Finalized succesfully.
			case <-time.After(15 * time.Second):
				// Py_Finalize probably won't return, so just exit.
				log.Println("Process", process, "finalize timed out, exiting.")
				os.Exit(0)
		}
	}()
	
	// This needs to be called from the same thread that 
	// called InitPythonInterpreter.
	DeinitPythonInterpreter()
	finalized <- true
	
	if shutdownTimedOut {
		log.Println("Process", process, "shutdown timed out, exiting.")
	} else {
		log.Println("Process", process, "shut down gracefully.")
	}
}
