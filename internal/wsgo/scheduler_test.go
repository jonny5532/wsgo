package wsgo

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestHandleJobDoesNotBlockWithoutIdleWorkers checks that queueing a job when
// no worker is waiting doesn't block the HTTP handler goroutine; the job just
// times out.
func TestHandleJobDoesNotBlockWithoutIdleWorkers(t *testing.T) {
	s := NewScheduler()

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	cw := NewNonCachingCacheWriter(httptest.NewRecorder())
	job := &RequestJob{
		w:    cw,
		req:  req,
		r:    NewBufferingReader(req.Body, 0),
		done: make(chan bool, 1),
	}

	returned := make(chan struct{})
	go func() {
		s.HandleJob(job, 50*time.Millisecond)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleJob blocked with no idle workers")
	}
}

func signalled(wake chan struct{}) bool {
	select {
	case <-wake:
		return true
	default:
		return false
	}
}

// TestWakeIdleWorkerIsLIFO checks that the most recently idle worker is woken
// first, so that light load is served by as few workers as possible.
func TestWakeIdleWorkerIsLIFO(t *testing.T) {
	s := NewScheduler()
	a, b, c := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}, 1)
	s.pushIdleWorker(a)
	s.pushIdleWorker(b)
	s.pushIdleWorker(c)

	s.wakeIdleWorker()
	if signalled(a) || signalled(b) || !signalled(c) {
		t.Fatal("expected only the most recently idle worker to be woken")
	}

	s.wakeIdleWorker()
	if signalled(a) || !signalled(b) {
		t.Fatal("expected the next most recently idle worker to be woken")
	}

	// Waking with nobody idle must not block.
	s.wakeIdleWorker()
	s.wakeIdleWorker()
	if !signalled(a) {
		t.Fatal("expected the last idle worker to be woken")
	}
}

// TestRemoveIdleWorkerPassesOnWakeup checks that a worker which stops waiting
// after being woken (e.g. having grabbed a job by polling) hands the wakeup on,
// so the job it was woken for isn't left waiting for a poll.
func TestRemoveIdleWorkerPassesOnWakeup(t *testing.T) {
	s := NewScheduler()
	a, b := make(chan struct{}, 1), make(chan struct{}, 1)
	s.pushIdleWorker(a)
	s.pushIdleWorker(b)

	s.wakeIdleWorker() // wakes b
	s.removeIdleWorker(b)

	if signalled(b) {
		t.Fatal("removed worker's wakeup should have been consumed")
	}
	if !signalled(a) {
		t.Fatal("wakeup should have been passed on to the other idle worker")
	}
	if len(s.idleWorkers) != 0 {
		t.Fatalf("idle stack should be empty, has %d", len(s.idleWorkers))
	}

	// Removing a worker that wasn't woken just takes it off the stack.
	s.pushIdleWorker(a)
	s.pushIdleWorker(b)
	s.removeIdleWorker(a)
	if len(s.idleWorkers) != 1 || s.idleWorkers[0] != b {
		t.Fatal("expected only b to remain on the idle stack")
	}
}

// TestGrabJobPrefersMostRecentlyIdle checks end to end that a queued job is
// taken by the most recently idle worker.
func TestGrabJobPrefersMostRecentlyIdle(t *testing.T) {
	oldStop := workerStop
	oldClosed := workerStopClosed.Load()
	workerStop = make(chan struct{})
	workerStopClosed.Store(false)
	defer func() {
		workerStop = oldStop
		workerStopClosed.Store(oldClosed)
	}()

	s := NewScheduler()

	first := make(chan *RequestJob, 1)
	second := make(chan *RequestJob, 1)
	go func() { first <- s.GrabJob() }()
	time.Sleep(50 * time.Millisecond)
	go func() { second <- s.GrabJob() }()
	time.Sleep(50 * time.Millisecond)

	job := &RequestJob{done: make(chan bool, 1)}
	s.jobQueueMutex.Lock()
	s.jobQueue = append(s.jobQueue, job)
	s.jobQueueMutex.Unlock()
	s.wakeIdleWorker()

	select {
	case got := <-second:
		if got != job {
			t.Fatal("most recently idle worker got the wrong job")
		}
	case got := <-first:
		t.Fatalf("longest idle worker took the job (%v)", got)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no worker took the job")
	}

	StopWorkers()
	select {
	case got := <-first:
		if got != nil {
			t.Fatal("first worker returned a job after stopping")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first worker did not stop")
	}
}
