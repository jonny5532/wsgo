package wsgo

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestHandleJobDoesNotBlockWhenJobsWaitingFull reproduces the case where
// wakeup tokens accumulate in jobsWaiting under sustained load. HandleJob
// must not block trying to signal a full channel, otherwise HTTP handler
// goroutines pile up past their request timeout.
func TestHandleJobDoesNotBlockWhenJobsWaitingFull(t *testing.T) {
	s := NewScheduler()

	for i := 0; i < cap(s.jobsWaiting); i++ {
		s.jobsWaiting <- true
	}

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
		// Good - a full jobsWaiting channel must not block the caller.
	case <-time.After(2 * time.Second):
		t.Fatal("HandleJob blocked sending to a full jobsWaiting channel")
	}
}
