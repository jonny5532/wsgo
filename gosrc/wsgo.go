package main

/*
#include <Python.h>
*/
import "C"

import (
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/projecthunt/reuseable"
)

type Job struct {
	w          http.ResponseWriter
	statusCode int
	req        *http.Request
	r          io.Reader
	done       chan bool

	// X-SendFile / X-Accel-Redirect file
	sendFile   string
}

type BackgroundJob struct {
	function *C.PyObject
}

var normalJobs chan *Job
var heavyJobs chan *Job
var backgroundJobs chan *BackgroundJob

var server *http.Server

func init() {
	// All job queues are blocking to avoid 'bufferbloat'
	normalJobs = make(chan *Job, 0)
	heavyJobs = make(chan *Job, 0)
	backgroundJobs = make(chan *BackgroundJob, 0)
}

func IsRequestHeavy(req *http.Request) bool {
	for _, prefix := range heavyPrefixes {
		if strings.HasPrefix(req.URL.Path, prefix) {
			return true
		}
	}

	ua := strings.ToLower(req.Header.Get("User-agent"))
	for _, uas := range []string{"facebook", "bot", "crawler"} {
		if strings.Contains(ua, uas) {
			return true
		}
	}

	lastTime := GetWeightedLoadTime(req.URL.Path + "?" + req.URL.RawQuery)
	if lastTime >= slowResponseThreshold {
		// was slow last time
		return true
	}

	if req.URL.RawQuery != "" {
		// demote anything with a query string
		return true
	}

	return false
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
		)
		return
	}
	alreadyResponded := try_cache_status == HIT_BUT_EXPIRING_SOON

	if AllWorkersAreStuck() {
		Shutdown("All worker threads are stuck, quitting!")
	}

	isHeavy := IsRequestHeavy(req)

	var cw *CacheWriter

	if maxAge > 0 && pageCacheLimit > 0 && (req.Method == "GET" || req.Method == "HEAD" || req.Method == "OPTIONS") {
		// We might be able to cache this
		if alreadyResponded {
			cw = NewCacheOnlyCacheWriter()
			isHeavy = true
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

	job := &Job{
		w:    cw,
		req:  req,
		r:	  requestReader,
		done: make(chan bool),
	}

	if isHeavy {
		heavyJobs <- job
	} else {
		normalJobs <- job
	}
	// wait for a worker thread to handle the job
	<-job.done

	ResolveAccel(job)

	cw.Flush()

	if !cw.skipCaching {
		CachePage(cw, req)
	}
}

func Shutdown(message string) {
	if server == nil {
		return
	}
	log.Println(message)
	server.Shutdown(context.Background())
	log.Fatalln("Server shut down.")
}

func main() {
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
	server.Serve(listener)
}
