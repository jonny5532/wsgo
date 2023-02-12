package wsgo

import (
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	//"time"
)

type Job struct {
	w          http.ResponseWriter
	statusCode int
	req        *http.Request
	r          io.Reader
	done       chan bool

	priority   int
	isSlow     bool

	// X-SendFile / X-Accel-Redirect file
	sendFile   string

	// for asynchronous responses
	//asyncId    string
	//asyncDone  chan bool
}

var jobQueue []*Job
var jobQueueMutex sync.Mutex
var hasJob chan bool

var readyJob chan *Job

var activeRequestsBySource map[string]int
var activeRequestsBySourceMutex sync.Mutex

var activeSlowRequests int64 = 0

func init() {
	hasJob = make(chan bool, 1)
	readyJob = make(chan *Job, 0)
	activeRequestsBySource = make(map[string]int)

	go JobGrabber()
}

func AddJobToQueue(job *Job) {
	jobQueueMutex.Lock()
	jobQueue = append(jobQueue, job)
	jobQueueMutex.Unlock()

	// Non-blocking send 
	
	// This will only wake up one worker, could we get a race where a single
	// worker looks for a job but there aren't any, but before it waits on on
	// the channel, a job comes in and this non-blocking send is wasted? Then
	// the worker will wait indefinitely?
	select {
    case hasJob <- true:
	// // 	//
    default:
    // //     //
    }
}

func GrabJobFromQueue() (*Job) {
	return <-readyJob
}

var maxSlowRequests int64 = 2

func JobGrabber() {
	for {
		jobQueueMutex.Lock()
		
		if len(jobQueue)>0 {
			// Find the highest priority job
			var job *Job
			jobIndex := -1
			for k, j := range jobQueue {
				if (job == nil || j.priority > job.priority) {
					// && (!j.isSlow || activeSlowRequests<maxSlowRequests) {
					job = j
					jobIndex = k
				}
			}
			if job != nil {
				jobQueue = append(jobQueue[:jobIndex], jobQueue[jobIndex+1:]...)
				jobQueueMutex.Unlock()
				
				activeRequestsBySourceMutex.Lock()
				activeRequestsBySource[GetRemoteAddr(job.req)] += 1
				activeRequestsBySourceMutex.Unlock()

				if job.isSlow {
					atomic.AddInt64(&activeSlowRequests, 1)
				}

				readyJob <- job
				continue
			}
		}
		
		jobQueueMutex.Unlock()
		
		// race testing
		//time.Sleep(100 * time.Millisecond)

		// Job queue was empty, wait for a send
		<- hasJob
		// TODO: should we have a timeout here too, incase we miss the notification somehow?
	}
}

func GetActiveRequestsBySource(source string) int {
	activeRequestsBySourceMutex.Lock()
	defer activeRequestsBySourceMutex.Unlock()
	return activeRequestsBySource[source]
}

func FlagJobFinished(job *Job) {
	ra := GetRemoteAddr(job.req)
	activeRequestsBySourceMutex.Lock()
	if activeRequestsBySource[ra] <= 1 {
		delete(activeRequestsBySource, ra)
	} else {
		activeRequestsBySource[ra] -= 1
	}
	activeRequestsBySourceMutex.Unlock()

	if job.isSlow {
		atomic.AddInt64(&activeSlowRequests, -1)
	}

	// Should be non-blocking
	job.done <- true
}
