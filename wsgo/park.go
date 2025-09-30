package wsgo

import (
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

import "C"

var parkedJobMutex sync.Mutex

type ParkedJob struct {
	job               *RequestJob
	notify            chan ParkedJobNotification
}

type ParkedJobNotification struct {
	action   int
	arg      string
}

const (
	DISCONNECT int = 0
	RETRY          = 1
	HTTP_204       = 2
	HTTP_504       = 3
)

var parkedJobs map[string][]*ParkedJob

func splitOnCommas(str string) []string {
	var ret []string
	for _, s := range strings.Split(str, ",") {
		s = strings.TrimSpace(s)
		if len(s)>0 {
			ret = append(ret, s)
		}
	}
	return ret
}

func init() {
	parkedJobs = make(map[string][]*ParkedJob)
}

func ParkJob(job *RequestJob) {
	log.Println("- parked request", job.parkedId)

	// TODO - limit max number of pending retries

	// retryId can contain a comma-separated list of multiple retry IDs
	parkedIds := splitOnCommas(job.parkedId)

	timeout, timeoutAction := 7200, "http-204"

	timeoutBits := strings.Split(job.w.Header().Get("X-WSGo-Park-Timeout"), " ")
	if len(timeoutBits)==2 && (timeoutBits[1]=="retry" || timeoutBits[1]=="disconnect" || timeoutBits[1]=="http-204" || timeoutBits[1]=="http-504") {
		t, err := strconv.Atoi(timeoutBits[0])
		if err == nil && t > 0 {
			timeout = t
			timeoutAction = timeoutBits[1]
		}
	}

	// remove old headers
	for k, _ := range job.w.Header() {
		job.w.Header().Del(k)
	}

	park := ParkedJob{
		job: job,
		notify: make(chan ParkedJobNotification, 0),
	}

	parkedJobMutex.Lock()
	for _, s := range parkedIds {
		parkedJobs[s] = append(parkedJobs[s], &park)
	}
	parkedJobMutex.Unlock()

	ctx := job.req.Context()

	var notification ParkedJobNotification

	select {
	case notification = <-park.notify:
		// got notification
	case <-ctx.Done():
		// other end disconnected
		notification.action = DISCONNECT
	case <-time.After(time.Duration(timeout) * time.Second):
		// timeout
		if timeoutAction == "retry" {
			notification.action = RETRY
		} else if timeoutAction == "http-204" {
			notification.action = HTTP_204
		} else if timeoutAction == "http-504" {
			notification.action = HTTP_504
		} else {
			notification.action = DISCONNECT
		}
	}

	parkedJobMutex.Lock()
	// remove job from lists
	for _, s := range parkedIds {
		for i, v := range parkedJobs[s] {
			if v==&park {
				if len(parkedJobs[s])==1 {
					delete(parkedJobs, s)
				} else {
					parkedJobs[s] = append(parkedJobs[s][:i], parkedJobs[s][i+1:]...)
				}
				break
			}
		}
	}
	parkedJobMutex.Unlock()
	
	log.Println("- retry", job.parkedId, "notified: action =", notification.action)

	if notification.action == RETRY {
		// pass
	} else if notification.action == DISCONNECT {
		return
	} else if notification.action == HTTP_204 {
		job.w.writer.WriteHeader(204)
		return
	} else { //if action == HTTP_504 {
		job.w.writer.WriteHeader(504)
		job.w.writer.Write([]byte("Gateway Timeout"))
		return
	}

	var requestReader RequestReader
	if requestBufferLength>0 && (job.req.Method == "POST" || job.req.Method == "PUT" || job.req.Method == "PATCH") {
		err := job.r.Rewind()
		if err != nil {
			// couldn't rewind, maybe we didn't store the whole request?
			job.w.writer.WriteHeader(502)
			job.w.writer.Write([]byte("Bad Gateway"))
			return
		}
		requestReader = job.r
	} else {
		requestReader = job.r
	}

	// pass the notification arg so the handler knows we were parked previously (and has the arg)
	job.req.Header.Set("X-WSGo-Park-Arg", notification.arg)

	cw := NewNonCachingCacheWriter(job.w.writer)

	newJob := &RequestJob{
		w:        cw,
		req:      job.req,
		r:	      requestReader,
		done:     make(chan bool, 1),
	}
	scheduler.HandleJob(newJob, time.Duration(requestTimeout) * time.Second)

	// we deliberately don't handle any accels from retries

	cw.Flush()
}

//export go_notify_parked
func go_notify_parked(parked_id *C.char, parked_id_length C.int, action C.int, param *C.char, param_length C.int) {
	parkedId := C.GoStringN(parked_id, parked_id_length)
	parkedIds := splitOnCommas(parkedId)

	notification := ParkedJobNotification{
		action: int(action),
	}

	if param != nil {
		notification.arg = C.GoStringN(param, param_length)
	}

	parkedJobMutex.Lock()
	for _, s := range parkedIds {
		for _, park := range parkedJobs[s] {
			select {
			case park.notify <- notification:
			default:
			}
		}
	}
	parkedJobMutex.Unlock()
}
