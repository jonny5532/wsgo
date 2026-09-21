package wsgo

import (
	"testing"
	"time"
)

// TestGrabJobStopsWhenStopped checks that closing the worker stop channel
// wakes idle workers so they can exit before Py_Finalize runs.
func TestGrabJobStopsWhenStopped(t *testing.T) {
	oldStop := workerStop
	oldClosed := workerStopClosed.Load()
	workerStop = make(chan struct{})
	workerStopClosed.Store(false)
	defer func() {
		workerStop = oldStop
		workerStopClosed.Store(oldClosed)
	}()

	s := NewScheduler()

	done := make(chan *RequestJob, 1)
	go func() {
		done <- s.GrabJob()
	}()

	// Give the goroutine time to start blocking in its wait select.
	time.Sleep(50 * time.Millisecond)

	if stopped := StopWorkers(); !stopped {
		t.Fatal("StopWorkers reported that workers weren't stopped")
	}

	select {
	case job := <-done:
		if job != nil {
			t.Fatal("GrabJob returned a job after StopWorkers")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GrabJob did not return after StopWorkers")
	}
}
