package wsgo

import (
	"net/http"
	"sync/atomic"
	"time"
)

type RequestJob struct {
	w          *CacheWriter
	statusCode int
	req        *http.Request
	r          RequestReader

	grabbed    atomic.Bool
	done       chan bool

	// stats used for logging
	finish     time.Time
	elapsed    int64
	cpuElapsed int64
	worker     int
	priority   int

	// X-SendFile / X-Accel-Redirect file
	sendFile   string

	parkedId   string
}

type Scheduler interface {
	// called from web handler threads, should it block or return + chan callback?
	HandleJob(job *RequestJob, timeout time.Duration) error

	// called from worker threads, blocks until a job is good to run
	GrabJob() *RequestJob

	// called from worker thread, indicates that a job is finished and should be released back to the web handler
	JobFinished(job *RequestJob)
}

var scheduler Scheduler
func init() {
	scheduler = NewFancyScheduler()
}
