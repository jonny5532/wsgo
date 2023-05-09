package wsgo

import (
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

import "C"

var retryJobMutex sync.Mutex

type Retry struct {
	job       *RequestJob
	notified  chan bool 
	failed    bool
}

var retryJobs map[string]*Retry

func init() {
	retryJobs = make(map[string]*Retry)
}

func DoRetry(job *RequestJob) {
	retryJobMutex.Lock()

	// TODO - limit max number of pending retries

	if retryJobs[job.retryId] != nil {
		// make existing one fail
		retryJobs[job.retryId].failed = true
		// nonblocking notify
		select {
		case retryJobs[job.retryId].notified <- true:
		default:
		}
	}

	retry := Retry{
		job: job,
		notified: make(chan bool, 0),
	}
	retryJobs[job.retryId] = &retry

	retryJobMutex.Unlock()

	ctx := job.req.Context()

	select {
	case <-retry.notified:
	case <-ctx.Done():
		log.Println("retry ctx done")
		retry.failed = true
	}

	retryJobMutex.Lock()
	delete(retryJobs, job.retryId)
	retryJobMutex.Unlock()
	
	log.Println("notified! failed?", retry.failed)

	if retry.failed {
		job.w.WriteHeader(502)
		job.w.Write([]byte("Bad Gateway"))
		return
	}

	var requestReader io.Reader
	if requestBufferLength>0 && (job.req.Method == "POST" || job.req.Method == "PUT" || job.req.Method == "PATCH") {
		err := job.r.(*BufferingReader).Rewind()
		if err != nil {
			job.w.WriteHeader(502)
			job.w.Write([]byte("Bad Gateway"))
			return
		}
		requestReader = job.r
	} else {
		requestReader = strings.NewReader("")
	}
	

	// pass the retry id so the handler knows we're in a retry
	job.req.Header.Set("X-WSGo-Retry", job.retryId)

	cw := NewNonCachingCacheWriter(job.w.(*CacheWriter).writer)
	// crudely remove all the headers that already got added to the response
	for k, _ := range cw.writer.Header() {
		cw.writer.Header().Del(k)
	}

	newJob := &RequestJob{
		w:        cw,
		req:      job.req,
		r:	      requestReader,
		done:     make(chan bool, 1),
	}
	scheduler.HandleJob(newJob, time.Duration(requestTimeout*2) * time.Second)
	cw.Flush()
}

//export go_notify_retry
func go_notify_retry(retry_id *C.char, retry_id_length C.int) {
	retryId := C.GoStringN(retry_id, retry_id_length)

	retryJobMutex.Lock()
	if retryJobs[retryId] != nil {
		select {
		case retryJobs[retryId].notified <- true:
		default:
		}
	}
	retryJobMutex.Unlock()
}
