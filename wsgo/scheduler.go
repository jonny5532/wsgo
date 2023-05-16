package wsgo

import (
	"net/http"
	"time"

)

type RequestJob struct {
	w          *CacheWriter
	statusCode int
	req        *http.Request
	r          RequestReader
	// job was cancelled before completion (eg, timeout, requester disconnected)
	cancelled  bool
	done       chan bool

	// does it make sense for priority to be externally visible? it's really an internal concern of schedulers?
	priority   int

	// X-SendFile / X-Accel-Redirect file
	sendFile   string

	parkedId    string
}

type Scheduler interface {
	// called from web handler threads, should it block or return + chan callback?
	HandleJob(job *RequestJob, timeout time.Duration) error

	// called from worker threads, blocks until a job is good to run
	GrabJob() *RequestJob

	// called from worker thread, indicates that a job is finished and should be released back to the web handler
	JobFinished(job *RequestJob, elapsed int64, cpu_elapsed int64)
}

var scheduler Scheduler
func init() {
	scheduler = NewFancyScheduler()
}
