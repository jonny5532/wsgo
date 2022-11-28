package wsgo

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

//var normalJobs chan *Job
//var heavyJobs chan *Job
var backgroundJobs chan *BackgroundJob

var server *http.Server

func init() {
	backgroundJobs = make(chan *BackgroundJob, 0)
}

func DetermineRequestPriority(req *http.Request) int {
	ret := 0
	ua := strings.ToLower(req.Header.Get("User-agent"))
	for _, uas := range []string{"facebook", "bot", "crawler", "spider"} {
		if strings.Contains(ua, uas) {
			ret -= 1000
			break
		}
	}

	if req.URL.RawQuery != "" {
		// demote anything with a query string
		ret -= 50
	}

	lastTime := GetWeightedCpuTime(req.URL.Path + "?" + req.URL.RawQuery)
	if lastTime > -1 {
		ret += int(200 - lastTime)
	}

	ret -= GetActiveRequestsBySource(GetRemoteAddr(req))*800

	return ret
}

func Serve(w http.ResponseWriter, req *http.Request) {
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
			//isHeavy = true
		} else {
			cw = NewCacheWriter(w)
		}
	} else if alreadyResponded {
		// We already responded, and are not going to cache, so bail
		return
	} else {
		// Don't cache 'unsafe' methods
		cw = NewNonCachingCacheWriter(w)
	}

	var requestReader io.Reader = req.Body

	if requestBufferLength>0 && (req.Method == "POST" || req.Method == "PUT" || req.Method == "PATCH") {
		reqBufLen, err := strconv.Atoi(req.Header.Get("Content-length"))
		if err != nil || reqBufLen > requestBufferLength {
			reqBufLen = requestBufferLength
		}
		// Read and buffer an initial chunk of the request body
		requestReader = NewBufferingReader(req.Body, reqBufLen)
	}

	priority := DetermineRequestPriority(req)

	job := &Job{
		w:        cw,
		req:      req,
		r:	      requestReader,
		done:     make(chan bool, 1),
		priority: priority,
		isSlow:   priority < 0,
	}

	AddJobToQueue(job)

	// wait for a worker thread to handle the job
	select {
	case <-job.done:
	case <-time.After(time.Duration(requestTimeout*2) * time.Second):
		// if we're very backlogged, a job might never get handled before the timeout
		log.Println("Job timed out without being handled!")
		// TODO: remove job from queue
	}

	ResolveAccel(job)

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
		shuttingDown <- true
    }()

	server.Serve(listener)

	select {
		case <-shuttingDown:
			log.Println("Process", process, "shut down gracefully.")
		case <-time.After(time.Duration(requestTimeout) * time.Second):
			log.Println("Process", process, "shutdown timed out, exiting.")
	}
}
