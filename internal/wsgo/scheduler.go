package wsgo

import (
	"errors"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

type RequestJob struct {
	w          *CacheWriter
	statusCode int
	req        *http.Request
	r          RequestReader

	grabbed atomic.Bool
	done    chan bool

	// stats used for logging
	finish     time.Time
	elapsed    int64
	cpuElapsed int64
	worker     int
	priority   int

	// Scheduling inputs that are fixed for the lifetime of a queued job
	basePriority   int
	rateLimitKey   string
	demoteBySource bool

	// X-SendFile / X-Accel-Redirect file
	sendFile string

	parkedId string

	// Whether this job is a retry of a parked request. Retries must not be
	// parked again, otherwise they could loop forever.
	isRetry bool
}

type RequestCount struct {
	count float64
	since time.Time
}

type Scheduler struct {
	jobQueue      []*RequestJob
	jobQueueMutex sync.Mutex
	jobsWaiting   chan bool

	activeRequestsBySource      map[string]int
	activeRequestsBySourceMutex sync.Mutex

	requestsBySource      *lru.TwoQueueCache[string, RequestCount]
	requestsBySourceMutex sync.Mutex

	activeRequests atomic.Int32
}

var scheduler *Scheduler

func init() {
	scheduler = NewScheduler()
}

func Send504(job *RequestJob) {
	job.w.WriteHeader(504)
	job.w.Write([]byte("Gateway Timeout"))
	job.finish = time.Now()
	job.statusCode = 504
}

func (sched *Scheduler) HandleJob(job *RequestJob, timeout time.Duration) error {
	requestCount.Add(1)

	// Work out the parts of the priority that won't change while the job is
	// queued, so the scheduling scan only has to look at per-source counts.
	sched.PrecomputeJobPriority(job)

	var dropJob *RequestJob
	var dropJobIndex int

	sched.jobQueueMutex.Lock()
	sched.jobQueue = append(sched.jobQueue, job)
	if len(sched.jobQueue) > maxQueueLength {
		// Queue is now too long, grab the lowest priority request so we can drop it
		dropJob, dropJobIndex = sched.GetLowestPriorityJob()
		sched.jobQueue = append(sched.jobQueue[:dropJobIndex], sched.jobQueue[dropJobIndex+1:]...)
	}
	sched.jobQueueMutex.Unlock()

	if dropJob != nil {
		// Drop the job we grabbed
		Send504(dropJob)
		droppedCount.Add(1)
		dropJob.done <- true
	} else {
		// We added a job to the list, so signal that to any waiting handlers.
		// This must be a non-blocking send: if the channel is already full then
		// there are plenty of wakeups pending, and blocking here would stall
		// the HTTP handler goroutine (past its request timeout) under load.
		select {
		case sched.jobsWaiting <- true:
		default:
		}
	}

	// this is ugly?
	ctx := job.req.Context()

	var err error
	shouldLog := true

	select {
	case <-job.done:
		// Job completed normally
	case <-ctx.Done():
		// Request socket probably closed
		if !job.grabbed.Swap(true) {
			// Never got started, so don't bother logging
			shouldLog = false
		} else {
			// Wait for job to finish so we can log properly
			<-job.done
			// Should we indicate on the log line that the response never got sent?
		}
		err = errors.New("Job context was done before being handled!")
	case <-time.After(timeout):
		// Timed out, so try to grab exclusively
		if !job.grabbed.Swap(true) {
			// Successfully grabbed, we can inflict a timeout
			Send504(job)
			timeoutCount.Add(1)
			err = errors.New("Job timed out without being handled!")
		} else {
			// Couldn't grab, job is being serviced, so wait for it
			<-job.done
		}
	}

	if shouldLog {
		LogRequestJob(job)
	}

	return err
}

// ageRequestCount decays a request count at a rate of one request per second
// since it was last updated.
func ageRequestCount(r RequestCount, now time.Time) float64 {
	if r.count > 0 {
		r.count -= now.Sub(r.since).Seconds()
		if r.count < 0 {
			r.count = 0
		}
	}
	return r.count
}

// GetAgedRequestCount returns a source's historic request count, decayed to now.
func (sched *Scheduler) GetAgedRequestCount(remoteAddr string, now time.Time) float64 {
	sched.requestsBySourceMutex.Lock()
	defer sched.requestsBySourceMutex.Unlock()

	r, _ := sched.requestsBySource.Get(remoteAddr)
	return ageRequestCount(r, now)
}

// AddAgedRequestCount atomically ages and increments a source's request count.
func (sched *Scheduler) AddAgedRequestCount(remoteAddr string, now time.Time) {
	sched.requestsBySourceMutex.Lock()
	defer sched.requestsBySourceMutex.Unlock()

	r, _ := sched.requestsBySource.Get(remoteAddr)
	r.count = ageRequestCount(r, now) + 1
	r.since = now
	sched.requestsBySource.Add(remoteAddr, r)
}

func GetRateLimitKey(ip net.IP) string {
	if ip4 := ip.To4(); ip4 != nil {
		// Is an IPv4 address, rate limit exact address
		return ip4.String()
	}

	// Is an IPv6 address, rate limit the whole /64
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

var crawlerUserAgents = []string{"facebook", "bot", "crawler", "spider", "index", "http:", "https:"}

// Calculate the parts of a job priority that won't change while it is queued,
// called before it is added to teh queue.
func (sched *Scheduler) PrecomputeJobPriority(job *RequestJob) {
	job.basePriority = 1000

	remoteAddrIp := net.ParseIP(GetRemoteAddr(job.req))

	// Always cache the key, so grabbing and finishing the job don't have to
	// re-parse the request either.
	job.rateLimitKey = GetRateLimitKey(remoteAddrIp)

	// We only demote crawlers and rate limit by source if we're sure the
	// request is non-local (otherwise recursive requests with a crawler-like
	// user-agent will hang).
	if remoteAddrIp != nil && !remoteAddrIp.IsLoopback() && !remoteAddrIp.IsPrivate() {
		job.demoteBySource = true

		ua := strings.ToLower(job.req.Header.Get("User-agent"))
		for _, crawler := range crawlerUserAgents {
			if strings.Contains(ua, crawler) {
				job.basePriority -= 8000
				break
			}
		}
	}

	if job.req.URL.RawQuery != "" {
		// Demote anything with a query string
		job.basePriority -= 500
	}
}

// CalculateJobPriority combines a job's precomputed base priority with the
// current per-source request counts. now is passed in so a whole scan ages
// every job against the same instant.
func (sched *Scheduler) CalculateJobPriority(job *RequestJob, now time.Time) int {
	priority := job.basePriority

	if job.demoteBySource {
		sched.activeRequestsBySourceMutex.Lock()
		priority -= 2000 * sched.activeRequestsBySource[job.rateLimitKey]
		sched.activeRequestsBySourceMutex.Unlock()

		r := sched.GetAgedRequestCount(job.rateLimitKey, now)
		priority -= int(1000 * r)
	}

	return priority
}

func (sched *Scheduler) GetHighestPriorityJob() (*RequestJob, int) {
	var job *RequestJob
	jobIndex := -1
	now := time.Now()
	for k, j := range sched.jobQueue {
		j.priority = sched.CalculateJobPriority(j, now)
		if job == nil || j.priority > job.priority {
			job = j
			jobIndex = k
		}
	}
	return job, jobIndex
}

func (sched *Scheduler) GetLowestPriorityJob() (*RequestJob, int) {
	var job *RequestJob
	jobIndex := -1
	now := time.Now()
	for k, j := range sched.jobQueue {
		j.priority = sched.CalculateJobPriority(j, now)
		if job == nil || j.priority < job.priority {
			job = j
			jobIndex = k
		}
	}
	return job, jobIndex
}

func (sched *Scheduler) FindJobInQueue(job *RequestJob) int {
	for k, j := range sched.jobQueue {
		if job == j {
			return k
		}
	}
	return -1
}

func (sched *Scheduler) grabQueuedJob() *RequestJob {
	sched.jobQueueMutex.Lock()
	defer sched.jobQueueMutex.Unlock()
	if len(sched.jobQueue) > 0 {
		job, jobIndex := sched.GetHighestPriorityJob()
		if job.priority <= -7000 && sched.activeRequests.Load() > 0 {
			// if we're busy, ignore extremely low priority tasks
			return nil
		}
		sched.jobQueue = append(sched.jobQueue[:jobIndex], sched.jobQueue[jobIndex+1:]...)
		return job
	}
	return nil
}

func (sched *Scheduler) GrabJob() *RequestJob {
	for {
		// Exit promptly if shutdown has been requested.
		select {
		case <-workerStop:
			return nil
		default:
		}

		// Try and grab a waiting job
		job := sched.grabQueuedJob()
		if job != nil {
			// Try to grab exclusively
			if job.grabbed.Swap(true) {
				// was already grabbed by someone else, skip
				continue
			}

			// Increment the global active-request-count
			sched.activeRequests.Add(1)

			key := job.rateLimitKey

			// Increment the active request count for the source
			sched.activeRequestsBySourceMutex.Lock()
			sched.activeRequestsBySource[key] += 1
			sched.activeRequestsBySourceMutex.Unlock()

			// Increment the historic request count for the source
			sched.AddAgedRequestCount(key, time.Now())

			return job
		}

		// Nothing to grab, so block
		select {
		case <-sched.jobsWaiting:
			// woke up for a waiting job
		case <-workerStop:
			return nil
		case <-time.After(time.Duration(rand.Intn(400)+800) * time.Millisecond):
			// woke up after timeout (to avoid potential deadlocks)
		}
	}
}

func (sched *Scheduler) JobFinished(job *RequestJob) {
	// Decrement the global active-request-count
	sched.activeRequests.Add(-1)

	// Remove the request from the currently active requests
	key := job.rateLimitKey
	sched.activeRequestsBySourceMutex.Lock()
	if sched.activeRequestsBySource[key] <= 1 {
		delete(sched.activeRequestsBySource, key)
	} else {
		sched.activeRequestsBySource[key] -= 1
	}
	sched.activeRequestsBySourceMutex.Unlock()

	// Signal that the job is done
	job.done <- true
}

func NewScheduler() *Scheduler {
	requestsBySource, err := lru.New2Q[string, RequestCount](16384)
	if err != nil {
		log.Fatalln(err)
	}

	return &Scheduler{
		jobsWaiting:            make(chan bool, maxQueueLength),
		activeRequestsBySource: make(map[string]int),
		requestsBySource:       requestsBySource,
	}
}
