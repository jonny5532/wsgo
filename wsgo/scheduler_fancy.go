package wsgo

import (
	"errors"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

type RequestCount struct {
	count float64
	since time.Time
}

type FancyScheduler struct {
	jobQueue 		[]*RequestJob
	jobQueueMutex   sync.Mutex
	jobsWaiting     chan bool

	activeRequestsBySource map[string]int
	activeRequestsBySourceMutex sync.Mutex

	requestsBySource *lru.TwoQueueCache[string, RequestCount]
	cpuTimeByUrl *lru.TwoQueueCache[string, *RollingAverage]

	activeRequests  atomic.Int32
}

func Send504(job *RequestJob) {
	job.w.WriteHeader(504)
	job.w.Write([]byte("Gateway Timeout"))
	job.finish = time.Now()
	job.statusCode = 504
}

func (sched *FancyScheduler) HandleJob(job *RequestJob, timeout time.Duration) error {
	requestCount.Add(1)

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
		// We added a job to the list, so signal that to any waiting handlers
		sched.jobsWaiting <- true
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
			// Should we indicate on the log line that the response never got sent?
		}
	}

	if shouldLog {
		LogRequestJob(job)
	}
	
	return err
}

func (sched *FancyScheduler) GetAgedRequestCount(remoteAddr string) RequestCount {
	r, _ := sched.requestsBySource.Get(remoteAddr)
	now := time.Now()
	if r.count > 0 {
		// age count by time since last one? at rate of 1 rq/s
		r.count -= now.Sub(r.since).Seconds()
		if r.count < 0 {
			r.count = 0
		}
	}
	r.since = now
	return r
}

func GetRateLimitKey(ip net.IP) string {
	if ip6 := ip.To16(); ip6 != nil {
		// Only consider the /64 when rate limiting
		return ip6.Mask(net.CIDRMask(64, 128)).String()
	}

	return ip.String()
}

func (sched *FancyScheduler) CalculateJobPriority(job *RequestJob) int {
	var priority int = 1000

	ua := strings.ToLower(job.req.Header.Get("User-agent"))
	for _, uas := range []string{"facebook", "bot", "crawler", "spider", "index"} {
		if strings.Contains(ua, uas) {
			priority -= 8000
			break
		}
	}

	if job.req.URL.RawQuery != "" {
		// demote anything with a query string
		priority -= 500
	}

	cpuAvg, _ := sched.cpuTimeByUrl.Get(job.req.RequestURI)
	if cpuAvg != nil {
		priority -= int(cpuAvg.GetFilteredMax()*10)
	}

	// TODO - also demote long-response-time requests? but don't want to make long-polling impossible?

	remoteAddr := GetRemoteAddr(job.req)
	remoteAddrIp := net.ParseIP(remoteAddr)

	if remoteAddrIp != nil && !remoteAddrIp.IsLoopback() && !remoteAddrIp.IsPrivate() {
		key := GetRateLimitKey(remoteAddrIp)

		sched.activeRequestsBySourceMutex.Lock()
		priority -= 2000*sched.activeRequestsBySource[key]
		sched.activeRequestsBySourceMutex.Unlock()

		r := sched.GetAgedRequestCount(key)
		priority -= int(1000*r.count)
	}

	return priority
}

func (sched *FancyScheduler) GetHighestPriorityJob() (*RequestJob, int) {
	var job *RequestJob
	jobIndex := -1
	for k, j := range sched.jobQueue {
		j.priority = sched.CalculateJobPriority(j)
		if (job == nil || j.priority > job.priority) {
			job = j
			jobIndex = k
		}
	}
	return job, jobIndex
}

func (sched *FancyScheduler) GetLowestPriorityJob() (*RequestJob, int) {
	var job *RequestJob
	jobIndex := -1
	for k, j := range sched.jobQueue {
		j.priority = sched.CalculateJobPriority(j)
		if (job == nil || j.priority < job.priority) {
			job = j
			jobIndex = k
		}
	}
	return job, jobIndex
}

func (sched *FancyScheduler) FindJobInQueue(job *RequestJob) int {
	for k, j := range sched.jobQueue {
		if job==j {
			return k
		}
	}
	return -1
}

func (sched *FancyScheduler) grabQueuedJob() *RequestJob {
	sched.jobQueueMutex.Lock()
	defer sched.jobQueueMutex.Unlock()
	if len(sched.jobQueue)>0 {
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

func (sched *FancyScheduler) GrabJob() *RequestJob {
	for {
		// Try and grab a waiting job
		job := sched.grabQueuedJob()
		if job != nil {
			// Try to grab exclusively
			if job.grabbed.Swap(true) {
				// was already grabbed by someone else, skip
				continue
			}

			key := GetRateLimitKey(net.ParseIP(GetRemoteAddr(job.req)))

			sched.activeRequests.Add(1)

			sched.activeRequestsBySourceMutex.Lock()
			sched.activeRequestsBySource[key] += 1
			sched.activeRequestsBySourceMutex.Unlock()

			r := sched.GetAgedRequestCount(key)
			r.count += 1
			sched.requestsBySource.Add(key, r)

			return job
		}

		// Nothing to grab, so block
		select {
		case <-sched.jobsWaiting:
			// woke up for a waiting job
		case <-time.After(time.Duration(rand.Intn(400) + 800) * time.Millisecond):
			// woke up after timeout
		}
	}
}

func (sched *FancyScheduler) JobFinished(job *RequestJob) {
	sched.activeRequests.Add(-1)

	key := GetRateLimitKey(net.ParseIP(GetRemoteAddr(job.req)))
	sched.activeRequestsBySourceMutex.Lock()
	if sched.activeRequestsBySource[key] <= 1 {
		delete(sched.activeRequestsBySource, key)
	} else {
		sched.activeRequestsBySource[key] -= 1
	}
	sched.activeRequestsBySourceMutex.Unlock()

	r, _ := sched.cpuTimeByUrl.Get(job.req.RequestURI)
	if r == nil {
		nr := NewRollingAverage()
		r = &nr
		sched.cpuTimeByUrl.Add(job.req.RequestURI, r)
	}
	r.Add(job.cpuElapsed)

	job.done <- true
}

func NewFancyScheduler() *FancyScheduler {
	requestsBySource, err := lru.New2Q[string, RequestCount](16384)
	if err != nil {
		log.Fatalln(err)
	}
	cpuTimeByUrl, err := lru.New2Q[string, *RollingAverage](16384)
	if err != nil {
		log.Fatalln(err)
	}

	return &FancyScheduler{
		jobsWaiting: make(chan bool, maxQueueLength),
		activeRequestsBySource: make(map[string]int),
		requestsBySource: requestsBySource,
		cpuTimeByUrl: cpuTimeByUrl,
	}
}
